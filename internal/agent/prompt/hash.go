package prompt

import (
	"crypto/sha256"
	"encoding/hex"
)

// Hash returns the first 12 hex characters of SHA256(s).
func Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:12]
}
