package api_key

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// generateAPIKey generates a secure API key with `ApiKeyPrefix` prefix
func generateAPIKey() (string, string, string, error) {
	// Generate `ApiKeyRandomBytes` random bytes
	bytes := make([]byte, ApiKeyRandomBytes)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", "", "", fmt.Errorf("error generating API key: %v", err)
	}

	// Encode to base64 and add prefix
	key := ApiKeyPrefix + base64.URLEncoding.EncodeToString(bytes)

	// Hash the key for storage
	keyHash, err := hashAPIKey(key)
	if err != nil {
		return "", "", "", fmt.Errorf("error hashing API key: %v", err)
	}

	// Generate display key (masked version)
	displayKey := maskAPIKey(key)

	return key, keyHash, displayKey, nil
}

// maskAPIKey creates a masked version of the API key for display
func maskAPIKey(apiKey string) string {
	if len(apiKey) <= 12 {
		return ApiKeyPrefix + "***"
	}
	return apiKey[:8] + "..." + apiKey[len(apiKey)-4:]
}

// hashAPIKey hashes an API key using bcrypt
func hashAPIKey(key string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(key), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// isValidAPIKeyFormat validates the format of an API key
func isValidAPIKeyFormat(key string) bool {
	// Check if it starts with `ApiKeyPrefix` and has reasonable length
	return len(key) >= 10 && len(key) <= 100 && 
		   len(key) > 3 && key[:3] == ApiKeyPrefix
}
