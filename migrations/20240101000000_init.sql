-- emberkit/migrations/20240101000000_init.sql
CREATE TABLE IF NOT EXISTS test_items (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);