package minisign

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/blake2b"
)

func keypair(t *testing.T) (ed25519.PrivateKey, []byte, string) {
	pub, sk, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	keyID := []byte("12345678")
	return sk, keyID, base64.StdEncoding.EncodeToString(append(append([]byte("Ed"), keyID...), pub...))
}

func sign(sk ed25519.PrivateKey, keyID, data []byte, prehash bool) string {
	msg, alg := data, "Ed"
	if prehash {
		h := blake2b.Sum512(data)
		msg, alg = h[:], "ED"
	}
	sig := ed25519.Sign(sk, msg)
	const comment = "timestamp:1"
	global := ed25519.Sign(sk, append(append([]byte(nil), sig...), comment...))
	return strings.Join([]string{
		"untrusted comment: test",
		base64.StdEncoding.EncodeToString(append(append([]byte(alg), keyID...), sig...)),
		"trusted comment: " + comment,
		base64.StdEncoding.EncodeToString(global),
	}, "\n") + "\n"
}

func TestAcceptsGoodSignature(t *testing.T) {
	for _, prehash := range []bool{true, false} {
		sk, keyID, pub := keypair(t)
		f := filepath.Join(t.TempDir(), "a.zip")
		data := []byte(strings.Repeat("hello yarg", 1000))
		os.WriteFile(f, data, 0o644)
		got, err := VerifyFile(f, sign(sk, keyID, data, prehash), pub)
		if err != nil || got != "timestamp:1" {
			t.Fatalf("prehash %v: %q %v", prehash, got, err)
		}
	}
}

func TestRejectsTamperedFile(t *testing.T) {
	sk, keyID, pub := keypair(t)
	f := filepath.Join(t.TempDir(), "a.zip")
	sig := sign(sk, keyID, []byte("original"), true)
	os.WriteFile(f, []byte("tampered"), 0o644)
	var se *SignatureError
	if _, err := VerifyFile(f, sig, pub); !errors.As(err, &se) {
		t.Fatalf("got %v", err)
	}
}

func TestRejectsOtherKey(t *testing.T) {
	sk, keyID, _ := keypair(t)
	f := filepath.Join(t.TempDir(), "a.zip")
	os.WriteFile(f, []byte("data"), 0o644)
	var se *SignatureError
	if _, err := VerifyFile(f, sign(sk, keyID, []byte("data"), true), YargPublicKey); !errors.As(err, &se) {
		t.Fatalf("got %v", err)
	}
}

func TestParsesRealYargKey(t *testing.T) {
	raw, err := base64.StdEncoding.DecodeString(YargPublicKey)
	if err != nil || string(raw[:2]) != "Ed" || len(raw) != 42 {
		t.Fatal("bad key")
	}
}
