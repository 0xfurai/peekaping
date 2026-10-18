package providers

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPClient_RefusesRedirectFromHTTPSToHTTP(t *testing.T) {
	var plainHit atomic.Bool
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plainHit.Store(true)
	}))
	defer plain.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/hook", http.StatusTemporaryRedirect)
	}))
	defer secure.Close()
	client := NewHTTPClient()
	client.Transport = secure.Client().Transport

	resp, err := client.Post(secure.URL+"/hook", "application/json", nil)

	// An error, not the 3xx response: providers that only check for 4xx and 5xx would
	// report the redirect as sent
	if resp != nil {
		resp.Body.Close()
	}
	require.Error(t, err)
	assert.Contains(t, err.Error(), "307 Temporary Redirect")
	assert.False(t, plainHit.Load(), "credentials must not follow a redirect to plain HTTP")
}
