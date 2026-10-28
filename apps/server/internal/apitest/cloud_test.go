package apitest

import (
	"context"
	"os"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/smtpclient/smtptest"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// cloudEnv switches the whole package to the settings of Peekaping Cloud when it is set to 1. CI
// runs the package twice, once with the self-host defaults and once with this, so that what a
// guard of the cloud adds can't break an API route unnoticed (PP-153, docs/adr/0005).
const cloudEnv = "PEE_TEST_CLOUD"

// cloudConfig applies the cloud's settings to the config of an app under test, before the test's
// own options, so that a test that needs one of the self-host defaults sets it itself. The guards
// of the cloud add to it as they land, each in its own ticket:
//
//   - open signup (PP-155), which needs a sender for account emails: a fake SMTP server
//   - monitor types that are disabled (PP-157): docker
//   - the private address block (PP-156): BLOCK_PRIVATE_TARGETS
//   - rate limits (PP-159)
//
// registerAdmin verifies the admin's email in a cloud run, since an open signup's workspace is
// suspended until then and the admin's tests expect their monitors to run.
func cloudConfig(t testing.TB, cfg *config.Config) {
	withAccountSMTP(smtptest.Start(t))(cfg)
	cfg.SignupMode = config.SignupModeOpen
	cfg.DisabledMonitorTypes = "docker"
	cfg.BlockPrivateTargets = true
}

// cloudRun reports whether this run uses the cloud's settings
func cloudRun() bool { return os.Getenv(cloudEnv) == "1" }

// withAllMonitorTypes turns every monitor type on, as a self-hosted install has them. A test that
// makes a Docker monitor through the API sets it, since the cloud turns that type off.
func withAllMonitorTypes(cfg *config.Config) { cfg.DisabledMonitorTypes = "" }

// selfHosted puts back the settings of a self-hosted install that cloudConfig changes: invite signup,
// and no sender for account emails. A test that is about those sets it.
func selfHosted(cfg *config.Config) {
	cfg.SignupMode = config.SignupModeInvite
	cfg.AccountSMTPHost, cfg.AccountSMTPUser, cfg.AccountSMTPPassword, cfg.AccountSMTPFrom = "", "", "", ""
}

// verifyEmailDirectly verifies the user's email and lifts the suspension of the workspaces they own,
// as following the link of the verification email does, for the tests that are about something else
// than verification
func (a *app) verifyEmailDirectly(t testing.TB, email string) {
	t.Helper()
	var id string
	require.NoError(t, a.container.Invoke(func(db *bun.DB, accounts auth.AccountRepository, workspaces workspace.Repository) error {
		ctx := context.Background()
		if err := db.NewSelect().Table("users").Column("id").Where("email = ?", email).Scan(ctx, &id); err != nil {
			return err
		}
		if err := accounts.MarkEmailVerified(ctx, id, time.Now().UTC()); err != nil {
			return err
		}
		_, err := workspaces.LiftUnverifiedOwnedBy(ctx, id)
		return err
	}))
}
