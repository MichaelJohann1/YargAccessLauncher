package sng

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type pair struct{ k, v string }

// makeSng builds a package the way the Python test does: seed 16..31, files in the given order.
func makeSng(t *testing.T, path string, names []string, files map[string][]byte, metadata []pair) {
	t.Helper()
	seed := make([]byte, 16)
	for i := range seed {
		seed[i] = byte(16 + i)
	}
	var mask [256]byte
	for i := range mask {
		mask[i] = seed[i%16] ^ byte(i)
	}
	le := binary.LittleEndian
	var meta bytes.Buffer
	for _, p := range metadata {
		binary.Write(&meta, le, int32(len(p.k)))
		meta.WriteString(p.k)
		binary.Write(&meta, le, int32(len(p.v)))
		meta.WriteString(p.v)
	}
	indexLen := 0
	for _, n := range names {
		indexLen += 1 + len(n) + 16
	}
	pos := 6 + 4 + 16 + 16 + meta.Len() + 16 + indexLen + 8
	var index, blobs bytes.Buffer
	for _, n := range names {
		data := files[n]
		index.WriteByte(byte(len(n)))
		index.WriteString(n)
		binary.Write(&index, le, int64(len(data)))
		binary.Write(&index, le, int64(pos+blobs.Len()))
		for i, b := range data {
			blobs.WriteByte(b ^ mask[i%256])
		}
	}
	var out bytes.Buffer
	out.WriteString("SNGPKG")
	binary.Write(&out, le, uint32(1))
	out.Write(seed)
	binary.Write(&out, le, int64(meta.Len()+8))
	binary.Write(&out, le, uint64(len(metadata)))
	out.Write(meta.Bytes())
	binary.Write(&out, le, int64(indexLen+8))
	binary.Write(&out, le, uint64(len(names)))
	out.Write(index.Bytes())
	binary.Write(&out, le, int64(blobs.Len()))
	out.Write(blobs.Bytes())
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSngRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.sng")
	var big []byte
	for k := 0; k < 40; k++ {
		for i := 0; i < 256; i++ {
			big = append(big, byte(i))
		}
	}
	big = append(big, "tail"...)
	makeSng(t, p, []string{"notes.chart", "Song.opus"},
		map[string][]byte{"notes.chart": []byte("[Song]"), "Song.opus": big},
		[]pair{{"delay", "120"}, {"name", "X"}})
	f, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.Metadata, map[string]string{"delay": "120", "name": "X"}) {
		t.Fatalf("metadata %v", f.Metadata)
	}
	if got, _ := f.Read("notes.chart"); string(got) != "[Song]" {
		t.Fatalf("notes.chart %q", got)
	}
	if got, _ := f.Read("song.opus"); !bytes.Equal(got, big) {
		t.Fatal("song.opus differs")
	}
	if !reflect.DeepEqual(f.Names, []string{"notes.chart", "song.opus"}) {
		t.Fatalf("names %v", f.Names)
	}
}

func TestNotSng(t *testing.T) {
	if _, err := Parse([]byte("RIFF....")); err == nil || err.Error() != "This is not a .sng file." {
		t.Fatalf("err %v", err)
	}
}

func TestDecodeReplace(t *testing.T) {
	// Python: b"a\xe2\x82b\xffc".decode("utf-8", "replace") == "a�b�c"
	if got := decode([]byte("a\xe2\x82b\xffc")); got != "a�b�c" {
		t.Fatalf("%q", got)
	}
}
