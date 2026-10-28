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

// The migrations that tie a table to a workspace (PP-147 and the tickets after it, docs/adr/0005
// section 2) share these two helpers, so that every table gets the same column, key and index.

const workspaceColumn = "workspace_id"

// sqliteWorkspaceColumnDefinition is the column as SQLite stores it. The empty default is not a
// workspace, so the foreign key refuses an insert that names none.
const sqliteWorkspaceColumnDefinition = "workspace_id UUID NOT NULL DEFAULT '' REFERENCES workspaces(id) ON DELETE CASCADE"

func workspaceIndexName(table string) string { return "idx_" + table + "_workspace_id" }

// addWorkspaceColumn gives the table a workspace_id column: NOT NULL, a foreign key to
// workspaces(id) that deletes the rows with their workspace, and an index. Every row it already
// has goes to the install's oldest workspace, the one the upgrade made. A table with rows on an
// install with no workspace is an error, and so is any other failure: the table is left as it was.
//
// A table that has the column is left alone, so a re-run changes nothing.
//
// On PostgreSQL the steps are one transaction. SQLite can't add a NOT NULL column with a foreign
// key while it checks foreign keys, so the check is off on one connection while the column is added
// and the rows are filled, and the rows are checked against the key before the transaction commits.
func addWorkspaceColumn(ctx context.Context, db *bun.DB, table string) error {
	if db.Dialect().Name() == dialect.SQLite {
		return addWorkspaceColumnSQLite(ctx, db, table)
	}
	return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		has, err := hasWorkspaceColumn(ctx, tx, table)
		if err != nil || has {
			return err
		}
		if _, err := tx.ExecContext(ctx, "ALTER TABLE ? ADD COLUMN ? UUID REFERENCES workspaces(id) ON DELETE CASCADE", bun.Ident(table), bun.Ident(workspaceColumn)); err != nil {
			return err
		}
		if err := fillWorkspaceColumn(ctx, tx, table); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "ALTER TABLE ? ALTER COLUMN ? SET NOT NULL", bun.Ident(table), bun.Ident(workspaceColumn)); err != nil {
			return err
		}
		return createWorkspaceIndex(ctx, tx, table)
	})
}

func addWorkspaceColumnSQLite(ctx context.Context, db *bun.DB, table string) (err error) {
	conn, restore, err := foreignKeysOff(ctx, db)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, restore()) }()

	return conn.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		has, err := hasWorkspaceColumn(ctx, tx, table)
		if err != nil || has {
			return err
		}
		if _, err := tx.ExecContext(ctx, "ALTER TABLE ? ADD COLUMN "+sqliteWorkspaceColumnDefinition, bun.Ident(table)); err != nil {
			return err
		}
		if err := fillWorkspaceColumn(ctx, tx, table); err != nil {
			return err
		}
		if err := createWorkspaceIndex(ctx, tx, table); err != nil {
			return err
		}
		return checkForeignKeys(ctx, tx, table)
	})
}

// dropWorkspaceColumn is the way back from addWorkspaceColumn: the column and its index go, and
// the table is as it was. A table without the column is left alone.
//
// SQLite can't drop a column that has a foreign key, so it rebuilds the table without the column:
// the new table is the old one's definition less the column, which SQLite appended to it last.
func dropWorkspaceColumn(ctx context.Context, db *bun.DB, table string) error {
	if db.Dialect().Name() == dialect.SQLite {
		return dropWorkspaceColumnSQLite(ctx, db, table)
	}
	return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx, "DROP INDEX IF EXISTS ?", bun.Ident(workspaceIndexName(table))); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "ALTER TABLE ? DROP COLUMN IF EXISTS ?", bun.Ident(table), bun.Ident(workspaceColumn))
		return err
	})
}

var createTableName = regexp.MustCompile(`^CREATE TABLE\s+(?:"[^"]+"|\S+)`)

func dropWorkspaceColumnSQLite(ctx context.Context, db *bun.DB, table string) (err error) {
	conn, restore, err := foreignKeysOff(ctx, db)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, restore()) }()

	return conn.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		has, err := hasWorkspaceColumn(ctx, tx, table)
		if err != nil || !has {
			return err
		}
		var definition string
		if err := tx.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&definition); err != nil {
			return err
		}
		var indexes []string
		rows, err := tx.QueryContext(ctx, "SELECT sql FROM sqlite_master WHERE type = 'index' AND tbl_name = ? AND sql IS NOT NULL AND name <> ?", table, workspaceIndexName(table))
		if err != nil {
			return err
		}
		for rows.Next() {
			var index string
			if err := rows.Scan(&index); err != nil {
				_ = rows.Close()
				return err
			}
			indexes = append(indexes, index)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}

		var columns []string
		info, err := tx.QueryContext(ctx, "SELECT name FROM pragma_table_info(?) WHERE name <> ? ORDER BY cid", table, workspaceColumn)
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

		// SQLite writes an added column at the end of the definition, after a comma
		withColumn := definition
		stripped := strings.Replace(withColumn, ", "+sqliteWorkspaceColumnDefinition, "", 1)
		if stripped == withColumn {
			return fmt.Errorf("the definition of %s doesn't end with the %s column that addWorkspaceColumn added: %s", table, workspaceColumn, definition)
		}
		rebuilt := table + "__without_workspace"
		stripped = createTableName.ReplaceAllString(stripped, `CREATE TABLE "`+rebuilt+`"`)
		list := strings.Join(columns, ", ")
		for _, statement := range []string{
			stripped,
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
	})
}

// foreignKeysOff takes one connection and turns SQLite's foreign key check off on it. The check is
// a setting of the connection, and it can't change inside a transaction, so this comes before one.
// restore puts the check back as it was and gives the connection up.
func foreignKeysOff(ctx context.Context, db *bun.DB) (bun.Conn, func() error, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return bun.Conn{}, nil, err
	}
	var was int
	if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&was); err != nil {
		_ = conn.Close()
		return bun.Conn{}, nil, err
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		_ = conn.Close()
		return bun.Conn{}, nil, err
	}
	restore := func() error {
		_, err := conn.ExecContext(context.WithoutCancel(ctx), fmt.Sprintf("PRAGMA foreign_keys = %d", was))
		return errors.Join(err, conn.Close())
	}
	return conn, restore, nil
}

// checkForeignKeys is an error when a row of the table breaks a foreign key
func checkForeignKeys(ctx context.Context, tx bun.Tx, table string) error {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`PRAGMA foreign_key_check("%s")`, table))
	if err != nil {
		return err
	}
	broken := rows.Next()
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if broken {
		return fmt.Errorf("a row of %s names a workspace that doesn't exist", table)
	}
	return nil
}

func hasWorkspaceColumn(ctx context.Context, q bun.IDB, table string) (bool, error) {
	var count int
	var err error
	if q.Dialect().Name() == dialect.SQLite {
		err = q.NewRaw("SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?", table, workspaceColumn).Scan(ctx, &count)
	} else {
		err = q.NewRaw("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?", table, workspaceColumn).Scan(ctx, &count)
	}
	return count > 0, err
}

// fillWorkspaceColumn puts every row of the table in the install's oldest workspace
func fillWorkspaceColumn(ctx context.Context, tx bun.Tx, table string) error {
	hasRows, err := tx.NewSelect().Table(table).Exists(ctx)
	if err != nil || !hasRows {
		return err
	}
	var oldest string
	err = tx.NewSelect().Table("workspaces").Column("id").Order("created_at", "id").Limit(1).Scan(ctx, &oldest)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%s has rows and the install has no workspace to give them", table)
		}
		return err
	}
	_, err = tx.NewUpdate().Table(table).Set("? = ?", bun.Ident(workspaceColumn), oldest).Where("TRUE").Exec(ctx)
	return err
}

func createWorkspaceIndex(ctx context.Context, tx bun.Tx, table string) error {
	_, err := tx.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS ? ON ? (?)", bun.Ident(workspaceIndexName(table)), bun.Ident(table), bun.Ident(workspaceColumn))
	return err
}
