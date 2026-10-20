package infra

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// An install on DB_TYPE=mongo is told once at startup that the storage backend
// is deprecated, and where to read about it.
func TestWarnMongoDeprecated(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)

	warnMongoDeprecated(zap.New(core).Sugar())

	entries := logs.All()
	require.Len(t, entries, 1)
	require.Equal(t, zapcore.WarnLevel, entries[0].Level)
	require.Contains(t, entries[0].Message, "MongoDB storage is deprecated")
	require.Contains(t, entries[0].Message, "https://docs.peekaping.com/self-hosting/docker-with-mongo")
}
