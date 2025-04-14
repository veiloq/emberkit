package config

// Supported PostgreSQL Versions
// Source: https://www.postgresql.org/support/versioning/
// (Add more versions as needed and supported by embedded-postgres)
type PostgresVersion string

const (
	// PostgreSQL 17
	V17_0 PostgresVersion = "17.0.0"
	V17_1 PostgresVersion = "17.1.0"
	V17_2 PostgresVersion = "17.2.0"
	V17_3 PostgresVersion = "17.3.0"
	V17_4 PostgresVersion = "17.4.0"

	// PostgreSQL 16
	V16_0 PostgresVersion = "16.0.0"
	V16_1 PostgresVersion = "16.1.0"
	V16_2 PostgresVersion = "16.2.0"
	V16_3 PostgresVersion = "16.3.0"
	V16_4 PostgresVersion = "16.4.0"
	V16_5 PostgresVersion = "16.5.0"
	V16_6 PostgresVersion = "16.6.0"
	V16_7 PostgresVersion = "16.7.0"
	V16_8 PostgresVersion = "16.8.0"

	// PostgreSQL 15
	V15_0  PostgresVersion = "15.0.0"
	V15_1  PostgresVersion = "15.1.0"
	V15_2  PostgresVersion = "15.2.0"
	V15_3  PostgresVersion = "15.3.0"
	V15_4  PostgresVersion = "15.4.0"
	V15_5  PostgresVersion = "15.5.0"
	V15_6  PostgresVersion = "15.6.0"
	V15_7  PostgresVersion = "15.7.0"
	V15_8  PostgresVersion = "15.8.0"
	V15_9  PostgresVersion = "15.9.0"
	V15_10 PostgresVersion = "15.10.0"
	V15_11 PostgresVersion = "15.11.0"
	V15_12 PostgresVersion = "15.12.0"
)
