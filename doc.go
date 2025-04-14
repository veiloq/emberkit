/*
Package emberkit provides a toolkit for creating isolated PostgreSQL environments
for integration testing in Go.

It simplifies the process of setting up and tearing down dedicated test databases
by managing:

  - Starting an embedded PostgreSQL instance or connecting to a shared server.
  - Creating unique, isolated databases for each test run.
  - Applying database schema migrations (e.g., using Atlas).
  - Providing standard `*sql.DB` and `*pgxpool.Pool` connection pools to the test database.
  - Offering helper functions (`RunTx`, `RunSQLTx`) to execute test logic within
    transactions that are automatically rolled back.
  - Managing the cleanup of resources (stopping the server, dropping the test database,
    closing connections) automatically when integrated with `*testing.T`.

Example Usage (within a test function):

	func TestMyFeature(t *testing.T) {
		ctx := context.Background()
		// Configure EmberKit (e.g., migration source)
		opts := []config.Option{
			// ... your options, e.g., migration.WithAtlas(...)
		}
		kit, err := emberkit.NewEmberKit(ctx, t, config.DefaultConfig, opts...) // Pass t for auto-cleanup
		if err != nil {
			t.Fatalf("Failed to initialize EmberKit: %v", err)
		}
		// kit.Cleanup() is automatically called via t.Cleanup()

		// Use the transaction runner
		kit.RunTx(ctx, t, func(ctx context.Context, tx pgx.Tx) error {
			// Your test logic using the transaction 'tx'
			// ... query, insert, update ...
			// No need to commit; rollback is automatic
			return nil // Return error if something goes wrong
		})

		// Or access the connection pool directly (less common for isolated tests)
		// rows, err := kit.Pool().Query(ctx, "SELECT ...")
		// ...
	}
*/
package emberkit
