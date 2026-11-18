// Package installer installs and updates YARG, MelonLoader, the accessibility mod and DLC (setlists
// and venues).
//
// The folder layout matches the YARC launcher, so both launchers and the game agree:
//
//	<content root>\YARG Installs\<id>\installation\YARG.exe   + sibling tag.txt
//	<content root>\Setlists\<id>\installation\*.yargsong     + sibling tag.txt
//	<content root>\Venues\<id>\installation\...               + sibling tag.txt
//
// YARG is started with -download-location <content root>, which is how it finds setlists and venues.
//
// Game updates are built in a staging folder next to the install and swapped in only when complete.
// The official launcher instead wipes the install first, which is what deletes MelonLoader and the
// mod after every update.
package installer

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	launcher "github.com/MichaelJohann1/YargAccessLauncher"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/archives"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/catalog"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/config"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/github"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/minisign"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/netx"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/yarc"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/yargsettings"
)

var SpeechDLLs = []string{"UniversalSpeech.dll", "nvdaControllerClient.dll", "ZDSRAPI.dll"}

const (
	ModDLL           = "YargAccess.dll"
	MelonLoaderRepo  = "LavaGang/MelonLoader"
	MelonLoaderAsset = "MelonLoader.x64.zip"
	StateFile        = "yarg-access.json"
)

// PlayerPrefsKey is where Unity keeps YARG's PlayerPrefs; a variable so tests can use another key.
var PlayerPrefsKey = `Software\YARC\YARG`

var (
	oldModDLLs        = []string{"YARGAccessibility.dll"}
	melonLoaderSHA256 = map[string]string{
		"v0.7.2": "5cedb66dc5595de19fd4dd6b4c178440da293c6d7f511e14bdf0fea407c9a1f7",
	}
	carryOver = []string{"Mods", "UserData", "UserLibs", "Plugins"}
)

// Error is an error with a message meant for the user.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

func userError(format string, a ...any) error { return &Error{fmt.Sprintf(format, a...)} }

type Status struct {
	GameDir          string
	GameInstalled    bool
	GameTag          string
	MelonLoader      bool
	Mod              bool
	ModVersion       string
	SpeechDLLMissing []string
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func (s Status) Summary() string {
	if !s.GameInstalled {
		return fmt.Sprintf("YARG is not installed in %s.", s.GameDir)
	}
	parts := []string{fmt.Sprintf("YARG %s installed in %s.", or(s.GameTag, "unknown version"), s.GameDir)}
	if s.MelonLoader {
		parts = append(parts, "MelonLoader installed.")
	} else {
		parts = append(parts, "MelonLoader missing.")
	}
	if s.Mod {
		parts = append(parts, fmt.Sprintf("Accessibility mod installed (%s).", or(s.ModVersion, "unknown version")))
	} else {
		parts = append(parts, "Accessibility mod missing.")
	}
	if len(s.SpeechDLLMissing) > 0 {
		parts = append(parts, "Missing speech files: "+strings.Join(s.SpeechDLLMissing, ", ")+".")
	}
	return strings.Join(parts, " ")
}

// Reporter is the progress sink. The UI implements it; the default just logs.
type Reporter interface {
	Stage(text string)
	Progress(done, total int64)
}

type logReporter struct{}

func (logReporter) Stage(text string)   { log.Print(text) }
func (logReporter) Progress(_, _ int64) {}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func sameFolder(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// runningExePaths are the full paths of running processes named exeName.
func runningExePaths(exeName string) []string {
	pids := make([]uint32, 4096)
	var needed uint32
	if windows.EnumProcesses(pids, &needed) != nil {
		return nil
	}
	var found []string
	for _, pid := range pids[:needed/4] {
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
		if err != nil {
			continue
		}
		buf := make([]uint16, 1024)
		size := uint32(len(buf))
		if windows.QueryFullProcessImageName(h, 0, &buf[0], &size) == nil {
			p := windows.UTF16ToString(buf[:size])
			if strings.EqualFold(filepath.Base(p), exeName) {
				found = append(found, p)
			}
		}
		windows.CloseHandle(h)
	}
	return found
}

// IsGameRunning reports whether YARG is running; with gameDir, only a YARG.exe started from that folder counts.
func IsGameRunning(gameDir string) bool {
	paths := runningExePaths("YARG.exe")
	if gameDir == "" {
		return len(paths) > 0
	}
	want, _ := filepath.Abs(gameDir)
	for _, p := range paths {
		if sameFolder(filepath.Dir(p), want) {
			return true
		}
	}
	return false
}

// PlayerDataDir is YARG's own data: settings.json, profiles\ (names, instruments, bindings), scores\,
// playlists\, custom\ presets and replays. One subfolder per build (release, nightly).
func PlayerDataDir() string { return yargsettings.LocalLow }

func PlayerDataExists() bool { return isDir(PlayerDataDir()) }

func deleteRegTree(root registry.Key, path string) {
	k, err := registry.OpenKey(root, path, registry.ALL_ACCESS)
	if err != nil {
		return
	}
	subs, _ := k.ReadSubKeyNames(-1)
	k.Close()
	for _, s := range subs {
		deleteRegTree(root, path+`\`+s)
	}
	registry.DeleteKey(root, path)
}

// RemovePlayerDataFiles deletes YARG's LocalLow folder and its PlayerPrefs registry key.
func RemovePlayerDataFiles() {
	folder := PlayerDataDir()
	os.RemoveAll(folder)
	os.Remove(filepath.Dir(folder))
	deleteRegTree(registry.CURRENT_USER, PlayerPrefsKey)
	registry.DeleteKey(registry.CURRENT_USER, filepath.Dir(PlayerPrefsKey))
}

func sha256File(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}

// copyFile copies src to dst with its modification time, like shutil.copy2.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if st, err := os.Stat(src); err == nil {
		os.Chtimes(dst, st.ModTime(), st.ModTime())
	}
	return nil
}

// copyMissing copies the src tree into dst, keeping files that already exist in dst.
func copyMissing(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if _, err := os.Stat(target); err == nil {
			return nil
		}
		return copyFile(p, target)
	})
}

type Installer struct {
	Cfg    *config.Config
	R      Reporter
	Cancel <-chan struct{}
}

func New(cfg *config.Config, r Reporter, cancel <-chan struct{}) *Installer {
	if r == nil {
		r = logReporter{}
	}
	if cancel == nil {
		cancel = make(chan struct{})
	}
	return &Installer{cfg, r, cancel}
}

func (i *Installer) GameDir() string    { return i.Cfg.ResolvedGameDir() }
func (i *Installer) ProfileDir() string { return filepath.Dir(i.GameDir()) }
func (i *Installer) TempDir() string {
	return filepath.Join(i.Cfg.ContentRoot, "YARG Access", "temp")
}

func (i *Installer) state() map[string]any {
	m := map[string]any{}
	b, err := os.ReadFile(filepath.Join(i.ProfileDir(), StateFile))
	if err == nil {
		json.Unmarshal(b, &m)
	}
	return m
}

func (i *Installer) stateString(key string) string {
	s, _ := i.state()[key].(string)
	return s
}

func (i *Installer) saveState(values map[string]string) error {
	st := i.state()
	for k, v := range values {
		st[k] = v
	}
	if err := os.MkdirAll(i.ProfileDir(), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(st, "", "  ")
	return os.WriteFile(filepath.Join(i.ProfileDir(), StateFile), b, 0o644)
}

func readTag(profileDir string) string {
	b, err := os.ReadFile(filepath.Join(profileDir, "tag.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func (i *Installer) Status() Status {
	g := i.GameDir()
	var missing []string
	for _, d := range SpeechDLLs {
		if !isFile(filepath.Join(g, d)) {
			missing = append(missing, d)
		}
	}
	return Status{
		GameDir:          g,
		GameInstalled:    isFile(filepath.Join(g, "YARG.exe")),
		GameTag:          readTag(i.ProfileDir()),
		MelonLoader:      isFile(filepath.Join(g, "version.dll")) && isDir(filepath.Join(g, "MelonLoader")),
		Mod:              isFile(filepath.Join(g, "Mods", ModDLL)),
		ModVersion:       i.stateString("mod"),
		SpeechDLLMissing: missing,
	}
}

func (i *Installer) LatestGame() (catalog.Release, error) {
	info, err := catalog.FetchProfile(catalog.GameProfileURL(i.Cfg.Channel))
	if err != nil {
		return catalog.Release{}, err
	}
	return info.Release, nil
}

func (i *Installer) checkCancel() error {
	select {
	case <-i.Cancel:
		return netx.ErrCancelled
	default:
		return nil
	}
}

func (i *Installer) download(url, dest, what string) error {
	i.R.Stage("Downloading " + what)
	_, err := netx.Download(url, dest, i.R.Progress, i.Cancel, nil)
	return err
}

// isGameRunning and latestModAsset are variables so tests can replace them.
var (
	isGameRunning  = IsGameRunning
	latestModAsset = github.LatestAsset
)

func (i *Installer) ensureNotRunning() error {
	if isGameRunning(i.GameDir()) {
		return userError("YARG is running. Close the game, then try again.")
	}
	return nil
}

func swapIn(staging, target string) error {
	old := target + ".old"
	os.RemoveAll(old)
	if isDir(target) {
		if err := os.Rename(target, old); err != nil {
			return err
		}
	}
	if err := os.Rename(staging, target); err != nil {
		if isDir(old) {
			os.Rename(old, target)
		}
		return err
	}
	os.RemoveAll(old)
	return nil
}

// InstallEverything installs or updates YARG, then makes sure MelonLoader, the mod and the speech
// DLLs are in place.
func (i *Installer) InstallEverything(forceGame bool) (string, error) {
	if err := i.ensureNotRunning(); err != nil {
		return "", err
	}
	os.MkdirAll(i.TempDir(), 0o755)
	i.R.Stage("Checking for the latest YARG release")
	rel, err := i.LatestGame()
	if err != nil {
		return "", err
	}
	current := i.Status()
	if forceGame || !current.GameInstalled || current.GameTag != rel.Tag {
		if err := i.installGame(rel); err != nil {
			return "", err
		}
	} else {
		i.R.Stage(fmt.Sprintf("YARG %s is already up to date", rel.Tag))
		if err := i.repairInPlace(false); err != nil {
			return "", err
		}
	}
	return i.Status().Summary(), nil
}

func (i *Installer) repairInPlace(force bool) error {
	v, err := i.installMelonLoader(i.GameDir(), force)
	if err != nil {
		return err
	}
	i.saveState(map[string]string{"melonloader": v})
	m, err := i.InstallMod(i.GameDir())
	if err != nil {
		return err
	}
	return i.saveState(map[string]string{"mod": m})
}

func (i *Installer) verify(sigURL, part, sigName string) error {
	sig := filepath.Join(i.TempDir(), sigName)
	if _, err := netx.Download(sigURL, sig, nil, i.Cancel, nil); err != nil {
		return err
	}
	text, err := os.ReadFile(sig)
	if err != nil {
		return err
	}
	_, err = minisign.VerifyFile(part, string(text), minisign.YargPublicKey)
	os.Remove(sig)
	return err
}

func (i *Installer) installGame(rel catalog.Release) error {
	if len(rel.Files) == 0 {
		return userError("The YARG release has no Windows download.")
	}
	g := i.GameDir()
	staging := g + ".staging"
	os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	for n, f := range rel.Files {
		if err := i.checkCancel(); err != nil {
			return err
		}
		part := filepath.Join(i.TempDir(), fmt.Sprintf("yarg-%d.download", n))
		if err := i.download(f.URL, part, "YARG "+rel.Tag); err != nil {
			return err
		}
		defer os.Remove(part)
		if f.SigURL != "" {
			i.R.Stage("Verifying the download signature")
			if err := i.verify(f.SigURL, part, fmt.Sprintf("yarg-%d.sig", n)); err != nil {
				var se *minisign.SignatureError
				if errors.As(err, &se) {
					return userError("YARG download failed verification: %s", se.Error())
				}
				return err
			}
		}
		if err := i.checkCancel(); err != nil {
			return err
		}
		i.R.Stage("Unpacking YARG " + rel.Tag)
		if err := archives.Extract(part, staging, f.FileType, i.checkCancel); err != nil {
			return err
		}
		os.Remove(part)
	}
	if isDir(g) {
		for _, name := range carryOver {
			if src := filepath.Join(g, name); isDir(src) {
				if err := copyMissing(src, filepath.Join(staging, name)); err != nil {
					return err
				}
			}
		}
		for _, dll := range SpeechDLLs {
			if src := filepath.Join(g, dll); isFile(src) {
				if err := copyFile(src, filepath.Join(staging, dll)); err != nil {
					return err
				}
			}
		}
	}
	loaderVersion, err := i.InstallMelonLoader(staging)
	if err != nil {
		return err
	}
	modVersion, err := i.InstallMod(staging)
	if err != nil {
		return err
	}
	if err := i.checkCancel(); err != nil {
		return err
	}
	i.R.Stage("Replacing the old game files")
	if err := swapIn(staging, g); err != nil {
		return err
	}
	i.saveState(map[string]string{"melonloader": loaderVersion, "mod": modVersion})
	return os.WriteFile(filepath.Join(i.ProfileDir(), "tag.txt"), []byte(rel.Tag), 0o644)
}

// InstallMelonLoader installs MelonLoader into target and returns its version.
func (i *Installer) InstallMelonLoader(target string) (string, error) {
	return i.installMelonLoader(target, false)
}

// installMelonLoader skips a MelonLoader that looks installed unless force is set (Repair).
func (i *Installer) installMelonLoader(target string, force bool) (string, error) {
	version := i.Cfg.MelonLoaderVer
	state := map[string]any{}
	if target == i.GameDir() {
		state = i.state()
	}
	if !force && state["melonloader"] == version && isFile(filepath.Join(target, "version.dll")) &&
		isDir(filepath.Join(target, "MelonLoader", "net35")) {
		return version, WriteLoaderCfg(target)
	}
	cache := filepath.Join(i.TempDir(), "MelonLoader-"+version+".x64.zip")
	expected := melonLoaderSHA256[version]
	if expected == "" {
		var rel github.Release
		if netx.GetJSON("https://api.github.com/repos/"+MelonLoaderRepo+"/releases/tags/"+version, nil, &rel) == nil {
			for _, a := range rel.Assets {
				if a.Name == MelonLoaderAsset && strings.HasPrefix(a.Digest, "sha256:") {
					expected = strings.SplitN(a.Digest, ":", 2)[1]
					break
				}
			}
		}
	}
	// MelonLoader is loaded into the game, so a version whose checksum isn't known is not installed.
	if expected == "" {
		return "", userError("MelonLoader %s can't be checked: its checksum isn't known and GitHub didn't give one. "+
			"Try again later, or set the MelonLoader version back to %s in the settings file.", version, "v0.7.2")
	}
	if !(isFile(cache) && sha256File(cache) == expected) {
		url := github.ReleaseAssetURL(MelonLoaderRepo, version, MelonLoaderAsset)
		if err := i.download(url, cache, "MelonLoader "+version); err != nil {
			return "", err
		}
	}
	if sha256File(cache) != expected {
		os.Remove(cache)
		return "", userError("The MelonLoader download is damaged. Try again.")
	}
	i.R.Stage("Installing MelonLoader " + version)
	for _, sub := range []string{"net35", "net6", "Dependencies"} {
		os.RemoveAll(filepath.Join(target, "MelonLoader", sub))
	}
	if err := archives.ExtractZip(cache, target, i.checkCancel); err != nil {
		return "", err
	}
	for _, name := range []string{"Mods", "UserLibs", "UserData", "Plugins"} {
		os.MkdirAll(filepath.Join(target, name), 0o755)
	}
	return version, WriteLoaderCfg(target)
}

var hideConsoleFalse = regexp.MustCompile(`(?m)^\s*hide_console\s*=\s*false`)

// WriteLoaderCfg hides the MelonLoader console window. The log file is still written.
func WriteLoaderCfg(target string) error {
	path := filepath.Join(target, "UserData", "Loader.cfg")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return os.WriteFile(path, launcher.LoaderCfg(), 0o644)
	}
	if updated := hideConsoleFalse.ReplaceAll(text, []byte("hide_console = true")); string(updated) != string(text) {
		return os.WriteFile(path, updated, 0o644)
	}
	return nil
}

// fetchModPackage returns the path to the mod's .zip or .dll and its version label.
func (i *Installer) fetchModPackage() (string, string, error) {
	if local := strings.TrimSpace(i.Cfg.ModLocalPath); local != "" {
		if !isFile(local) {
			return "", "", userError("The local mod file was not found: %s", local)
		}
		return local, "local file " + filepath.Base(local), nil
	}
	repo := strings.TrimSpace(i.Cfg.ModRepo)
	if repo == "" {
		return "", "", userError("No mod download source is set. On the Settings tab, enter the GitHub " +
			"repository of the mod, or choose a local mod file.")
	}
	i.R.Stage("Checking for the latest accessibility mod release")
	asset, err := github.LatestAsset(repo, i.Cfg.ModAssetPattern)
	if err != nil {
		if netx.IsStatus(err, 404) {
			return "", "", userError("No public release found in %s. Use a local mod file instead.", repo)
		}
		return "", "", err
	}
	dest := filepath.Join(i.TempDir(), asset.Name)
	i.R.Stage("Downloading the accessibility mod " + asset.Tag)
	if _, err := github.DownloadAsset(asset, dest, i.R.Progress, i.Cancel); err != nil {
		return "", "", err
	}
	return dest, asset.Tag, nil
}

// CheckModUpdate returns the newest mod release when its version number is higher than the one
// installed, else nil. Nothing to check with a local mod file, no mod installed, or no version number
// recorded.
func (i *Installer) CheckModUpdate() (*github.Asset, error) {
	if strings.TrimSpace(i.Cfg.ModLocalPath) != "" || strings.TrimSpace(i.Cfg.ModRepo) == "" || !i.Status().Mod {
		return nil, nil
	}
	label := i.stateString("mod")
	if !regexp.MustCompile(`^v?\d`).MatchString(label) {
		return nil, nil
	}
	asset, err := latestModAsset(strings.TrimSpace(i.Cfg.ModRepo), i.Cfg.ModAssetPattern)
	if err != nil {
		return nil, err
	}
	if github.Newer(asset.Tag, label) {
		return asset, nil
	}
	return nil, nil
}

// UpdateMod installs the newest mod release into the game folder.
func (i *Installer) UpdateMod() (string, error) {
	if err := i.ensureNotRunning(); err != nil {
		return "", err
	}
	if !i.Status().GameInstalled {
		return "", userError("YARG is not installed yet. Use Install or update first.")
	}
	os.MkdirAll(i.TempDir(), 0o755)
	m, err := i.InstallMod(i.GameDir())
	if err != nil {
		return "", err
	}
	i.saveState(map[string]string{"mod": m})
	return "The accessibility mod was updated. " + i.Status().Summary(), nil
}

// InstallMod installs the mod into target and returns its version label.
func (i *Installer) InstallMod(target string) (string, error) {
	pkg, version, err := i.fetchModPackage()
	if err != nil {
		return "", err
	}
	if err := i.checkCancel(); err != nil {
		return "", err
	}
	i.R.Stage("Installing the accessibility mod")
	mods := filepath.Join(target, "Mods")
	os.MkdirAll(mods, 0o755)
	for _, old := range oldModDLLs {
		if p := filepath.Join(mods, old); isFile(p) {
			os.Remove(p)
		}
	}
	if strings.HasSuffix(strings.ToLower(pkg), ".dll") {
		err = copyFile(pkg, filepath.Join(mods, ModDLL))
	} else {
		err = InstallModZip(pkg, target)
	}
	if err != nil {
		return "", err
	}
	var missing []string
	for _, d := range SpeechDLLs {
		if !isFile(filepath.Join(target, d)) {
			missing = append(missing, d)
		}
	}
	if len(missing) > 0 {
		return "", userError("The mod is installed, but these speech files are missing from the game "+
			"folder, so the game cannot talk: %s. Use a mod package that includes them.", strings.Join(missing, ", "))
	}
	return version, nil
}

// InstallModZip unpacks a mod zip, which uses the game folder layout: Mods/YargAccess.dll plus the
// speech DLLs at the root.
//
// Only Mods/, UserLibs/, UserData/ and the speech DLLs are written. One wrapping folder (e.g.
// YargAccess-1.0/) is tolerated, and a YargAccess.dll at the top level goes into Mods/.
func InstallModZip(pkg, target string) error {
	topDirs := map[string]string{"mods": "Mods", "userlibs": "UserLibs", "userdata": "UserData"}
	speech := map[string]string{}
	for _, d := range SpeechDLLs {
		speech[strings.ToLower(d)] = d
	}
	z, err := zip.OpenReader(pkg)
	if err != nil {
		return err
	}
	defer z.Close()
	type item struct {
		f     *zip.File
		parts []string
	}
	var entries []item
	firsts := map[string]bool{}
	roots := 0
	for _, f := range z.File {
		if strings.HasSuffix(f.Name, "/") {
			continue
		}
		var parts []string
		for _, p := range strings.Split(strings.ReplaceAll(f.Name, "\\", "/"), "/") {
			if p != "" {
				parts = append(parts, p)
			}
		}
		entries = append(entries, item{f, parts})
		if len(parts) > 1 {
			firsts[strings.ToLower(parts[0])] = true
		} else if len(parts) == 1 {
			roots++
		}
	}
	if len(firsts) == 1 && roots == 0 {
		wrapped := true
		for k := range firsts {
			if _, ok := topDirs[k]; ok {
				wrapped = false
			}
		}
		if wrapped {
			for n := range entries {
				entries[n].parts = entries[n].parts[1:]
			}
		}
	}
	for _, e := range entries {
		parts := e.parts
		if len(parts) == 0 || contains(parts, "..") {
			continue
		}
		head := strings.ToLower(parts[0])
		var rel string
		switch {
		case len(parts) == 1 && speech[head] != "":
			rel = speech[head]
		case len(parts) == 1 && head == strings.ToLower(ModDLL):
			rel = filepath.Join("Mods", ModDLL)
		case len(parts) > 1 && topDirs[head] != "":
			rel = filepath.Join(append([]string{topDirs[head]}, parts[1:]...)...)
		default:
			log.Printf("mod package: skipping %s", e.f.Name)
			continue
		}
		out, err := archives.SafeTarget(target, rel)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		src, err := e.f.Open()
		if err != nil {
			return err
		}
		dst, err := os.Create(out)
		if err != nil {
			src.Close()
			return err
		}
		_, err = io.Copy(dst, src)
		src.Close()
		if cerr := dst.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (i *Installer) RepairMod() (string, error) {
	if err := i.ensureNotRunning(); err != nil {
		return "", err
	}
	if !i.Status().GameInstalled {
		return "", userError("YARG is not installed yet. Use Install or update first.")
	}
	os.MkdirAll(i.TempDir(), 0o755)
	if err := i.repairInPlace(true); err != nil {
		return "", err
	}
	return i.Status().Summary(), nil
}

// InstalledPackages are the profile folders of every installed setlist and venue under the content folder.
func (i *Installer) InstalledPackages() []string {
	var found []string
	for _, sub := range []string{"Setlists", "Venues"} {
		root := filepath.Join(i.Cfg.ContentRoot, sub)
		names, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		var list []string
		for _, n := range names {
			list = append(list, n.Name())
		}
		sort.Strings(list)
		for _, n := range list {
			folder := filepath.Join(root, n)
			if isDir(filepath.Join(folder, "installation")) {
				found = append(found, folder)
			}
		}
	}
	return found
}

// UninstallGame removes the game folder with MelonLoader, the mod and the speech DLLs. DLC (setlists
// and venues), player data (settings, profiles, scores, playlists in LocalLow) and Chorus downloads
// only go when asked.
func (i *Installer) UninstallGame(removePackages, removePlayerData, removeChorusSongs bool) (string, error) {
	if err := i.ensureNotRunning(); err != nil {
		return "", err
	}
	g := i.GameDir()
	if isDir(g) {
		if !isFile(filepath.Join(g, "YARG.exe")) {
			return "", userError("%s does not contain YARG.exe, so it was not removed.", g)
		}
		i.R.Stage("Removing YARG")
		if err := os.RemoveAll(g); err != nil {
			return "", err
		}
	}
	os.RemoveAll(g + ".staging")
	os.RemoveAll(g + ".old")
	for _, name := range []string{"tag.txt", StateFile} {
		os.Remove(filepath.Join(i.ProfileDir(), name))
	}
	os.Remove(i.ProfileDir())
	msg := "YARG, MelonLoader and the accessibility mod were removed."
	if removePackages {
		folders := i.InstalledPackages()
		for n, folder := range folders {
			i.R.Stage(fmt.Sprintf("Removing DLC %d of %d", n+1, len(folders)))
			if err := os.RemoveAll(filepath.Join(folder, "installation")); err != nil {
				return "", err
			}
			os.Remove(filepath.Join(folder, "tag.txt"))
			os.Remove(folder)
		}
		msg += fmt.Sprintf(" %d DLC setlists and venues were removed.", len(folders))
	}
	if removePlayerData {
		i.R.Stage("Removing your YARG settings, profiles and scores")
		RemovePlayerDataFiles()
		msg += " Your YARG settings, profiles, scores and playlists were removed."
	} else {
		msg += " Your YARG settings, profiles and scores were kept."
	}
	if removeChorusSongs {
		removed, err := i.RemoveChorusSongs()
		if err != nil {
			return "", err
		}
		word := "songs"
		if removed == 1 {
			word = "song"
		}
		msg += fmt.Sprintf(" %d %s downloaded through Chorus removed.", removed, word)
	}
	os.RemoveAll(i.TempDir())
	return msg, nil
}

// ChorusSongs are the .sng files in the Chorus download folder.
func (i *Installer) ChorusSongs() []string {
	folder := i.Cfg.ResolvedSongsDir()
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if strings.HasSuffix(strings.ToLower(e.Name()), ".sng") {
			out = append(out, filepath.Join(folder, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

func (i *Installer) RemoveChorusSongs() (int, error) {
	files := i.ChorusSongs()
	for _, p := range files {
		if err := os.Remove(p); err != nil {
			return 0, err
		}
	}
	os.Remove(i.Cfg.ResolvedSongsDir())
	return len(files), nil
}

func (i *Installer) Launch() error {
	exe := filepath.Join(i.GameDir(), "YARG.exe")
	if !isFile(exe) {
		return userError("YARG is not installed yet.")
	}
	cmd := exec.Command(exe, "-download-location", i.Cfg.ContentRoot)
	cmd.Dir = i.GameDir()
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// PackageFolder is the profile folder for a setlist or venue. It reuses the official launcher's copy
// if it has one, so the same pack never ends up installed twice.
func (i *Installer) PackageFolder(e *catalog.Entry, official *yarc.OfficialLauncher) string {
	if official != nil && sameFolder(official.DownloadLocation, i.Cfg.ContentRoot) {
		if inst, ok := official.ByMarketplaceUUID()[e.UUID]; ok && isDir(inst.Folder) {
			return inst.Folder
		}
	}
	sub := "Setlists"
	if e.Type == "venue" {
		sub = "Venues"
	}
	return filepath.Join(i.Cfg.ContentRoot, sub, e.UUID)
}

func (i *Installer) PackageTag(folder string) string {
	if !isDir(filepath.Join(folder, "installation")) {
		return ""
	}
	return readTag(folder)
}

func (i *Installer) InstallPackage(e *catalog.Entry, folder string) (string, error) {
	os.MkdirAll(i.TempDir(), 0o755)
	i.R.Stage("Checking " + e.Name)
	info, err := catalog.FetchProfile(e.URL)
	if err != nil {
		return "", err
	}
	rel := info.Release
	if len(rel.Files) == 0 {
		return "", userError("%s has no Windows download.", e.Name)
	}
	target := filepath.Join(folder, "installation")
	staging := target + ".staging"
	os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	total := len(rel.Files)
	for n, f := range rel.Files {
		if err := i.checkCancel(); err != nil {
			return "", err
		}
		part := filepath.Join(i.TempDir(), fmt.Sprintf("%s-%d.download", e.UUID, n))
		label := e.Name
		if total > 1 {
			label = fmt.Sprintf("%s, part %d of %d", e.Name, n+1, total)
		}
		if err := i.download(f.URL, part, label); err != nil {
			return "", err
		}
		defer os.Remove(part)
		if f.SigURL != "" {
			if err := i.verify(f.SigURL, part, fmt.Sprintf("%s-%d.sig", e.UUID, n)); err != nil {
				return "", err
			}
		}
		if err := i.checkCancel(); err != nil {
			return "", err
		}
		i.R.Stage("Unpacking " + label)
		if err := archives.Extract(part, staging, f.FileType, i.checkCancel); err != nil {
			return "", err
		}
		os.Remove(part)
	}
	if err := swapIn(staging, target); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(folder, "tag.txt"), []byte(rel.Tag), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s %s installed.", e.Name, rel.Tag), nil
}

func (i *Installer) UninstallPackage(e *catalog.Entry, folder string) (string, error) {
	if err := os.RemoveAll(filepath.Join(folder, "installation")); err != nil {
		return "", err
	}
	os.Remove(filepath.Join(folder, "tag.txt"))
	os.Remove(folder)
	return e.Name + " removed.", nil
}

// FetchProfiles fetches every entry's profile (newest tag, description, songs) in parallel, reporting
// each as it arrives; nil when the lookup failed.
func FetchProfiles(entries []*catalog.Entry, onResult func(*catalog.Entry, *catalog.ProfileInfo), workers int) {
	jobs := make(chan *catalog.Entry)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for e := range jobs {
				info, err := catalog.FetchProfile(e.URL)
				if err != nil {
					log.Printf("profile lookup failed for %s: %v", e.Name, err)
					info = nil
				}
				onResult(e, info)
			}
		}()
	}
	for _, e := range entries {
		jobs <- e
	}
	close(jobs)
	wg.Wait()
}
