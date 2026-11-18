// Package sng reads .sng song packages, the single-file format Chorus Encore serves and YARG loads
// directly.
//
// Layout (all integers little-endian), as YARG.Core's SngFile reads it:
//
//	"SNGPKG", uint32 version, 16-byte mask seed,
//	int64 metadata size, uint64 pair count, then (int32 length, key, int32 length, value) pairs,
//	int64 file index size, uint64 file count, then (byte name length, name, int64 size, int64 offset),
//	then the file data.
//
// Each file is masked: byte i of a file is XORed with seed[i % 16] ^ (i & 0xFF).
package sng

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

// Magic starts every .sng file.
var Magic = []byte("SNGPKG")

// Error is a .sng file that is not one, or is damaged.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

// Entry is where one file's (masked) bytes sit in the package.
type Entry struct {
	Offset int64
	Length int64
}

// File is an opened .sng package, held in memory.
type File struct {
	// Metadata is the package's song.ini-style metadata, by lower-case key.
	Metadata map[string]string
	// Files is each packed file by lower-case name.
	Files map[string]Entry
	// Names is the lower-case file names in the order the package stores them.
	Names []string

	data []byte
	mask [256]byte
}

// Open reads a whole .sng file.
func Open(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse reads a .sng package already in memory. The slice is kept, not copied.
func Parse(data []byte) (*File, error) {
	if len(data) < 6 || !bytes.Equal(data[:6], Magic) {
		return nil, &Error{"This is not a .sng file."}
	}
	damaged := &Error{"The .sng file is damaged."}
	f := &File{Metadata: map[string]string{}, Files: map[string]Entry{}, data: data}
	pos := 6 + 4
	if len(data) < pos+16 {
		return nil, damaged
	}
	seed := data[pos : pos+16]
	pos += 16
	for i := range f.mask {
		f.mask[i] = seed[i%16] ^ byte(i)
	}

	need := func(n int64) bool { return n >= 0 && int64(pos)+n <= int64(len(data)) }
	i32 := func() (int, bool) {
		if !need(4) {
			return 0, false
		}
		v := int32(binary.LittleEndian.Uint32(data[pos:]))
		pos += 4
		return int(v), true
	}

	if !need(16) {
		return nil, damaged
	}
	size := int64(binary.LittleEndian.Uint64(data[pos:]))
	count := binary.LittleEndian.Uint64(data[pos+8:])
	end := int64(pos) + 8 + size
	pos += 16
	for k := uint64(0); k < count; k++ {
		n, ok := i32()
		if !ok || !need(int64(n)) {
			return nil, damaged
		}
		key := decode(data[pos : pos+n])
		pos += n
		n, ok = i32()
		if !ok || !need(int64(n)) {
			return nil, damaged
		}
		f.Metadata[strings.ToLower(key)] = decode(data[pos : pos+n])
		pos += n
	}
	if end < 0 || end > int64(len(data)) {
		return nil, damaged
	}
	pos = int(end)

	if !need(16) {
		return nil, damaged
	}
	count = binary.LittleEndian.Uint64(data[pos+8:])
	pos += 16
	for k := uint64(0); k < count; k++ {
		if !need(1) {
			return nil, damaged
		}
		n := int(data[pos])
		if !need(1 + int64(n) + 16) {
			return nil, damaged
		}
		name := decode(data[pos+1 : pos+1+n])
		pos += 1 + n
		length := int64(binary.LittleEndian.Uint64(data[pos:]))
		offset := int64(binary.LittleEndian.Uint64(data[pos+8:]))
		pos += 16
		if offset < 0 || length < 0 || offset+length > int64(len(data)) || offset+length < 0 {
			return nil, &Error{fmt.Sprintf("The .sng file is damaged (%s).", name)}
		}
		lower := strings.ToLower(name)
		if _, seen := f.Files[lower]; !seen {
			f.Names = append(f.Names, lower)
		}
		f.Files[lower] = Entry{offset, length}
	}
	return f, nil
}

// Read returns one packed file, unmasked. The name is matched without case.
func (f *File) Read(name string) ([]byte, error) {
	e, ok := f.Files[strings.ToLower(name)]
	if !ok {
		return nil, fmt.Errorf("sng: no file %q in the package", name)
	}
	raw := f.data[e.Offset : e.Offset+e.Length]
	out := make([]byte, len(raw))
	for i, b := range raw {
		out[i] = b ^ f.mask[i&0xFF]
	}
	return out, nil
}

// decode is Python's bytes.decode("utf-8", "replace"): each maximal invalid subpart becomes U+FFFD.
func decode(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var sb strings.Builder
	for i := 0; i < len(b); {
		r, n := utf8.DecodeRune(b[i:])
		if r != utf8.RuneError || n > 1 {
			sb.WriteRune(r)
			i += n
			continue
		}
		sb.WriteRune(utf8.RuneError)
		i += invalidLen(b[i:])
	}
	return sb.String()
}

// invalidLen is the length of the maximal subpart of an ill-formed UTF-8 sequence at b[0], the unit
// Python replaces with one U+FFFD.
func invalidLen(b []byte) int {
	c := b[0]
	need, lo, hi := 0, byte(0x80), byte(0xBF)
	switch {
	case c >= 0xC2 && c <= 0xDF:
		need = 1
	case c == 0xE0:
		need, lo = 2, 0xA0
	case c == 0xED:
		need, hi = 2, 0x9F
	case c >= 0xE1 && c <= 0xEF:
		need = 2
	case c == 0xF0:
		need, lo = 3, 0x90
	case c >= 0xF1 && c <= 0xF3:
		need = 3
	case c == 0xF4:
		need, hi = 3, 0x8F
	default:
		return 1
	}
	k := 1
	for k <= need && k < len(b) {
		x := b[k]
		if k == 1 && (x < lo || x > hi) || k > 1 && (x < 0x80 || x > 0xBF) {
			break
		}
		k++
	}
	return k
}
