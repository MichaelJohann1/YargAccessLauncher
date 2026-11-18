package installer

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/config"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/github"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/yargsettings"
)

func modZip(t *testing.T, path string, entries []string) {
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for _, name := range entries {
		w, _ := z.Create(name)
		w.Write([]byte("x"))
	}
	z.Close()
	f.Close()
}

func TestModZipLayouts(t *testing.T) {
	var prefixed []string
	for _, d := range SpeechDLLs {
		prefixed = append(prefixed, "pkg/"+d)
	}
	for _, entries := range [][]string{
		append([]string{"Mods/YargAccess.dll"}, SpeechDLLs...),
		append([]string{"YargAccess.dll"}, SpeechDLLs...),
		append([]string{"pkg/Mods/YargAccess.dll"}, prefixed...),
	} {
		dir := t.TempDir()
		pkg := filepath.Join(dir, "mod.zip")
		if strings.HasPrefix(entries[0], "Mods") {
			entries = append(entries, "README.md", "Mods/Other.dll")
		}
		modZip(t, pkg, entries)
		game := filepath.Join(dir, "game")
		os.Mkdir(game, 0o755)
		if err := InstallModZip(pkg, game); err != nil {
			t.Fatal(err)
		}
		if !isFile(filepath.Join(game, "Mods", "YargAccess.dll")) {
			t.Fatalf("%v: no mod", entries)
		}
		for _, d := range SpeechDLLs {
			if !isFile(filepath.Join(game, d)) {
				t.Fatalf("%v: no %s", entries, d)
			}
		}
		if isFile(filepath.Join(game, "README.md")) {
			t.Fatal("README copied")
		}
	}
}

func fakeInstall(t *testing.T, root string) (*Installer, string) {
	cfg := config.Default()
	cfg.ContentRoot = root
	inst := New(cfg, nil, nil)
	game := inst.GameDir()
	os.MkdirAll(filepath.Join(game, "Mods"), 0o755)
	os.WriteFile(filepath.Join(game, "YARG.exe"), nil, 0o644)
	os.WriteFile(filepath.Join(inst.ProfileDir(), "tag.txt"), []byte("v0.15.0"), 0o644)
	pack := filepath.Join(root, "Setlists", "abc")
	os.MkdirAll(filepath.Join(pack, "installation"), 0o755)
	os.WriteFile(filepath.Join(pack, "tag.txt"), []byte("t"), 0o644)
	return inst, pack
}

func notRunning(t *testing.T) {
	old := isGameRunning
	isGameRunning = func(string) bool { return false }
	t.Cleanup(func() { isGameRunning = old })
}

func TestModUpdateCheckComparesVersionNumbers(t *testing.T) {
	inst, _ := fakeInstall(t, t.TempDir())
	os.WriteFile(filepath.Join(inst.GameDir(), "Mods", "YargAccess.dll"), nil, 0o644)
	asset := &github.Asset{Name: "YargAccess-1.0.1.zip", Tag: "v1.0.1", Notes: "New: bots"}
	old := latestModAsset
	latestModAsset = func(string, string) (*github.Asset, error) { return asset, nil }
	t.Cleanup(func() { latestModAsset = old })
	check := func(label string, want *github.Asset) {
		t.Helper()
		inst.saveState(map[string]string{"mod": label})
		got, err := inst.CheckModUpdate()
		if err != nil || got != want {
			t.Fatalf("%s: got %v %v", label, got, err)
		}
	}
	check("v1.0.0", asset)
	check("v1.0.1", nil)
	check("v1.2.0", nil)
	check("local file YargAccess.dll", nil)
	check("local file YargAccess-1.0.dll", nil)
	check("v1.0", asset)
	if github.Newer("v1.0.0", "1.0") {
		t.Fatal("1.0.0 newer than 1.0")
	}
	inst.Cfg.ModLocalPath = "C:/mods/YargAccess.dll"
	check("v1.0.0", nil)
}

func TestModUpdateCheckNeedsTheMod(t *testing.T) {
	inst, _ := fakeInstall(t, t.TempDir())
	old := latestModAsset
	latestModAsset = func(string, string) (*github.Asset, error) {
		t.Fatal("no check without the mod")
		return nil, nil
	}
	t.Cleanup(func() { latestModAsset = old })
	if a, _ := inst.CheckModUpdate(); a != nil {
		t.Fatal(a)
	}
}

func TestUninstallGame(t *testing.T) {
	notRunning(t)
	for _, removePackages := range []bool{false, true} {
		inst, pack := fakeInstall(t, t.TempDir())
		msg, err := inst.UninstallGame(removePackages, false, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(inst.ProfileDir()); err == nil {
			t.Fatal("profile folder left")
		}
		if isDir(pack) == removePackages {
			t.Fatalf("pack kept=%v with removePackages=%v", isDir(pack), removePackages)
		}
		if !strings.Contains(msg, "kept") {
			t.Fatal(msg)
		}
	}
}

func TestUninstallRefusesFolderWithoutYarg(t *testing.T) {
	notRunning(t)
	inst, _ := fakeInstall(t, t.TempDir())
	os.Remove(filepath.Join(inst.GameDir(), "YARG.exe"))
	var ue *Error
	if _, err := inst.UninstallGame(false, false, false); !errors.As(err, &ue) {
		t.Fatalf("got %v", err)
	}
	if !isDir(inst.GameDir()) {
		t.Fatal("folder removed")
	}
}

func TestUninstallRemovesPlayerDataAndChorusSongs(t *testing.T) {
	notRunning(t)
	dir := t.TempDir()
	locallow := filepath.Join(dir, "LocalLow", "YARC", "YARG")
	os.MkdirAll(filepath.Join(locallow, "release", "profiles"), 0o755)
	os.WriteFile(filepath.Join(locallow, "release", "profiles", "profiles.json"), []byte("[]"), 0o644)
	oldLL, oldKey := yargsettings.LocalLow, PlayerPrefsKey
	yargsettings.LocalLow = locallow
	PlayerPrefsKey = `Software\YargAccessLauncherTest\YARG`
	t.Cleanup(func() { yargsettings.LocalLow, PlayerPrefsKey = oldLL, oldKey })
	k, _, err := registry.CreateKey(registry.CURRENT_USER, PlayerPrefsKey+`\child`, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	k.Close()

	inst, _ := fakeInstall(t, filepath.Join(dir, "content"))
	songs := inst.Cfg.ResolvedSongsDir()
	os.MkdirAll(songs, 0o755)
	os.WriteFile(filepath.Join(songs, "a.sng"), nil, 0o644)
	os.WriteFile(filepath.Join(songs, "keep.txt"), nil, 0o644)

	msg, err := inst.UninstallGame(false, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if isDir(locallow) {
		t.Fatal("player data left")
	}
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\YargAccessLauncherTest`, registry.READ); err == nil {
		k.Close()
		t.Fatal("registry key left")
	}
	entries, _ := os.ReadDir(songs)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "keep.txt" {
		t.Fatal(names)
	}
	if !strings.Contains(msg, "1 song downloaded through Chorus removed") {
		t.Fatal(msg)
	}
}
