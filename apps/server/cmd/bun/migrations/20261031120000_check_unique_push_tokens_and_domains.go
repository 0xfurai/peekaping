package migrations

import (
	"context"
	"errors"

	"github.com/uptrace/bun"
)

// Stops the upgrade to workspaces before it changes anything, when a push token or a custom
// domain is shared (PP-146, docs/adr/0005 section 6).
//
// Making push tokens and custom domains unique can't be done for a duplicate: the upgrade doesn't
// know which monitor or page should keep it. The migrations that make them unique check this
// themselves, but they come after the ones that add the workspace tables, adopt the install and
// rebuild the TLS info tables, and those are committed by then. So this one comes first (its name
// sorts before every migration of the upgrade) and only reads. One error lists every problem of
// both kinds, so the person fixing the previous version fixes them in one go.
//
// The rules are those of the migrations that follow, the same functions: the token a monitor has,
// and a domain normalized.

func init() {
	Migrations.MustRegister(checkUniquePushTokensAndDomains, func(ctx context.Context, db *bun.DB) error { return nil })
}

func checkUniquePushTokensAndDomains(ctx context.Context, db *bun.DB) error {
	var problems []error
	for _, check := range []func(context.Context, bun.IDB) error{checkPushTokensAreUnique, checkCustomDomainsAreUnique} {
		if err := check(ctx, db); err != nil {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}
