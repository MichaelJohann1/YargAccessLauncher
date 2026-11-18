// Package yarc discovers, read-only, the installs made by the official YARC launcher.
//
// The official launcher keeps an in-memory copy of its settings and overwrites external edits, so
// this package never writes to its settings file.
package yarc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

var SettingsPath = filepath.Join(os.Getenv("APPDATA"), "in.yarg.launcher", "settings.json")

type OfficialInstall struct {
	Name            string
	MarketplaceUUID string
	Folder          string
	Type            string
}

func (i OfficialInstall) Installation() string { return filepath.Join(i.Folder, "installation") }

func (i OfficialInstall) Tag() string {
	b, err := os.ReadFile(filepath.Join(i.Folder, "tag.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

type OfficialLauncher struct {
	DownloadLocation string
	Installs         []OfficialInstall
}

func (l *OfficialLauncher) ByMarketplaceUUID() map[string]OfficialInstall {
	m := map[string]OfficialInstall{}
	for _, i := range l.Installs {
		m[i.MarketplaceUUID] = i
	}
	return m
}

// Games are the installed YARG builds.
func (l *OfficialLauncher) Games() []OfficialInstall {
	var out []OfficialInstall
	for _, i := range l.Installs {
		if i.Type != "application" {
			continue
		}
		if _, err := os.Stat(filepath.Join(i.Installation(), "YARG.exe")); err == nil {
			out = append(out, i)
		}
	}
	return out
}

var subdir = map[string]string{"application": "YARG Installs", "setlist": "Setlists", "venue": "Venues"}

// Detect reads the official launcher's settings; nil when it is not set up.
func Detect() *OfficialLauncher {
	b, err := os.ReadFile(SettingsPath)
	if err != nil {
		return nil
	}
	var data struct {
		DownloadLocation string `json:"downloadLocation"`
		ActiveProfiles   []struct {
			UUID        string `json:"uuid"`
			DisplayName string `json:"displayName"`
			Profile     struct {
				Type     string `json:"type"`
				UUID     string `json:"uuid"`
				Metadata struct {
					Name string `json:"name"`
				} `json:"metadata"`
			} `json:"profile"`
		} `json:"activeProfiles"`
	}
	if json.Unmarshal(b, &data) != nil {
		return nil
	}
	root := data.DownloadLocation
	if root == "" {
		root = filepath.Join(os.Getenv("LOCALAPPDATA"), "YARC")
	}
	var installs []OfficialInstall
	for _, p := range data.ActiveProfiles {
		sub, ok := subdir[p.Profile.Type]
		if !ok {
			continue
		}
		name := p.DisplayName
		if name == "" {
			name = p.Profile.Metadata.Name
		}
		installs = append(installs, OfficialInstall{
			Name: name, MarketplaceUUID: p.Profile.UUID,
			Folder: filepath.Join(root, sub, p.UUID), Type: p.Profile.Type,
		})
	}
	return &OfficialLauncher{root, installs}
}
