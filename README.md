# emberkit - Go PostgreSQL Integration Testing Toolkit

`emberkit` is a Go testing toolkit designed to simplify integration testing with PostgreSQL. It manages embedded PostgreSQL database instances, providing a clean and isolated environment for each test run, ensuring reliable and repeatable tests without external dependencies.

## Features

*   **Isolated Environments:** Starts an embedded PostgreSQL instance or uses a shared one, creating a unique database for each test.
*   **Automatic Cleanup:** Integrates with `testing.T` to automatically stop the server, drop the test database, and clean up runtime files.
*   **Migration Support (OPTIONAL):** Built-in support for [Atlas](https://github.com/ariga/atlas) migrations and a flexible `Migrator` interface for custom solutions.
*   **Transaction Helpers:** Provides `RunTx` and `RunSQLTx` helpers to execute test logic within automatically rolled-back transactions (`pgx` and `database/sql`).
*   **Connection Pooling:** Offers ready-to-use `*sql.DB` and `*pgxpool.Pool` connection pools.
*   **Customization Hooks:** Allows injecting custom logic before migrations (`WithBeforeMigrationHook`) and after connection (`WithAfterConnectionHook`).
*   **Configurable:** Uses functional options for easy configuration of ports, versions, logging, transaction options, etc.

## Installation

Pre-compiled binaries for various operating systems are available on the [GitHub Releases page](https://github.com/veiloq/emberkit/releases). Please download the binary corresponding to the latest release version suitable for your system.

Alternatively, if you have Go installed, you can add the library to your project (primarily for use as a dependency in your tests) using:

```sh
# Get the latest version
go get github.com/veiloq/emberkit@latest

# Or to get a specific version (vMAJOR.YYYYMM.PATCH):
go get github.com/veiloq/emberkit@v1.202504.2
```

Note: `emberkit` is intended to be used as a library within your Go tests, not as a standalone command-line tool.

## Basic Usage

Here's a basic example demonstrating how to use `emberkit` in a Go test:

```go
package main_test

import (
	"context"
	"database/sql" // Needed for RunSQLTx example
	"testing"

	"github.com/jackc/pgx/v5" // Needed for RunTx example
	"github.com/stretchr/testify/require"
	"github.com/veiloq/emberkit/atlas" // If using Atlas migrations
	"github.com/veiloq/emberkit/config"
	"github.com/veiloq/emberkit/kit"
)

// TestMyFeatureWithEmberKit demonstrates basic usage of EmberKit.
func TestMyFeatureWithEmberKit(t *testing.T) {
	// 1. Configure EmberKit (often defaults are sufficient)
	// Use default config as a base
	cfg := config.DefaultConfig()
	// Customize if needed:
	// cfg.KeepDatabase = true

	// 2. Initialize EmberKit, potentially with options like Atlas integration
	// Use context.Background() or a test-specific context
	// Pass 't' to enable automatic cleanup via t.Cleanup()
	tk, err := kit.NewEmberKit(context.Background(), t, cfg, atlas.WithAtlas())
	require.NoError(t, err, "Failed to initialize EmberKit")
	// Cleanup is now automatic because 't' was passed to NewEmberKit

	// --- Example Test Case 1: Using pgx.Tx ---
	t.Run("CreateAndQueryUserWithPgx", func(t *testing.T) {
		tk.RunTx(ctx, t, func(ctx context.Context, tx pgx.Tx) error { // Pass context to RunTx
			// Assume 'users' table exists due to Atlas migrations
			userName := "Alice"
			userEmail := "alice@example.com"

			// Insert a new user
			_, err := tx.Exec(ctx, "INSERT INTO users (name, email) VALUES ($1, $2)", userName, userEmail)
			require.NoError(t, err, "Failed to insert user")

			// Verify the user was inserted
			var count int
			err = tx.QueryRow(ctx, "SELECT COUNT(*) FROM users WHERE email = $1", userEmail).Scan(&count)
			require.NoError(t, err, "Failed to query user count")
			require.Equal(t, 1, count, "Expected exactly one user with email %s", userEmail)

			// Retrieve the user's ID (example of querying data)
			var userID int
			err = tx.QueryRow(ctx, "SELECT id FROM users WHERE email = $1", userEmail).Scan(&userID)
			require.NoError(t, err, "Failed to query user ID")
			require.Positive(t, userID, "User ID should be positive")
			return nil // Indicate success within the transaction function
		})
		// Transaction is automatically rolled back here
	})

	// --- Example Test Case 2: Using *sql.Tx ---
	t.Run("UpdateUserWithSqlTx", func(t *testing.T) {
		// Setup: Insert a user first (in a separate transaction for clarity)
		initialEmail := "bob@example.com"
		tk.RunTx(ctx, t, func(ctx context.Context, tx pgx.Tx) error { // Pass context
			_, err := tx.Exec(ctx, "INSERT INTO users (name, email) VALUES ($1, $2)", "Bob", initialEmail)
			require.NoError(t, err)
			return nil
		})

		// Now, test updating the user within a *sql.Tx
		tk.RunSQLTx(ctx, t, func(ctx context.Context, tx *sql.Tx) error { // Pass context
			newName := "Robert"

			// Update the user's name
			result, err := tx.ExecContext(ctx, "UPDATE users SET name = $1 WHERE email = $2", newName, initialEmail)
			require.NoError(t, err, "Failed to update user name")

			rowsAffected, err := result.RowsAffected()
			require.NoError(t, err, "Failed to get rows affected")
			require.Equal(t, int64(1), rowsAffected, "Expected one row to be updated")

			// Verify the name was updated
			var updatedName string
			err = tx.QueryRowContext(ctx, "SELECT name FROM users WHERE email = $1", initialEmail).Scan(&updatedName)
			require.NoError(t, err, "Failed to query updated name")
			require.Equal(t, newName, updatedName, "Expected name to be updated")
			return nil // Indicate success
		})
		// Transaction is automatically rolled back here
	})
}
```

This example shows how to set up `emberkit`, ensure cleanup, and run test logic within isolated database transactions using both `pgx` and standard `database/sql` interfaces.

## Customization

EmberKit provides hooks and interfaces to customize its behavior during setup.

### Using Hooks

You can inject custom logic at specific points in the setup process using hooks provided via functional options:

*   **`WithBeforeMigrationHook`**: Runs after the test database is created but *before* any migrations are applied. Useful for seeding initial data or setting up database extensions.
*   **`WithAfterConnectionHook`**: Runs *after* the `*sql.DB` and `*pgxpool.Pool` connections are established but before migrations. Useful for preparing connection-specific settings (e.g., `SET TIME ZONE`) or registering custom types.

```go
package main_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/veiloq/emberkit/config"
	"github.com/veiloq/emberkit/kit"
	"go.uber.org/zap"
)

func TestWithHooks(t *testing.T) {
	ctx := context.Background()

	beforeHook := func(ctx context.Context, dsn string, logger *zap.Logger) error {
		logger.Info("Running before migration hook!", zap.String("dsn_prefix", dsn[:15]+"..."))
		// Example: Could connect using dsn here to create extensions or seed static data
		// Note: Migrations haven't run yet.
		return nil
	}

	afterHook := func(ctx context.Context, db *sql.DB, pool *pgxpool.Pool, logger *zap.Logger) error {
		logger.Info("Running after connection hook!")
		// Example: Set session parameters or register custom types with the pool
		_, err := pool.Exec(ctx, "SET TIME ZONE 'UTC';")
		return err
	}

	opts := []config.Option{
		config.WithBeforeMigrationHook(beforeHook),
		config.WithAfterConnectionHook(afterHook),
		// config.WithAtlas(), // Add other options like migrator if needed
	}

	// Use default config, but provide options
	k, err := kit.NewEmberKit(ctx, t, config.DefaultConfig(), opts...) // Pass t for auto-cleanup
	require.NoError(t, err, "Failed to initialize EmberKit with hooks")
	// Cleanup is automatic via t.Cleanup

	// Test logic can now assume hooks have run
	k.RunTx(ctx, t, func(ctx context.Context, tx pgx.Tx) error { // Pass context
		var timeZone string
		err := tx.QueryRow(ctx, "SHOW TIME ZONE;").Scan(&timeZone)
		require.NoError(t, err)
		require.Equal(t, "UTC", timeZone, "Timezone should be set by afterHook")
		return nil
	})
}
```

### Skipping Migrations (Default)

By default, if you do not provide a specific migrator option (like `atlas.WithAtlas()` or a custom migrator), `emberkit` uses the `migration.NoOpMigrator`. This means no migrations will be applied, and the test database will be created empty (containing only default PostgreSQL objects).

This is useful if your test setup involves creating the schema dynamically within the test itself or if you are testing against a pre-existing schema structure.

```go
func TestWithoutMigrations(t *testing.T) {
	ctx := context.Background()

	// No migrator options are provided
	opts := []config.Option{
		// Add other options like hooks if needed
	}

	// Initialize using default config and no specific migrator
	k, err := kit.NewEmberKit(ctx, t, config.DefaultConfig(), opts...) // Pass t
	require.NoError(t, err)
	// Cleanup is automatic via t.Cleanup

	// The database connected via k.Pool() or k.DB() will be empty.
	// You might create tables/schema here if needed for the test.
	k.RunTx(ctx, t, func(ctx context.Context, tx pgx.Tx) error { // Pass context
		// Example: Check that a table expected from migrations does NOT exist
		_, err := tx.Exec(ctx, "SELECT 1 FROM users LIMIT 1")
		require.Error(t, err) // Expect an error because 'users' table shouldn't exist
		require.Contains(t, err.Error(), "relation \"users\" does not exist")
		return nil
	})
}
```

### Custom Migrator

While `emberkit` provides built-in support for [Atlas](https://github.com/ariga/atlas) migrations (`atlas.WithAtlas()`) and defaults to no migrations (`migration.NoOpMigrator`), you can provide your own implementation of the `migration.Migrator` interface.

```go
// 1. Define your custom migrator
type MyCustomMigrator struct {
	// ... any fields needed ...
}

func (m *MyCustomMigrator) Apply(ctx context.Context, pool *pgxpool.Pool, logger *zap.Logger) error {
	// This Apply method is called during NewEmberKit initialization, after
	// the BeforeMigrationHook (if any) and after the DB connections are ready.
	logger.Info("Applying migrations using MyCustomMigrator...")
	// Implement your custom migration logic here.
	// Example: Run specific SQL commands or use another migration tool.
	_, err := pool.Exec(ctx, "-- Your custom migration SQL here --")
	return err
}

// 2. Create a functional option to set the migrator
func WithMyMigrator(migrator *MyCustomMigrator) config.Option {
	return func(opts *config.Settings) { opts.SetMigrator(migrator) }
}

// 3. Use the option during initialization
func TestWithCustomMigrator(t *testing.T) {
	ctx := context.Background()
	myMigrator := &MyCustomMigrator{}
	opts := []config.Option{
		WithMyMigrator(myMigrator),
	}
	k, err := kit.NewEmberKit(ctx, t, config.DefaultConfig(), opts...) // Pass t
	require.NoError(t, err)
	// ... rest of test ...
}
```

This allows complete control over the migration process if the built-in options don't fit your needs.

## Versioning

This project uses Calendar Versioning (CalVer) with the format `vMAJOR.YYYYMM.PATCH`.

*   `MAJOR`: Incremented for breaking changes.
*   `YYYYMM`: Represents the year and month of the release.
*   `PATCH`: Incremented for bug fixes and minor changes within the same month.

## Default Storage Locations

By default, `emberkit` uses the following locations:

*   **Runtime Data:** A unique temporary directory is created within `./.emberkit` (relative to the current working directory) for each dedicated test server instance. This directory is automatically removed during cleanup unless configured otherwise.
*   **PostgreSQL Binaries:** If not specified via the `BinariesPath` configuration option, the required PostgreSQL binaries are downloaded and cached by the underlying `embedded-postgres` library, typically within `~/.embedded-postgres-go/` in the user's home directory.
*   **Logs:** Internal `emberkit` logs and raw PostgreSQL output default to `os.Stderr`. If `NewEmberKit` is initialized with a `*testing.T`, internal logs use the test runner's logger (`zaptest`).

## Project Structure

```
.
├── .gitignore           # Git ignore patterns
├── .goreleaser.yml      # GoReleaser configuration for releases
├── atlas.hcl            # Atlas HCL configuration (optional, for schema management)
├── emberkit_test.go     # Top-level tests for the EmberKit struct
├── emberkit.go          # Deprecated entry point (use kit.go)
├── go.mod               # Go module definition
├── go.sum               # Go module checksums
├── kit.go               # Main entry point: Defines the EmberKit struct and core methods
├── LICENSE              # Project license (MIT)
├── README.md            # This file
├── sqlc.yaml            # sqlc configuration (optional, for code generation from SQL)
├── atlas/               # [Atlas](https://github.com/ariga/atlas) migration tool integration
│   ├── atlas_test.go    # Tests for Atlas integration
│   ├── atlas.go         # Atlas migrator implementation
│   └── options.go       # Configuration options specific to Atlas
├── config/              # Configuration management for EmberKit
│   ├── config.go        # Defines the main Config struct
│   └── options.go       # Functional options for configuring EmberKit
├── connection/          # Database connection management
│   ├── connection.go    # Handles connection string generation and details
│   └── port.go          # Dynamic port allocation for isolated instances
├── db/                  # Core database server management
│   ├── database.go      # Functions for creating/dropping test databases
│   └── server.go        # Manages the embedded PostgreSQL server process
├── internal/            # Internal helper packages
│   ├── cleanup/         # Resource cleanup logic (temp dirs, server process)
│   │   └── cleanup.go
│   └── logger/          # Internal logging setup
│       └── logger.go
├── migration/           # Generic migration interface and runner
│   └── migrator.go      # Defines the Migrator interface
├── migrations/          # Default directory for database migration files
│   ├── 20240101000000_init.sql # Example initial migration
│   └── atlas.sum        # Atlas migration checksum file
└── snippets/            # Example usage snippets (used in tests/documentation)
    ├── db.go            # Generated by sqlc
    ├── models.go        # Generated by sqlc
    ├── queries.sql      # Example SQL queries for sqlc
    └── queries.sql.go   # Go code generated by sqlc from queries.sql
