package cas

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// Hash is the SHA-256 content hash that identifies a blob in the CAS.
type Hash [32]byte

// HashBytes returns the SHA-256 of data.
func HashBytes(data []byte) Hash {
	return sha256.Sum256(data)
}

// HashReader returns the SHA-256 of the bytes read from r and the number of
// bytes consumed. It reads r to EOF.
func HashReader(r io.Reader) (Hash, int64, error) {
	h := sha256.New()
	n, err := io.Copy(h, r)
	if err != nil {
		return Hash{}, 0, err
	}
	var out Hash
	copy(out[:], h.Sum(nil))
	return out, n, nil
}

// String returns the lowercase hex encoding of the hash.
func (h Hash) String() string {
	return hex.EncodeToString(h[:])
}

// IsZero reports whether h is the zero hash.
func (h Hash) IsZero() bool {
	var zero Hash
	return h == zero
}

// MarshalJSON encodes the hash as a hex string.
func (h Hash) MarshalJSON() ([]byte, error) {
	return []byte(`"` + h.String() + `"`), nil
}

// UnmarshalJSON decodes a hex-encoded hash string.
func (h *Hash) UnmarshalJSON(data []byte) error {
	if len(data) < 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return errors.New("cas.Hash: expected quoted string")
	}
	return h.parse(string(data[1 : len(data)-1]))
}

// ParseHash parses a hex-encoded SHA-256 string.
func ParseHash(s string) (Hash, error) {
	var h Hash
	if err := h.parse(s); err != nil {
		return Hash{}, err
	}
	return h, nil
}

func (h *Hash) parse(s string) error {
	if len(s) != 64 {
		return fmt.Errorf("cas.Hash: expected 64 hex characters, got %d", len(s))
	}
	raw, err := hex.DecodeString(s)
	if err != nil {
		return fmt.Errorf("cas.Hash: %w", err)
	}
	copy(h[:], raw)
	return nil
}
