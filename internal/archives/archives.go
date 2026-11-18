// Package archives unpacks every file type the YARG release server uses: zip, 7z, encrypted 7z.
package archives

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bodgit/sevenzip"
)

const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// SetlistPassword is the password for "encrypted" setlist archives.
//
// Same derivation as extract_encrypted() in the official launcher (src-tauri/src/utils.rs). The
// archives are only ever unpacked into the setlist folder YARG reads, exactly as the official
// launcher does.
func SetlistPassword() string {
	b := make([]byte, 64)
	for i := 0; i < 64; i++ {
		a := (5 + ((i * 104729) & 0xFF)) & 0xFF
		c := (9 + ((i * 224737) & 0xFF)) & 0xFF
		b[i] = letters[(a%c)%52]
	}
	return string(b)
}

// SafeTarget joins name onto root and refuses names that escape root.
func SafeTarget(root, name string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target := filepath.Clean(filepath.Join(rootAbs, filepath.FromSlash(strings.ReplaceAll(name, "\\", "/"))))
	if target != rootAbs && !strings.HasPrefix(strings.ToLower(target), strings.ToLower(rootAbs)+string(os.PathSeparator)) {
		return "", fmt.Errorf("Archive entry escapes the install folder: %s", name)
	}
	return target, nil
}

type entry interface {
	name() string
	isDir() bool
	open() (io.ReadCloser, error)
}

// check is an optional cancel test (nil = none): a non-nil error stops the unpacking with that error.
type check func() error

func first(c []func() error) check {
	if len(c) > 0 {
		return c[0]
	}
	return nil
}

// checkReader runs the cancel test every 4 MB read, so a large file can be stopped part way.
type checkReader struct {
	r     io.Reader
	c     check
	since int
}

func (cr *checkReader) Read(p []byte) (int, error) {
	if cr.since >= 4<<20 {
		cr.since = 0
		if err := cr.c(); err != nil {
			return 0, err
		}
	}
	n, err := cr.r.Read(p)
	cr.since += n
	return n, err
}

func extractAll(dest string, entries []entry, c check) error {
	targets := make([]string, len(entries))
	for i, e := range entries {
		t, err := SafeTarget(dest, e.name())
		if err != nil {
			return err
		}
		targets[i] = t
	}
	for i, e := range entries {
		if c != nil {
			if err := c(); err != nil {
				return err
			}
		}
		if e.isDir() {
			if err := os.MkdirAll(targets[i], 0o755); err != nil {
				return err
			}
			continue
		}
		if err := writeFile(targets[i], e, c); err != nil {
			return err
		}
	}
	return nil
}

func writeFile(target string, e entry, c check) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	src, err := e.open()
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.Create(target)
	if err != nil {
		return err
	}
	var r io.Reader = src
	if c != nil {
		r = &checkReader{r: src, c: c}
	}
	if _, err := io.Copy(dst, r); err != nil {
		dst.Close()
		os.Remove(target)
		return err
	}
	return dst.Close()
}

type zipEntry struct{ f *zip.File }

func (z zipEntry) name() string                 { return z.f.Name }
func (z zipEntry) isDir() bool                  { return strings.HasSuffix(z.f.Name, "/") }
func (z zipEntry) open() (io.ReadCloser, error) { return z.f.Open() }

// ExtractZip unpacks a zip; an optional cancel test stops it part way.
func ExtractZip(path, dest string, cancel ...func() error) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()
	var entries []entry
	for _, f := range r.File {
		entries = append(entries, zipEntry{f})
	}
	return extractAll(dest, entries, first(cancel))
}

type sevenEntry struct{ f *sevenzip.File }

func (s sevenEntry) name() string                 { return s.f.Name }
func (s sevenEntry) isDir() bool                  { return s.f.FileInfo().IsDir() }
func (s sevenEntry) open() (io.ReadCloser, error) { return s.f.Open() }

func Extract7z(path, dest, password string, cancel ...func() error) error {
	var r *sevenzip.ReadCloser
	var err error
	if password != "" {
		r, err = sevenzip.OpenReaderWithPassword(path, password)
	} else {
		r, err = sevenzip.OpenReader(path)
	}
	if err != nil {
		return err
	}
	defer r.Close()
	var entries []entry
	for _, f := range r.File {
		entries = append(entries, sevenEntry{f})
	}
	return extractAll(dest, entries, first(cancel))
}

// Extract unpacks path into dest; fileType is the release server's "zip", "7z" or "encrypted".
func Extract(path, dest, fileType string, cancel ...func() error) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	switch fileType {
	case "zip":
		return ExtractZip(path, dest, cancel...)
	case "7z":
		return Extract7z(path, dest, "", cancel...)
	case "encrypted":
		return Extract7z(path, dest, SetlistPassword(), cancel...)
	}
	return fmt.Errorf("Unknown archive type: %s", fileType)
}
