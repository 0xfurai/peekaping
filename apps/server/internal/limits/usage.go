package limits

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/dbtx"
	"peekaping/internal/installsettings"
	"peekaping/internal/modules/workspace/workspaceid"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
)

// What each limit counts. The services count with these when they enforce a limit, and Usage when
// it reports one, so the two agree. Each joins the transaction ctx carries.

// CountActiveMonitors is how many active monitors the workspace has: a paused one doesn't count
func CountActiveMonitors(ctx context.Context, db *bun.DB, workspaceID workspaceid.ID) (int, error) {
	return dbtx.DB(ctx, db).NewSelect().Table("monitors").
		Where("workspace_id = ?", string(workspaceID)).Where("active = ?", true).Count(ctx)
}

// CountStatusPages is how many status pages the workspace has
func CountStatusPages(ctx context.Context, db *bun.DB, workspaceID workspaceid.ID) (int, error) {
	return dbtx.DB(ctx, db).NewSelect().Table("status_pages").Where("workspace_id = ?", string(workspaceID)).Count(ctx)
}

// CountCustomDomains is how many custom domains the workspace's status pages have, together
func CountCustomDomains(ctx context.Context, db *bun.DB, workspaceID workspaceid.ID) (int, error) {
	return dbtx.DB(ctx, db).NewSelect().TableExpr("domain_status_page AS d").
		Join("JOIN status_pages AS sp ON sp.id = d.status_page_id").
		Where("sp.workspace_id = ?", string(workspaceID)).Count(ctx)
}

// CountMembers is how many members the workspace has, owners included
func CountMembers(ctx context.Context, db *bun.DB, workspaceID workspaceid.ID) (int, error) {
	return dbtx.DB(ctx, db).NewSelect().Table("workspace_members").Where("workspace_id = ?", string(workspaceID)).Count(ctx)
}

// CountPendingInvites is how many of the workspace's invites can still be accepted at now: neither
// used nor revoked, and not expired
func CountPendingInvites(ctx context.Context, db *bun.DB, workspaceID workspaceid.ID, now time.Time) (int, error) {
	return dbtx.DB(ctx, db).NewSelect().Table("workspace_invites").
		Where("workspace_id = ?", string(workspaceID)).
		Where("accepted_at IS NULL").Where("revoked_at IS NULL").Where("expires_at > ?", now.UTC()).Count(ctx)
}

// CountOwnedWorkspaces is how many workspaces the user is an owner of
func CountOwnedWorkspaces(ctx context.Context, db *bun.DB, userID string) (int, error) {
	return dbtx.DB(ctx, db).NewSelect().Table("workspace_members").
		Where("user_id = ?", userID).Where("role = ?", "owner").Count(ctx)
}

// MostProbesPerMonitor is the most probes any monitor of the workspace runs on, 0 when it has none.
// A monitor with no probe of its own runs on the local probe, which is one; a push monitor runs on
// none and doesn't count.
func MostProbesPerMonitor(ctx context.Context, db *bun.DB, workspaceID workspaceid.ID) (int, error) {
	var most sql.NullInt64
	err := dbtx.DB(ctx, db).NewSelect().TableExpr("monitors AS m").
		ColumnExpr("MAX((SELECT COUNT(*) FROM monitor_probes AS mp WHERE mp.monitor_id = m.id))").
		Where("m.workspace_id = ?", string(workspaceID)).Where("m.type <> ?", "push").
		Scan(ctx, &most)
	if err != nil || !most.Valid {
		return 0, err
	}
	return max(int(most.Int64), 1), nil
}

// LockUser holds the user until the transaction ctx carries ends, as LockWorkspace holds a
// workspace, for a limit that counts what a user has: the workspaces they own
func LockUser(ctx context.Context, db *bun.DB, userID string) error {
	if db.Dialect().Name() != dialect.PG {
		return nil
	}
	var locked []string
	return dbtx.DB(ctx, db).NewSelect().Table("users").Column("id").
		Where("id = ?", userID).For("UPDATE").Scan(ctx, &locked)
}

// retentionKey is the workspace setting that holds its data retention period
// (workspace.DataRetentionPeriodKey, which this package can't import)
const retentionKey = "KEEP_DATA_PERIOD_DAYS"

// DefaultRetentionDays is the data retention period of a workspace that has none stored
// (setting.DefaultDataRetentionDays, which this package can't import)
const DefaultRetentionDays = 365

// retentionDays is the workspace's data retention period, in days, as the cleanup job reads it
func retentionDays(ctx context.Context, db *bun.DB, workspaceID workspaceid.ID) (int, error) {
	var value string
	err := dbtx.DB(ctx, db).NewSelect().Table("workspace_settings").Column("value").
		Where("workspace_id = ?", string(workspaceID)).Where("key = ?", retentionKey).Scan(ctx, &value)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultRetentionDays, nil
	}
	if err != nil {
		return 0, err
	}
	days, err := strconv.Atoi(value)
	if err != nil {
		// A value stored before the settings API checked it, which the cleanup job leaves alone
		return 0, nil
	}
	return days, nil
}

// Use is one limit of a workspace as GET /workspace/limits shows it
type Use struct {
	Max    *int   `json:"max" nullable:"true" doc:"The limit, or null for none."`
	Used   int    `json:"used" doc:"What the workspace has of it. For min_check_interval, the shortest interval that applies; for retention_days, the workspace's data retention period; for probes_per_monitor, the most probes a monitor runs on; for owned_workspaces, how many the caller owns (0 for an API key)."`
	Source Origin `json:"source" enum:"workspace,install,none" doc:"Where the limit comes from: the workspace's own limits, the install's defaults, or none."`
}

// Uses are every limit of a workspace, with what it has of each
type Uses struct {
	Monitors         Use `json:"monitors"`
	MinCheckInterval Use `json:"min_check_interval"`
	StatusPages      Use `json:"status_pages"`
	CustomDomains    Use `json:"custom_domains"`
	Members          Use `json:"members" doc:"Members, with the pending invites counted."`
	OwnedWorkspaces  Use `json:"owned_workspaces" doc:"Counted per user, against the install's defaults alone."`
	ProbesPerMonitor Use `json:"probes_per_monitor"`
	RetentionDays    Use `json:"retention_days"`
}

// Report is what GET /workspace/limits answers
type Report struct {
	Source Origin `json:"source" enum:"workspace,install,none" doc:"Where the workspace's limits come from."`
	Limits Uses   `json:"limits"`
	// InstallLimitsName is the install's name for its default limits (PP-163)
	InstallLimitsName *string `json:"installLimitsName" nullable:"true" doc:"The install's name for its default limits, such as Free tier (WORKSPACE_LIMITS_NAME, or Admin → Settings), or null when it has none." example:"Free tier"`
}

// LimitsName is the install's name for its default limits, empty for none
// (installsettings.Resolver is one)
type LimitsName interface {
	WorkspaceLimitsName(ctx context.Context) string
}

// Usage reports a workspace's limits and what it has of each
type Usage struct {
	db     *bun.DB
	source Source
	// name is the install's name for its default limits, nil for none
	name LimitsName
	// minCheckInterval is the install's MIN_CHECK_INTERVAL
	minCheckInterval int
	now              func() time.Time
}

func NewUsage(db *bun.DB, source Source, cfg *config.Config, settings *installsettings.Resolver) *Usage {
	minimum := config.AbsoluteMinCheckInterval
	if cfg != nil {
		minimum = cfg.MinCheckIntervalSeconds()
	}
	u := &Usage{db: db, source: source, minCheckInterval: minimum, now: time.Now}
	if settings != nil {
		u.name = settings
	}
	return u
}

// Report is the workspace's limits, where they come from and what it has of each. userID is the
// caller, whose owned workspaces it counts, or "" for an API key.
func (u *Usage) Report(ctx context.Context, workspaceID workspaceid.ID, userID string) (*Report, error) {
	snapshot, err := u.source.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	l, origin := snapshot.For(workspaceID)
	// The workspaces a user owns are counted against the install's defaults: the one they would
	// create next has no limits of its own yet
	installOrigin := OriginNone
	if !snapshot.Defaults.IsZero() {
		installOrigin = OriginInstall
	}

	var used struct{ monitors, pages, domains, members, invites, owned, probes, retention int }
	for _, count := range []struct {
		into *int
		fn   func() (int, error)
	}{
		{&used.monitors, func() (int, error) { return CountActiveMonitors(ctx, u.db, workspaceID) }},
		{&used.pages, func() (int, error) { return CountStatusPages(ctx, u.db, workspaceID) }},
		{&used.domains, func() (int, error) { return CountCustomDomains(ctx, u.db, workspaceID) }},
		{&used.members, func() (int, error) { return CountMembers(ctx, u.db, workspaceID) }},
		{&used.invites, func() (int, error) { return CountPendingInvites(ctx, u.db, workspaceID, u.now()) }},
		{&used.probes, func() (int, error) { return MostProbesPerMonitor(ctx, u.db, workspaceID) }},
		{&used.retention, func() (int, error) { return retentionDays(ctx, u.db, workspaceID) }},
	} {
		if *count.into, err = count.fn(); err != nil {
			return nil, err
		}
	}
	if userID != "" {
		if used.owned, err = CountOwnedWorkspaces(ctx, u.db, userID); err != nil {
			return nil, err
		}
	}

	minimum := u.minCheckInterval
	if l.MinCheckInterval != nil {
		minimum = max(minimum, *l.MinCheckInterval)
	}
	var name *string
	if u.name != nil {
		if n := u.name.WorkspaceLimitsName(ctx); n != "" {
			name = &n
		}
	}
	of := func(limit *int, n int) Use { return Use{Max: limit, Used: n, Source: origin} }
	return &Report{Source: origin, InstallLimitsName: name, Limits: Uses{
		Monitors:         of(l.Monitors, used.monitors),
		MinCheckInterval: of(l.MinCheckInterval, minimum),
		StatusPages:      of(l.StatusPages, used.pages),
		CustomDomains:    of(l.CustomDomains, used.domains),
		Members:          of(l.Members, used.members+used.invites),
		OwnedWorkspaces:  Use{Max: snapshot.Defaults.OwnedWorkspaces, Used: used.owned, Source: installOrigin},
		ProbesPerMonitor: of(l.ProbesPerMonitor, used.probes),
		RetentionDays:    of(l.RetentionDays, used.retention),
	}}, nil
}
