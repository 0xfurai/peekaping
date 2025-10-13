package utils

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
