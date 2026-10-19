package utils

import (
	"context"
	"net/http"
	"peekaping/internal/httpx"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"go.uber.org/zap"
)

type ApiResponse[T any] struct {
	Message string `json:"message" binding:"required"`
	Data    T      `json:"data" binding:"required"`
}

// NullableApiResponse describes endpoints that successfully return null data,
// such as a monitor without TLS information. Its wire envelope is unchanged.
type NullableApiResponse[T any] ApiResponse[T]

func (NullableApiResponse[T]) TransformSchema(_ huma.Registry, schema *huma.Schema) *huma.Schema {
	if data := schema.Properties["data"]; data != nil {
		schema.Properties["data"] = &huma.Schema{AnyOf: []*huma.Schema{data, {Type: "null"}}}
	}
	return schema
}

func NewNullableSuccessResponse[T any](message string, data T) NullableApiResponse[T] {
	return NullableApiResponse[T](NewSuccessResponse(message, data))
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

// RequireRecord is the typed handler equivalent of FindOr404.
func RequireRecord[T any](ctx context.Context, logger *zap.SugaredLogger, name string, find func(context.Context, string) (*T, error), id string) (*T, error) {
	record, err := find(ctx, id)
	if err != nil {
		logger.Errorw("Failed to find "+strings.ToLower(name), "id", id, "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, NewFailResponse("Internal server error"))
	}
	if record == nil {
		return nil, httpx.Fail(http.StatusNotFound, NewFailResponse(name+" not found"))
	}
	return record, nil
}
