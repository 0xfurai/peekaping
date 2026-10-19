package utils

import (
	"context"
	"net/http"
	"peekaping/internal/httpx"
	"strings"

	"go.uber.org/zap"
)

type APIError struct {
	Status  string `json:"status" binding:"required"`
	Message string `json:"message" binding:"required"`
}

type ApiResponse[T any] struct {
	Message string `json:"message" binding:"required"`
	Data    T      `json:"data" binding:"required"`
}

// NewSuccessResponse creates a successful API response.
func NewSuccessResponse[T any](message string, data T) ApiResponse[T] {
	return ApiResponse[T]{
		Message: message,
		Data:    data,
	}
}

// NewFailResponse creates a failed API response.
func NewFailResponse(message string) ApiResponse[any] {
	return ApiResponse[any]{
		Message: message,
		Data:    nil,
	}
}

type URIParams struct {
	ID string `uri:"id" binding:"required"` // e.g., /items/:id
}

type PaginatedQueryParams struct {
	Page  int `form:"page" binding:"numeric"`
	Limit int `form:"limit" binding:"numeric,max=50"`
}

// FindOr404 looks up the record a handler is about to read, change or delete.
// When there's none it responds 404, and when the lookup fails it logs and
// responds 500. Either way it returns false and the handler should stop.
func FindOr404[T any](ctx context.Context, w http.ResponseWriter, logger *zap.SugaredLogger, name string, find func(context.Context, string) (*T, error), id string) (*T, bool) {
	record, err := find(ctx, id)
	if err != nil {
		logger.Errorw("Failed to find "+strings.ToLower(name), "id", id, "error", err)
		httpx.JSON(w, http.StatusInternalServerError, NewFailResponse("Internal server error"))
		return nil, false
	}
	if record == nil {
		httpx.JSON(w, http.StatusNotFound, NewFailResponse(name+" not found"))
		return nil, false
	}
	return record, true
}
