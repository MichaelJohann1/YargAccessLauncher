// Package selfupdate updates the launcher from the GitHub releases of the launcher repository.
//
// A release ships YargAccessLauncher-<version>.zip: YargAccessLauncher.exe with prism.dll in
// _internal. A running exe cannot replace itself, so the update is unpacked next to the install and a
// small PowerShell script waits for the launcher to exit, swaps the files in and starts the new
// version. The layout and script match the Python launcher's, so either can update to the other.
package selfupdate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/app"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/archives"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/github"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/netx"
)

const (
	Repo  = "MichaelJohann1/YargAccessLauncher"
	Asset = `^YargAccessLauncher-.*\.zip$`
	Exe   = "YargAccessLauncher.exe"
)

// currentVersion and latestRelease are variables so tests can replace them.
var (
	currentVersion = app.Version
	latestRelease  = github.LatestRelease
)

type Update struct {
	Version string
	Notes   string
	Asset   *github.Asset
	Page    string
}

// IsFrozen reports whether this is a built launcher in its release folder, rather than one run with
// `go run` (whose exe sits in a temporary build folder and cannot be updated in place).
func IsFrozen() bool {
	exe, err := os.Executable()
	return err == nil && strings.EqualFold(filepath.Base(exe), Exe)
}

func InstallDir() string {
	exe, _ := os.Executable()
	return filepath.Dir(exe)
}

// Check returns the newest release when it is newer than this launcher, else nil.
func Check() (*Update, error) {
	rel, err := latestRelease(Repo, map[string]string{"Accept": "application/vnd.github+json"})
	if err != nil {
		return nil, err
	}
	if github.Compare(github.ParseVersion(rel.TagName), github.ParseVersion(currentVersion)) <= 0 {
		return nil, nil
	}
	a, err := rel.FindAsset(Asset)
	if err != nil || a == nil {
		return nil, err
	}
	a.Notes, a.Page = "", ""
	return &Update{strings.TrimPrefix(rel.TagName, "v"), rel.Body, a, rel.HTMLURL}, nil
}

// Download fetches and unpacks the update and returns the folder holding the new YargAccessLauncher.exe.
func Download(u *Update, progress netx.Progress, cancel <-chan struct{}) (dir string, err error) {
	work, err := os.MkdirTemp("", "yarg-access-update-")
	if err != nil {
		return "", err
	}
	// A failed or cancelled update leaves nothing behind.
	defer func() {
		if err != nil {
			os.RemoveAll(work)
		}
	}()
	// A launcher update replaces this program, so it is only installed when its checksum can be checked.
	if u.Asset.SHA256 == "" {
		return "", errors.New("This update can't be checked: its release has no checksum. Download it from the release page instead.")
	}
	zipPath := filepath.Join(work, u.Asset.Name)
	if _, err := github.DownloadAsset(u.Asset, zipPath, progress, cancel); err != nil {
		return "", err
	}
	out := filepath.Join(work, "new")
	if err := archives.ExtractZip(zipPath, out); err != nil {
		return "", err
	}
	os.Remove(zipPath)
	found := ""
	filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == Exe {
			found = filepath.Dir(p)
			return filepath.SkipAll
		}
		return nil
	})
	if found == "" {
		return "", errors.New("The update does not contain " + Exe + ".")
	}
	return found, nil
}

const script = `
$ErrorActionPreference = 'Stop'
$pidToWait = %d
$src = '%s'
$dst = '%s'
$log = '%s'
try {
    $p = Get-Process -Id $pidToWait -ErrorAction SilentlyContinue
    if ($p) { $p.WaitForExit(60000) | Out-Null }
    Start-Sleep -Milliseconds 500
    # _internal is replaced as a whole so no files from the old version are left behind.
    robocopy (Join-Path $src '_internal') (Join-Path $dst '_internal') /MIR /R:5 /W:1 /NFL /NDL /NJH /NJS /NP | Out-Null
    if ($LASTEXITCODE -ge 8) { throw "robocopy _internal failed ($LASTEXITCODE)" }
    Copy-Item (Join-Path $src '%s') (Join-Path $dst '%s') -Force
    'updated' | Set-Content $log
} catch {
    $_ | Out-String | Set-Content $log
}
Start-Process (Join-Path $dst '%s') -WorkingDirectory $dst
`

// ApplyAndRestart starts the swap script. The caller must exit right after.
func ApplyAndRestart(newDir, logPath string) error {
	q := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
	text := fmt.Sprintf(script, os.Getpid(), q(newDir), q(InstallDir()), q(logPath), Exe, Exe, Exe)
	path := filepath.Join(filepath.Dir(newDir), "apply-update.ps1")
	if err := os.WriteFile(path, append([]byte("\xef\xbb\xbf"), text...), 0o644); err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(logPath), 0o755)
	const createNoWindow, breakaway = 0x08000000, 0x01000000
	start := func(flags uint32) error {
		cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-File", path)
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags, HideWindow: true}
		return cmd.Start()
	}
	if err := start(createNoWindow | breakaway); err != nil {
		return start(createNoWindow)
	}
	return nil
}
