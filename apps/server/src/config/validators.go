package config

import (
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
)

// RegisterCustomValidators registers custom validators for configuration
func RegisterCustomValidators() {
	validate.RegisterValidation("duration_min", validateDurationMin)
	validate.RegisterValidation("numeric", validateNumeric)
	validate.RegisterValidation("port", validatePort)
	validate.RegisterValidation("db_type", validateDBType)
}

// validateDurationMin validates that a time.Duration is at least the specified minimum
func validateDurationMin(fl validator.FieldLevel) bool {
	duration, ok := fl.Field().Interface().(time.Duration)
	if !ok {
		return false
	}

	minParam := fl.Param()
	minDuration, err := time.ParseDuration(minParam)
	if err != nil {
		return false
	}

	return duration >= minDuration
}

// validateNumeric validates that a string represents a valid number
func validateNumeric(fl validator.FieldLevel) bool {
	value := fl.Field().String()
	if value == "" {
		return false
	}

	_, err := strconv.ParseInt(value, 10, 64)
	return err == nil
}

// validatePort validates that a string represents a valid port number
func validatePort(fl validator.FieldLevel) bool {
	value := fl.Field().String()
	if value == "" {
		return false
	}

	port, err := strconv.Atoi(value)
	if err != nil {
		return false
	}

	return port >= 1 && port <= 65535
}

// validateDBType validates that the database type is supported
func validateDBType(fl validator.FieldLevel) bool {
	dbType := strings.ToLower(fl.Field().String())
	validTypes := []string{"postgres", "postgresql", "mysql", "sqlite", "mongo", "mongodb"}

	for _, validType := range validTypes {
		if dbType == validType {
			return true
		}
	}

	return false
}
