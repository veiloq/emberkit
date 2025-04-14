package atlas_test

import (
	"context"
	// Unused imports removed
	// "os" // No longer needed, MkdirAll moved to testkit.go
	"testing"
	"time" // Import time

	// Unused import removed
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/veiloq/emberkit"
	"github.com/veiloq/emberkit/atlas"
	"github.com/veiloq/emberkit/config"
	"github.com/veiloq/emberkit/snippets" // Import generated sqlc code from root
)

// Helper function to check if a table exists (copied for self-containment)
func tableExists(t *testing.T, pool *pgxpool.Pool, tableName string) bool {
	t.Helper()
	// Query moved to snippets/queries.sql (TableExists)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	q := snippets.New(pool) // Use the imported snippets package
	exists, err := q.TableExists(ctx, tableName)
	// sqlc returns the error directly, the boolean indicates existence.
	// No need to check for ErrNoRows specifically for the existence check.
	require.NoError(t, err, "Failed to query for table existence using sqlc")
	return exists
}

func TestAtlasMigrator_IntegrationWithEmberKit(t *testing.T) {
	// This test specifically focuses on the Atlas integration via EmberKit options.
	ctx := context.Background()
	baseCfg := config.DefaultConfig() // Use value

	// Configure Atlas using options
	opts := []config.Option{
		config.WithAtlasHCLPath("../atlas.hcl"), // Set HCL path relative to test execution dir
		atlas.WithAtlas(),                       // Enable Atlas migrator
	}

	kit, err := emberkit.NewEmberKit(ctx, t, baseCfg, opts...) // Pass value
	// The error happens within NewEmberKit during Apply, so we check err first
	if err != nil {
		// If kit creation failed due to migration error, try to get schema state *before* failing
		// This requires a way to get the pool even if NewEmberKit partially failed,
		// or modifying NewEmberKit to return partial state on migration failure.
		// For now, we can only log if kit creation *succeeded* but assertions failed later.
		// Let's adjust the test logic slightly.
		t.Logf("NewEmberKit failed during setup (likely migration): %v", err)
		// Attempt to proceed might panic if kit is nil, so we stop here if setup failed.
		require.NoError(t, err, "NewEmberKit with Atlas options failed")
	}
	require.NotNil(t, kit)

	// --- Debugging with sqlc ---
	t.Logf("--- Debugging Schema State (sqlc) ---")
	q := snippets.New(kit.Pool()) // Use pgxpool.Pool
	tables, queryErr := q.ListPublicTables(ctx)
	if queryErr != nil {
		t.Logf("Error querying schema state via sqlc: %v", queryErr)
	} else {
		if len(tables) == 0 {
			t.Logf("sqlc: No tables found in 'public' schema.")
		}
		for _, table := range tables {
			// Adjust based on sqlc generated struct fields (assuming TableSchema, TableName)
			t.Logf("sqlc: Found table: %s.%s", table.TableSchema, table.TableName)
		}
	}
	t.Logf("--- End Debugging Schema State (sqlc) ---")
	// --- End Debugging ---

	// Verify the table created by the migration exists
	assert.True(t, tableExists(t, kit.Pool(), "test_items"), "test_items should exist after Atlas migration")

	// Optional: Verify table structure using sqlc
	colCount, err := q.CountColumn(ctx, snippets.CountColumnParams{
		TableSchema: "public",
		TableName:   "test_items",
		ColumnName:  "id",
	})
	require.NoError(t, err, "sqlc CountColumn query failed")
	assert.EqualValues(t, 1, colCount, "Column 'id' should exist in 'test_items'")

	// Cleanup is handled automatically by EmberKit's t.Cleanup
}

// Rationale: Tests that enabling Atlas via atlas.WithAtlas() and setting the HCL path
// via config.WithAtlasHCLPath() correctly applies the migrations defined in the project.

func TestAtlasMigrator_NoOpByDefault(t *testing.T) {
	// Verify that without atlas.WithAtlas(), the default NoOpMigrator is used.
	ctx := context.Background()
	baseCfg := config.DefaultConfig() // Use value

	// DO NOT include atlas.WithAtlas() or config.WithAtlasHCLPath()
	kit, err := emberkit.NewEmberKit(ctx, t, baseCfg) // Pass value
	require.NoError(t, err, "NewEmberKit with default migrator failed")
	require.NotNil(t, kit)

	// Verify the table from migrations does NOT exist
	assert.False(t, tableExists(t, kit.Pool(), "test_table"), "test_table should NOT exist when Atlas is not configured")

	// Cleanup is handled automatically
}
