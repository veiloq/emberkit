package emberkit

import (
	"context"
	"database/sql"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Kit defines the interface for a test kit that manages a temporary PostgreSQL database.
// It provides methods for accessing the database connection, running tests within transactions,
// and cleaning up resources.
type Kit interface {
	// DB returns the standard library *sql.DB connection pool.
	DB() *sql.DB
	// Pool returns the pgx *pgxpool.Pool connection pool.
	Pool() *pgxpool.Pool
	// ConnectionString returns the Data Source Name (connection string) for the test database.
	ConnectionString() string
	// RunSQLTx executes a test function within a standard library sql.Tx transaction.
	// The transaction is automatically rolled back at the end of the test.
	RunSQLTx(ctx context.Context, t *testing.T, testFn func(ctx context.Context, tx *sql.Tx) error)
	// RunTx executes a test function within a pgx.Tx transaction.
	// The transaction is automatically rolled back at the end of the test.
	RunTx(ctx context.Context, t *testing.T, testFn func(ctx context.Context, tx pgx.Tx) error)
	// Cleanup stops the PostgreSQL instance and removes temporary data directories.
	// It should be called once all tests using the kit are complete (e.g., via t.Cleanup).
	Cleanup() error
}
