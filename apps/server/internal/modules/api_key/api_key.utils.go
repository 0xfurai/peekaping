package api_key

// maskAPIKey creates a masked version of the API key for display
func maskAPIKey(apiKey string) string {
	// MARK: maskAPIKey
	
	if len(apiKey) <= 12 {
		return ApiKeyPrefix + "***"
	}
	return apiKey[:8] + "..." + apiKey[len(apiKey)-4:]
}

// isValidAPIKeyFormat validates the format of an API key
func isValidAPIKeyFormat(key string) bool {
	// MARK: isValidAPIKeyFormat
	
	// Check if it starts with `ApiKeyPrefix` and has reasonable length (for base64 encode!)
	return len(key) >= 10 && len(key) <= 200 && key[:len(ApiKeyPrefix)] == ApiKeyPrefix
}
