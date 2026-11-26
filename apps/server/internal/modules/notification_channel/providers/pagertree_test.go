package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pagerTreeIDs sends each notification to PagerTree in order and returns the Id of each request
func pagerTreeIDs(t *testing.T, notifications ...pagerTreeNotification) []string {
	t.Helper()
	var ids []string
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID string `json:"id"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		ids = append(ids, body.ID)
		_, _ = w.Write([]byte("{}"))
	}))
	defer endpoint.Close()

	sender := NewPagerTreeSender(nopLogger(), endpoint.Client())
	config := `{"integrationUrl": "` + endpoint.URL + `", "autoResolve": true}`
	for _, n := range notifications {
		require.NoError(t, sender.Send(context.Background(), config, "msg", n.monitor, n.heartbeat))
	}
	return ids
}

type pagerTreeNotification struct {
	monitor   *monitor.Model
	heartbeat *heartbeat.Model
}

// A monitor's Down creates the alert and its Up resolves it, and PagerTree matches the two on Id
func TestPagerTreeResolveCarriesTheIDOfTheCreate(t *testing.T) {
	ids := pagerTreeIDs(t,
		pagerTreeNotification{goldenMonitor, goldenDown},
		pagerTreeNotification{goldenMonitor, goldenUp},
	)

	// Not just equal: sends in different seconds must give the same Id, so compare to the literal
	assert.Equal(t, []string{"monitor-mon-1", "monitor-mon-1"}, ids)
}

// A Down sent again while the alert is open updates it instead of opening another
func TestPagerTreeResentDownCarriesTheSameID(t *testing.T) {
	ids := pagerTreeIDs(t,
		pagerTreeNotification{goldenMonitor, goldenDown},
		pagerTreeNotification{goldenMonitor, goldenDown},
	)

	// Not just equal: sends in different seconds must give the same Id, so compare to the literal
	assert.Equal(t, []string{"monitor-mon-1", "monitor-mon-1"}, ids)
}

func TestPagerTreeMonitorsHaveDifferentIDs(t *testing.T) {
	other := *goldenMonitor
	other.ID = "mon-2"

	ids := pagerTreeIDs(t,
		pagerTreeNotification{goldenMonitor, goldenDown},
		pagerTreeNotification{&other, goldenDown},
	)

	require.Len(t, ids, 2)
	assert.Equal(t, "monitor-mon-1", ids[0])
	assert.Equal(t, "monitor-mon-2", ids[1])
}

// A certificate-expiry warning carries no heartbeat. It must not merge into the outage's alert,
// where the outage's Up would resolve it.
func TestPagerTreeCertificateExpiryIDDiffersFromTheOutageID(t *testing.T) {
	ids := pagerTreeIDs(t,
		pagerTreeNotification{goldenMonitor, goldenDown},
		pagerTreeNotification{goldenMonitor, nil},
	)

	require.Len(t, ids, 2)
	assert.Equal(t, "monitor-mon-1", ids[0])
	assert.NotEqual(t, ids[0], ids[1])
	assert.Regexp(t, `^monitor-mon-1-\d+$`, ids[1])
}

// PagerTree's webhook v3 integration reads its params by these lowercase keys and ignores any
// other spelling, so a capitalised "Id" would leave every alert with a random ID of its own
func TestPagerTreeSendsTheKeysItsWebhookReads(t *testing.T) {
	var body map[string]any
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		_, _ = w.Write([]byte("{}"))
	}))
	defer endpoint.Close()

	sender := NewPagerTreeSender(nopLogger(), endpoint.Client())
	config := `{"integrationUrl": "` + endpoint.URL + `", "urgency": "high"}`
	require.NoError(t, sender.Send(context.Background(), config, "msg", goldenMonitor, goldenDown))

	keys := make([]string, 0, len(body))
	for key := range body {
		keys = append(keys, key)
	}
	assert.ElementsMatch(t, []string{"event_type", "id", "title", "description", "urgency", "tags", "meta"}, keys)
}
