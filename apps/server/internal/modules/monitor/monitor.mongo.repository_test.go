package monitor

import (
	"context"
	"testing"
	"time"

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

// The service derives the push token from the config when the request does not send one,
// so both MongoDB update builders have to carry it into the $set map. Without it the
// stored token goes stale and heartbeats keep arriving under the old one.
func TestBuildSetMapFromModelForUpdate_CarriesPushToken(t *testing.T) {
	set := buildSetMapFromModelForUpdate(
		&Model{Type: "push", Config: `{"pushToken":"token-from-config"}`, PushToken: "token-from-config"},
		time.Now().UTC(),
		false,
		primitive.NilObjectID,
	)

	assert.Equal(t, "token-from-config", set["push_token"])
}

func TestBuildSetMapFromModelForUpdate_KeepsEmptyPushToken(t *testing.T) {
	set := buildSetMapFromModelForUpdate(
		&Model{Type: "http"},
		time.Now().UTC(),
		false,
		primitive.NilObjectID,
	)

	// Written rather than omitted, so a full update clears it the way the SQL writer does.
	assert.Equal(t, "", set["push_token"])
}

func TestBuildSetMapFromUpdateModel_CarriesPushTokenWhenSet(t *testing.T) {
	token := "token-from-config"

	set, err := buildSetMapFromUpdateModel(&mongoUpdateModel{PushToken: &token}, false, nil)

	require.NoError(t, err)
	assert.Equal(t, "token-from-config", set["push_token"])
}

func TestBuildSetMapFromUpdateModel_LeavesPushTokenAloneWhenNil(t *testing.T) {
	name := "Renamed"

	set, err := buildSetMapFromUpdateModel(&mongoUpdateModel{Name: &name}, false, nil)

	require.NoError(t, err)
	_, ok := set["push_token"]
	assert.False(t, ok, "a PATCH that does not send push_token must not write the column")
}
