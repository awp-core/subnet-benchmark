// Package testutil provides shared test infrastructure for integration tests.
package testutil

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

const defaultTestDSN = "host=localhost dbname=benchmark_test user=ubuntu password=evo_test sslmode=disable"

// NewTestDB opens a connection to the test database and resets the schema.
func NewTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("BENCHMARK_TEST_DSN")
	if dsn == "" {
		dsn = defaultTestDSN
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	resetSchema(t, db)
	return db
}

func resetSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	schema, err := os.ReadFile(schemaPath(t))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	tables := []string{
		"system_config", "merkle_proofs", "worker_epoch_rewards",
		"epochs", "assignments", "questions", "benchmark_sets", "workers",
	}
	for _, table := range tables {
		if _, err := db.Exec("DROP TABLE IF EXISTS " + table + " CASCADE"); err != nil {
			t.Fatalf("drop table %s: %v", table, err)
		}
	}
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatalf("exec schema: %v", err)
	}
}

func schemaPath(t *testing.T) string {
	candidates := []string{
		"../../migrations/001_init.sql",
		"../../../migrations/001_init.sql",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Fatal("could not find migrations/001_init.sql")
	return ""
}
