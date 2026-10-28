package migrations

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
)

// Makes a tag name unique within a workspace and not across the install (PP-149, docs/adr/0005
// section 6). The column constraint `name UNIQUE` goes, and a unique index on (workspace_id, name)
// takes its place. The rule is as case sensitive as it was.
//
// Going down puts the install-wide rule back. Two workspaces that share a tag name stop it, with
// nothing changed: one of the tags has to be renamed first.

const (
	tagNameIndex = "idx_tags_workspace_name"
	// tagNameConstraint is what PostgreSQL calls the constraint of the column's UNIQUE
	tagNameConstraint = "tags_name_key"
)

func init() {
	Migrations.MustRegister(uniqueTagNamesPerWorkspace, uniqueTagNamesPerInstall)
}

func uniqueTagNamesPerWorkspace(ctx context.Context, db *bun.DB) error {
	if db.Dialect().Name() == dialect.SQLite {
		return rebuildSQLiteTags(ctx, db, "", func(definition string) (string, bool) {
			changed := tagNameUnique.ReplaceAllString(definition, "${1}${2}")
			return changed, changed != definition
		}, fmt.Sprintf(`CREATE UNIQUE INDEX IF NOT EXISTS "%s" ON tags (workspace_id, name)`, tagNameIndex))
	}
	return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx, "ALTER TABLE tags DROP CONSTRAINT IF EXISTS ?", bun.Ident(tagNameConstraint)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "CREATE UNIQUE INDEX IF NOT EXISTS ? ON tags (workspace_id, name)", bun.Ident(tagNameIndex))
		return err
	})
}

func uniqueTagNamesPerInstall(ctx context.Context, db *bun.DB) error {
	if db.Dialect().Name() == dialect.SQLite {
		return downTagNames(rebuildSQLiteTags(ctx, db, fmt.Sprintf(`DROP INDEX IF EXISTS "%s"`, tagNameIndex), func(definition string) (string, bool) {
			changed := tagNameNotUnique.ReplaceAllString(definition, "${1} UNIQUE${2}")
			return changed, changed != definition
		}, ""))
	}
	return downTagNames(db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx, "DROP INDEX IF EXISTS ?", bun.Ident(tagNameIndex)); err != nil {
			return err
		}
		var constraints int
		if err := tx.NewRaw("SELECT COUNT(*) FROM pg_constraint WHERE conrelid = 'tags'::regclass AND conname = ?", tagNameConstraint).Scan(ctx, &constraints); err != nil {
			return err
		}
		if constraints > 0 {
			return nil
		}
		_, err := tx.ExecContext(ctx, "ALTER TABLE tags ADD CONSTRAINT ? UNIQUE (name)", bun.Ident(tagNameConstraint))
		return err
	}))
}

// downTagNames explains a failure of the way down: it is a name that two workspaces share
func downTagNames(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("make tag names unique across the install again (rename a tag that two workspaces share, and nothing was changed): %w", err)
}

var (
	// tagNameUnique finds the column `name VARCHAR(100) UNIQUE NOT NULL` of the tags table
	tagNameUnique = regexp.MustCompile(`(?i)(\bname\s+VARCHAR\(100\))\s+UNIQUE(\s+NOT\s+NULL)`)
	// tagNameNotUnique finds the same column without UNIQUE
	tagNameNotUnique = regexp.MustCompile(`(?i)(\bname\s+VARCHAR\(100\))(\s+NOT\s+NULL)`)
)

// rebuildSQLiteTags rebuilds the tags table with the definition that change returns, keeping its
// rows, indexes and the keys that point at it, since SQLite can't drop a column's UNIQUE. A
// definition that change leaves as it is means the table is already as wanted, and nothing is
// rebuilt. before and afterwards, when they aren't empty, are run first and last in the same
// transaction.
//
// The rows are copied into the new table before the old one is dropped, so a unique violation
// (down, with a name two workspaces share) stops the transaction with the old table in place.
func rebuildSQLiteTags(ctx context.Context, db *bun.DB, before string, change func(definition string) (string, bool), afterwards string) (err error) {
	conn, restore, err := foreignKeysOff(ctx, db)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, restore()) }()

	return conn.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var definition string
		if err := tx.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'tags'").Scan(&definition); err != nil {
			return err
		}
		if before != "" {
			if _, err := tx.ExecContext(ctx, before); err != nil {
				return err
			}
		}
		rebuilt, needed := change(definition)
		if needed {
			if err := rebuildSQLiteTable(ctx, tx, "tags", rebuilt); err != nil {
				return err
			}
		}
		if afterwards != "" {
			if _, err := tx.ExecContext(ctx, afterwards); err != nil {
				return err
			}
		}
		return checkForeignKeys(ctx, tx, "tags")
	})
}

// rebuildSQLiteTable replaces the table with one made from the definition, which must be the
// table's own with some change to it, and copies the rows and re-creates the indexes. It runs in
// the caller's transaction, with foreign keys off on its connection (see foreignKeysOff).
func rebuildSQLiteTable(ctx context.Context, tx bun.Tx, table, definition string) error {
	var indexes []string
	rows, err := tx.QueryContext(ctx, "SELECT sql FROM sqlite_master WHERE type = 'index' AND tbl_name = ? AND sql IS NOT NULL", table)
	if err != nil {
		return err
	}
	for rows.Next() {
		var index string
		if err := rows.Scan(&index); err != nil {
			_ = rows.Close()
			return err
		}
		// an index of the table that the change makes pointless is made again by the caller
		indexes = append(indexes, index)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}

	var columns []string
	info, err := tx.QueryContext(ctx, "SELECT name FROM pragma_table_info(?) ORDER BY cid", table)
	if err != nil {
		return err
	}
	for info.Next() {
		var name string
		if err := info.Scan(&name); err != nil {
			_ = info.Close()
			return err
		}
		columns = append(columns, `"`+name+`"`)
	}
	if err := errors.Join(info.Err(), info.Close()); err != nil {
		return err
	}

	rebuilt := table + "__rebuilt"
	definition = createTableName.ReplaceAllString(definition, `CREATE TABLE "`+rebuilt+`"`)
	list := strings.Join(columns, ", ")
	for _, statement := range []string{
		definition,
		fmt.Sprintf(`INSERT INTO "%s" (%s) SELECT %s FROM "%s"`, rebuilt, list, list, table),
		fmt.Sprintf(`DROP TABLE "%s"`, table),
		fmt.Sprintf(`ALTER TABLE "%s" RENAME TO "%s"`, rebuilt, table),
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	for _, index := range indexes {
		if _, err := tx.ExecContext(ctx, index); err != nil {
			return err
		}
	}
	return nil
}
