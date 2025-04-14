package migration

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// Migrator defines the interface for applying database migrations.
type Migrator interface {
	// Apply applies migrations to the database specified by the DSN.
	Apply(ctx context.Context, pool *pgxpool.Pool, logger *zap.Logger) error
}

// --- NoOpMigrator ---

// NoOpMigrator is a Migrator implementation that does nothing.
// This is the default migrator if no specific migrator is configured.
type NoOpMigrator struct{}

// Apply for NoOpMigrator does nothing and returns nil.
func (m *NoOpMigrator) Apply(ctx context.Context, pool *pgxpool.Pool, logger *zap.Logger) error {
	logger.Debug("Migration skipped (NoOpMigrator).")
	return nil
}
