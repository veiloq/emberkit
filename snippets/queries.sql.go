// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.28.0
// source: queries.sql

package snippets

import (
	"context"
)

const countColumn = `-- name: CountColumn :one
SELECT count(*)::int
FROM information_schema.columns
WHERE table_schema = $1
  AND table_name = $2
  AND column_name = $3
`

type CountColumnParams struct {
	TableSchema interface{} `db:"table_schema" json:"tableSchema"`
	TableName   interface{} `db:"table_name" json:"tableName"`
	ColumnName  interface{} `db:"column_name" json:"columnName"`
}

func (q *Queries) CountColumn(ctx context.Context, arg CountColumnParams) (int32, error) {
	row := q.db.QueryRow(ctx, countColumn, arg.TableSchema, arg.TableName, arg.ColumnName)
	var column_1 int32
	err := row.Scan(&column_1)
	return column_1, err
}

const countTestItemByID = `-- name: CountTestItemByID :one
SELECT count(*) FROM test_items WHERE id = $1
`

func (q *Queries) CountTestItemByID(ctx context.Context, id int32) (int64, error) {
	row := q.db.QueryRow(ctx, countTestItemByID, id)
	var count int64
	err := row.Scan(&count)
	return count, err
}

const getTestItemName = `-- name: GetTestItemName :one
SELECT name FROM test_items WHERE id = $1
`

func (q *Queries) GetTestItemName(ctx context.Context, id int32) (string, error) {
	row := q.db.QueryRow(ctx, getTestItemName, id)
	var name string
	err := row.Scan(&name)
	return name, err
}

const insertTestItem = `-- name: InsertTestItem :one
INSERT INTO test_items (name) VALUES ($1) RETURNING id
`

func (q *Queries) InsertTestItem(ctx context.Context, name string) (int32, error) {
	row := q.db.QueryRow(ctx, insertTestItem, name)
	var id int32
	err := row.Scan(&id)
	return id, err
}

const listPublicTables = `-- name: ListPublicTables :many
SELECT table_schema, table_name
FROM information_schema.tables
WHERE table_schema = 'public'
ORDER BY table_schema, table_name
`

type ListPublicTablesRow struct {
	TableSchema interface{} `db:"table_schema" json:"tableSchema"`
	TableName   interface{} `db:"table_name" json:"tableName"`
}

func (q *Queries) ListPublicTables(ctx context.Context) ([]ListPublicTablesRow, error) {
	rows, err := q.db.Query(ctx, listPublicTables)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ListPublicTablesRow{}
	for rows.Next() {
		var i ListPublicTablesRow
		if err := rows.Scan(&i.TableSchema, &i.TableName); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const showApplicationName = `-- name: ShowApplicationName :one
SHOW application_name
`

type ShowApplicationNameRow struct {
}

func (q *Queries) ShowApplicationName(ctx context.Context) (ShowApplicationNameRow, error) {
	row := q.db.QueryRow(ctx, showApplicationName)
	var i ShowApplicationNameRow
	err := row.Scan()
	return i, err
}

const tableExists = `-- name: TableExists :one
SELECT EXISTS (
    SELECT 1
    FROM information_schema.tables
    WHERE table_schema = 'public' AND table_name = $1
)
`

func (q *Queries) TableExists(ctx context.Context, tableName interface{}) (bool, error) {
	row := q.db.QueryRow(ctx, tableExists, tableName)
	var exists bool
	err := row.Scan(&exists)
	return exists, err
}

const updateTestItemName = `-- name: UpdateTestItemName :exec
UPDATE test_items SET name = $1 WHERE id = $2
`

type UpdateTestItemNameParams struct {
	Name string `db:"name" json:"name"`
	ID   int32  `db:"id" json:"id"`
}

func (q *Queries) UpdateTestItemName(ctx context.Context, arg UpdateTestItemNameParams) error {
	_, err := q.db.Exec(ctx, updateTestItemName, arg.Name, arg.ID)
	return err
}
