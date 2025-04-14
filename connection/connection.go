// Package connection handles the creation, management, and cleanup of database
// connections (both standard library `sql.DB` and `pgxpool.Pool`) for the
// isolated test database used by emberkit.
package connection

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/veiloq/emberkit/config"
	"github.com/veiloq/emberkit/internal/cleanup"
	"go.uber.org/zap"
)

// ConnectPools establishes both a standard library `sql.DB` connection pool and
// a `pgxpool.Pool` connection pool to the specified test database.
//
// It constructs the DSN based on the provided config and testDBName, attempts to
// connect and ping both pools, and returns the established pools along with the
// DSN used. If any step fails, it ensures previously opened resources are closed
// before returning an error.
func ConnectPools(ctx context.Context, config config.Config, testDBName string, logger *zap.Logger) (*sql.DB, *pgxpool.Pool, string, error) {
	// --- Prepare DSN for the Test Database ---
	testDbConfig := config             // Copy config
	testDbConfig.Database = testDBName // Point to the new test DB
	testDbDSN := testDbConfig.DSN()

	var db *sql.DB
	var pool *pgxpool.Pool
	var err error

	// --- Connect sql.DB ---
	logger.Debug("Connecting to test database (sql.DB)", zap.String("database", testDBName), zap.String("dsn", testDbDSN))
	db, err = sql.Open("postgres", testDbDSN)
	if err != nil {
		return nil, nil, testDbDSN, fmt.Errorf("failed to open connection to test database '%s': %w", testDBName, err)
	}

	// Ping sql.DB
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err = db.PingContext(pingCtx); err != nil {
		db.Close() // Close connection on ping failure
		return nil, nil, testDbDSN, fmt.Errorf("failed to ping test database '%s' (sql.DB): %w", testDBName, err)
	}
	logger.Debug("Successfully connected to test database (sql.DB)", zap.String("database", testDBName))

	// --- Connect pgxpool.Pool ---
	logger.Debug("Creating pgx connection pool", zap.String("database", testDBName), zap.String("dsn", testDbDSN))
	pgxConfig, err := pgxpool.ParseConfig(testDbDSN)
	if err != nil {
		db.Close() // Close the already opened sql.DB connection
		return nil, nil, testDbDSN, fmt.Errorf("failed to parse DSN for pgx pool: %w", err)
	}

	// Set up a context for creating the pool.
	poolCtx, poolCancel := context.WithTimeout(ctx, 10*time.Second)
	defer poolCancel()
	pool, err = pgxpool.NewWithConfig(poolCtx, pgxConfig)
	if err != nil {
		db.Close() // Close the already opened sql.DB connection
		return nil, nil, testDbDSN, fmt.Errorf("failed to create pgx connection pool: %w", err)
	}

	// Ping pgxpool.Pool
	pingPoolCtx, pingPoolCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingPoolCancel()
	if err = pool.Ping(pingPoolCtx); err != nil {
		pool.Close() // Close pool if ping fails
		db.Close()   // Close the already opened sql.DB connection
		return nil, nil, testDbDSN, fmt.Errorf("failed to ping pgx pool for test database '%s': %w", testDBName, err)
	}
	logger.Debug("Successfully created and pinged pgx pool", zap.String("database", testDBName))

	// Return the established pools and the DSN used.
	return db, pool, testDbDSN, nil
}

// CloseTestDBConnection returns a cleanup function suitable for use with
// `cleanup.Manager`. The returned function closes the provided `sql.DB`
// connection pool.
//
// It takes a pointer-to-a-pointer (`**sql.DB`) to the database pool. This allows
// the cleanup function to set the original `*sql.DB` variable to `nil` after
// successfully closing the connection, preventing potential double-close issues.
// The DSN is used solely for logging purposes to identify the database being closed.
func CloseTestDBConnection(dbPtr **sql.DB, dsn string, logger *zap.Logger) cleanup.Func {
	return func() error {
		db := *dbPtr
		if db == nil {
			logger.Debug("sql.DB connection already closed or never opened.")
			return nil
		}
		logDBName := GetDBNameFromDSN(dsn)
		logger.Debug("Closing sql.DB connection", zap.String("database", logDBName))
		err := db.Close()
		if err != nil {
			logger.Error("Error closing sql.DB test database connection", zap.String("database", logDBName), zap.Error(err))
			// Don't nil the pointer here, state is uncertain.
			return fmt.Errorf("error closing sql.DB test database connection (%s): %w", logDBName, err)
		}
		logger.Debug("Successfully closed sql.DB connection", zap.String("database", logDBName))
		*dbPtr = nil // Mark as closed
		return nil
	}
}

// ClosePgxPool returns a cleanup function suitable for use with `cleanup.Manager`.
// The returned function closes the provided `pgxpool.Pool`.
//
// It takes a pointer-to-a-pointer (`**pgxpool.Pool`) to the pool. This allows the
// cleanup function to set the original `*pgxpool.Pool` variable to `nil` after
// closing the pool, preventing potential issues with using a closed pool.
// The DSN is used solely for logging purposes to identify the database being closed.
// Note: `pgxpool.Pool.Close()` does not return an error.
func ClosePgxPool(poolPtr **pgxpool.Pool, dsn string, logger *zap.Logger) cleanup.Func {
	return func() error {
		pool := *poolPtr
		if pool == nil {
			logger.Debug("pgxpool.Pool connection already closed or never opened.")
			return nil
		}
		logDBName := GetDBNameFromDSN(dsn)
		logger.Debug("Closing pgxpool.Pool connection", zap.String("database", logDBName))
		pool.Close() // pgxpool.Close() doesn't return an error.
		logger.Debug("Successfully closed pgxpool.Pool connection", zap.String("database", logDBName))
		*poolPtr = nil // Mark as closed
		return nil
	}
}

// GetDBNameFromDSN attempts to extract the database name from a PostgreSQL DSN
// string (e.g., "postgres://user:pass@host:port/dbname?sslmode=disable").
// It's primarily used for providing more informative log messages during cleanup.
//
// Returns the extracted database name or "unknown" if parsing fails or the DSN
// format is unexpected.
func GetDBNameFromDSN(dsn string) string {
	// Example DSN: postgres://user:pass@host:port/dbname?sslmode=disable
	// Find the last '/'
	lastSlash := strings.LastIndex(dsn, "/")
	if lastSlash == -1 || lastSlash == len(dsn)-1 {
		return "unknown" // No '/' or it's the last character
	}

	// Extract the part after the last '/'
	dbPart := dsn[lastSlash+1:]

	// Find the first '?' which indicates start of parameters
	queryStart := strings.Index(dbPart, "?")
	if queryStart == -1 {
		return dbPart // No parameters, the whole part is the dbname
	}

	// Return the part before the '?'
	return dbPart[:queryStart]
}
