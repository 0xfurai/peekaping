package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestToInternalConfigPropagatesRedisUsername(t *testing.T) {
	cfg := &Config{
		RedisHost:     "redis",
		RedisPort:     "6379",
		RedisUsername: "peekaping",
		RedisPassword: "secret",
		RedisDB:       2,
	}

	internalCfg := cfg.ToInternalConfig()

	require.Equal(t, "peekaping", internalCfg.RedisUsername)
	require.Equal(t, "secret", internalCfg.RedisPassword)
	require.Equal(t, 2, internalCfg.RedisDB)
}
