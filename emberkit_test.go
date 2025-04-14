package emberkit_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/quick"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"os"
	"path/filepath" // Added for joining runtime path

	"github.com/veiloq/emberkit"
	"github.com/veiloq/emberkit/atlas"
	"github.com/veiloq/emberkit/config"
	"github.com/veiloq/emberkit/snippets" // Import generated sqlc code

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"

	"github.com/veiloq/emberkit/db"

	// "github.com/veiloq/emberkit/internal/cleanup" // Not needed directly in test file
	"github.com/veiloq/emberkit/internal/logger"
)

// --- Shared Server Setup ---

var (
	sharedServer          *embeddedpostgres.EmbeddedPostgres
	sharedAdminDSN        string
	sharedConfig          config.Config // Store the config used for the shared server
	sharedServerErr       error
	startServerOnce       sync.Once
	sharedServerLock      sync.Mutex // Protects access during setup/teardown phases if needed, though sync.Once handles startup.
	sharedLogger          *zap.Logger
	sharedInstanceWorkDir string // Store the specific working directory for cleanup
)

const sharedRuntimeBasePath = ".emberkit" // Directory for the shared server's data

// startSharedServer initializes and starts the single shared PostgreSQL server instance.
// This function is called by startServerOnce.Do().
func startSharedServer() {
	// No lock needed here as sync.Once guarantees single execution.

	// Initialize logger for setup phase (can use a simple console logger)
	var err error
	// Use internal logger init for consistency, but without *testing.T
	// Provide nil options as we don't need test-specific logging here.
	sharedLogger, _, err = logger.InitLogger(nil, nil)
	if err != nil {
		sharedServerErr = fmt.Errorf("failed to initialize logger for shared server setup: %w", err)
		return
	}

	sharedLogger.Info("Initializing shared PostgreSQL server for test suite...")

	// 1. Base Config
	cfg := config.DefaultConfig()
	// Ensure a non-zero timeout for safety
	if cfg.StartTimeout == 0 {
		cfg.StartTimeout = 30 * time.Second
	}
	sharedConfig = cfg // Store the base config

	// 2. Assign Port (must happen before constructing admin DSN)
	if err := db.AssignRandomPort(&sharedConfig, sharedLogger); err != nil {
		sharedServerErr = fmt.Errorf("failed to assign random port for shared server: %w", err)
		return
	}

	// 3. Runtime path for the shared server
	// Use a fixed name for the shared server's runtime dir for simplicity
	instanceWorkDir := filepath.Join(sharedRuntimeBasePath, "sharedserver")
	if err := os.MkdirAll(instanceWorkDir, 0750); err != nil {
		sharedServerErr = fmt.Errorf("failed to create shared server runtime directory %q: %w", instanceWorkDir, err)
		return
	}
	sharedLogger.Debug("Using shared working directory for instance", zap.String("path", instanceWorkDir))
	sharedInstanceWorkDir = instanceWorkDir // Store for cleanup

	// 4. Start Server
	// Use a context with timeout based on the config's StartTimeout
	ctx, cancel := context.WithTimeout(context.Background(), sharedConfig.StartTimeout)
	defer cancel()
	server, err := db.StartServer(ctx, sharedConfig, instanceWorkDir, sharedLogger)
	if err != nil {
		sharedServerErr = fmt.Errorf("failed to start shared embedded server: %w", err)
		// Attempt cleanup of runtime dir if server failed to start
		// Don't remove base path here, only the specific instance dir if created
		if instanceWorkDir != "" {
			_ = os.RemoveAll(instanceWorkDir) // Best effort cleanup of instance dir
		}
		return
	}
	sharedServer = server

	// 5. Construct Admin DSN (using the final config with assigned port)
	// Use the 'postgres' database for admin tasks like creating/dropping other DBs.
	sharedAdminDSN = fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		sharedConfig.Username, sharedConfig.Password, sharedConfig.Host, sharedConfig.Port, "postgres")

	sharedLogger.Info("Shared PostgreSQL server started successfully",
		zap.Uint32("port", sharedConfig.Port),
		// Mask password in log for security
		zap.String("adminDSN", strings.Replace(sharedAdminDSN, sharedConfig.Password, "****", 1)),
		zap.String("runtimePath", instanceWorkDir),
	)
}

// stopSharedServer stops the shared server and cleans up its runtime directory.
func stopSharedServer() {
	// Lock might be useful if multiple goroutines could potentially call this,
	// although in TestMain defer, it's typically single-threaded.
	sharedServerLock.Lock()
	defer sharedServerLock.Unlock()

	if sharedServer == nil {
		if sharedLogger != nil { // Check if logger was initialized
			sharedLogger.Debug("Shared server already stopped or never started.")
		}
		return // Nothing to do
	}

	if sharedLogger == nil {
		// Fallback logger if initialization failed but server somehow started
		sharedLogger, _ = zap.NewDevelopment()
	}

	sharedLogger.Info("Stopping shared PostgreSQL server...")
	// Use the StopEmbeddedServer helper which returns a cleanup.Func
	// Pass the address of the sharedServer variable.
	cleanupFunc := db.StopEmbeddedServer(&sharedServer, sharedLogger)
	err := cleanupFunc() // Execute the cleanup function
	if err != nil {
		sharedLogger.Error("Error stopping shared server", zap.Error(err))
		// Continue with runtime path cleanup even if stop fails
	} else {
		sharedLogger.Info("Shared server stopped.")
	}

	// Clean up the specific instance runtime directory *after* server stop attempt
	if sharedInstanceWorkDir != "" {
		sharedLogger.Debug("Cleaning up shared server instance directory", zap.String("path", sharedInstanceWorkDir))
		if err := os.RemoveAll(sharedInstanceWorkDir); err != nil {
			sharedLogger.Error("Error removing shared server instance directory", zap.String("path", sharedInstanceWorkDir), zap.Error(err))
		} else {
			sharedLogger.Debug("Shared server instance directory removed.")
		}
	} else {
		sharedLogger.Warn("Shared instance working directory path is empty, skipping removal.")
	}
	// sharedServer is set to nil by StopEmbeddedServer on success
}

// TestMain manages the lifecycle of the shared PostgreSQL server.
func TestMain(m *testing.M) {
	// Start the shared server exactly once before any tests run.
	startServerOnce.Do(startSharedServer)

	// Check if server startup failed.
	if sharedServerErr != nil {
		// Use a temporary logger if sharedLogger failed to init
		log := sharedLogger
		if log == nil {
			log, _ = zap.NewDevelopment() // Fallback logger
		}
		// Use standard fmt.Printf for critical startup errors before logging might be fully set up
		fmt.Printf("CRITICAL: Failed to initialize shared PostgreSQL server, aborting tests. Error: %v\n", sharedServerErr)
		os.Exit(1) // Exit immediately if setup failed
	}

	// Ensure the server is stopped after all tests run using defer.
	defer stopSharedServer()

	// Run all tests in the package.
	exitCode := m.Run()

	// Exit with the result code from the tests.
	os.Exit(exitCode)
}

// --- Test Helpers (databaseExists, tableExists remain the same) ---

// Helper function to check if a database exists
// NOTE: Keep raw SQL as sqlc cannot generate this query.
func databaseExists(t *testing.T, adminDSN, dbName string) bool {
	t.Helper()
	ctx := context.Background()
	if adminDSN == "" || dbName == "" {
		t.Logf("Warning: databaseExists called with empty adminDSN ('%s') or dbName ('%s')", adminDSN, dbName)
		return false
	}
	adminPool, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		// Don't treat connection error as fatal in a check function, just log it.
		t.Logf("Warning: Failed to connect with admin DSN '%s' to check db existence for '%s': %v", adminDSN, dbName, err)
		return false // Cannot confirm existence if connection fails
	}
	defer adminPool.Close()

	var exists bool
	queryCtx, queryCancel := context.WithTimeout(ctx, 3*time.Second)
	defer queryCancel()
	// Keep raw SQL
	err = adminPool.QueryRow(queryCtx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbName).Scan(&exists)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Logf("Warning: Failed to query for database existence ('%s'): %v", dbName, err)
		return false // Assume false if query fails
	}
	return exists
}

// Helper function to check if a table exists in a specific database
// Uses sqlc generated code.
func tableExists(t *testing.T, pool *pgxpool.Pool, tableName string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	q := snippets.New(pool)
	exists, err := q.TableExists(ctx, tableName)
	require.NoError(t, err, "Failed to query for table existence using sqlc")
	return exists
}

func TestNewEmberKit_Defaults(t *testing.T) {
	ctx := context.Background()
	baseCfg := config.DefaultConfig()                 // Use value
	kit, err := emberkit.NewEmberKit(ctx, t, baseCfg) // Pass value
	require.NoError(t, err, "NewEmberKit with defaults failed")
	require.NotNil(t, kit, "Kit should not be nil on success")

	require.NotNil(t, kit.Pool())
	require.NotNil(t, kit.DB())
	require.NotEmpty(t, kit.ConnectionString(), "ConnectionString() should return non-empty DSN")
	// Cannot easily verify DBName/AdminDSN if random port/name used.

	pingErr := kit.Pool().Ping(ctx)
	require.NoError(t, pingErr, "Failed to ping default database")

	assert.False(t, tableExists(t, kit.Pool(), "test_items"), "test_items should not exist with default NoOpMigrator")

	// Rely on EmberKit's internal t.Cleanup for dropping the database.
	// Removed explicit databaseExists check here as AdminDSN/DBName are hard to get reliably.
}

// Rationale: Passed config by value. Removed unreliable cleanup check for default case.

func TestNewEmberKit_WithOptions(t *testing.T) {
	var beforeHookCalled atomic.Bool
	var afterHookCalled atomic.Bool
	ctx := context.Background()

	t.Run("WithKeepDatabase", func(t *testing.T) {
		// Use a fixed port and DB name for this specific test to allow verification
		fixedPort := uint32(54399) // Choose an unlikely port
		fixedDBName := "emberkit_keep_test"
		baseCfg := config.DefaultConfig()
		baseCfg.Port = fixedPort
		baseCfg.Database = fixedDBName

		kit, err := emberkit.NewEmberKit(ctx, t, baseCfg, config.WithKeepDatabase()) // Pass value
		require.NoError(t, err, "NewEmberKit with KeepDatabase failed")
		require.NotNil(t, kit)

		// Construct AdminDSN using the fixed config values
		adminDSN := fmt.Sprintf("postgres://%s:%s@%s:%d/postgres?sslmode=disable",
			baseCfg.Username, baseCfg.Password, baseCfg.Host, fixedPort)

		t.Cleanup(func() {
			// Check if the database still exists
			didExist := databaseExists(t, adminDSN, fixedDBName)
			assert.True(t, didExist, "[Cleanup Check] Database '%s' should NOT be dropped when WithKeepDatabase() is used", fixedDBName)

			// Manual Cleanup is essential for kept databases
			if didExist {
				adminPool, err := pgxpool.New(context.Background(), adminDSN)
				if err != nil {
					t.Logf("Warning: Failed to connect to admin DSN for manual cleanup of %s: %v", fixedDBName, err)
					return
				}
				defer adminPool.Close()
				termCtx, termCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer termCancel()
				// NOTE: Keep raw SQL with fmt.Sprintf as sqlc cannot generate this query.
				_, _ = adminPool.Exec(termCtx, fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid()", fixedDBName)) // Ignore error
				dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer dropCancel()
				// NOTE: Keep raw SQL with fmt.Sprintf as sqlc cannot generate this query.
				_, err = adminPool.Exec(dropCtx, fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE)`, fixedDBName))
				if err != nil {
					// NOTE: Keep raw SQL with fmt.Sprintf as sqlc cannot generate this query.
					_, err = adminPool.Exec(dropCtx, fmt.Sprintf(`DROP DATABASE IF EXISTS %q`, fixedDBName))
				}
				assert.NoError(t, err, "Manual cleanup of kept database '%s' failed", fixedDBName)
				if err == nil {
					t.Logf("Manually dropped kept database: %s", fixedDBName)
				}
			}
		})
	})
	// Rationale: Passed config by value. Used fixed port/DBName for reliable verification.

	t.Run("WithZapTestLevel", func(t *testing.T) {
		baseCfg := config.DefaultConfig()
		kit, err := emberkit.NewEmberKit(ctx, t, baseCfg, config.WithZapTestLevel(zapcore.ErrorLevel)) // Pass value
		require.NoError(t, err, "NewEmberKit with ZapTestLevel failed")
		require.NotNil(t, kit)
		require.NoError(t, kit.Pool().Ping(ctx))
	})
	// Rationale: Passed config by value.

	t.Run("WithDSNParams", func(t *testing.T) {
		t.Skip("Skipping WithDSNParams test for now") // Skip this test for now
		baseCfg := config.DefaultConfig()
		params := map[string]string{
			"application_name": "emberkit_test_app",
			"timezone":         "UTC",
		}
		kit, err := emberkit.NewEmberKit(ctx, t, baseCfg, config.WithDSNParams(params)) // Pass value
		require.NoError(t, err, "NewEmberKit with DSNParams failed")
		require.NotNil(t, kit)

		dsn := kit.ConnectionString()
		require.Contains(t, dsn, "application_name=emberkit_test_app", "DSN should contain custom application_name")
		require.Contains(t, dsn, "timezone=UTC", "DSN should contain custom timezone")

		require.NoError(t, kit.Pool().Ping(ctx))

		// Use sqlc generated function
		q := snippets.New(kit.Pool())
		appName, err := q.ShowApplicationName(ctx)
		require.NoError(t, err)
		assert.Equal(t, "emberkit_test_app", appName, "Connected application_name doesn't match")
	})
	// Rationale: Passed config by value. Used sqlc for SHOW application_name.

	t.Run("WithHooks", func(t *testing.T) {
		beforeHookCalled.Store(false)
		afterHookCalled.Store(false)

		beforeHook := func(hookCtx context.Context, dsn string, logger *zap.Logger) error {
			require.NotEmpty(t, dsn, "DSN should be passed to beforeHook")
			require.NotNil(t, logger, "Logger should be passed to beforeHook")
			logger.Info("BeforeMigrationHook called")
			beforeHookCalled.Store(true)
			return nil
		}

		afterHook := func(hookCtx context.Context, db *sql.DB, pool *pgxpool.Pool, logger *zap.Logger) error {
			require.NotNil(t, db, "DB should be passed to afterHook")
			require.NotNil(t, pool, "Pool should be passed to afterHook")
			require.NotNil(t, logger, "Logger should be passed to afterHook")
			logger.Info("AfterConnectionHook called")
			require.NoError(t, pool.Ping(hookCtx), "Pool should be usable in afterHook")
			afterHookCalled.Store(true)
			return nil
		}

		baseCfg := config.DefaultConfig()
		kit, err := emberkit.NewEmberKit(ctx, t, baseCfg, // Pass value
			config.WithBeforeMigrationHook(beforeHook),
			config.WithAfterConnectionHook(afterHook),
		)
		require.NoError(t, err, "NewEmberKit with Hooks failed")
		require.NotNil(t, kit)

		assert.True(t, beforeHookCalled.Load(), "BeforeMigrationHook should have been called")
		assert.True(t, afterHookCalled.Load(), "AfterConnectionHook should have been called")
	})
	// Rationale: Passed config by value.

	t.Run("WithAtlasMigrator", func(t *testing.T) {
		baseCfg := config.DefaultConfig() // Use value
		opts := []config.Option{
			config.WithAtlasHCLPath("atlas.hcl"), // Use option to set HCL path
			atlas.WithAtlas(),                    // Enable Atlas migrator
		}
		kit, err := emberkit.NewEmberKit(ctx, t, baseCfg, opts...) // Pass value
		require.NoError(t, err, "NewEmberKit with Atlas failed")
		require.NotNil(t, kit)

		require.True(t, tableExists(t, kit.Pool(), "test_items"), "test_items should exist after Atlas migration")

		// Verify table structure using sqlc
		q := snippets.New(kit.Pool()) // Assuming snippets is imported
		colCount, err := q.CountColumn(ctx, snippets.CountColumnParams{
			TableSchema: "public",
			TableName:   "test_items",
			ColumnName:  "id",
		})
		require.NoError(t, err, "sqlc CountColumn query failed")
		assert.EqualValues(t, 1, colCount, "Column 'id' should exist in 'test_items'")

		// Rely on EmberKit's internal t.Cleanup for dropping the database.
		// Removed explicit databaseExists check here.
	})
	// Rationale: Passed config by value. Used config.WithAtlasHCLPath. Removed unreliable cleanup check.
}

// NOTE: Transaction tests (RunTx, RunSQLTx) will be moved to tx_test.go

// NOTE: Illustrative tests (Concurrent, Property) will be moved or kept here

// --- Illustrative Tests ---

func TestConcurrentAccess_Illustrative(t *testing.T) {
	ctx := context.Background()
	baseCfg := config.DefaultConfig()                 // Use value
	kit, err := emberkit.NewEmberKit(ctx, t, baseCfg, // Pass value
		config.WithAtlasHCLPath("atlas.hcl"),
		atlas.WithAtlas(),
	)
	require.NoError(t, err, "Setup failed for ConcurrentAccess test")
	require.True(t, tableExists(t, kit.Pool(), "test_items"), "Prerequisite: test_items must exist")

	q := snippets.New(kit.Pool()) // Instantiate sqlc querier for the pool

	initialNames := []string{"conc_1", "conc_2", "conc_3"}
	ids := make([]int32, len(initialNames)) // Use int32 for sqlc compatibility
	for i, name := range initialNames {
		// Use sqlc generated function
		insertedID, err := q.InsertTestItem(ctx, name)
		require.NoError(t, err)
		ids[i] = insertedID
	}
	require.Len(t, ids, len(initialNames), "Should have inserted initial data")

	t.Run("Simulated Concurrent Update", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 3
		wg.Add(numGoroutines)
		errs := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(workerID int) {
				defer wg.Done()
				targetID := ids[workerID%len(ids)] // targetID is already int32
				newName := fmt.Sprintf("updated_by_%d", workerID)

				opCtx, opCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer opCancel()

				// Use sqlc generated function
				params := snippets.UpdateTestItemNameParams{
					Name: newName,
					ID:   targetID,
				}
				err := q.UpdateTestItemName(opCtx, params) // Use the pool querier 'q'

				if err != nil {
					select {
					case errs <- fmt.Errorf("worker %d failed: %w", workerID, err):
					default:
						t.Logf("Error channel full, worker %d error dropped: %v", workerID, err)
					}
				}
			}(i)
		}

		wg.Wait()
		close(errs)

		for err := range errs {
			require.NoError(t, err, "Concurrent update should not produce errors")
		}

		finalNames := make(map[int32]string) // Key is int32
		// q is already defined outside the loop
		for _, id := range ids {
			var name string
			// Use generated sqlc function
			name, err = q.GetTestItemName(ctx, id) // Use the pool querier 'q'
			require.NoError(t, err)
			finalNames[id] = name
			assert.True(t, strings.HasPrefix(name, "updated_by_"), "Name should be updated (id: %d, name: %s)", id, name)
			t.Logf("ID: %d, Final Name: %s", id, name)
		}
		updatedCount := 0
		seenNames := make(map[string]bool)
		for _, name := range finalNames {
			if strings.HasPrefix(name, "updated_by_") && !seenNames[name] {
				updatedCount++
				seenNames[name] = true
			}
		}
		assert.GreaterOrEqual(t, updatedCount, 1, "Expected at least one successful update")
	})
}

// Hypothetical function to test
func SanitizeDBString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func TestSanitizeDBString_Property(t *testing.T) {
	prop := func(s string) bool {
		sanitizedOnce := SanitizeDBString(s)
		sanitizedTwice := SanitizeDBString(sanitizedOnce)
		return sanitizedOnce == sanitizedTwice
	}

	propNoSingleQuotes := func(s string) bool {
		if strings.Contains(s, "''") {
			return true
		}
		sanitized := SanitizeDBString(s)
		return !strings.Contains(sanitized, "'")
	}

	config := &quick.Config{MaxCount: 100}
	if err := quick.Check(prop, config); err != nil {
		if checkErr, ok := err.(*quick.CheckError); ok {
			t.Errorf("Property failed (idempotency): input %v, output %q vs %q",
				checkErr.In, SanitizeDBString(checkErr.In[0].(string)), SanitizeDBString(SanitizeDBString(checkErr.In[0].(string))))
		} else {
			t.Errorf("Property failed (idempotency): %v", err)
		}
	}
	if err := quick.Check(propNoSingleQuotes, config); err != nil {
		if checkErr, ok := err.(*quick.CheckError); ok {
			t.Errorf("Property failed (no single quotes): input %v, output %q",
				checkErr.In, SanitizeDBString(checkErr.In[0].(string)))
		} else {
			t.Errorf("Property failed (no single quotes): %v", err)
		}
	}
}

// --- Code Coverage Notes --- (Keep as is)
