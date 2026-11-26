package domain_status_page

import (
	"crypto/rand"
	"encoding/hex"
)

// NewToken is a verification token: 32 random bytes in lower-case hex
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
