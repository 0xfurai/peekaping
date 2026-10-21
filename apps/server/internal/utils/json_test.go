package utils_test

import (
	"testing"

	"peekaping/internal/utils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHasJSONKey(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "missing key", body: `{"name":"x"}`, want: false},
		{name: "exact key", body: `{"active":false}`, want: true},
		{name: "key in another case", body: `{"Active":false}`, want: true},
		{name: "null value", body: `{"active":null}`, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := utils.HasJSONKey([]byte(tt.body), "active")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHasJSONKeyRejectsNonObject(t *testing.T) {
	_, err := utils.HasJSONKey([]byte(`[]`), "active")
	assert.Error(t, err)
}
