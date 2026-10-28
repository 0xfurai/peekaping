package migrations

import (
	"context"
	"database/sql"
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

const tagNameIndex = "idx_tags_workspace_name"

func init() {
	Migrations.MustRegister(uniqueTagNamesPerWorkspace, uniqueTagNamesPerInstall)
}

func uniqueTagNamesPerWorkspace(ctx context.Context, db *bun.DB) error {
	if db.Dialect().Name() == dialect.SQLite {
		return rebuildSQLiteTags(ctx, db, "", false, func(definition string) string {
			definition = tagNameColumnUnique.ReplaceAllString(definition, "${1}")
			return tagNameTableUnique.ReplaceAllString(definition, "")
		}, fmt.Sprintf(`CREATE UNIQUE INDEX IF NOT EXISTS "%s" ON tags (workspace_id, name)`, tagNameIndex))
	}
	return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// The constraint is looked up and not named: PostgreSQL calls the one of a column's UNIQUE
		// tags_name_key, but an install whose table was made another way may call it anything.
		rules, err := postgresTagNameRules(ctx, tx)
		if err != nil {
			return err
		}
		for _, rule := range rules {
			statement, name := "DROP INDEX ?", rule.Index
			if rule.Constraint.Valid {
				statement, name = "ALTER TABLE tags DROP CONSTRAINT ?", rule.Constraint.String
			}
			if _, err := tx.ExecContext(ctx, statement, bun.Ident(name)); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, "CREATE UNIQUE INDEX IF NOT EXISTS ? ON tags (workspace_id, name)", bun.Ident(tagNameIndex))
		return err
	})
}

func uniqueTagNamesPerInstall(ctx context.Context, db *bun.DB) error {
	if db.Dialect().Name() == dialect.SQLite {
		return downTagNames(rebuildSQLiteTags(ctx, db, fmt.Sprintf(`DROP INDEX IF EXISTS "%s"`, tagNameIndex), true, func(definition string) string {
			return tagNameNotUnique.ReplaceAllString(definition, "${1} UNIQUE${2}")
		}, ""))
	}
	return downTagNames(db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx, "DROP INDEX IF EXISTS ?", bun.Ident(tagNameIndex)); err != nil {
			return err
		}
		rules, err := postgresTagNameRules(ctx, tx)
		if err != nil {
			return err
		}
		if len(rules) > 0 {
			return nil
		}
		_, err = tx.ExecContext(ctx, "ALTER TABLE tags ADD CONSTRAINT tags_name_key UNIQUE (name)")
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

// postgresTagNameRule is a unique index on tags(name) alone, and the constraint that owns it when
// there is one
type postgresTagNameRule struct {
	Index      string         `bun:"index_name"`
	Constraint sql.NullString `bun:"constraint_name"`
}

// postgresTagNameRules finds the unique indexes of the tags table that cover the name column alone,
// which are the install-wide rule. The one on (workspace_id, name) isn't among them.
func postgresTagNameRules(ctx context.Context, tx bun.Tx) ([]postgresTagNameRule, error) {
	var rules []postgresTagNameRule
	err := tx.NewRaw(`SELECT i.relname AS index_name, c.conname AS constraint_name
		FROM pg_index x
		JOIN pg_class i ON i.oid = x.indexrelid
		LEFT JOIN pg_constraint c ON c.conindid = x.indexrelid AND c.conrelid = x.indrelid AND c.contype = 'u'
		WHERE x.indrelid = 'tags'::regclass
			AND x.indisunique AND NOT x.indisprimary
			AND x.indnatts = 1 AND x.indpred IS NULL AND x.indexprs IS NULL
			AND x.indkey[0] = (SELECT attnum FROM pg_attribute WHERE attrelid = 'tags'::regclass AND attname = 'name')`).Scan(ctx, &rules)
	return rules, err
}

var (
	// tagNameColumnUnique finds UNIQUE in the definition of the name column, however its type and
	// the rest of its constraints are written, and keeps what comes before it
	tagNameColumnUnique = regexp.MustCompile(`(?i)(\bname["\x60\]]?\s[^,]*?)\s+UNIQUE\b`)
	// tagNameTableUnique finds a table constraint UNIQUE (name), with its name when it has one
	tagNameTableUnique = regexp.MustCompile(`(?i),\s*(?:CONSTRAINT\s+(?:"[^"]+"|\S+)\s+)?UNIQUE\s*\(\s*["\x60]?name["\x60]?\s*\)`)
	// tagNameNotUnique finds the name column, with its type, where UNIQUE is to be put back
	tagNameNotUnique = regexp.MustCompile(`(?i)(\bname["\x60\]]?\s+\w+(?:\(\d+\))?)(\s+NOT\s+NULL)`)
)

// sqliteTagNameUnique reports whether the tags table has a unique index on the name column alone:
// the autoindex of a column's UNIQUE or of a table constraint UNIQUE (name), or an index made by
// CREATE UNIQUE INDEX. That is the install-wide rule, whatever way the table's definition spells it.
func sqliteTagNameUnique(ctx context.Context, tx bun.Tx) (bool, error) {
	var indexes []string
	rows, err := tx.QueryContext(ctx, `SELECT name FROM pragma_index_list('tags') WHERE "unique" = 1 AND partial = 0`)
	if err != nil {
		return false, err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return false, err
		}
		indexes = append(indexes, name)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return false, err
	}
	for _, index := range indexes {
		var columns []string
		info, err := tx.QueryContext(ctx, "SELECT name FROM pragma_index_info(?) ORDER BY seqno", index)
		if err != nil {
			return false, err
		}
		for info.Next() {
			var column sql.NullString
			if err := info.Scan(&column); err != nil {
				_ = info.Close()
				return false, err
			}
			columns = append(columns, column.String)
		}
		if err := errors.Join(info.Err(), info.Close()); err != nil {
			return false, err
		}
		if len(columns) == 1 && columns[0] == "name" {
			return true, nil
		}
	}
	return false, nil
}

// rebuildSQLiteTags makes the tags table have, or not have, a unique index on the name column
// alone, as wantUnique says. SQLite can't drop a column's UNIQUE, so when the table isn't as wanted
// it is rebuilt with the definition that change returns, keeping its rows, indexes and the keys
// that point at it. The schema decides, not the text of the definition, and a rebuild that leaves
// the table as it was is an error: a rule the migration can't remove (an index made by hand, a
// definition written in a way change doesn't know) must not pass unseen. before and afterwards,
// when they aren't empty, are run first and last in the same transaction.
//
// The rows are copied into the new table before the old one is dropped, so a unique violation
// (down, with a name two workspaces share) stops the transaction with the old table in place.
func rebuildSQLiteTags(ctx context.Context, db *bun.DB, before string, wantUnique bool, change func(definition string) string, afterwards string) (err error) {
	conn, restore, err := foreignKeysOff(ctx, db)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, restore()) }()

	return conn.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if before != "" {
			if _, err := tx.ExecContext(ctx, before); err != nil {
				return err
			}
		}
		unique, err := sqliteTagNameUnique(ctx, tx)
		if err != nil {
			return err
		}
		if unique != wantUnique {
			var definition string
			if err := tx.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'tags'").Scan(&definition); err != nil {
				return err
			}
			if err := rebuildSQLiteTable(ctx, tx, "tags", change(definition)); err != nil {
				return err
			}
			if unique, err = sqliteTagNameUnique(ctx, tx); err != nil {
				return err
			}
			if unique != wantUnique {
				if wantUnique {
					return fmt.Errorf("the migration can't add a unique rule on the name of the tags table: %s", definition)
				}
				return fmt.Errorf("the tags table has a unique rule on name alone that the migration can't remove: %s", definition)
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
