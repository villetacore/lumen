package state

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// DB is the handle feature packages use. Their SQL is written once, with "?"
// placeholders and syntax both engines accept; DB translates the placeholders
// for PostgreSQL, so a query cannot behave differently by accident.
type DB struct {
	sql      *sql.DB
	postgres bool
}

// Tx is a transaction on a DB.
type Tx struct {
	tx       *sql.Tx
	postgres bool
}

func (d *DB) q(query string) string {
	if d.postgres {
		return rebind(query)
	}
	return query
}

func (d *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.sql.ExecContext(ctx, d.q(query), args...)
}

func (d *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.sql.QueryContext(ctx, d.q(query), args...)
}

func (d *DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.sql.QueryRowContext(ctx, d.q(query), args...)
}

// QueryRow is QueryRowContext without a deadline, for tests.
func (d *DB) QueryRow(query string, args ...any) *sql.Row {
	return d.sql.QueryRow(d.q(query), args...)
}

// PingContext checks the connection.
func (d *DB) PingContext(ctx context.Context) error { return d.sql.PingContext(ctx) }

func (d *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	tx, err := d.sql.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &Tx{tx: tx, postgres: d.postgres}, nil
}

func (t *Tx) q(query string) string {
	if t.postgres {
		return rebind(query)
	}
	return query
}

func (t *Tx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, t.q(query), args...)
}

func (t *Tx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, t.q(query), args...)
}

func (t *Tx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, t.q(query), args...)
}

func (t *Tx) Commit() error   { return t.tx.Commit() }
func (t *Tx) Rollback() error { return t.tx.Rollback() }

// rebind turns "?" into "$1", "$2"… and leaves string literals alone.
func rebind(query string) string {
	var b strings.Builder
	b.Grow(len(query) + 8)
	n, quoted := 0, false
	for i := 0; i < len(query); i++ {
		c := query[i]
		switch {
		case c == '\'':
			quoted = !quoted
		case c == '?' && !quoted:
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
