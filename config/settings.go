package config

import (
	"context"
	"database/sql"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/veiloq/emberkit/migration"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Settings holds configuration applied via functional options.
type Settings struct {
	atlasHCLPath        string             // Path to the atlas.hcl file
	migrator            migration.Migrator // Migrator instance (defaults to NoOpMigrator)
	keepDatabase        bool               // Explicitly keep the database via option
	sqlTxOptions        *sql.TxOptions     // Custom transaction options for database/sql
	pgxTxOptions        pgx.TxOptions      // Custom transaction options for pgx
	dsnParams           map[string]string  // Additional DSN parameters
	startupParams       map[string]string  // Additional server startup parameters (ignored if using shared server)
	zapOptions          []zap.Option       // Options for zap logger creation (e.g., zap.AddCaller(false))
	zapTestLevel        *zap.AtomicLevel   // Specific level for zaptest logger (e.g., zap.NewAtomicLevelAt(zap.WarnLevel))
	beforeMigrationHook func(ctx context.Context, dsn string, logger *zap.Logger) error
	afterConnectionHook func(ctx context.Context, db *sql.DB, pool *pgxpool.Pool, logger *zap.Logger) error

	// Shared server options
	// useSharedServer, when true, instructs emberkit to connect to a pre-existing, shared
	// PostgreSQL server instance instead of starting and managing its own temporary server.
	// This is typically used via the WithSharedServer option. Using a shared server can
	// speed up test setup significantly but requires external management of the server's
	// lifecycle. Database-level isolation between tests is maintained as emberkit creates
	// a unique, randomly named database on the shared server for each test kit instance.
	// Startup parameters (WithStartupParams) are ignored when using a shared server.
	useSharedServer bool
	dsn             string // DSN for the shared server's admin connection (e.g., to 'postgres' db)
	sharedConfig    Config // Config used by the shared server (primarily for Host, Port, User, Pass)
}

// --- Getters ---

func (sts *Settings) AtlasHCLPath() string {
	return sts.atlasHCLPath
}

func (sts *Settings) Migrator() migration.Migrator {
	return sts.migrator
}

func (sts *Settings) BeforeMigrationHook() func(ctx context.Context, dsn string, logger *zap.Logger) error {
	return sts.beforeMigrationHook
}

func (sts *Settings) AfterConnectionHook() func(ctx context.Context, db *sql.DB, pool *pgxpool.Pool, logger *zap.Logger) error {
	return sts.afterConnectionHook
}

func (sts *Settings) ZapTestLevel() *zap.AtomicLevel {
	return sts.zapTestLevel
}

func (sts *Settings) ZapOptions() []zap.Option {
	return sts.zapOptions
}

func (sts *Settings) UseSharedServer() bool {
	return sts.useSharedServer
}

func (sts *Settings) DSN() string {
	return sts.dsn
}

func (sts *Settings) SharedConfig() Config {
	return sts.sharedConfig
}

// --- Setters ---

func (sts *Settings) SetMigrator(m migration.Migrator) {
	sts.migrator = m
}

// Option defines a function type for configuring the test kit.
type Option func(*Settings)

// WithAtlasHCLPath specifies the path to the atlas.hcl configuration file.
func WithAtlasHCLPath(path string) Option {
	return func(sts *Settings) { sts.atlasHCLPath = path }
}

// WithAtlas configures the EmberKit to use Atlas for migrations.
// It uses the HCL path specified by WithAtlasHCLPath (or the default "atlas.hcl").
// The logger passed to NewAtlasMigrator here is temporary; the actual logger
// WithKeepDatabase prevents the test database from being dropped during cleanup.
func WithKeepDatabase() Option {
	return func(sts *Settings) { sts.keepDatabase = true }
}

// WithSQLTxOptions provides custom transaction options for database/sql tests.
func WithSQLTxOptions(txsts *sql.TxOptions) Option {
	return func(sts *Settings) { sts.sqlTxOptions = txsts }
}

// WithPgxTxOptions provides custom transaction options for pgx tests.
func WithPgxTxOptions(txsts pgx.TxOptions) Option {
	return func(sts *Settings) { sts.pgxTxOptions = txsts }
}

// WithZapOptions provides additional options for the zap logger.
func WithZapOptions(zapsts ...zap.Option) Option {
	return func(sts *Settings) { sts.zapOptions = append(sts.zapOptions, zapsts...) }
}

// WithZapTestLevel sets the minimum log level specifically for the zaptest logger.
func WithZapTestLevel(level zapcore.Level) Option {
	return func(sts *Settings) {
		atomicLevel := zap.NewAtomicLevelAt(level)
		sts.zapTestLevel = &atomicLevel
	}
}

// WithDSNParams provides additional parameters to be appended to the DSN.
func WithDSNParams(params map[string]string) Option {
	return func(sts *Settings) {
		if sts.dsnParams == nil {
			sts.dsnParams = make(map[string]string)
		}
		for k, v := range params {
			sts.dsnParams[k] = v
		}
	}
}

// WithStartupParams provides additional parameters for the PostgreSQL server startup.
func WithStartupParams(params map[string]string) Option {
	return func(sts *Settings) {
		if sts.startupParams == nil {
			sts.startupParams = make(map[string]string)
		}
		for k, v := range params {
			sts.startupParams[k] = v
		}
	}
}

// WithBeforeMigrationHook registers a function to run before migrations are applied.
func WithBeforeMigrationHook(hook func(ctx context.Context, dsn string, logger *zap.Logger) error) Option {
	return func(sts *Settings) { sts.beforeMigrationHook = hook }
}

// WithAfterConnectionHook registers a function to run after database connections (sql.DB, pgxpool.Pool) are established.
func WithAfterConnectionHook(hook func(ctx context.Context, db *sql.DB, pool *pgxpool.Pool, logger *zap.Logger) error) Option {
	return func(sts *Settings) { sts.afterConnectionHook = hook }
}

// WithSharedServer configures the EmberKit to use a pre-existing shared server instance.
// It provides the necessary admin DSN and the configuration that was used to start the shared server.
// When this option is used, NewEmberKit will skip starting/stopping its own server instance.
func WithSharedServer(dsn string, cfg Config) Option {
	return func(sts *Settings) {
		sts.useSharedServer = true
		sts.dsn = dsn
		sts.sharedConfig = cfg // Store the config of the shared server
	}
}

// applyOptions processes functional options and merges them into an initial Config.
// It returns the processed Settings struct and the final merged Config.
func ApplyOptions(initialConfig *Config, options ...Option) (*Settings, Config) {
	// Initialize with defaults, including the NoOpMigrator
	settings := &Settings{
		atlasHCLPath:  "atlas.hcl",               // Default HCL path
		migrator:      &migration.NoOpMigrator{}, // Default to no-op migrations
		dsnParams:     make(map[string]string),
		startupParams: make(map[string]string),
		zapOptions:    make([]zap.Option, 0),
	}
	for _, opt := range options {
		opt(settings)
	}

	// Start with a copy of the initial config
	finalConfig := *initialConfig

	// Merge DSN Params (options override config)
	mergedDSNParams := make(map[string]string)
	for k, v := range finalConfig.DSNParams {
		mergedDSNParams[k] = v
	}
	for k, v := range settings.dsnParams {
		mergedDSNParams[k] = v // Option overrides
	}
	finalConfig.DSNParams = mergedDSNParams

	// Merge Startup Params (options override config)
	mergedStartupParams := make(map[string]string)
	for k, v := range finalConfig.StartupParams {
		mergedStartupParams[k] = v
	}
	for k, v := range settings.startupParams {
		mergedStartupParams[k] = v // Option overrides
	}
	finalConfig.StartupParams = mergedStartupParams

	// Migrator is set directly by WithAtlas or defaults to NoOpMigrator.
	// Remove the old UseAtlas logic.

	// Determine final KeepDatabase setting (config OR option enables it)
	finalConfig.KeepDatabase = finalConfig.KeepDatabase || settings.keepDatabase // Use getter if available

	// Copy transaction options from functional options to config
	if settings.sqlTxOptions != nil { // Use getter if available
		finalConfig.SQLTxOptions = settings.sqlTxOptions
	}
	if settings.pgxTxOptions != (pgx.TxOptions{}) { // Use getter if available
		finalConfig.PgxTxOptions = settings.pgxTxOptions
	}

	return settings, finalConfig
}
