package update

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Releases are signed: the release build signs SHA256SUMS.txt with a private
// Ed25519 key that only lives in the build secrets, and every easyTTY carries
// the public key. A checksum file from the release page alone proves nothing
// (whoever can upload the exe can upload the checksums too); the signature
// can only come from the key holder.

// SigName is the signature of the checksum file in a release.
const SigName = sumsName + ".sig"

// ParsePublicKey reads a base64 Ed25519 public key.
func ParsePublicKey(b64 string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("Prüfschlüssel für Updates ungültig")
	}
	return ed25519.PublicKey(raw), nil
}

// ParsePrivateKey reads a base64 Ed25519 key: the 32-byte seed or the full
// 64-byte private key.
func ParsePrivateKey(b64 string) (ed25519.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return nil, errors.New("Signierschlüssel ist kein Base64")
	}
	switch len(raw) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(raw), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(raw), nil
	}
	return nil, fmt.Errorf("Signierschlüssel hat %d Bytes, erwartet %d oder %d", len(raw), ed25519.SeedSize, ed25519.PrivateKeySize)
}

// Sign returns the signature file content for data.
func Sign(key ed25519.PrivateKey, data []byte) []byte {
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(key, data)) + "\n")
}

// verifySig checks a signature file against data.
func verifySig(pub ed25519.PublicKey, data, sigFile []byte) error {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigFile)))
	if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(pub, data, sig) {
		return errors.New("Signatur der Prüfsummen ungültig, Update verworfen")
	}
	return nil
}
