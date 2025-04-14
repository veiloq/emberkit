package db

import (
	"context"
	"fmt"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/veiloq/emberkit/config"
	"github.com/veiloq/emberkit/connection"
	"github.com/veiloq/emberkit/internal/cleanup"
	"go.uber.org/zap"
)

// assignRandomPort assigns a random port if config.Port is 0.
// Modifies the config in place.
func AssignRandomPort(config *config.Config, logger *zap.Logger) error {
	if config.Port == 0 {
		// GetFreePort is defined in port.go within the same package
		freePort, err := connection.GetFreePort(config.Host)
		if err != nil {
			return fmt.Errorf("failed to get free port: %w", err)
		}
		config.Port = uint32(freePort)
		logger.Info("Assigned random free port", zap.Uint32("port", config.Port))
	}
	return nil
}

// startServer initializes and starts the embedded PostgreSQL server.
// It takes the configuration and logger, and returns the server instance or an error.
func StartServer(ctx context.Context, config config.Config, instanceWorkDir string, logger *zap.Logger) (*embeddedpostgres.EmbeddedPostgres, error) {
	embeddedPostgresConfig := embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.PostgresVersion(config.Version)).
		Port(config.Port).
		Database(config.Database). // Initial DB (e.g., 'postgres')
		Username(config.Username).
		Password(config.Password).
		RuntimePath(instanceWorkDir). // Use the passed-in path
		BinariesPath(config.BinariesPath).
		StartTimeout(config.StartTimeout)

	// Use the logger specified in the config for embedded-postgres internal logs, if any.
	if config.Logger != nil {
		embeddedPostgresConfig = embeddedPostgresConfig.Logger(config.Logger)
	} else {
		// Otherwise, disable embedded-postgres internal logging.
		embeddedPostgresConfig = embeddedPostgresConfig.Logger(nil)
	}

	// Warn about potential limitations of StartupParams with embedded-postgres.
	if len(config.StartupParams) > 0 {
		logger.Warn("Applying emberkit.Config.StartupParams may have limitations",
			zap.Any("params", config.StartupParams),
			zap.String("reason", "Embedded-postgres library support for arbitrary startup flags is limited."))
		// Note: The embedded-postgres library doesn't directly expose a way to pass arbitrary params easily.
		// This warning remains relevant.
	}

	embeddedDB := embeddedpostgres.NewDatabase(embeddedPostgresConfig)
	logger.Info("Starting embedded postgres server...", zap.Uint32("port", config.Port), zap.String("version", string(config.Version)))

	// Start the server.
	if err := embeddedDB.Start(); err != nil {
		return nil, fmt.Errorf("failed to start embedded postgres: %w", err)
	}

	logger.Info("Embedded postgres server started successfully.")
	return embeddedDB, nil
}

// stopEmbeddedServer creates a cleanup function that stops the given embedded server instance.
// It uses a pointer-to-pointer for embeddedDB to allow setting the original variable to nil upon successful stop.
func StopEmbeddedServer(embeddedDBPtr **embeddedpostgres.EmbeddedPostgres, logger *zap.Logger) cleanup.Func {
	return func() error {
		// Dereference the pointer-to-pointer to get the actual embeddedDB instance pointer.
		embeddedDB := *embeddedDBPtr
		if embeddedDB == nil {
			logger.Debug("Embedded postgres server already stopped or never started.")
			return nil // Nothing to do.
		}

		logger.Debug("Stopping embedded postgres server...")
		err := embeddedDB.Stop()
		if err != nil {
			// Log the error but don't necessarily prevent other cleanup tasks.
			logger.Error("Error stopping embedded postgres server", zap.Error(err))
			// We might not want to nil the pointer here, as the state is uncertain.
			return fmt.Errorf("error stopping embedded postgres: %w", err)
		}

		logger.Debug("Embedded postgres server stopped successfully.")
		// Set the original pointer variable (outside this function) to nil to indicate it's stopped.
		*embeddedDBPtr = nil
		return nil
	}
}
