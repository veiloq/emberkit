package atlas

import (
	"go.uber.org/zap"

	"github.com/veiloq/emberkit/config"
)

// WithAtlas configures the EmberKit to use Atlas for migrations.
// It uses the HCL path specified by WithAtlasHCLPath (or the default "atlas.hcl").
// The logger passed to NewAtlasMigrator here is temporary; the actual logger
// from the EmberKit instance will be used during the Apply phase.
func WithAtlas() config.Option {
	return func(opts *config.Settings) {
		// Create the AtlasMigrator here. It needs the HCL path.
		// The logger isn't strictly needed at creation time anymore,
		// but we pass a discard logger to satisfy the NewAtlasMigrator signature
		// if it were still required. The real logger is used in Apply.
		// NOTE: Assuming NewAtlasMigrator is updated to not require logger at creation,
		// or handles a nil logger gracefully. If not, this needs adjustment.
		// Based on the previous step, NewAtlasMigrator takes hclPath and logger.
		// We'll create a temporary discard logger here.
		tempLogger := zap.NewNop() // Or zap.NewDevelopment() for debugging option setup
		// opts.migrator is of type migration.Migrator
		// Assuming KitOptions has methods to access/set these fields
		opts.SetMigrator(NewAtlasMigrator(opts.AtlasHCLPath(), tempLogger))
		// The actual logger from EmberKit will be passed during migrator.Apply()
	}
}
