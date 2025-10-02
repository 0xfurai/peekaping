package utils

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// GetQueryInt extracts an integer query parameter from the context.
// If the parameter is not provided or invalid, it returns the default value.
func GetQueryInt(ctx *gin.Context, key string, defaultValue int) (int, error) {
	valueStr := ctx.Query(key)
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
func GetQueryBool(ctx *gin.Context, key string) (*bool, error) {
	valueStr := ctx.Query(key)
	if valueStr == "" {
		return nil, nil
	}
	value, err := strconv.ParseBool(valueStr)
	if err != nil {
		return nil, err
	}
	return &value, nil
}
