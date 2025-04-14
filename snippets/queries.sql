-- name: ListPublicTables :many
SELECT table_schema, table_name
FROM information_schema.tables
WHERE table_schema = 'public'
ORDER BY table_schema, table_name;

-- name: CountColumn :one
SELECT count(*)::int
FROM information_schema.columns
WHERE table_schema = $1
  AND table_name = $2
  AND column_name = $3;

-- name: TableExists :one
SELECT EXISTS (
    SELECT 1
    FROM information_schema.tables
    WHERE table_schema = 'public' AND table_name = $1
);

-- name: GetTestItemName :one
SELECT name FROM test_items WHERE id = $1;

-- name: InsertTestItem :one
INSERT INTO test_items (name) VALUES ($1) RETURNING id;

-- name: CountTestItemByID :one
SELECT count(*) FROM test_items WHERE id = $1;

-- name: ShowApplicationName :one
SHOW application_name;

-- name: UpdateTestItemName :exec
UPDATE test_items SET name = $1 WHERE id = $2;