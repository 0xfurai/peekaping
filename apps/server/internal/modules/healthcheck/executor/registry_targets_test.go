package executor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestExecutorRegistry_TargetsListsTheHostsACheckDials(t *testing.T) {
	registry := NewExecutorRegistry(zap.NewNop().Sugar())

	assert.Equal(t, []string{"db.example.com"}, registry.Targets("tcp", `{"host":"db.example.com","port":5432}`))
	assert.Equal(t, []string{"example.com"}, registry.Targets("http", `{"url":"https://example.com/health","method":"GET"}`))
}

func TestExecutorRegistry_TargetsListsNothingWhereACheckDialsNothing(t *testing.T) {
	registry := NewExecutorRegistry(zap.NewNop().Sugar())

	assert.Empty(t, registry.Targets("push", `{}`), "a push monitor runs no check of its own")
	assert.Empty(t, registry.Targets("no-such-type", `{}`))
	assert.Empty(t, registry.Targets("tcp", `not json`), "a config that can't be read")
	assert.Empty(t, registry.Targets("tcp", `{"host":"","port":1}`), "an empty host is no host")
}

func TestExecutorRegistry_TargetsOfANilRegistryIsEmpty(t *testing.T) {
	var registry *ExecutorRegistry

	assert.Empty(t, registry.Targets("tcp", `{"host":"example.com","port":1}`))
}
