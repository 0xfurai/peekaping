package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigIncludesRedisUsername(t *testing.T) {
	cfg := Config{
		RedisHost:     "redis",
		RedisPort:     "6379",
		RedisUsername: "peekaping",
		RedisPassword: "secret",
		RedisDB:       2,
	}

	require.Equal(t, "peekaping", cfg.RedisUsername)
}
