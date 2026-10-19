package utils

import (
	"strconv"

	"net/http"
)

const MaxPageSize = 1000

// GetQueryInt extracts an integer query parameter from the context.
// If the parameter is not provided or invalid, it returns the default value.
func GetQueryInt(r *http.Request, key string, defaultValue int) (int, error) {
	valueStr := r.URL.Query().Get(key)
	if valueStr == "" {
		return defaultValue, nil
	}

	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return 0, err
	}

	return value, nil
}

// GetQueryBool extracts a boolean query parameter from the context.
// If the parameter is not provided, it returns nil.
func GetQueryBool(r *http.Request, key string) (*bool, error) {
	valueStr := r.URL.Query().Get(key)
	if valueStr == "" {
		return nil, nil
	}
	value, err := strconv.ParseBool(valueStr)
	if err != nil {
		return nil, err
	}
	return &value, nil
}
