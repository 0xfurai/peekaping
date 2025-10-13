package api_key

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMaskAPIKey(t *testing.T) {
	tests := []struct {
		name     string
		apiKey   string
		expected string
	}{
		{
			name:     "masks long API key",
			apiKey:   "pk_abcdefghijklmnopqrstuvwxyz",
			expected: "pk_abcde...wxyz",
		},
		{
			name:     "masks medium API key",
			apiKey:   "pk_1234567890abcdef",
			expected: "pk_12345...cdef",
		},
		{
			name:     "handles short API key (12 chars or less)",
			apiKey:   "pk_short",
			expected: ApiKeyPrefix + "***",
		},
		{
			name:     "handles very short API key",
			apiKey:   "pk_abc",
			expected: ApiKeyPrefix + "***",
		},
		{
			name:     "handles exact 12 char length",
			apiKey:   "pk_12345678",
			expected: ApiKeyPrefix + "***",
		},
		{
			name:     "handles 13 char length (just over threshold)",
			apiKey:   "pk_1234567890",
			expected: "pk_12345...7890",
		},
		{
			name:     "handles empty string",
			apiKey:   "",
			expected: ApiKeyPrefix + "***",
		},
		{
			name:     "preserves prefix in mask",
			apiKey:   "pk_verylongapikeywithmanycharacters",
			expected: "pk_veryl...ters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maskAPIKey(tt.apiKey)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsValidAPIKeyFormat(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected bool
	}{
		{
			name:     "valid API key with correct prefix and length",
			key:      "pk_abcdefghijklmnop",
			expected: true,
		},
		{
			name:     "valid API key with longer content",
			key:      "pk_" + "a" + string(make([]byte, 50)),
			expected: true,
		},
		{
			name:     "invalid - too short (less than 10)",
			key:      "pk_abc",
			expected: false,
		},
		{
			name:     "invalid - missing prefix",
			key:      "abcdefghijklmnop",
			expected: false,
		},
		{
			name:     "invalid - wrong prefix",
			key:      "sk_abcdefghijklmnop",
			expected: false,
		},
		{
			name:     "invalid - empty string",
			key:      "",
			expected: false,
		},
		{
			name:     "invalid - only prefix",
			key:      "pk_",
			expected: false,
		},
		{
			name:     "invalid - too long (over 200)",
			key:      "pk_" + string(make([]byte, 200)),
			expected: false,
		},
		{
			name:     "valid - exactly 10 characters",
			key:      "pk_1234567",
			expected: true,
		},
		{
			name:     "valid - exactly 200 characters",
			key:      "pk_" + string(make([]byte, 197)),
			expected: true,
		},
		{
			name:     "invalid - 201 characters",
			key:      "pk_" + string(make([]byte, 198)),
			expected: false,
		},
		{
			name:     "valid - typical base64 encoded key",
			key:      "pk_eyJpZCI6InRlc3QiLCJrZXkiOiJhY3R1YWxLZXkifQ==",
			expected: true,
		},
		{
			name:     "invalid - contains only prefix without content",
			key:      ApiKeyPrefix,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidAPIKeyFormat(tt.key)
			assert.Equal(t, tt.expected, result, "Expected isValidAPIKeyFormat(%q) to be %v", tt.key, tt.expected)
		})
	}
}

func TestAPIKeyFormatValidation(t *testing.T) {
	t.Run("valid keys should have correct format", func(t *testing.T) {
		// These should all be valid
		validKeys := []string{
			"pk_abcdef1234",
			"pk_ABCDEF5678",
			"pk_" + string(make([]byte, 20)),
			"pk_mixedCaseKey123",
			"pk_key_with_underscores",
			"pk_key-with-dashes",
			"pk_key.with.dots",
		}

		for _, key := range validKeys {
			if len(key) >= 10 && len(key) <= 200 {
				assert.True(t, isValidAPIKeyFormat(key), "Expected %q to be valid", key)
			}
		}
	})

	t.Run("invalid keys should be rejected", func(t *testing.T) {
		// These should all be invalid
		invalidKeys := []string{
			"",
			"pk",
			"pk_",
			"wrong_prefix_key",
			"no_prefix_at_all_but_long_enough",
			"ak_wrong_prefix_key",
		}

		for _, key := range invalidKeys {
			assert.False(t, isValidAPIKeyFormat(key), "Expected %q to be invalid", key)
		}
	})
}

func TestMaskAPIKeyConsistency(t *testing.T) {
	t.Run("masking same key twice returns same result", func(t *testing.T) {
		apiKey := "pk_1234567890abcdefghijk"

		result1 := maskAPIKey(apiKey)
		result2 := maskAPIKey(apiKey)

		assert.Equal(t, result1, result2)
	})

	t.Run("masked key should be shorter or equal to original", func(t *testing.T) {
		apiKey := "pk_verylongapikeythatshouldbemask"

		masked := maskAPIKey(apiKey)

		// Masked format: first 8 chars + "..." + last 4 chars = 15 chars
		assert.LessOrEqual(t, len(masked), len(apiKey))
	})

	t.Run("masked key should always contain prefix", func(t *testing.T) {
		keys := []string{
			"pk_shortkey",
			"pk_mediumlengthkey",
			"pk_verylongapikeywithmanycharacters",
		}

		for _, key := range keys {
			masked := maskAPIKey(key)
			assert.Contains(t, masked, ApiKeyPrefix)
		}
	})
}

func TestAPIKeyFormatEdgeCases(t *testing.T) {
	t.Run("null byte in key", func(t *testing.T) {
		keyWithNull := "pk_key\x00withNull"
		// Should still validate based on length and prefix
		if len(keyWithNull) >= 10 && len(keyWithNull) <= 200 {
			assert.True(t, isValidAPIKeyFormat(keyWithNull))
		}
	})

	t.Run("unicode characters in key", func(t *testing.T) {
		unicodeKey := "pk_key_with_émojis_😀"
		// Should validate based on length and prefix
		if len(unicodeKey) >= 10 && len(unicodeKey) <= 200 {
			assert.True(t, isValidAPIKeyFormat(unicodeKey))
		}
	})

	t.Run("key with special characters", func(t *testing.T) {
		specialKey := "pk_!@#$%^&*()"
		// Should validate based on length and prefix
		if len(specialKey) >= 10 && len(specialKey) <= 200 {
			assert.True(t, isValidAPIKeyFormat(specialKey))
		}
	})
}

func TestAPIKeyConstants(t *testing.T) {
	t.Run("ApiKeyPrefix is correct", func(t *testing.T) {
		assert.Equal(t, "pk_", ApiKeyPrefix)
	})

	t.Run("ApiKeyRandomBytes is reasonable", func(t *testing.T) {
		assert.Equal(t, 32, ApiKeyRandomBytes)
		assert.Greater(t, ApiKeyRandomBytes, 0)
	})
}
