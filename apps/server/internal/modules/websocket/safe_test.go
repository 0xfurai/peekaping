package websocket

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// engine.io runs each socket handler in its own goroutine and nothing above it
// recovers, so a panic in a handler would end the process.
func TestSafeRecoversAPanic(t *testing.T) {
	core, logs := observer.New(zap.ErrorLevel)
	handler := safe(zap.New(core).Sugar(), "join_room", func(...interface{}) {
		panic("boom")
	})

	assert.NotPanics(t, func() { handler("monitor:a") })

	entries := logs.All()
	require.Len(t, entries, 1)
	assert.Equal(t, zap.ErrorLevel, entries[0].Level)
	assert.Equal(t, "join_room", entries[0].ContextMap()["event"])
	assert.Equal(t, "boom", entries[0].ContextMap()["panic"])
}
