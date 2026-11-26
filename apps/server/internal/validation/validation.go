// Package validation holds the one validator that checks the structs of the server: the request
// DTOs and the configs of the monitor types and notification channels. It is shared on purpose,
// since the struct rules that modules register on it apply wherever a struct is validated.
package validation

import (
	"encoding/json"
	"sync"
	"unicode"
	"unicode/utf8"

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

// minPasswordLength is counted in characters (runes), not bytes: a password of 7 multi-byte
// letters is as short as one of 7 ASCII letters. The limit in bytes that bcrypt has is checked
// where the password is hashed.
const minPasswordLength = 8

// validatePassword checks if the password meets all requirements:
// - minimum length of 8 characters
// - at least one uppercase letter
// - at least one lowercase letter
// - at least one number
// - at least one special character: any character that is no letter and no number, a space too
//
// The web app checks the same rules in apps/web/src/lib/password.ts, with the same Unicode classes
// (\p{Lu}, \p{Ll}, \p{L}, \p{N}). Change the two together.
func validatePassword(fl validator.FieldLevel) bool {
	password := fl.Field().String()
	if utf8.RuneCountInString(password) < minPasswordLength {
		return false
	}

	var hasUpper, hasLower, hasNumber, hasSpecial bool
	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsNumber(char):
			hasNumber = true
		case !unicode.IsLetter(char):
			hasSpecial = true
		}
	}

	return hasUpper && hasLower && hasNumber && hasSpecial
}

// registerCustom registers the custom rules once. The validator's Register methods are not safe
// while another goroutine validates, and the API tests build many servers in one process.
var registerCustom sync.Once

func RegisterCustomValidators() {
	registerCustom.Do(func() { Validate.RegisterValidation("password", validatePassword) })
}

// InitializeValidator registers all custom validators on the global Validate instance
func InitializeValidator() {
	RegisterCustomValidators()
}
