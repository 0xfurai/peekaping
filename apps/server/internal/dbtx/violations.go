package dbtx

import (
	"errors"
	"strings"

	"github.com/uptrace/bun/driver/pgdriver"
)

// IsForeignKeyViolation reports whether the database refused a statement because of a foreign key.
// PostgreSQL says so with a code. SQLite's two drivers word it the same, and neither is a
// dependency of ours.
func IsForeignKeyViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr pgdriver.Error
	if errors.As(err, &pgErr) {
		return pgErr.Field('C') == "23503" // foreign_key_violation
	}
	return strings.Contains(err.Error(), "FOREIGN KEY constraint failed")
}

// IsUniqueViolation reports whether the database refused a statement because a unique index or
// constraint already holds the value. Worded like IsForeignKeyViolation's.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr pgdriver.Error
	if errors.As(err, &pgErr) {
		return pgErr.Field('C') == "23505" // unique_violation
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
