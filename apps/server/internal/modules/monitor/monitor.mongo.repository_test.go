package monitor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// MongoDB has no probes (ADR 0001): every monitor it holds runs on the local probe
func TestMongoRepository_MonitorsRunOnTheLocalProbe(t *testing.T) {
	monitor := toDomainModel(&mongoModel{ID: primitive.NewObjectID(), Type: "http", Name: "Checkout", Active: true})

	assert.Equal(t, []string{"local"}, monitor.ProbeIDs)
}

func TestMongoRepository_NoOtherProbeRunsAnyMonitor(t *testing.T) {
	// No collection: a remote probe's question is answered without asking the database
	repo := &MonitorRepositoryImpl{}

	found, err := repo.FindActiveByProbe(context.Background(), "frankfurt", 0, 10)

	require.NoError(t, err)
	assert.NotNil(t, found)
	assert.Empty(t, found)
}
