package internal_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/github"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/news"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/yargsettings"
)

func TestAddSongFolder(t *testing.T) {
	dir := t.TempDir()
	build := filepath.Join(dir, "release")
	os.Mkdir(build, 0o755)
	os.WriteFile(filepath.Join(build, "settings.json"), []byte(`{"SongFolders": [], "Other": 1}`), 0o644)
	old := yargsettings.LocalLow
	yargsettings.LocalLow = dir
	t.Cleanup(func() { yargsettings.LocalLow = old })
	folder := filepath.Join(dir, "Songs")
	if len(yargsettings.AddSongFolder(folder)) == 0 || !yargsettings.HasSongFolder(folder) {
		t.Fatal("not added")
	}
	if len(yargsettings.AddSongFolder(folder)) != 0 {
		t.Fatal("added twice")
	}
	b, _ := os.ReadFile(filepath.Join(build, "settings.json"))
	var data map[string]any
	json.Unmarshal(b, &data)
	if !reflect.DeepEqual(data, map[string]any{"SongFolders": []any{folder}, "Other": 1.0}) {
		t.Fatal(string(b))
	}
	if !strings.HasPrefix(string(b), "{\n  \"SongFolders\"") {
		t.Fatal("key order or indent changed: " + string(b))
	}
}

func TestParseRepo(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/owner/repo/": "owner/repo",
		"owner/repo":                     "owner/repo",
		"not a repo":                     "",
	} {
		if got := github.ParseRepo(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}

func TestMarkdownToText(t *testing.T) {
	md := "---\ntitle: T\nauthors:\n  - A\n---\n\n# Big **news**\n\n" +
		"Read [the notes](https://yarg.in/notes) and *try* it.\n\n![banner](x.webp)\n\n* one\n* two\n\n---\n"
	text := news.MarkdownToText(md)
	if strings.Split(text, "\n")[0] != "Big news" {
		t.Fatal(text)
	}
	for _, want := range []string{"Read the notes (https://yarg.in/notes) and try it.", "- one\n- two", "(Image: banner)"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %q", want, text)
		}
	}
	if strings.Contains(text, "title:") || strings.Contains(text, "---") {
		t.Fatal(text)
	}
}

func TestVersionOrder(t *testing.T) {
	if github.Compare(github.ParseVersion("v0.10.0"), github.ParseVersion("0.9.9")) <= 0 {
		t.Fatal("0.10.0 not after 0.9.9")
	}
}
