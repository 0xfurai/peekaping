package ingester

import (
	"testing"

	"peekaping/internal/modules/certificate"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The monitor types whose TLS info the ingester keeps are the ones named http... and smtp, in any case.
func TestIngester_TLSInfoMonitorTypes(t *testing.T) {
	tlsInfo := &certificate.TLSInfo{Valid: true}

	for monitorType, kept := range map[string]bool{
		"smtp":         true,
		"SMTP":         true,
		"Smtp":         true,
		"https":        true,
		"HTTP":         true,
		"Http-Keyword": true,
		"http-json":    true,
		"smtps":        false,
		"tcp":          false,
		"ping":         false,
		"dns":          false,
		"":             false,
		"xhttp":        false,
	} {
		t.Run(monitorType, func(t *testing.T) {
			ti := newTestIngester()
			payload := checkResult(monitorSettings{}, up)
			payload.MonitorType = monitorType
			payload.TLSInfo = tlsInfo
			payload.CheckCertExpiry = true

			require.NoError(t, ti.ingest(t, payload))

			if kept {
				assert.Equal(t, []string{monitorID}, ti.certs.updated)
				assert.Equal(t, []string{monitorID}, ti.certs.checked)
			} else {
				assert.Empty(t, ti.certs.updated)
				assert.Empty(t, ti.certs.checked)
			}
		})
	}
}
