package config

import (
	"database/sql" // Added for sql.TxOptions
	"fmt"

	// "log/slog" // No longer needed directly here
	"os"
	"strings" // Added for validation checks
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/jackc/pgx/v5" // Added for pgx.TxOptions
)

// Config defines the configuration for the embedded PostgreSQL instance.
type Config struct {
	Version  embeddedpostgres.PostgresVersion // e.g., embeddedpostgres.V16
	Host     string                           // Host for the DB to listen on. Defaults to "localhost".
	Port     uint32                           // Port for the DB to listen on. 0 means select a random free port.
	Database string                           // Initial database to create and connect to. Must not be empty.
	Username string                           // Database user. Must not be empty.
	Password string                           // Database password. Must not be empty.
	// RuntimePath removed - now handled internally by EmberKit
	BinariesPath string        // Optional: Path to existing postgres binaries. If empty, downloads.
	StartTimeout time.Duration // How long to wait for Postgres to start. Default 15s.
	Logger       *os.File      // Where to log raw Postgres output. Default os.Stderr. Set to nil to discard.
	// SlogLogger and SlogLevel removed, zap logger is now created internally or passed via options.
	StartupParams map[string]string // Additional server parameters for postgresql.conf
	DSNParams     map[string]string // Additional parameters to append to the DSN (e.g., "search_path=public").
	// UseAtlas removed - Atlas usage is now implicit based on migrator configuration
	KeepDatabase bool           // If true, do not drop the database on cleanup. Default false.
	SQLTxOptions *sql.TxOptions // Custom transaction options for database/sql. Default nil.
	PgxTxOptions pgx.TxOptions  // Custom transaction options for pgx. Default empty struct.
}

// Validate checks if the essential configuration fields are set correctly.
func (c *Config) Validate() error {
	var errs []string
	// Port 0 is now valid, indicating random port selection.
	if c.Database == "" {
		errs = append(errs, "Database must not be empty")
	}
	if c.Username == "" {
		errs = append(errs, "Username must not be empty")
	}
	// Password can technically be empty for some auth methods, but for typical
	// local testing, it's usually required. Let's enforce it.
	if c.Password == "" {
		errs = append(errs, "Password must not be empty")
	}

	if len(errs) > 0 {
		return fmt.Errorf("config validation failed: %s", strings.Join(errs, ", "))
	}
	return nil
}

// DefaultConfig returns a default configuration for the embedded database.
func DefaultConfig() Config {
	return Config{
		Version:       embeddedpostgres.V16, // Default to PostgreSQL 16
		Host:          "localhost",          // Default host
		Port:          0,                    // Default to random port selection
		Database:      "testdb",
		Username:      "testuser",
		Password:      "testpassword",
		StartTimeout:  15 * time.Second,
		StartupParams: map[string]string{
			// Parameters that cannot be set via PGOPTIONS after startup are commented out.
			// "shared_buffers":               "512MB",
			// "effective_cache_size": "1536MB",
			// "work_mem":             "16MB",
			// "maintenance_work_mem": "256MB",
			// "checkpoint_timeout":           "15min",
			// "max_wal_size":                 "2GB",
			// "checkpoint_completion_target": "0.9",
			// "wal_buffers":                  "16MB",
			// "synchronous_commit": "off", // This *can* often be set via PGOPTIONS
			// "full_page_writes":             "off",
			// "fsync":                        "off",
		},
		// RuntimePath removed
		Logger: os.Stderr, // Default raw Postgres logs to Stderr
		// SlogLogger/SlogLevel removed. Default zap logger created in NewEmberKit.
		DSNParams: nil, // No extra DSN params by default
		// UseAtlas removed
		KeepDatabase: false,           // Default to dropping the database
		SQLTxOptions: nil,             // Default transaction options for database/sql
		PgxTxOptions: pgx.TxOptions{}, // Default transaction options for pgx
	}
}

// DSN constructs a DSN string from the Config struct.
// Note: Assumes config.Port has been assigned (either initially or randomly).
func (c *Config) DSN() string {
	host := c.Host
	if host == "" {
		host = "localhost" // Ensure host is not empty
	}
	baseDSN := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		c.Username,
		c.Password,
		host,
		c.Port,
		c.Database,
	)

	// Append additional DSN parameters
	if len(c.DSNParams) > 0 {
		var params []string
		for k, v := range c.DSNParams {
			// Basic escaping might be needed for values in real-world scenarios
			params = append(params, fmt.Sprintf("%s=%s", k, v))
		}
		return baseDSN + "&" + strings.Join(params, "&")
	}

	return baseDSN
}
