// Package dbtx lets a service run several repository calls as one database transaction without
// the repositories' signatures carrying it. The transaction travels in the context: WithTx
// puts it there, and a SQL repository takes its handle from DB(ctx, r.db) instead of from r.db.
package dbtx

import (
	"context"

	"github.com/uptrace/bun"
)

// Transactor runs fn so that every repository call made with the ctx it is given is one
// transaction: they all commit when fn returns nil, and none is kept when fn returns an error
// or panics. WithTx returns fn's error.
//
// A WithTx call made with a ctx that already carries a transaction joins it. fn runs in the
// outer transaction, which the outermost WithTx commits or rolls back, so an error from the
// inner call undoes nothing until it reaches that outermost call.
type Transactor interface {
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type txKey struct{}

// DB returns the transaction ctx carries, or db when it carries none. A SQL repository reads
// its handle through it in every method that a service may call inside WithTx.
func DB(ctx context.Context, db bun.IDB) bun.IDB {
	if tx, ok := ctx.Value(txKey{}).(bun.Tx); ok {
		return tx
	}
	return db
}

type sqlTransactor struct {
	db *bun.DB
}

// NewSQLTransactor returns the Transactor for SQLite and PostgreSQL. It begins a bun
// transaction on db and carries it in the context it gives fn.
func NewSQLTransactor(db *bun.DB) Transactor {
	return &sqlTransactor{db: db}
}

func (t *sqlTransactor) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(bun.Tx); ok {
		return fn(ctx)
	}
	return t.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		return fn(context.WithValue(ctx, txKey{}, tx))
	})
}
