package utils_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"peekaping/internal/utils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type record struct{ Name string }

func findOr404(t *testing.T, find func(context.Context, string) (*record, error)) (*record, bool, *httptest.ResponseRecorder) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tags/42", nil)
	found, ok := utils.FindOr404(req.Context(), rec, zap.NewNop().Sugar(), "Tag", find, "42")
	return found, ok, rec
}

func message(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body utils.ApiResponse[any]
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.Message
}

func TestFindOr404_ReturnsTheRecordAndWritesNothing(t *testing.T) {
	var askedFor string
	found, ok, rec := findOr404(t, func(_ context.Context, id string) (*record, error) {
		askedFor = id
		return &record{Name: "Production"}, nil
	})

	assert.True(t, ok)
	assert.Equal(t, &record{Name: "Production"}, found)
	assert.Equal(t, "42", askedFor)
	assert.Empty(t, rec.Body.String(), "the handler writes the response")
}

func TestFindOr404_RespondsNotFoundWhenThereIsNoRecord(t *testing.T) {
	found, ok, rec := findOr404(t, func(context.Context, string) (*record, error) { return nil, nil })

	assert.False(t, ok)
	assert.Nil(t, found)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "Tag not found", message(t, rec))
}

func TestFindOr404_RespondsInternalErrorWhenTheLookupFails(t *testing.T) {
	found, ok, rec := findOr404(t, func(context.Context, string) (*record, error) {
		return nil, errors.New("database is locked")
	})

	assert.False(t, ok)
	assert.Nil(t, found)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "Internal server error", message(t, rec), "the lookup error stays out of the response")
}
