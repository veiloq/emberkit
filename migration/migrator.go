// Package migration defines the interface for applying database migrations
// within the emberkit testing toolkit. It allows for different migration
// strategies (e.g., Atlas, custom scripts) to be plugged in.
package migration

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// Migrator defines the interface for types that can apply database schema migrations.
// EmberKit uses an implementation of this interface during its initialization phase
// to bring the test database schema to the desired state.
type Migrator interface {
	// Apply executes the migration process against the database accessible via the
	// provided pgxpool.Pool. It should apply all necessary migrations to reach
	// the target schema state. Implementations should handle logging using the
	// provided logger and respect the context for cancellation.
	Apply(ctx context.Context, pool *pgxpool.Pool, logger *zap.Logger) error
}

// NoOpMigrator is a Migrator implementation that performs no operations.
// It serves as the default migrator for EmberKit if no specific migrator
// (like AtlasMigrator or a custom one) is configured via options. Using this
// results in an empty test database being created.
type NoOpMigrator struct{}

// Apply implements the Migrator interface for NoOpMigrator. It logs a debug
// message indicating that migrations are being skipped and returns nil.
func (m *NoOpMigrator) Apply(ctx context.Context, pool *pgxpool.Pool, logger *zap.Logger) error {
	logger.Debug("Migration skipped (NoOpMigrator).")
	return nil
}
