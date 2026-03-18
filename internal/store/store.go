package store

import (
	"context"
	"database/sql"
	"fmt"
)

// DBTX is the interface satisfied by both *sql.DB and *sql.Tx.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Store struct {
	db DBTX
}

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// DB returns the underlying database connection (for testing).
func (s *Store) DB() *sql.DB {
	if db, ok := s.db.(*sql.DB); ok {
		return db
	}
	return nil
}

// Tx runs fn inside a database transaction. The transaction-scoped Store
// shares the same methods but all queries go through the transaction.
func (s *Store) Tx(ctx context.Context, fn func(tx *Store) error) error {
	db, ok := s.db.(*sql.DB)
	if !ok {
		// Already in a transaction — just run directly
		return fn(s)
	}
	sqlTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	txStore := &Store{db: sqlTx}
	if err := fn(txStore); err != nil {
		sqlTx.Rollback()
		return err
	}
	return sqlTx.Commit()
}
