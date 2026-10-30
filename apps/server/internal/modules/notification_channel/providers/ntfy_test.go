package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Validation allows a priority of 1 to 5, but Send takes the config as it is: a priority that is
// left out is the default one, and one below it sends no priority
func TestNTFYSendPriorityOutsideValidation(t *testing.T) {
	cases := []struct {
		name         string
		priority     string
		wantPriority string
	}{
		{"left out", ``, "3"},
		{"zero", `, "priority": 0`, "3"},
		{"set", `, "priority": 5`, "5"},
		{"below one", `, "priority": -1`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var priority string
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				priority = r.Header.Get("X-Priority")
			}))
			defer endpoint.Close()

			config := `{"server_url": "` + endpoint.URL + `", "topic": "peekaping", "authentication_type": "none"` + tc.priority + `}`
			message, mon, hb := goldenNotification(downEvent)
			err := NewNTFYSender(nopLogger(), endpoint.Client()).Send(context.Background(), config, message, mon, hb)

			require.NoError(t, err)
			assert.Equal(t, tc.wantPriority, priority)
		})
	}
}
