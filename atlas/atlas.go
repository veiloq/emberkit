package atlas

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ariga.io/atlas/sql/migrate"
	postgres "ariga.io/atlas/sql/postgres" // Atlas driver for postgres (aliased)
	"github.com/jackc/pgx/v5/pgxpool"      // Added for Apply signature

	// Atlas high-level client
	"github.com/hashicorp/hcl/v2/hclsimple"
	_ "github.com/jackc/pgx/v5/stdlib" // Blank import for database/sql driver registration
	"github.com/veiloq/emberkit/connection"
	"go.uber.org/zap"
)

// AtlasMigrator implements the Migrator interface using the Atlas library.
type AtlasMigrator struct {
	hclPath    string       // Path to the atlas.hcl file.
	logger     *zap.Logger  // Logger instance.
	initOnce   func() error // Function to initialize migration dir once.
	migrateDir migrate.Dir  // Atlas migration directory object
	dirPath    string       // Resolved path to the migration directory (e.g., "migrations")
	dirURL     string       // Resolved file URL for the migration directory
	initErr    error        // Stores the *first* critical error from initialization attempt.
}

// NewAtlasMigrator creates a new AtlasMigrator.
// Initialization (client creation, HCL parsing) is deferred until Apply is called.
func NewAtlasMigrator(hclPath string, logger *zap.Logger) *AtlasMigrator {
	am := &AtlasMigrator{
		hclPath: hclPath,
		logger:  logger.With(zap.String("migrator", "atlas")),
	}
	// Use a sync.Once equivalent pattern for lazy initialization
	var ran bool
	am.initOnce = func() error {
		if ran {
			return am.initErr // Return stored error if already initialized
		}
		ran = true
		// Initialize the migrator directory and URL
		am.migrateDir, am.dirPath, am.dirURL = am.initializeMigrator()
		// Note: initializeMigrator now stores am.initErr internally via recordInitError

		if am.initErr != nil {
			am.logger.Warn("Atlas migrator initialization failed. Apply will likely be skipped.", zap.Error(am.initErr))
		} else if am.migrateDir == nil || am.dirURL == "" {
			// This case might occur if HCL is found but no migration dir is defined.
			// initializeMigrator might not set initErr in this specific scenario if it's not considered a critical failure.
			am.logger.Info("Atlas migrator initialization complete, but no migration directory found or resolved. Apply will be skipped.")
		} else {
			am.logger.Info("Atlas migrator initialized successfully.", zap.String("migration_dir", am.dirPath), zap.String("migration_url", am.dirURL))
		}
		return am.initErr // Return the stored error status
	}
	return am
}

// Apply applies migrations using the core Atlas library.
// It initializes the migration directory on the first call.
func (am *AtlasMigrator) Apply(ctx context.Context, pool *pgxpool.Pool, logger *zap.Logger) error {
	// Initialize on first call (idempotent)
	_ = am.initOnce() // We check am.initErr and am.migrateDir below

	// Check if initialization failed critically or didn't yield necessary components
	if am.initErr != nil {
		logger.Warn("Migrations skipped due to Atlas initialization error.", zap.Error(am.initErr))
		return nil // Don't return the init error itself, just skip applying
	}
	if am.migrateDir == nil {
		logger.Warn("Migrations skipped: Atlas migration directory is missing or could not be resolved.")
		return nil // Cannot apply migrations
	}

	// Get DSN from pool config
	dsn := pool.Config().ConnString()
	dbName := connection.GetDBNameFromDSN(dsn)
	dsnPrefix := getDSNPrefix(dsn) // Use helper for safe logging

	if dbName == "unknown" {
		logger.Warn("Could not extract database name from DSN for logging", zap.String("dsn_prefix", dsnPrefix))
	}

	logger.Info("Applying Atlas migrations using core library...",
		zap.String("database", dbName),
		zap.String("source_dir", am.dirPath),
		zap.String("target_dsn_prefix", dsnPrefix),
	)

	// Use a timeout for the entire migration process (driver open + execution).
	applyCtx, applyCancel := context.WithTimeout(ctx, 90*time.Second)
	defer applyCancel()

	// Open Atlas driver
	drv, cleanup, err := am.openAtlasDriver(applyCtx, dsn)
	if err != nil {
		// Log the specific error from opening the driver.
		logger.Error("Failed to open Atlas driver",
			zap.String("database", dbName),
			zap.Error(err))
		return fmt.Errorf("failed to prepare Atlas driver for %q: %w", dbName, err)
	}
	defer cleanup() // Ensure standard DB connection is closed

	// Execute migrations
	err = am.executeMigrations(applyCtx, drv, dbName)
	if err != nil {
		// Error is already logged within executeMigrations
		return fmt.Errorf("failed to apply Atlas migrations to database %q from %q: %w", dbName, am.dirPath, err)
	}

	// Success logging is handled within executeMigrations
	return nil
}

// recordInitError logs an error and stores it in am.initErr if it's the first error.
// It always returns the formatted error passed to it.
func (am *AtlasMigrator) recordInitError(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	formattedErr := fmt.Errorf(format, args...)
	am.logger.Error("Atlas initialization error", zap.Error(formattedErr), zap.NamedError("original_error", err))
	if am.initErr == nil { // Store only the first critical error
		am.initErr = formattedErr
	}
	return formattedErr
}

// initializeMigrator processes HCL, finds migration dir, and creates migrate.Dir.
// It stores the first critical error encountered in am.initErr via recordInitError.
func (am *AtlasMigrator) initializeMigrator() (migrate.Dir, string, string) {
	// 1. Resolve and check HCL file path.
	absHCLPath, err := filepath.Abs(am.hclPath)
	if err = am.recordInitError(err, "failed to determine absolute path for atlas HCL file %q", am.hclPath); err != nil {
		return nil, "", ""
	}
	am.logger.Debug("Resolved Atlas HCL path", zap.String("relative", am.hclPath), zap.String("absolute", absHCLPath))

	if _, statErr := os.Stat(absHCLPath); statErr != nil {
		if os.IsNotExist(statErr) {
			am.logger.Info("Atlas HCL file not found, skipping Atlas analysis.", zap.String("path", absHCLPath))
			return nil, "", "" // Not an error if Atlas isn't used
		}
		// File exists but cannot be stat'd.
		_ = am.recordInitError(statErr, "failed to stat atlas HCL file %q", absHCLPath)
		return nil, "", ""
	}

	// 2. Parse HCL file.
	var atlasConf atlasConfigHCL
	err = hclsimple.DecodeFile(absHCLPath, nil, &atlasConf)
	if err = am.recordInitError(err, "failed to decode atlas HCL file %q", absHCLPath); err != nil {
		return nil, "", ""
	}
	am.logger.Debug("Atlas HCL file parsed successfully.", zap.String("path", absHCLPath))

	// 3. Discover migration directory from parsed HCL.
	migrationDirRel, found := findMigrationDirInHCL(&atlasConf, absHCLPath, am.logger)
	if !found {
		// If HCL was parsed but no dir found, it might be a config issue, but maybe not critical failure yet.
		// Logged within findMigrationDirInHCL. Let Apply handle the nil migrateDir.
		return nil, "", ""
	}

	// 4. Validate and resolve migration directory path and URL.
	hclDir := filepath.Dir(absHCLPath)
	relativePath := strings.TrimPrefix(migrationDirRel, "file://") // Remove scheme if present
	absMigrationDir, err := filepath.Abs(filepath.Join(hclDir, relativePath))
	if err = am.recordInitError(err, "failed to resolve absolute path for migration dir %q (relative to %q)", migrationDirRel, hclDir); err != nil {
		return nil, "", ""
	}

	// 5. Create the migrate.Dir object
	dir, err := migrate.NewLocalDir(absMigrationDir)
	if err = am.recordInitError(err, "failed to create migrate.Dir for %q", absMigrationDir); err != nil {
		return nil, absMigrationDir, "" // Return path even if dir creation failed
	}

	// Success! Convert the absolute path to a file URL for logging/consistency.
	migrationURL := fmt.Sprintf("file://%s", filepath.ToSlash(absMigrationDir))
	am.logger.Debug("Resolved migration directory.", zap.String("path", absMigrationDir), zap.String("url", migrationURL))

	return dir, absMigrationDir, migrationURL
}

// findMigrationDirInHCL searches the parsed HCL config for the migration directory path.
func findMigrationDirInHCL(atlasConf *atlasConfigHCL, hclPath string, logger *zap.Logger) (dir string, found bool) {
	// Prioritize 'local' environment block.
	for _, env := range atlasConf.Envs {
		if env.Name == "local" && env.Migration != nil && env.Migration.Dir != "" {
			logger.Debug("Found migration directory in 'local' env.", zap.String("dir", env.Migration.Dir))
			return env.Migration.Dir, true
		}
	}
	// Fallback to the first environment block if 'local' isn't found or configured.
	if len(atlasConf.Envs) > 0 && atlasConf.Envs[0].Migration != nil && atlasConf.Envs[0].Migration.Dir != "" {
		envName := atlasConf.Envs[0].Name
		if envName == "" {
			envName = "[unnamed first env]"
		}
		dir = atlasConf.Envs[0].Migration.Dir
		logger.Warn("Atlas 'local' env not found or missing migration dir. Falling back to first env.",
			zap.String("hcl_path", hclPath),
			zap.String("fallback_env", envName),
			zap.String("dir", dir))
		return dir, true
	}

	logger.Warn("Could not find migration directory definition (env.migration.dir) in atlas config", zap.String("hcl_path", hclPath))
	return "", false
}

// openAtlasDriver opens the standard DB connection and the Atlas driver.
func (am *AtlasMigrator) openAtlasDriver(ctx context.Context, dsn string) (drv migrate.Driver, cleanup func(), err error) {
	// Default cleanup is a no-op
	cleanup = func() {}

	// Open standard connection using pgx stdlib driver
	stdDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, cleanup, fmt.Errorf("failed to open standard db connection via pgx: %w", err)
	}

	// Set cleanup function to close the standard DB
	cleanup = func() {
		closeErr := stdDB.Close()
		if closeErr != nil {
			am.logger.Warn("Error closing standard DB connection used for Atlas driver", zap.Error(closeErr))
		}
	}

	// Ping to verify connection early
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()
	if err = stdDB.PingContext(pingCtx); err != nil {
		cleanup() // Close DB if ping fails
		return nil, func() {}, fmt.Errorf("failed to ping database via standard connection: %w", err)
	}

	// Open Atlas driver using the standard connection.
	drv, err = postgres.Open(stdDB)
	if err != nil {
		cleanup() // Close DB if driver opening fails
		return nil, func() {}, fmt.Errorf("failed to open atlas postgres driver: %w", err)
	}

	return drv, cleanup, nil // Return driver, cleanup func, and nil error
}

// executeMigrations creates and runs the Atlas executor.
func (am *AtlasMigrator) executeMigrations(ctx context.Context, drv migrate.Driver, dbName string) error {
	// Create an executor using the driver.
	exec, err := migrate.NewExecutor(drv, am.migrateDir, migrate.NopRevisionReadWriter{}, migrate.WithLogger(&zapMigrateLogger{logger: am.logger}))
	if err != nil {
		am.logger.Error("Failed to create atlas executor", zap.String("database", dbName), zap.Error(err))
		return fmt.Errorf("failed to create atlas executor for %q: %w", dbName, err)
	}

	// Execute all pending migrations (n=0 means all).
	err = exec.ExecuteN(ctx, 0)
	if err != nil {
		if errors.Is(err, migrate.ErrNoPendingFiles) {
			am.logger.Info("No pending Atlas migrations to apply.", zap.String("database", dbName))
			return nil // Not a failure
		}
		// Log the specific execution error.
		am.logger.Error("Failed to apply Atlas migrations via executor",
			zap.String("database", dbName),
			zap.String("source_dir", am.dirPath),
			zap.Error(err))
		return err // Return the actual execution error
	}

	am.logger.Info("Successfully applied Atlas migrations", zap.String("database", dbName))
	return nil
}

// getDSNPrefix returns a DSN string suitable for logging (removes password).
func getDSNPrefix(dsn string) string {
	parts := strings.SplitN(dsn, "password=", 2)
	if len(parts) > 0 {
		return parts[0] // Return the part before "password="
	}
	return dsn // Should not happen with standard DSNs, but return original as fallback
}

// --- HCL Parsing Structs ---

// atlasConfigHCL helps parse relevant parts of atlas.hcl.
type atlasConfigHCL struct {
	Envs []*atlasEnvHCL `hcl:"env,block"`
}

type atlasEnvHCL struct {
	Name      string             `hcl:"name,label"`
	Migration *atlasMigrationHCL `hcl:"migration,block"`
}

type atlasMigrationHCL struct {
	Dir string `hcl:"dir"`
}

// zapMigrateLogger adapts a *zap.Logger to the migrate.Logger interface.
type zapMigrateLogger struct {
	logger *zap.Logger
}

// Log implements migrate.Logger.
func (l *zapMigrateLogger) Log(entry migrate.LogEntry) {
	switch e := entry.(type) {
	case migrate.LogExecution:
		l.logger.Info("Atlas migration execution starting",
			zap.String("from_version", e.From),
			zap.String("to_version", e.To),
			zap.Int("num_files", len(e.Files)),
		)
	case migrate.LogFile:
		l.logger.Info("Applying migration file",
			zap.String("file", e.File.Name()),
			zap.Int("skip_stmts", e.Skip),
		)
	case migrate.LogStmt:
		// Log statements at Debug level to avoid excessive noise
		l.logger.Debug("Executing statement", zap.String("sql", e.SQL))
	case migrate.LogError:
		l.logger.Error("Atlas migration error",
			zap.Stringp("sql", &e.SQL), // Use Stringp for pointer
			zap.Error(e.Error),
		)
	case migrate.LogDone:
		l.logger.Info("Atlas migration execution finished")
	// Add cases for LogChecks, LogCheck, LogChecksDone if needed
	default:
		l.logger.Warn("Received unknown Atlas log entry type", zap.Any("entry", entry))
	}
}
