package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Court and realm status values stored in the database.
const (
	courtRunning = "running"
	courtFallen  = "fallen"

	realmAlive  = "alive"
	realmFallen = "fallen"
)

// querier is what both *sql.DB and *sql.Tx can do.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// withTx runs fn in a transaction, committing if it returns nil.
func (s *Store) withTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// queryAll runs a query and scans every row. The rows are closed before it
// returns, so the single connection is free again for the caller.
func queryAll[T any](ctx context.Context, q querier, query string, scan func(scanner) (T, error), args ...any) ([]T, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// scanString reads a one-column row of text.
func scanString(r scanner) (string, error) {
	var v string
	return v, r.Scan(&v)
}

// inList is "(?,?,…)" for len(vals) placeholders, and vals as args.
// vals must not be empty.
func inList[T any](vals []T) (string, []any) {
	args := make([]any, len(vals))
	for i, v := range vals {
		args[i] = v
	}
	return "(?" + strings.Repeat(",?", len(vals)-1) + ")", args
}

// spend takes gold from a character, or fails if they don't have it.
func spend(ctx context.Context, tx *sql.Tx, charID int64, gold int) error {
	return spendOr(ctx, tx, charID, gold, fmt.Sprintf("that needs %d gold", gold))
}

// spendOr is spend with the player-facing message to fail with.
func spendOr(ctx context.Context, tx *sql.Tx, charID int64, gold int, short string) error {
	if gold <= 0 {
		return nil
	}
	res, err := tx.ExecContext(ctx, `UPDATE characters SET wealth = wealth - ? WHERE id = ? AND wealth >= ?`, gold, charID, gold)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%s", short)
	}
	return nil
}

// credit gives a character gold.
func credit(ctx context.Context, tx *sql.Tx, charID int64, gold int) error {
	_, err := tx.ExecContext(ctx, `UPDATE characters SET wealth = wealth + ? WHERE id = ?`, gold, charID)
	return err
}

// resolveOnce marks an activity resolved, or returns ErrEnded if something
// else already did.
func resolveOnce(ctx context.Context, tx *sql.Tx, activityID int64, now time.Time) error {
	res, err := tx.ExecContext(ctx, `UPDATE activities SET resolved_at = ? WHERE id = ? AND resolved_at IS NULL`, now.Unix(), activityID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrEnded
	}
	return nil
}
