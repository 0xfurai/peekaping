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
		{name: "tilde as the special character", password: "Abcdefg1~", valid: true},
		{name: "trailing space as the special character", password: "Abcdefg1 ", valid: true},
		{name: "apostrophe", password: "Abcdefg1'", valid: true},
		{name: "double quote", password: `Abcdefg1"`, valid: true},
		{name: "slash", password: "Abcdefg1/", valid: true},
		{name: "backslash", password: `Abcdefg1\`, valid: true},
		{name: "backtick", password: "Abcdefg1`", valid: true},
		{name: "7 characters, multi-byte letters with a digit and a special one", password: "Ééééé1!", valid: false},
		{name: "7 characters with a multi-byte special one", password: "Abcde1€", valid: false},
		{name: "7 characters, one an emoji outside the BMP", password: "Abcde1😀", valid: false},
		{name: "8 characters with a multi-byte special one", password: "Abcdef1€", valid: true},
		{name: "8 characters, one an emoji outside the BMP", password: "Abcdef1😀", valid: true},
		{name: "multi-byte upper and lower case letters count", password: "Éééééé1!", valid: true},
		{name: "multi-byte letters only, no number", password: "Éééééééé!", valid: false},
		{name: "other letters and a number are no special character", password: "Abcdéf1ß", valid: false},
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
