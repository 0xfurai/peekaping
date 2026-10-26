package job_test

import (
	"testing"

	"peekaping/internal/modules/check/job"

	"github.com/stretchr/testify/assert"
)

func TestCertExpiryRequested(t *testing.T) {
	tests := []struct {
		name      string
		config    string
		requested bool
		fails     bool
	}{
		{name: "an empty config asks for nothing", config: ""},
		{name: "the flag set", config: `{"url":"https://example.com","check_cert_expiry":true}`, requested: true},
		{name: "the flag cleared", config: `{"check_cert_expiry":false}`},
		{name: "no flag", config: `{"url":"https://example.com"}`},
		{name: "a config that isn't JSON asks for nothing and says why", config: `not json`, fails: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requested, err := job.CertExpiryRequested(tt.config)

			assert.Equal(t, tt.requested, requested)
			if tt.fails {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
