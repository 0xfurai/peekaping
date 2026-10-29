package monitor_tls_info_test

import (
	"context"
	"path/filepath"
	"peekaping/internal/modules/workspace/workspacetest"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/shared"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

// newService returns a TLS info service on an empty SQLite database, and the
// ID of a monitor in it.
func newService(t *testing.T) (monitor_tls_info.Service, string) {
	t.Helper()
	ctx := context.Background()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "tls.db")}, zap.NewNop().Sugar())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	migrator := migrate.NewMigrator(db, migrations.Migrations)
	require.NoError(t, migrator.Init(ctx))
	_, err = migrator.Migrate(ctx)
	require.NoError(t, err)
	monitorID := uuid.NewString()
	workspacetest.OldestOrAdd(t, db)
	_, err = db.Exec(`INSERT INTO monitors (id, workspace_id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, (SELECT id FROM workspaces ORDER BY created_at, id LIMIT 1), 'http', 'Monitor', 60, 10, 1, 10, 0)`, monitorID)
	require.NoError(t, err)
	return monitor_tls_info.NewService(monitor_tls_info.NewSQLRepository(db), zap.NewNop().Sugar()), monitorID
}

// tlsInfo is a server certificate signed by an intermediate.
func tlsInfo(subject string, daysRemaining int) *shared.TLSInfo {
	validFrom := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return &shared.TLSInfo{
		Valid: true,
		CertInfo: &shared.CertificateInfo{
			Subject:        "CN=" + subject,
			Issuer:         "CN=Example Intermediate",
			Fingerprint256: "AA:BB:" + subject,
			ValidFrom:      validFrom,
			ValidTo:        validFrom.AddDate(0, 3, 0),
			DaysRemaining:  daysRemaining,
			CertType:       "server",
			ValidFor:       []string{subject, "www." + subject},
			IssuerCertificate: &shared.CertificateInfo{
				Subject:       "CN=Example Intermediate",
				Issuer:        "CN=Example Root",
				ValidFrom:     validFrom,
				ValidTo:       validFrom.AddDate(5, 0, 0),
				DaysRemaining: 1500,
				CertType:      "intermediate CA",
			},
		},
	}
}

func TestService_ReadsBackTheTLSInfoItStored(t *testing.T) {
	svc, monitorID := newService(t)
	ctx := context.Background()
	stored := tlsInfo("example.com", 30)

	require.NoError(t, svc.StoreTLSInfoObject(ctx, monitorID, stored))

	var read shared.TLSInfo
	require.NoError(t, svc.GetTLSInfoObject(ctx, monitorID, &read))
	assert.Equal(t, stored, &read)
	got, err := svc.GetTLSInfo(ctx, monitorID)
	require.NoError(t, err)
	assert.Equal(t, stored, got)
}

func TestService_ReplacesStoredTLSInfo(t *testing.T) {
	svc, monitorID := newService(t)
	ctx := context.Background()

	require.NoError(t, svc.StoreTLSInfoObject(ctx, monitorID, tlsInfo("old.example.com", 30)))
	require.NoError(t, svc.StoreTLSInfoObject(ctx, monitorID, tlsInfo("new.example.com", 90)))

	got, err := svc.GetTLSInfo(ctx, monitorID)
	require.NoError(t, err)
	assert.Equal(t, tlsInfo("new.example.com", 90), got)
}

func TestService_FindsNothingForAMonitorWithoutTLSInfo(t *testing.T) {
	svc, monitorID := newService(t)
	ctx := context.Background()
	require.NoError(t, svc.StoreTLSInfoObject(ctx, monitorID, tlsInfo("example.com", 30)))
	require.NoError(t, svc.DeleteTLSInfo(ctx, monitorID))

	for name, id := range map[string]string{"deleted": monitorID, "never stored": uuid.NewString()} {
		t.Run(name, func(t *testing.T) {
			got, err := svc.GetTLSInfo(ctx, id)
			require.NoError(t, err)
			assert.Nil(t, got)

			read := shared.TLSInfo{Valid: true}
			require.NoError(t, svc.GetTLSInfoObject(ctx, id, &read))
			assert.Equal(t, shared.TLSInfo{Valid: true}, read, "the target is left as it was")
		})
	}
}

// A monitor can be deleted while a check of it is still being handled. The TLS info has nothing to
// belong to then, and is dropped without a failure.
func TestService_StoringTLSInfoForADeletedMonitorIsSkipped(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	deleted := uuid.NewString()

	require.NoError(t, svc.StoreTLSInfoObject(ctx, deleted, tlsInfo("example.com", 30)))

	read, err := svc.GetTLSInfo(ctx, deleted)
	require.NoError(t, err)
	assert.Nil(t, read)
}
