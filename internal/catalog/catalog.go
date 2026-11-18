// Package catalog reads the official YARG release server (the same catalog the YARC launcher uses).
//
//	Catalog:  https://releases.yarg.in/profiles/index.json -> list of profiles
//	Profile:  {type: application|setlist|venue, uuid, metadata, version: VersionInfo}
//	VersionInfo is one of
//	  {type: "list", listUrl}      -> list of {uuid, url, tag, release}; first entry is newest
//	  {type: "url", releaseUrl}    -> one Version
//	  {type: "embedded", version}  -> one Version
//	Version: {uuid, tag, release, content: [{platforms, files: [{url, sigUrl?, fileType}]}],
//	          alternatives?, launchOptions?}
package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/netx"
)

const (
	Server   = "https://releases.yarg.in"
	IndexURL = Server + "/profiles/index.json"
	Platform = "windows"

	YargStable      = "2d78800c-1397-496a-83c1-50759607999a"
	YargNightly     = "35dc0e7e-e9b5-49f3-881d-8dbed8678dc7"
	OfficialSetlist = "8474d8ad-15bd-492c-927b-003940100fa1"
)

type Entry struct {
	UUID     string
	Type     string
	Category string
	Name     string
	URL      string
	SubText  string
}

func (e *Entry) KindLabel() string {
	switch e.Type {
	case "setlist":
		if e.Category == "yarn_setlist" {
			return "YARN setlist"
		}
		return "Official setlist"
	case "venue":
		return "Venue"
	}
	return "Game"
}

type ReleaseFile struct {
	URL      string
	FileType string
	SigURL   string
}

type Release struct {
	Tag                 string
	ReleaseDate         string
	Files               []ReleaseFile
	Executable          string
	DownloadLocationArg string
}

type Song struct {
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Length any    `json:"length"`
}

type ProfileInfo struct {
	EntryUUID   string
	Name        string
	Description string
	Songs       []Song
	Release     Release
}

func FetchCatalog() ([]*Entry, error) {
	var data struct {
		Profiles []struct {
			UUID     string `json:"uuid"`
			Type     string `json:"type"`
			Category string `json:"category"`
			Name     string `json:"name"`
			URL      string `json:"url"`
			SubText  string `json:"subText"`
		} `json:"profiles"`
	}
	if err := netx.GetJSON(IndexURL, nil, &data); err != nil {
		return nil, err
	}
	var out []*Entry
	for _, p := range data.Profiles {
		name := p.Name
		if name == "" {
			name = p.UUID
		}
		out = append(out, &Entry{p.UUID, p.Type, p.Category, name, p.URL, p.SubText})
	}
	return out, nil
}

type fileJSON struct {
	URL      string `json:"url"`
	SigURL   string `json:"sigUrl"`
	FileType string `json:"fileType"`
}

type contentJSON struct {
	Platforms []string   `json:"platforms"`
	Files     []fileJSON `json:"files"`
}

type versionJSON struct {
	Tag          string        `json:"tag"`
	Release      string        `json:"release"`
	Content      []contentJSON `json:"content"`
	Alternatives []struct {
		Default bool           `json:"default"`
		Content *[]contentJSON `json:"content"`
	} `json:"alternatives"`
	LaunchOptions map[string]struct {
		ExecutablePath           *string `json:"executablePath"`
		DownloadLocationArgument string  `json:"downloadLocationArgument"`
	} `json:"launchOptions"`
}

type versionInfo struct {
	Type       string          `json:"type"`
	Version    json.RawMessage `json:"version"`
	ReleaseURL string          `json:"releaseUrl"`
	ListURL    string          `json:"listUrl"`
}

func resolveVersion(info versionInfo) (*versionJSON, error) {
	var v versionJSON
	switch info.Type {
	case "embedded":
		return &v, json.Unmarshal(info.Version, &v)
	case "url":
		return &v, netx.GetJSON(info.ReleaseURL, nil, &v)
	case "list":
		var versions []struct {
			URL string `json:"url"`
		}
		if err := netx.GetJSON(info.ListURL, nil, &versions); err != nil {
			return nil, err
		}
		if len(versions) == 0 {
			return nil, errors.New("The release list is empty")
		}
		return &v, netx.GetJSON(versions[0].URL, nil, &v)
	}
	return nil, fmt.Errorf("Unknown version type: %s", info.Type)
}

func windowsRelease(v *versionJSON) Release {
	content := v.Content
	for _, a := range v.Alternatives {
		if a.Default {
			if a.Content != nil {
				content = *a.Content
			}
			break
		}
	}
	var files []ReleaseFile
	for _, c := range content {
		if len(c.Platforms) > 0 && !contains(c.Platforms, Platform) {
			continue
		}
		for _, f := range c.Files {
			files = append(files, ReleaseFile{f.URL, f.FileType, f.SigURL})
		}
	}
	rel := Release{Tag: v.Tag, ReleaseDate: v.Release, Files: files, Executable: "YARG.exe"}
	if launch, ok := v.LaunchOptions[Platform]; ok {
		if launch.ExecutablePath != nil {
			rel.Executable = strings.TrimLeft(*launch.ExecutablePath, "./")
		}
		rel.DownloadLocationArg = launch.DownloadLocationArgument
	}
	return rel
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func FetchProfile(profileURL string) (*ProfileInfo, error) {
	var data struct {
		UUID     string `json:"uuid"`
		Metadata struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Songs       []Song `json:"songs"`
		} `json:"metadata"`
		Version versionInfo `json:"version"`
	}
	if err := netx.GetJSON(profileURL, nil, &data); err != nil {
		return nil, err
	}
	v, err := resolveVersion(data.Version)
	if err != nil {
		return nil, err
	}
	return &ProfileInfo{
		EntryUUID:   data.UUID,
		Name:        data.Metadata.Name,
		Description: data.Metadata.Description,
		Songs:       data.Metadata.Songs,
		Release:     windowsRelease(v),
	}, nil
}

func OfficialSetlistEntry() *Entry {
	return &Entry{UUID: OfficialSetlist, Type: "setlist", Category: "official_setlist",
		Name: "YARG Official Setlist", URL: Server + "/profiles/" + OfficialSetlist + "/"}
}

func GameProfileURL(channel string) string {
	uuid := YargStable
	if channel == "nightly" {
		uuid = YargNightly
	}
	return Server + "/profiles/" + uuid + "/"
}
