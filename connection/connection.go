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

// connectPools connects the sql.DB and pgxpool.Pool to the specified test DB.
// It returns the sql.DB pool, the pgxpool.Pool, the final DSN string, and an error.
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

// closeTestDBConnection returns a cleanup function to close the sql.DB pool.
// It uses a pointer-to-pointer for db to allow setting the original variable to nil upon successful close.
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

// closePgxPool returns a cleanup function to close the pgxpool.Pool.
// It uses a pointer-to-pointer for pool to allow setting the original variable to nil upon successful close.
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

// GetDBNameFromDSN extracts the database name from a DSN string for logging purposes.
// Returns "unknown" if parsing fails.
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
