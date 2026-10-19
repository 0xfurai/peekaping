package utils_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"peekaping/internal/utils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func contextWithQuery(rawQuery string) *http.Request {
	return httptest.NewRequest(http.MethodGet, "/items?"+rawQuery, nil)
}

func TestGetQueryInt(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		want    int
		wantErr bool
	}{
		{name: "missing gives the default", query: "", want: 25},
		{name: "empty gives the default", query: "limit=", want: 25},
		{name: "a number", query: "limit=10", want: 10},
		{name: "a negative number", query: "limit=-3", want: -3},
		{name: "not a number", query: "limit=ten", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := utils.GetQueryInt(contextWithQuery(tt.query), "limit", 25)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetQueryBool(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name    string
		query   string
		want    *bool
		wantErr bool
	}{
		{name: "missing gives nil", query: "", want: nil},
		{name: "empty gives nil", query: "important=", want: nil},
		{name: "true", query: "important=true", want: &yes},
		{name: "1", query: "important=1", want: &yes},
		{name: "false", query: "important=false", want: &no},
		{name: "not a bool", query: "important=maybe", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := utils.GetQueryBool(contextWithQuery(tt.query), "important")
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
