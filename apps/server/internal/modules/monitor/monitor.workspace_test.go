package monitor

import (
	"context"
	"testing"

	"peekaping/internal/modules/workspace"
	"peekaping/internal/modules/workspace/workspacetest"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// The workspaces the tests' monitors belong to. forEachSQLBackend makes both.
const (
	testWorkspace  = workspacetest.ID
	otherWorkspace = workspacetest.Other
)

// upgradedWorkspace is the workspace an upgrade gave the monitors that were there before
func upgradedWorkspace(t *testing.T, db *bun.DB) workspace.ID {
	t.Helper()
	var id string
	require.NoError(t, db.NewSelect().Table("workspaces").Column("id").Order("created_at", "id").Limit(1).Scan(context.Background(), &id))
	return workspace.ID(id)
}
