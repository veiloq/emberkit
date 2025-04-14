// Package db also includes functions related to database management tasks like
// creating, dropping, and generating unique names for test databases within emberkit.
package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/veiloq/emberkit/config"
	"github.com/veiloq/emberkit/internal/cleanup"
	"go.uber.org/zap"
)

// CreateDatabase connects to the administrative database (e.g., "postgres") specified
// in the config, using the provided admin DSN, and executes a `CREATE DATABASE`
// command to create the uniquely named test database (`testDBName`).
//
// It ensures connectivity to the admin database before attempting creation.
// Returns the admin DSN used (for potential use in cleanup registration) and an
// error if connection or creation fails.
func CreateDatabase(ctx context.Context, config config.Config, testDBName string, logger *zap.Logger) (adminDSN string, err error) {
	adminDSN = config.DSN() // DSN for the initial/admin database (e.g., 'postgres')
	logger.Debug("Connecting to admin database to create test database", zap.String("db", config.Database), zap.String("dsn", adminDSN))

	db, err := sql.Open("postgres", adminDSN)
	if err != nil {
		return adminDSN, fmt.Errorf("failed to open connection to admin database '%s': %w", config.Database, err)
	}
	defer db.Close() // Close admin connection when this function scope ends

	// Ping admin DB to ensure connectivity.
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err = db.PingContext(pingCtx); err != nil {
		return adminDSN, fmt.Errorf("failed to ping admin database '%s': %w", config.Database, err)
	}
	logger.Debug("Successfully connected to admin database", zap.String("database", config.Database))

	// testDBName is now passed as an argument.

	// Create the test database.
	quotedTestDBName := pgx.Identifier{testDBName}.Sanitize()
	logger.Debug("Creating test database", zap.String("database", testDBName), zap.String("quoted_name", quotedTestDBName))
	_, err = db.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s", quotedTestDBName))
	if err != nil {
		// Attempt to clean up if DB creation failed partially (though unlikely for CREATE DATABASE)
		// We don't have the drop function registered yet, so manual attempt might be complex.
		// Best effort is to return the error.
		return adminDSN, fmt.Errorf("failed to execute create database command for %q: %w", testDBName, err)
	}

	logger.Info("Successfully created test database", zap.String("database", testDBName))
	// Return the admin DSN so the caller can register the drop function.
	return adminDSN, nil
}

// DropTestDatabaseFunc returns a cleanup function suitable for use with
// `cleanup.Manager`. The returned function connects to the administrative database
// using `adminDSN`, terminates any active connections to the `testDBName`, and
// then drops the `testDBName` database.
//
// It respects the `keepDatabase` flag; if true, it logs that the drop is skipped
// and returns nil. It includes timeouts and logging for robustness during cleanup.
func DropTestDatabaseFunc(adminDSN, testDBName string, keepDatabase bool, logger *zap.Logger) cleanup.Func {
	return func() error {
		if keepDatabase {
			logger.Info("Skipping database drop because KeepDatabase is enabled.", zap.String("database", testDBName))
			return nil
		}

		logger.Debug("Attempting to drop test database", zap.String("database", testDBName))
		dropAdminDb, err := sql.Open("postgres", adminDSN)
		if err != nil {
			// Log error, but don't prevent other cleanup tasks if connection fails here.
			logger.Error("Cleanup: error connecting to admin DB to drop test DB", zap.String("database", testDBName), zap.Error(err))
			return fmt.Errorf("cleanup: error connecting to admin DB to drop test DB %q: %w", testDBName, err)
		}
		defer dropAdminDb.Close()

		// Terminate any active connections to the test database.
		// Use a background context for cleanup operations.
		termCtx, termCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer termCancel()
		_, termErr := dropAdminDb.ExecContext(termCtx,
			`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`,
			testDBName,
		)
		if termErr != nil {
			// Log warning, but proceed with drop attempt.
			logger.Warn("Cleanup: failed to terminate connections to test DB before drop, proceeding anyway", zap.String("database", testDBName), zap.Error(termErr))
		} else {
			logger.Debug("Cleanup: terminated connections to test DB", zap.String("database", testDBName))
		}

		// Drop the database.
		dropDbCtx, dropDbCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer dropDbCancel()
		quotedTestDBName := pgx.Identifier{testDBName}.Sanitize()
		_, dropErr := dropAdminDb.ExecContext(dropDbCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", quotedTestDBName))
		if dropErr != nil {
			logger.Error("Cleanup: error dropping test database", zap.String("database", testDBName), zap.Error(dropErr))
			return fmt.Errorf("cleanup: error dropping test database %q: %w", testDBName, dropErr)
		}

		logger.Info("Cleanup: successfully dropped test database", zap.String("database", testDBName))
		return nil
	}
}

// GenerateUniqueDBName creates a unique, sanitized database or runtime directory name
// suitable for use with PostgreSQL. It combines the given prefix with a random
// hexadecimal string.
//
// The name is lowercased, hyphens are replaced with underscores, and it's truncated
// to a maximum length of 63 characters to comply with PostgreSQL identifier limits.
func GenerateUniqueDBName(prefix string) (string, error) {
	b := make([]byte, 8) // 8 bytes = 16 hex characters
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to read random bytes for db name: %w", err)
	}
	// Build name: prefix + hex encoded random bytes
	name := prefix + hex.EncodeToString(b)
	// Sanitize: lowercase, replace hyphens (not typically allowed anyway)
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "-", "_")
	// Ensure name length is within PostgreSQL limits (usually 63 chars)
	if len(name) > 63 {
		name = name[:63]
	}
	return name, nil
}
