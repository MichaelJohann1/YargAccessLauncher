// Package github looks up GitHub releases and downloads their assets (public repositories).
package github

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/netx"
)

type Asset struct {
	Name       string
	Tag        string
	APIURL     string
	BrowserURL string
	Size       int64
	Notes      string
	Page       string
	// SHA256 is GitHub's checksum of the file (hex), empty when GitHub gives none.
	SHA256 string
}

// Release is the part of GitHub's release JSON the launcher reads.
type Release struct {
	TagName string `json:"tag_name"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name               string `json:"name"`
		URL                string `json:"url"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
		Digest             string `json:"digest"`
	} `json:"assets"`
}

func Headers() map[string]string {
	return map[string]string{"Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28"}
}

// LatestRelease fetches the newest release of repo.
func LatestRelease(repo string, headers map[string]string) (*Release, error) {
	var rel Release
	err := netx.GetJSON("https://api.github.com/repos/"+repo+"/releases/latest", headers, &rel)
	return &rel, err
}

// FindAsset is the first asset of rel whose name matches pattern (case-insensitive), or nil.
func (rel *Release) FindAsset(pattern string) (*Asset, error) {
	rx, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return nil, err
	}
	for _, a := range rel.Assets {
		if rx.MatchString(a.Name) {
			sum := ""
			if strings.HasPrefix(a.Digest, "sha256:") {
				sum = strings.ToLower(strings.TrimPrefix(a.Digest, "sha256:"))
			}
			return &Asset{a.Name, rel.TagName, a.URL, a.BrowserDownloadURL, a.Size, rel.Body, rel.HTMLURL, sum}, nil
		}
	}
	return nil, nil
}

// LatestAsset finds the first asset of the newest release whose name matches pattern.
func LatestAsset(repo, pattern string) (*Asset, error) {
	rel, err := LatestRelease(repo, Headers())
	if err != nil {
		return nil, err
	}
	a, err := rel.FindAsset(pattern)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, fmt.Errorf("Release %s of %s has no file matching %s", rel.TagName, repo, pattern)
	}
	return a, nil
}

// DownloadAsset downloads a release file and, when GitHub gives its checksum, checks it.
func DownloadAsset(a *Asset, dest string, progress netx.Progress, cancel <-chan struct{}) (string, error) {
	path, err := netx.Download(a.BrowserURL, dest, progress, cancel, nil)
	if err != nil || a.SHA256 == "" {
		return path, err
	}
	if SHA256File(path) != a.SHA256 {
		os.Remove(path)
		return "", fmt.Errorf("The download of %s is damaged (its checksum does not match). Try again.", a.Name)
	}
	return path, nil
}

// SHA256File is the hex SHA-256 of a file, empty when it can't be read.
func SHA256File(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func ReleaseAssetURL(repo, tag, name string) string {
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, tag, name)
}

var digits = regexp.MustCompile(`\d+`)

// ParseVersion turns "v1.2.3" into [1 2 3]; empty when the text has no number.
func ParseVersion(text string) []int {
	var out []int
	for _, m := range digits.FindAllString(text, 3) {
		n, _ := strconv.Atoi(m)
		out = append(out, n)
	}
	return out
}

// Compare compares two parsed versions like Python tuples (a shorter prefix is smaller).
func Compare(a, b []int) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

// Newer reports whether tag is a higher version than installed; "1.0" counts as "1.0.0".
func Newer(tag, installed string) bool {
	a, b := ParseVersion(tag), ParseVersion(installed)
	for len(a) < len(b) {
		a = append(a, 0)
	}
	for len(b) < len(a) {
		b = append(b, 0)
	}
	return Compare(a, b) > 0
}

var repoRx = regexp.MustCompile(`(?:github\.com/)?([\w.-]+/[\w.-]+)$`)

// ParseRepo takes "owner/name" or a GitHub address and returns "owner/name", or "" when it is neither.
func ParseRepo(text string) string {
	text = strings.TrimRight(strings.TrimSpace(text), "/")
	m := repoRx.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	return m[1]
}
