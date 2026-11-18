// Package minisign checks minisign signatures, matching the YARC launcher's verification of YARG builds.
//
// Format (https://jedisct1.github.io/minisign/):
//
//	public key  = base64("Ed" + key_id[8] + ed25519_pk[32])
//	signature   = line 2: base64(alg[2] + key_id[8] + sig[64])
//	              line 3: "trusted comment: <text>"
//	              line 4: base64(global_sig[64]) over sig[64] + <text>
//	alg "Ed" signs the file itself, "ED" signs its BLAKE2b-512 hash.
package minisign

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"io"
	"os"
	"strings"

	"golang.org/x/crypto/blake2b"
)

const YargPublicKey = "RWSB28HEvrtuwvPn3pweVBodgVi/d+UH22xDsL3K8VBgeRqaIrDdTvps"

// SignatureError is a failed check.
type SignatureError struct{ msg string }

func (e *SignatureError) Error() string { return e.msg }

func fail(msg string) error { return &SignatureError{msg} }

func fileBlake2b(path string) ([]byte, error) {
	h, _ := blake2b.New512(nil)
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// VerifyFile checks path against a minisign signature and returns the trusted comment.
func VerifyFile(path, sigText, publicKeyB64 string) (string, error) {
	pk, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil || len(pk) != 42 || string(pk[:2]) != "Ed" {
		return "", fail("Bad public key")
	}
	keyID, key := pk[2:10], ed25519.PublicKey(pk[10:])

	var lines []string
	for _, ln := range strings.Split(strings.ReplaceAll(sigText, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(ln) != "" {
			lines = append(lines, ln)
		}
	}
	if len(lines) < 4 {
		return "", fail("Signature file is incomplete")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil || len(sig) != 74 {
		return "", fail("Signature has the wrong length")
	}
	alg, sigKeyID, signature := string(sig[:2]), sig[2:10], sig[10:]
	if !bytes.Equal(sigKeyID, keyID) {
		return "", fail("Signed with a different key")
	}
	const prefix = "trusted comment: "
	if !strings.HasPrefix(lines[2], prefix) {
		return "", fail("Missing trusted comment")
	}
	trusted := strings.TrimRight(lines[2][len(prefix):], "\r")
	globalSig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[3]))
	if err != nil {
		return "", fail("The download does not match its signature")
	}

	var message []byte
	switch alg {
	case "ED":
		if message, err = fileBlake2b(path); err != nil {
			return "", err
		}
	case "Ed":
		if message, err = os.ReadFile(path); err != nil {
			return "", err
		}
	default:
		return "", fail("Unknown signature algorithm")
	}
	if !ed25519.Verify(key, message, signature) ||
		!ed25519.Verify(key, append(append([]byte(nil), signature...), trusted...), globalSig) {
		return "", fail("The download does not match its signature")
	}
	return trusted, nil
}
