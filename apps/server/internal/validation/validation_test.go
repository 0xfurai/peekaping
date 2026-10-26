package validation_test

import (
	"testing"

	"peekaping/internal/validation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPasswordRule(t *testing.T) {
	validation.RegisterCustomValidators()
	type form struct {
		Password string `validate:"password"`
	}

	tests := []struct {
		name     string
		password string
		valid    bool
	}{
		{name: "all classes and 8 characters", password: "Abcdef1!", valid: true},
		{name: "long passphrase", password: "Correct-horse-battery-1", valid: true},
		{name: "7 characters", password: "Abcde1!", valid: false},
		{name: "no uppercase letter", password: "abcdefg1!", valid: false},
		{name: "no lowercase letter", password: "ABCDEFG1!", valid: false},
		{name: "no number", password: "Abcdefgh!", valid: false},
		{name: "no special character", password: "Abcdefgh1", valid: false},
		{name: "empty", password: "", valid: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validation.Validate.Struct(form{Password: tt.password})
			if tt.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestValidateConfig(t *testing.T) {
	type webhook struct {
		URL     string `json:"url" validate:"required,url"`
		Retries int    `json:"retries" validate:"min=0,max=5"`
	}

	cfg, err := validation.ValidateConfig[webhook](`{"url":"https://example.com/hook","retries":3}`)
	require.NoError(t, err)
	assert.Equal(t, &webhook{URL: "https://example.com/hook", Retries: 3}, cfg)

	for name, raw := range map[string]string{
		"not JSON":        `{"url":`,
		"missing field":   `{"retries":3}`,
		"field too large": `{"url":"https://example.com/hook","retries":9}`,
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := validation.ValidateConfig[webhook](raw)
			assert.Error(t, err)
			assert.Nil(t, cfg)
		})
	}
}
