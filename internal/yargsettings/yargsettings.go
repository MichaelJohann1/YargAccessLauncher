// Package yargsettings adds a song folder to YARG's own settings so downloaded charts show up in the game.
//
// YARG keeps settings in %USERPROFILE%\AppData\LocalLow\YARC\YARG\<build>\settings.json (build is
// "release" for stable). The game rewrites the file on exit, so this is only done while YARG is closed.
package yargsettings

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

// LocalLow is YARG's data folder; a variable so tests can point it elsewhere.
var LocalLow = filepath.Join(home(), "AppData", "LocalLow", "YARC", "YARG")

func SettingsFiles() []string {
	files, _ := filepath.Glob(filepath.Join(LocalLow, "*", "settings.json"))
	sort.Strings(files)
	return files
}

func norm(p string) string { return strings.ToLower(filepath.Clean(p)) }

// object is a JSON object that keeps its key order, so the file is rewritten as YARG wrote it.
type object struct {
	keys []string
	vals map[string]json.RawMessage
}

func readJSON(path string) (*object, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))))
	if t, err := d.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("not a JSON object")
	}
	o := &object{vals: map[string]json.RawMessage{}}
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		k, _ := t.(string)
		var v json.RawMessage
		if err := d.Decode(&v); err != nil {
			return nil, err
		}
		if _, seen := o.vals[k]; !seen {
			o.keys = append(o.keys, k)
		}
		o.vals[k] = v
	}
	return o, nil
}

func (o *object) folders() ([]any, bool) {
	raw, ok := o.vals["SongFolders"]
	if !ok {
		return nil, false
	}
	var folders []any
	if json.Unmarshal(raw, &folders) != nil || folders == nil && string(raw) != "[]" {
		return nil, false
	}
	return folders, true
}

func (o *object) marshal() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(o.vals[k])
	}
	buf.WriteByte('}')
	var out bytes.Buffer
	if err := json.Indent(&out, buf.Bytes(), "", "  "); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func contains(folders []any, folder string) bool {
	n := norm(folder)
	for _, p := range folders {
		if s, ok := p.(string); ok && norm(s) == n {
			return true
		}
	}
	return false
}

// AddSongFolder adds folder to SongFolders in every YARG settings file and returns the files changed.
func AddSongFolder(folder string) []string {
	var changed []string
	for _, path := range SettingsFiles() {
		data, err := readJSON(path)
		if err != nil {
			continue
		}
		folders, ok := data.folders()
		if !ok || contains(folders, folder) {
			continue
		}
		raw, _ := json.Marshal(append(folders, folder))
		data.vals["SongFolders"] = raw
		out, err := data.marshal()
		if err != nil {
			continue
		}
		tmp := path + ".tmp"
		if os.WriteFile(tmp, out, 0o644) != nil || os.Rename(tmp, path) != nil {
			continue
		}
		changed = append(changed, path)
	}
	return changed
}

func HasSongFolder(folder string) bool {
	for _, path := range SettingsFiles() {
		data, err := readJSON(path)
		if err != nil {
			continue
		}
		if folders, ok := data.folders(); ok && contains(folders, folder) {
			return true
		}
	}
	return false
}
