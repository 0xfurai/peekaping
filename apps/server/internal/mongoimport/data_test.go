package mongoimport_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/status_page"
	"peekaping/internal/modules/tag"
	"peekaping/internal/mongoimport"
	"peekaping/internal/mongoimport/mongoimporttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
	"go.mongodb.org/mongo-driver/bson"
)

// renamed is the nth value of a unique column that was cut to width characters, and collided: its
// tail is replaced by ~n.
func renamed(value string, width, n int) string {
	suffix := "~" + string(rune('0'+n))
	return value[:width-len(suffix)] + suffix
}

// PostgreSQL cuts a value to the width of its column, and SQLite keeps it whole. Values that differ only
// after the width collide in a unique column once cut: every row is still kept, with the later values
// renamed to stay unique, and the report counts them.
func TestImportKeepsEveryRowWhenCutValuesCollide(t *testing.T) {
	in := mongoimporttest.Empty(t)
	ctx := context.Background()
	now := time.Now().UTC()

	monitorDocument := func(name, config string) bson.D {
		return bson.D{
			{Key: "type", Value: "http"}, {Key: "name", Value: name}, {Key: "interval", Value: 60}, {Key: "timeout", Value: 16},
			{Key: "max_retries", Value: 0}, {Key: "retry_interval", Value: 60}, {Key: "resend_interval", Value: 0}, {Key: "active", Value: true},
			{Key: "status", Value: 1}, {Key: "config", Value: config}, {Key: "push_token", Value: ""},
			{Key: "created_at", Value: now}, {Key: "updated_at", Value: now},
		}
	}
	wideMonitor := in.Insert(t, "monitor", monitorDocument(mongoimporttest.LongName, `{}`))

	// Values that differ only after the width of their column: PostgreSQL keeps the first 100
	// characters of a tag name, 255 of a slug, an email address and a setting key
	tagNames := []string{strings.Repeat("a", 100) + "1", strings.Repeat("a", 100) + "2", strings.Repeat("a", 100) + "3"}
	for _, name := range tagNames {
		in.Insert(t, "tags", bson.D{{Key: "name", Value: name}, {Key: "color", Value: "#3B82F6"}, {Key: "created_at", Value: now}, {Key: "updated_at", Value: now}})
	}
	slugs := []string{strings.Repeat("s", 255) + "1", strings.Repeat("s", 255) + "2"}
	for _, slug := range slugs {
		in.Insert(t, "status_pages", bson.D{
			{Key: "slug", Value: slug}, {Key: "title", Value: "Status"}, {Key: "theme", Value: "light"}, {Key: "created_at", Value: now}, {Key: "updated_at", Value: now},
		})
	}
	emails := []string{strings.Repeat("e", 255) + "1@example.test", strings.Repeat("e", 255) + "2@example.test"}
	for _, email := range emails {
		in.Insert(t, "users", bson.D{{Key: "email", Value: email}, {Key: "password", Value: "hash"}, {Key: "active", Value: true}})
	}
	keys := []string{strings.Repeat("k", 255) + "1", strings.Repeat("k", 255) + "2"}
	for _, key := range keys {
		in.Insert(t, "setting", bson.D{{Key: "key", Value: key}, {Key: "value", Value: "v"}, {Key: "type", Value: "string"}, {Key: "updated_at", Value: now}})
	}

	forEachTarget(t, func(t *testing.T, db *bun.DB) {
		report, err := mongoimport.Run(ctx, db, in.Options())
		require.NoError(t, err)
		postgres := db.Dialect().Name() == dialect.PG

		monitors := monitor.NewSQLRepository(db)
		// A name wider than the column is cut on PostgreSQL only
		wide, err := monitors.FindByID(ctx, importedWorkspace(t, db), uuidOf(t, wideMonitor.Hex()))
		require.NoError(t, err)
		assert.Equal(t, cut(db, mongoimporttest.LongName, 150), wide.Name)
		if postgres {
			assert.Equal(t, map[string]int{"name cut to 150 characters": 1}, collectionReport(t, report, "monitor").Fixed)
		} else {
			assert.Empty(t, collectionReport(t, report, "monitor").Fixed, "SQLite doesn't limit the width of a name")
		}

		// Names that collide once cut are all kept, the later ones renamed to stay unique
		tags := tag.NewSQLRepository(db)
		for i, name := range tagNames {
			want := name
			if postgres {
				want = strings.Repeat("a", 100)
				if i > 0 {
					want = renamed(want, 100, i+1)
				}
			}
			got, err := tags.FindByName(ctx, want)
			require.NoError(t, err)
			assert.NotNil(t, got, "no tag named %q", want)
		}
		pages := status_page.NewSQLRepository(db)
		for i, slug := range slugs {
			want := slug
			if postgres {
				want = strings.Repeat("s", 255)
				if i > 0 {
					want = renamed(want, 255, i+1)
				}
			}
			got, err := pages.FindBySlug(ctx, want)
			require.NoError(t, err)
			assert.NotNil(t, got, "no status page with the slug %q", want)
		}
		users := auth.NewSQLRepository(db)
		for i, email := range emails {
			want := email
			if postgres {
				want = strings.Repeat("e", 255)
				if i > 0 {
					want = renamed(want, 255, i+1)
				}
			}
			got, err := users.FindByEmail(ctx, want)
			require.NoError(t, err)
			assert.NotNil(t, got, "no user with the email %q", want)
		}
		settings := setting.NewSQLRepository(db)
		for i, key := range keys {
			want := key
			if postgres {
				want = strings.Repeat("k", 255)
				if i > 0 {
					want = renamed(want, 255, i+1)
				}
			}
			got, err := settings.GetByKey(ctx, want)
			require.NoError(t, err)
			assert.NotNil(t, got, "no setting with the key %q", want)
		}

		tagReport := collectionReport(t, report, "tags")
		assert.Equal(t, 3, tagReport.Read)
		assert.Equal(t, 3, tagReport.Copied)
		assert.Zero(t, tagReport.SkippedTotal())
		if postgres {
			assert.Equal(t, map[string]int{
				"name cut to 100 characters":                                    3,
				"name renamed to stay unique after being cut to 100 characters": 2,
			}, tagReport.Fixed)
			assert.Equal(t, 1, collectionReport(t, report, "status_pages").Fixed["slug renamed to stay unique after being cut to 255 characters"])
		} else {
			assert.Empty(t, tagReport.Fixed, "SQLite doesn't limit the width of a name")
			assert.Empty(t, collectionReport(t, report, "status_pages").Fixed)
		}
	})
}
