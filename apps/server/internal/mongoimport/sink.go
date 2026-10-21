package mongoimport

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
)

// insertBatchSize is how many rows go into one INSERT. Even the widest table stays
// well under the limits on the number of values in a statement.
const insertBatchSize = 500

// batcher writes the rows of one table in batches. The rows are structs whose
// bun tags name the columns, and whose max tags give the width of a column
// PostgreSQL limits.
type batcher[T any] struct {
	tx     bun.Tx
	table  string
	report *CollectionReport
	// cut is whether text wider than its column is cut: only PostgreSQL enforces a column's width
	cut  bool
	rows []T
}

func newBatcher[T any](tx bun.Tx, table string, report *CollectionReport) *batcher[T] {
	return &batcher[T]{tx: tx, table: table, report: report, cut: cutsText(tx)}
}

// cutsText is whether the database enforces the width of a VARCHAR column. SQLite doesn't, so it
// keeps text whole.
func cutsText(db bun.IDB) bool {
	return db.Dialect().Name() == dialect.PG
}

// add queues a row and counts it as copied. A row that doesn't fit its columns
// as it is gets cleaned first, and the report says so.
func (b *batcher[T]) add(ctx context.Context, row T) error {
	cleanText(&row, b.report, b.cut)
	b.rows = append(b.rows, row)
	b.report.Copied++
	if len(b.rows) >= insertBatchSize {
		return b.flush(ctx)
	}
	return nil
}

func (b *batcher[T]) flush(ctx context.Context) error {
	if len(b.rows) == 0 {
		return nil
	}
	_, err := b.tx.NewInsert().Model(&b.rows).ModelTableExpr(b.table).Exec(ctx)
	b.rows = b.rows[:0]
	if err != nil {
		return fmt.Errorf("write %s: %w", b.table, err)
	}
	return nil
}

// cleanText makes the text fields of a row fit what PostgreSQL accepts. It
// removes NUL characters and invalid UTF-8, which text columns reject, and, if
// cut is set, cuts a field to the width its max tag gives, which a VARCHAR
// column enforces. The API limits these fields, so a document of an install
// only needs this when something wrote around the API.
func cleanText(row any, report *CollectionReport, cut bool) {
	v := reflect.ValueOf(row).Elem()
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		var text *string
		switch field := v.Field(i); {
		case field.Kind() == reflect.String:
			text = field.Addr().Interface().(*string)
		case field.Kind() == reflect.Pointer && field.Type().Elem().Kind() == reflect.String && !field.IsNil():
			text = field.Interface().(*string)
		default:
			continue
		}
		column, _, _ := strings.Cut(t.Field(i).Tag.Get("bun"), ",")

		if cleaned := strings.ReplaceAll(strings.ToValidUTF8(*text, "�"), "\x00", ""); cleaned != *text {
			*text = cleaned
			report.fix("text with NUL characters or invalid UTF-8 cleaned (" + column + ")")
		}
		if limit, err := strconv.Atoi(t.Field(i).Tag.Get("max")); cut && err == nil && utf8.RuneCountInString(*text) > limit {
			*text = string([]rune(*text)[:limit])
			report.fix(column + " cut to " + strconv.Itoa(limit) + " characters")
		}
	}
}
