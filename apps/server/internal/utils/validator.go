package utils

import (
	"encoding/json"
	"strings"
	"unicode"

	"github.com/go-playground/validator/v10"
)

var Validate = validator.New()

// ValidateConfig unmarshals the config string into the given struct and validates it using struct tags.
func ValidateConfig[T any](config string) (*T, error) {
	var cfg T
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return nil, err
	}
	if err := Validate.Struct(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// validatePassword checks if the password meets all requirements:
// - minimum length of 8 characters
// - at least one uppercase letter
// - at least one lowercase letter
// - at least one number
// - at least one special character
func validatePassword(fl validator.FieldLevel) bool {
	password := fl.Field().String()

	// Check minimum length
	if len(password) < 8 {
		return false
	}

	var (
		hasUpper   bool
		hasLower   bool
		hasNumber  bool
		hasSpecial bool
	)

	specialChars := "!@#$%^&*()_+-=[]{}|;:,.<>?"

	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsNumber(char):
			hasNumber = true
		case strings.ContainsRune(specialChars, char):
			hasSpecial = true
		}
	}

	return hasUpper && hasLower && hasNumber && hasSpecial
}

func RegisterCustomValidators() {
	Validate.RegisterValidation("password", validatePassword)
}
