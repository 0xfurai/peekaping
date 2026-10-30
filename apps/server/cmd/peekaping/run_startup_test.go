package main

import (
	"context"
	"net"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A database that can't be opened stops startup, and run closes the listener it was given
func TestRun_StopsStartupWhenTheDatabaseCantBeOpened(t *testing.T) {
	cfg := loadConfig(t, nil)
	cfg.DBName = filepath.Join(t.TempDir(), "missing", "peekaping.db")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	logger, _ := observedLogger()

	err = run(context.Background(), cfg, logger, listener)

	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to build peekaping: ")
	assert.ErrorContains(t, err, "failed to ping database: unable to open database file")
	_, err = listener.Accept()
	assert.ErrorIs(t, err, net.ErrClosed, "run closed the API's listener")
}
