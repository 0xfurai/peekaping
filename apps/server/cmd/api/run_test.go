package main

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListenAddress(t *testing.T) {
	assert.Equal(t, ":8034", listenAddress("8034"))
	assert.Equal(t, ":9000", listenAddress(":9000"))
	assert.Equal(t, ":8084", listenAddress(""), "the port when none is set")
}

func TestRun_ServesTheAPIUntilItsContextIsDone(t *testing.T) {
	cfg := loadConfig(t, nil)
	logger, logs := observedLogger()
	in := startAPI(t, cfg, logger, logs)

	resp, err := http.Get(in.url + "/api/v1/version")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	assert.NoError(t, in.stop(t), "run returns nil, so the API exits 0")
}
