package archives

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
	"unicode"
)

func TestSetlistPasswordShape(t *testing.T) {
	p := SetlistPassword()
	if len(p) != 64 || p != SetlistPassword() {
		t.Fatal(p)
	}
	for _, r := range p {
		if !unicode.IsLetter(r) {
			t.Fatal(p)
		}
	}
}

func TestZipRejectsPathEscape(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.zip")
	f, _ := os.Create(bad)
	z := zip.NewWriter(f)
	w, _ := z.Create("../evil.txt")
	w.Write([]byte("x"))
	z.Close()
	f.Close()
	if err := ExtractZip(bad, filepath.Join(dir, "out")); err == nil {
		t.Fatal("escape allowed")
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.txt")); err == nil {
		t.Fatal("file written outside")
	}
}
