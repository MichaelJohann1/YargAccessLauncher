package selfupdate

import (
	"encoding/json"
	"testing"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/github"
)

func TestCheck(t *testing.T) {
	var rel github.Release
	json.Unmarshal([]byte(`{"tag_name": "v0.2.0", "body": "## What's new\n- thing", "html_url": "https://x",
		"assets": [{"name": "YargAccessLauncher-0.2.0.zip", "url": "api", "browser_download_url": "dl"}]}`), &rel)
	oldRel, oldVer := latestRelease, currentVersion
	latestRelease = func(string, map[string]string) (*github.Release, error) { return &rel, nil }
	t.Cleanup(func() { latestRelease, currentVersion = oldRel, oldVer })

	currentVersion = "0.1.1"
	up, err := Check()
	if err != nil || up == nil || up.Version != "0.2.0" || up.Asset.BrowserURL != "dl" {
		t.Fatal(up, err)
	}
	currentVersion = "0.2.0"
	if up, _ := Check(); up != nil {
		t.Fatal(up)
	}
}
