package infra

import (
	"testing"

	"peekaping/internal/config"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestProvideRedisClientPropagatesUsername(t *testing.T) {
	client, err := ProvideRedisClient(&config.Config{
		RedisHost:     "redis",
		RedisPort:     "6379",
		RedisUsername: "peekaping",
		RedisPassword: "secret",
		RedisDB:       2,
	}, zap.NewNop().Sugar())
	require.NoError(t, err)
	defer client.Close()

	opts := client.Options()
	require.Equal(t, "redis:6379", opts.Addr)
	require.Equal(t, "peekaping", opts.Username)
	require.Equal(t, "secret", opts.Password)
	require.Equal(t, 2, opts.DB)
}

func TestProvideRedisClientKeepsPasswordOnlyMode(t *testing.T) {
	client, err := ProvideRedisClient(&config.Config{
		RedisHost:     "redis",
		RedisPort:     "6379",
		RedisPassword: "secret",
		RedisDB:       2,
	}, zap.NewNop().Sugar())
	require.NoError(t, err)
	defer client.Close()

	opts := client.Options()
	require.Equal(t, "", opts.Username)
	require.Equal(t, "secret", opts.Password)
	require.Equal(t, 2, opts.DB)
}
