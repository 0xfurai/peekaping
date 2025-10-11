package api_key

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// generateAPIKey generates a secure API key with the new format: prefix + base64encode({id: api_key_id, key: actual_key})
func generateAPIKey(apiKeyID string) (string, string, string, error) {
	// MARK: generateAPIKey
	
	// Generate random bytes for the actual key
	bytes := make([]byte, ApiKeyRandomBytes)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", "", "", fmt.Errorf("error generating API key: %v", err)
	}

	// Create the actual key (without prefix)
	actualKey := base64.URLEncoding.EncodeToString(bytes)

	// Hash the key for storage
	keyHash, err := hashAPIKey(actualKey)
	if err != nil {
		return "", "", "", fmt.Errorf("error hashing API key: %v", err)
	}

	// Create the payload to encode in the token
	payload := map[string]string{
		"id":  apiKeyID,
		"key": actualKey, // Store the actual key, not the hash
	}

	// Encode the payload as JSON
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", "", "", fmt.Errorf("error marshaling payload: %v", err)
	}

	// Create the final API key token: prefix + base64encode(payload)
	token := ApiKeyPrefix + base64.URLEncoding.EncodeToString(payloadJSON)

	// Generate display key (masked version)
	displayKey := maskAPIKey(token)

	return token, keyHash, displayKey, nil
}

// maskAPIKey creates a masked version of the API key for display
func maskAPIKey(apiKey string) string {
	// MARK: maskAPIKey
	
	if len(apiKey) <= 12 {
		return ApiKeyPrefix + "***"
	}
	return apiKey[:8] + "..." + apiKey[len(apiKey)-4:]
}

// hashAPIKey hashes an API key using bcrypt
func hashAPIKey(key string) (string, error) {
	// MARK: hashAPIKey
	
	hash, err := bcrypt.GenerateFromPassword([]byte(key), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// isValidAPIKeyFormat validates the format of an API key
func isValidAPIKeyFormat(key string) bool {
	// MARK: isValidAPIKeyFormat
	
	// Check if it starts with `ApiKeyPrefix` and has reasonable length
	return len(key) >= 10 && len(key) <= 100 && 
		   len(key) > 3 && key[:3] == ApiKeyPrefix
}

// parseAPIKeyToken parses an API key token and extracts the ID and actual key
func parseAPIKeyToken(token string) (string, string, error) {
	// MARK: parseAPIKeyToken
	
	// Remove prefix
	if !isValidAPIKeyFormat(token) {
		return "", "", fmt.Errorf("invalid API key format")
	}

	// Decode base64 payload
	payloadB64 := token[len(ApiKeyPrefix):]
	payloadJSON, err := base64.URLEncoding.DecodeString(payloadB64)
	if err != nil {
		return "", "", fmt.Errorf("error decoding API key payload: %v", err)
	}

	// Parse JSON payload
	var payload map[string]string
	err = json.Unmarshal(payloadJSON, &payload)
	if err != nil {
		return "", "", fmt.Errorf("error parsing API key payload: %v", err)
	}

	// Extract ID and actual key
	apiKeyID, hasID := payload["id"]
	actualKey, hasKey := payload["key"]

	if !hasID || !hasKey {
		return "", "", fmt.Errorf("invalid API key payload structure")
	}

	return apiKeyID, actualKey, nil
}
