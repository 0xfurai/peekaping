// Package mongoimport copies a MongoDB storage backend's data into a SQLite or
// PostgreSQL database that holds no data of its own. It is what "bun db
// import-mongo" runs.
//
// The MongoDB repositories can't do the copy: the SQL repositories give every
// row a new ID and the current time, and several MongoDB collections have no
// listing. So the import reads the MongoDB collections as raw documents and
// writes the SQL tables with explicit IDs and timestamps, in one transaction.
//
// Every ObjectID becomes the UUID that legacyid.FromObjectID maps it to, and
// every reference to it is mapped the same way.
package mongoimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"peekaping/internal/legacyid"
	"peekaping/internal/modules/workspace"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/mongo/driver/connstring"
)

// DefaultDatabase is the MongoDB database name that the Peekaping images use when DB_NAME isn't set.
const DefaultDatabase = "peekaping"

// Options say where to copy from.
type Options struct {
	// MongoURI is the connection string of the MongoDB server.
	MongoURI string
	// MongoDatabase is the database that holds the install. It defaults to the
	// database named in the URI, and then to DefaultDatabase.
	MongoDatabase string
	// Progress, if set, gets a line every time a large collection has been
	// copied a further stretch.
	Progress io.Writer
	// Migrate applies the SQL migrations to the target, once it is known to hold no
	// data of its own. The caller says how, so that the import takes the same migration
	// lock as "bun db migrate".
	Migrate func(ctx context.Context, db *bun.DB) error
}

// ErrTargetNotEmpty is returned when the SQL database already holds data of its own.
var ErrTargetNotEmpty = errors.New("the target database already holds data")

// dataTables are the tables that hold what a user made, in the order the import
// fills them. The target must have no row in any of them. Its settings table is
// not among them: a Peekaping's first start writes the default settings, the
// JWT signing secrets included, and nothing else, so a database that only
// started once has only those. The import replaces them with the install's.
var dataTables = []string{
	"users", "api_keys",
	"proxies", "monitors", "notification_channels", "tags", "maintenances", "status_pages",
	"monitor_notifications", "monitor_tags", "monitor_maintenances", "monitor_status_pages", "domain_status_page",
	"monitor_tls_info", "notification_sent_history",
	"heartbeats", "stat_minutely", "stat_hourly", "stat_daily",
}

// Run copies the MongoDB install named by opts into db, which must be a SQLite
// or PostgreSQL database that holds no data but the default settings of a first
// start, which the copy replaces. It applies the SQL migrations first. The copy
// is one transaction: if anything fails, db is left as it was, apart from the
// migrations. It returns what it did with every collection.
func Run(ctx context.Context, db *bun.DB, opts Options) (*Report, error) {
	if name := db.Dialect().Name(); name != dialect.SQLite && name != dialect.PG {
		return nil, fmt.Errorf("the target must be a SQLite or PostgreSQL database, not %s", name)
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(opts.MongoURI))
	if err != nil {
		return nil, fmt.Errorf("connect to MongoDB: %w", err)
	}
	defer client.Disconnect(context.Background()) //nolint:errcheck
	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("connect to MongoDB: %w", err)
	}
	database, err := resolveDatabase(opts)
	if err != nil {
		return nil, err
	}
	src := &source{db: client.Database(database)}
	if err := src.requirePeekaping(ctx); err != nil {
		return nil, err
	}

	// Look at the target before changing it: the migrations of a database that holds data are not ours to run
	if err := requireEmpty(ctx, db); err != nil {
		return nil, err
	}
	if opts.Migrate == nil {
		return nil, errors.New("mongoimport: Options.Migrate is required")
	}
	if err := opts.Migrate(ctx, db); err != nil {
		return nil, fmt.Errorf("apply the SQL migrations: %w", err)
	}

	report := &Report{}
	err = db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := requireEmpty(ctx, tx); err != nil {
			return err
		}
		return newCopier(tx, src, report, opts.Progress).copyAll(ctx)
	})
	if err != nil {
		return nil, err
	}
	return report, nil
}

func resolveDatabase(opts Options) (string, error) {
	if opts.MongoDatabase != "" {
		return opts.MongoDatabase, nil
	}
	parsed, err := connstring.Parse(opts.MongoURI)
	if err != nil {
		return "", fmt.Errorf("parse the MongoDB URI: %w", err)
	}
	if parsed.Database != "" {
		return parsed.Database, nil
	}
	return DefaultDatabase, nil
}

// peekapingCollections are the collections of a Peekaping install, except login_state.
var peekapingCollections = []string{
	"users", "setting", "api_keys", "proxies", "monitor", "notification_channel", "tags", "maintenance", "status_pages",
	"monitor_notification", "monitor_tags", "monitor_maintenance", "monitor_status_page", "domain_status_page",
	"monitor_tls_info", "notification_sent_history", "heartbeat", "stat_minutely", "stat_hourly", "stat_daily",
}

// requirePeekaping stops the import when the database has none of Peekaping's
// collections, which is what a mistyped database name looks like.
func (s *source) requirePeekaping(ctx context.Context) error {
	names, err := s.db.ListCollectionNames(ctx, struct{}{})
	if err != nil {
		return fmt.Errorf("list the collections of MongoDB database %q: %w", s.db.Name(), err)
	}
	have := map[string]bool{}
	for _, name := range names {
		have[name] = true
	}
	for _, name := range peekapingCollections {
		if have[name] {
			return nil
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return fmt.Errorf("MongoDB database %q has no collections: pass the name of the database the install uses with --mongo-db (DB_NAME)", s.db.Name())
	}
	return fmt.Errorf("MongoDB database %q has none of Peekaping's collections (it holds %s): pass the name of the database the install uses with --mongo-db (DB_NAME)", s.db.Name(), strings.Join(names, ", "))
}

// requireEmpty returns an ErrTargetNotEmpty error naming the tables that hold rows.
func requireEmpty(ctx context.Context, q bun.IDB) error {
	existing, err := existingTables(ctx, q)
	if err != nil {
		return fmt.Errorf("look at the target database: %w", err)
	}
	var filled []string
	for _, table := range dataTables {
		if !existing[table] {
			continue
		}
		var found bool
		if err := q.NewSelect().TableExpr("?", bun.Ident(table)).ColumnExpr("true").Limit(1).Scan(ctx, &found); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return fmt.Errorf("look at table %s: %w", table, err)
		}
		filled = append(filled, table)
	}
	if len(filled) > 0 {
		return fmt.Errorf("%w (%s have rows). Import into a database that no one has used yet. One that Peekaping only started on is fine, but not one with users, monitors or history of its own", ErrTargetNotEmpty, strings.Join(filled, ", "))
	}
	return nil
}

func existingTables(ctx context.Context, q bun.IDB) (map[string]bool, error) {
	query := "SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema()"
	if q.Dialect().Name() == dialect.SQLite {
		query = "SELECT name FROM sqlite_master WHERE type = 'table'"
	}
	var names []string
	if err := q.NewRaw(query).Scan(ctx, &names); err != nil {
		return nil, err
	}
	tables := make(map[string]bool, len(names))
	for _, name := range names {
		tables[name] = true
	}
	return tables, nil
}

// copier holds what the copy of each collection shares: the transaction, the
// report, and the ObjectIDs of the documents copied so far, which the documents
// that refer to them look up.
type copier struct {
	tx       bun.Tx
	src      *source
	report   *Report
	progress io.Writer
	// cutsText is whether text wider than its column has to be cut
	cutsText bool

	proxies, monitors, channels, tags, maintenances idSet
	statusPages                                     map[primitive.ObjectID]*statusPage
	statusPageOrder                                 []*statusPage
}

func newCopier(tx bun.Tx, src *source, report *Report, progress io.Writer) *copier {
	return &copier{
		tx: tx, src: src, report: report, progress: progress, cutsText: cutsText(tx),
		proxies: idSet{}, monitors: idSet{}, channels: idSet{}, tags: idSet{}, maintenances: idSet{},
		statusPages: map[primitive.ObjectID]*statusPage{},
	}
}

// uuidOf is the UUID that an ObjectID becomes.
func uuidOf(id primitive.ObjectID) string {
	mapped, _ := legacyid.FromObjectID(id.Hex())
	return mapped
}

// adoptWorkspace gives the copied install its workspace, as the upgrade to workspaces gives one
// to an install it finds: one workspace, with every imported user its owner and an instance
// admin. The migrations ran on an empty database, so they made none.
func (c *copier) adoptWorkspace(ctx context.Context) error {
	adopted, err := workspace.AdoptExistingInstall(ctx, c.tx)
	if err != nil {
		return fmt.Errorf("make the workspace: %w", err)
	}
	if adopted != nil {
		c.report.Workspace = &WorkspaceReport{Name: adopted.Workspace.Name, Owners: adopted.Owners}
	}
	return nil
}

// copyAll copies the collections in the order the foreign keys allow, each after
// the collections it refers to.
func (c *copier) copyAll(ctx context.Context) error {
	for _, step := range []func(context.Context) error{
		c.copyUsers, c.copySettings, c.copyAPIKeys,
		c.copyProxies, c.copyMonitors, c.copyChannels, c.copyTags, c.copyMaintenance, c.copyStatusPages,
		c.copyMonitorNotifications, c.copyMonitorTags, c.copyMonitorMaintenances, c.copyMonitorStatusPages, c.copyDomains,
		c.copyTLSInfo, c.copySentHistory, c.copyHeartbeats,
		func(ctx context.Context) error { return c.copyStats(ctx, "stat_minutely") },
		func(ctx context.Context) error { return c.copyStats(ctx, "stat_hourly") },
		func(ctx context.Context) error { return c.copyStats(ctx, "stat_daily") },
		c.adoptWorkspace,
	} {
		if err := step(ctx); err != nil {
			return err
		}
	}
	return nil
}
