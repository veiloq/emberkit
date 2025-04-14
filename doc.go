/*
// Package emberkit provides the core implementation for the pgkit testing toolkit,
// designed to simplify PostgreSQL integration testing in Go.
//
// Use kit.NewEmberKit to create instances. The returned value satisfies the kit.Kit
// interface, which defines the primary user-facing methods.

// It simplifies the process of setting up and tearing down dedicated test databases
// by managing:
//
//   - Starting an embedded PostgreSQL instance or connecting to a shared server.
//   - Creating unique, isolated databases for each test run.
//   - Applying database schema migrations (e.g., using Atlas or custom migrators).
//   - Providing standard `*sql.DB` and `*pgxpool.Pool` connection pools.
//   - Offering helper functions (`RunTx`, `RunSQLTx`) for transactional testing.
//   - Handling automatic resource cleanup when used with `*testing.T`.

Example Usage (within a test function):

	func TestMyFeature(t *testing.T) {
		ctx := context.Background()
		// Configure EmberKit (e.g., migration source)
		opts := []config.Option{
			// ... your options, e.g., migration.WithAtlas(...)
		}
		// Use kit.NewEmberKit and config/atlas packages
		k, err := kit.NewEmberKit(ctx, t, config.DefaultConfig(), opts...) // Pass t for auto-cleanup
		if err != nil {
			t.Fatalf("Failed to initialize EmberKit: %v", err)
		}
		// k.Cleanup() is automatically called via t.Cleanup()

		// Use the transaction runner
		k.RunTx(ctx, t, func(ctx context.Context, tx pgx.Tx) error {
			// Your test logic using the transaction 'tx'
			// ... query, insert, update ...
			// No need to commit; rollback is automatic
			return nil // Return error if something goes wrong
		})

		// Or access the connection pool directly (less common for isolated tests)
		// rows, err := k.Pool().Query(ctx, "SELECT ...")
		// ...
	}
*/
package emberkit
