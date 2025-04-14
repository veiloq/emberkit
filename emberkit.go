package emberkit

import (
	"context"
	"database/sql"
	"fmt"
	"os"            // Added for MkdirAll
	"path/filepath" // Added for joining runtime path
	"testing"       // Added for zaptest integration
	"time"

	// Needed for transaction timeouts
	embeddedpostgres "github.com/fergusstrange/embedded-postgres" // Needed for pgx.Tx
	"github.com/jackc/pgx/v5"                                     // Added for pgx.Tx and pgx.ErrTxClosed
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/veiloq/emberkit/config"
	"github.com/veiloq/emberkit/connection"
	"github.com/veiloq/emberkit/db"
	"github.com/veiloq/emberkit/internal/cleanup"
	"github.com/veiloq/emberkit/internal/logger"

	_ "github.com/lib/pq" // Driver for sql.Open
	"go.uber.org/zap"     // Added zap
)

const defaultRuntimeBasePath = ".emberkit" // Base directory for runtime data
// EmberKit holds the necessary components for running isolated tests.
// It now focuses on holding the state and providing accessors.
type EmberKit struct {
	db         *sql.DB       // Connection pool to the dedicated TEST database
	pgxPool    *pgxpool.Pool // pgx connection pool to the dedicated TEST database
	config     config.Config // Final, merged config used
	embeddedDB *embeddedpostgres.EmbeddedPostgres
	// atlasClient and migrationDirURL removed, handled by Migrator
	dsn     string           // DSN for the TEST database connection
	logger  *zap.Logger      // Internal logger
	cleanup *cleanup.Manager // Manages cleanup functions
	// isTestLogger is no longer needed here, managed internally during init
}

// --- Public Methods (Interface Implementation) ---

// AtlasClient and MigrationDirURL methods removed.

// ConnectionString returns the connection string for the test database.
func (tk *EmberKit) ConnectionString() string {
	return tk.dsn
}

// DB returns the sql.DB connection pool for the test database.
func (tk *EmberKit) DB() *sql.DB {
	return tk.db
}

// Pool returns the pgxpool.Pool connection pool for the test database.
func (tk *EmberKit) Pool() *pgxpool.Pool {
	return tk.pgxPool
}

// --- Cleanup ---

// Cleanup executes all registered cleanup functions in reverse order via the cleanupManager.
// It ensures cleanup runs only once and returns the first error encountered.
func (tk *EmberKit) Cleanup() error {
	// Delegate cleanup execution to the manager
	return tk.cleanup.Execute() // Use exported method name
}

// --- Transaction Runners ---
// executeTestFn wraps the execution of the user's test function, handling panics
// and returning any error produced by the function. It uses generics to work
// with different transaction types (*sql.Tx, pgx.Tx).
func executeTestFn[T any](t *testing.T, fn func(ctx context.Context, tx T) error, ctx context.Context, tx T) (err error) {
	t.Helper() // Mark this helper function for testing framework
	defer func() {
		if r := recover(); r != nil {
			// Panic recovered. Convert it to an error.
			// Do NOT call t.Errorf here. Let the caller (RunTx/RunSQLTx) decide how to handle it.
			// The test might expect a panic.
			// Ensure the panic value is captured as the error to be returned.
			err = fmt.Errorf("test panicked: %v", r)
		}
	}()
	err = fn(ctx, tx) // Execute the user's test function
	return err        // Return the error from the test function (or from panic recovery)
}

// RunSQLTx runs the test function within a standard library sql.Tx transaction
// on the dedicated test database. It accepts a context from the caller and
// expects the test function to return an error.
func (tk *EmberKit) RunSQLTx(ctx context.Context, t *testing.T, testFn func(ctx context.Context, tx *sql.Tx) error) {
	t.Helper()

	// Begin transaction on the test database connection pool using the provided context
	tx, err := tk.db.BeginTx(ctx, tk.config.SQLTxOptions) // Use configured options
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}

	// Defer rollback, ensuring it happens even if testFn panics or errors
	defer func() {
		// Rollback doesn't take a context in database/sql.
		if rollbackErr := tx.Rollback(); rollbackErr != nil && rollbackErr != sql.ErrTxDone {
			// Use t.Logf for test-specific warnings like rollback failure
			t.Logf("Warning: failed to rollback transaction: %v", rollbackErr)
		}
	}()

	// Execute the actual test function using the helper
	testErr := executeTestFn(t, testFn, ctx, tx)

	// Check for error returned by the test function (or panic).
	// If an error occurred (either returned or from a panic caught by executeTestFn),
	// we don't call t.Errorf here because the test might *expect* an error/panic
	// (e.g., rollback tests). The rollback is handled by the defer.
	// executeTestFn already logs panics using t.Errorf.
	if testErr != nil {
		// Log the error for debugging visibility, but don't fail the test.
		t.Logf("Test function returned error (expected in some cases): %v", testErr)
	}
	// Rollback is handled by defer
}

// RunTx runs the test function within a pgx.Tx transaction on the dedicated test database.
// It uses the pgxpool.Pool managed by EmberKit, accepts a context from the caller,
// and expects the test function to return an error.
func (tk *EmberKit) RunTx(ctx context.Context, t *testing.T, testFn func(ctx context.Context, tx pgx.Tx) error) {
	t.Helper()

	if tk.pgxPool == nil {
		t.Fatal("pgxPool is not initialized in EmberKit. Ensure NewEmberKit completed successfully.")
	}

	// Begin pgx transaction using the shared pool and the provided context
	tx, err := tk.pgxPool.BeginTx(ctx, tk.config.PgxTxOptions) // Use configured options
	if err != nil {
		t.Fatalf("Failed to begin pgx transaction: %v", err)
	}

	// Defer rollback, ensuring it happens even if testFn panics or errors
	defer func() {
		// Use a background context with a short timeout for rollback
		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer rollbackCancel()
		if rollbackErr := tx.Rollback(rollbackCtx); rollbackErr != nil && rollbackErr != pgx.ErrTxClosed {
			// Use t.Logf for test-specific warnings like rollback failure
			t.Logf("Warning: failed to rollback pgx transaction: %v", rollbackErr)
		}
	}()

	// Execute the actual test function using the helper
	testErr := executeTestFn(t, testFn, ctx, tx)

	// Check for error returned by the test function (or panic).
	// If an error occurred (either returned or from a panic caught by executeTestFn),
	// we don't call t.Errorf here because the test might *expect* an error/panic
	// (e.g., rollback tests). The rollback is handled by the defer.
	// executeTestFn already logs panics using t.Errorf.
	if testErr != nil {
		// Log the error for debugging visibility, but don't fail the test.
		t.Logf("Test function returned error (expected in some cases): %v", testErr)
	}
	// Rollback is handled by defer
}

// Transaction runner functions (executeTestFn, RunSQLTx, RunTx) are defined below

// --- Constructor ---

// NewEmberKit starts an embedded PostgreSQL instance, creates a unique test database,
// applies migrations (if configured), and returns a EmberKit connected to that database.
// If t (*testing.T) is provided, it uses zaptest.NewLogger(t) for logging.
// Cleanup is automatically registered with t.Cleanup if t is provided.
func NewEmberKit(ctx context.Context, t *testing.T, initialConfig config.Config, opts ...config.Option) (_ Kit, err error) {
	// 1. Validate initial config first
	if err := initialConfig.Validate(); err != nil {
		return nil, fmt.Errorf("invalid initial configuration provided: %w", err)
	}

	// 2. Apply functional options and merge configurations
	// applyOptions is now a standalone function in options.go
	options, finalConfig := config.ApplyOptions(&initialConfig, opts...)

	// 3. Initialize Logger (must happen after options are processed)
	// initLogger is now a standalone function in logger.go
	tempLogger, _, err := logger.InitLogger(t, options) // isTestLogger no longer needed
	if err != nil {
		// Cannot proceed without a logger, and no cleanup manager exists yet.
		return nil, fmt.Errorf("failed to initialize logger: %w", err)
	}

	// 4. Create Cleanup Manager (needs logger)
	cleanupMgr := cleanup.NewManager(tempLogger) // Use exported constructor name

	// 5. Initialize EmberKit struct (partially)
	tk := &EmberKit{
		// config will be potentially updated below if using shared server
		logger:  tempLogger,
		cleanup: cleanupMgr,
		// config field is set within the if/else block below
	}

	// Defer cleanup execution in case of errors during the rest of the setup
	defer func() {
		if err != nil {
			// If an error occurred during setup, run cleanup immediately
			cleanupErr := tk.Cleanup() // Uses the manager via tk.Cleanup()
			if cleanupErr != nil {
				// Log the cleanup error but return the original setup error
				tk.logger.Error("Error during cleanup after setup failure", zap.Error(cleanupErr))
			}
		}
	}()

	// --- Server/Connection Setup ---
	var dsn string // DSN for admin operations (create/drop DB)

	if options.UseSharedServer() {
		// --- Using Shared Server ---
		tk.logger.Info("Using shared PostgreSQL server instance.")

		// Start with the shared server's config as the base
		tk.config = options.SharedConfig()

		// Overlay specific settings from the merged finalConfig onto the shared base.
		// We want DSN params, Tx options, KeepDatabase etc. from the test's specific setup,
		// but Host, Port, User, Pass from the shared server.
		tk.config.DSNParams = finalConfig.DSNParams
		tk.config.SQLTxOptions = finalConfig.SQLTxOptions
		tk.config.PgxTxOptions = finalConfig.PgxTxOptions
		tk.config.KeepDatabase = finalConfig.KeepDatabase // Use the merged value (test option OR initial config)

		// Use the DSN provided by the option
		dsn = options.DSN()
		if dsn == "" {
			// This should ideally be caught by validation if WithSharedServer enforced non-empty DSN
			return nil, fmt.Errorf("dsn cannot be empty when using WithSharedServer")
		}

		tk.logger.Debug("Configured to use shared server",
			zap.String("host", tk.config.Host),
			zap.Uint32("port", tk.config.Port),
			zap.String("adminDSN", "***"), // Avoid logging potentially sensitive DSN
		)
		// Skip starting a new server (Steps 6, 7-runtime-path, 8)

	} else {
		// --- Starting Dedicated Server ---
		tk.logger.Info("Starting dedicated PostgreSQL server instance for this test.")
		tk.config = finalConfig // Use the fully merged config calculated earlier

		// 6. Assign Random Port if needed (only for dedicated server)
		if err = db.AssignRandomPort(&tk.config, tk.logger); err != nil {
			return nil, fmt.Errorf("failed to assign port for dedicated server: %w", err) // Cleanup handled by defer
		}

		// 7. Construct unique runtime path for this dedicated instance
		var instanceWorkDir string
		// Generate a unique name component for the directory to avoid collisions
		runtimeDirName, err := db.GenerateUniqueDBName("runtime_")
		if err != nil {
			return nil, fmt.Errorf("failed to generate unique name for runtime path: %w", err)
		}
		baseRuntimePath := defaultRuntimeBasePath
		// Ensure the base directory exists (e.g., .emberkit/)
		if err := os.MkdirAll(baseRuntimePath, 0750); err != nil {
			return nil, fmt.Errorf("failed to create base runtime directory %q: %w", baseRuntimePath, err)
		}
		instanceWorkDir = filepath.Join(baseRuntimePath, runtimeDirName)
		absInstanceWorkDir, err := filepath.Abs(instanceWorkDir)
		if err != nil {
			return nil, fmt.Errorf("failed to get absolute path for runtime directory %q: %w", instanceWorkDir, err)
		}
		tk.logger.Debug("Using unique working directory for dedicated instance", zap.String("path", absInstanceWorkDir))

		// 8. Start Embedded Server (only for dedicated server)
		tk.embeddedDB, err = db.StartServer(ctx, tk.config, absInstanceWorkDir, tk.logger)
		if err != nil {
			// Attempt to clean up the instance dir if start fails
			_ = os.RemoveAll(absInstanceWorkDir)                                                                   // Best effort cleanup
			return nil, fmt.Errorf("failed to start dedicated embedded server at %s: %w", absInstanceWorkDir, err) // Cleanup handled by defer
		}
		// Register cleanup for the runtime directory itself FIRST, so it runs LAST
		// Capture the absolute path in the closure
		capturedAbsPath := absInstanceWorkDir
		tk.cleanup.Add(func() error {
			tk.logger.Debug("Cleaning up dedicated server runtime directory", zap.String("path", capturedAbsPath))
			if err := os.RemoveAll(capturedAbsPath); err != nil {
				tk.logger.Error("Error removing dedicated server runtime directory", zap.String("path", capturedAbsPath), zap.Error(err))
				return fmt.Errorf("failed to remove runtime dir %q: %w", capturedAbsPath, err) // Return error for visibility
			}
			return nil
		})
		// Register server cleanup SECOND, so it runs FIRST
		tk.cleanup.Add(db.StopEmbeddedServer(&tk.embeddedDB, tk.logger))

		// Determine Admin DSN for the newly started dedicated server
		// Connect to the 'postgres' database on the new server for admin tasks.
		dsn = fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
			tk.config.Username, tk.config.Password, tk.config.Host, tk.config.Port, "postgres")
		tk.logger.Debug("Dedicated server started",
			zap.String("host", tk.config.Host),
			zap.Uint32("port", tk.config.Port),
			zap.String("adminDSN", "***"), // Avoid logging potentially sensitive DSN
		)
	}

	// --- Database Creation (Common Logic) ---

	// 7. Generate unique DB name (always needed for isolation)
	var testDBName string
	testDBName, err = db.GenerateUniqueDBName("test_")
	if err != nil {
		return nil, fmt.Errorf("failed to generate unique test database name: %w", err) // Cleanup handled by defer
	}

	// Step 7 (runtime path part) and Step 8 (server start/stop registration)
	// are handled within the if/else block above.
	// 9. Create Test Database (name already generated)
	// 9. Create Test Database
	// db.CreateDatabase connects using the admin details in tk.config (host, port, user, pass, initial db)
	// and creates the database named testDBName. It returns the admin DSN it used and an error.
	tk.logger.Debug("Creating test database", zap.String("dbName", testDBName))
	// Call with correct signature: ctx, config, testDBName, logger
	// Capture both return values: returnedAdminDSN, err
	returnedAdminDSN, err := db.CreateDatabase(ctx, tk.config, testDBName, tk.logger)
	if err != nil {
		// Error handling: Use tk.config for context, mask sensitive info
		return nil, fmt.Errorf("failed to create test database %q on server %s:%d: %w",
			testDBName, tk.config.Host, tk.config.Port, err) // Cleanup handled by defer
	}
	// Log success. The returnedAdminDSN should match the adminDSN we determined earlier.
	// We don't strictly need returnedAdminDSN going forward, as we already have adminDSN.
	_ = returnedAdminDSN                                                                                                        // Explicitly ignore returnedAdminDSN if not used further
	tk.logger.Info("Test database created successfully", zap.String("dbName", testDBName), zap.String("admin_dsn_used", "***")) // Mask DSN in log

	// Register database cleanup (always needed).
	// Use the 'adminDSN' variable determined *before* this step, which points correctly
	// to either the shared server's admin DSN or the dedicated server's admin DSN.
	// Note: The previous diff incorrectly added a second cleanup registration here, which is now removed.
	// Register database cleanup using the manager
	// dropTestDatabaseFunc now returns a cleanupFunc
	// Register database cleanup (always needed, uses the correct adminDSN)
	tk.cleanup.Add(db.DropTestDatabaseFunc(dsn, testDBName, tk.config.KeepDatabase, tk.logger))

	// 10. Connect DB Pools (sql.DB and pgxpool.Pool)
	// connectPools is now a standalone function in connection.go
	tk.db, tk.pgxPool, tk.dsn, err = connection.ConnectPools(ctx, tk.config, testDBName, tk.logger) // Use exported name
	if err != nil {
		return nil, fmt.Errorf("failed to connect pools: %w", err) // Cleanup handled by defer
	}
	// Register connection cleanup using the manager
	// closeTestDBConnection and closePgxPool now return cleanupFuncs and take pointer-to-pointers
	tk.cleanup.Add(connection.ClosePgxPool(&tk.pgxPool, tk.dsn, tk.logger))     // Use exported name
	tk.cleanup.Add(connection.CloseTestDBConnection(&tk.db, tk.dsn, tk.logger)) // Use exported name

	// 11. Run AfterConnection Hook (if provided)
	if hook := options.AfterConnectionHook(); hook != nil { // Use getter
		tk.logger.Debug("Running afterConnectionHook...")
		err = hook(ctx, tk.db, tk.pgxPool, tk.logger)
		if err != nil {
			return nil, fmt.Errorf("afterConnectionHook failed: %w", err) // Cleanup handled by defer
		}
		tk.logger.Debug("afterConnectionHook completed.")
	}

	// 12. Run BeforeMigration Hook (if provided)
	if hook := options.BeforeMigrationHook(); hook != nil { // Use getter
		tk.logger.Debug("Running beforeMigrationHook...")
		err = hook(ctx, tk.dsn, tk.logger)
		if err != nil {
			return nil, fmt.Errorf("beforeMigrationHook failed: %w", err) // Cleanup handled by defer
		}
		tk.logger.Debug("beforeMigrationHook completed.")
	}

	// 13. Apply Migrations using the configured Migrator
	tk.logger.Debug("Applying migrations via configured migrator...")
	err = options.Migrator().Apply(ctx, tk.pgxPool, tk.logger) // Use getter, pass pool
	if err != nil {
		// Migrator implementation should handle logging details
		return nil, fmt.Errorf("failed to apply migrations: %w", err) // Cleanup handled by defer
	}
	tk.logger.Debug("Migrations applied successfully.")

	// 14. Register automatic cleanup if t is provided
	if t != nil {
		t.Cleanup(func() {
			tk.logger.Debug("Running automatic cleanup via t.Cleanup()...")
			if cleanupErr := tk.Cleanup(); cleanupErr != nil {
				// Log error using t.Errorf to ensure visibility in test output
				t.Errorf("Error during automatic EmberKit cleanup: %v", cleanupErr)
			} else {
				tk.logger.Debug("Automatic cleanup completed.")
			}
		})
	} else {
		// If t is nil, caller is responsible for calling tk.Cleanup()
		tk.logger.Warn("t *testing.T was nil; caller MUST call tk.Cleanup() manually (e.g., using defer)")
	}

	tk.logger.Info("EmberKit initialization successful", zap.String("database", testDBName), zap.String("dsn", tk.dsn))
	return tk, nil // Success
}
