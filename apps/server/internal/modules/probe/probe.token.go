package probe

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

const (
	// tokenScheme starts every token the API makes, so that a leaked one is recognisable
	tokenScheme = "pkp_"

	// tokenRandomBytes is how many random bytes a token holds
	tokenRandomBytes = 32

	// tokenHashPrefix starts the stored hash of a token hashed with SHA-256, as for an API key
	tokenHashPrefix = "sha256$"

	// displayPrefixLength is how many of a token's first characters the API shows in its place
	displayPrefixLength = 8

	// MinBootstrapTokenLength is the fewest characters PROBE_BOOTSTRAP_TOKEN may have
	MinBootstrapTokenLength = 32
)

// generateToken makes a probe token: "pkp_" and 32 random bytes in base64url, without padding
func generateToken() (string, error) {
	random := make([]byte, tokenRandomBytes)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("error generating probe token: %w", err)
	}
	return tokenScheme + base64.RawURLEncoding.EncodeToString(random), nil
}

// hashToken hashes a probe token with SHA-256, as "sha256$" and the hex digest. A token is 32
// random bytes, so it needs no slow hash, and a fast one keeps the check on every request cheap.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return tokenHashPrefix + hex.EncodeToString(sum[:])
}

// tokenDisplayPrefix is the first characters of a token, which the API shows in its place
func tokenDisplayPrefix(token string) string {
	if len(token) <= displayPrefixLength {
		return token
	}
	return token[:displayPrefixLength]
}
