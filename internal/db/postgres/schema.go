package postgres

import (
	"context"

	"github.com/chinhtran52/queryme/internal/db"
)

// schemaSQL lists user tables, views and their columns in order, with primary and foreign keys.
const schemaSQL = `
SELECT n.nspname, c.relname, a.attname, format_type(a.atttypid, a.atttypmod),
       EXISTS (SELECT 1 FROM pg_constraint p
               WHERE p.conrelid = c.oid AND p.contype = 'p' AND a.attnum = ANY (p.conkey)),
       COALESCE((SELECT fn.nspname || '.' || fc.relname || '.' || fa.attname
                 FROM pg_constraint f
                 JOIN pg_class fc ON fc.oid = f.confrelid
                 JOIN pg_namespace fn ON fn.oid = fc.relnamespace
                 JOIN pg_attribute fa ON fa.attrelid = f.confrelid
                                     AND fa.attnum = f.confkey[array_position(f.conkey, a.attnum)]
                 WHERE f.conrelid = c.oid AND f.contype = 'f' AND a.attnum = ANY (f.conkey)
                 LIMIT 1), '')
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f')
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname NOT LIKE 'pg_toast%'
  AND n.nspname NOT LIKE 'pg_temp%'
ORDER BY n.nspname, c.relname, a.attnum`

func (d *Driver) Schema(ctx context.Context) (*db.Schema, error) {
	schema := &db.Schema{}
	if err := d.pool.QueryRow(ctx, "SELECT version()").Scan(&schema.Version); err != nil {
		return nil, err
	}
	rows, err := d.pool.Query(ctx, schemaSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var nsp, table string
		var col db.Column
		if err := rows.Scan(&nsp, &table, &col.Name, &col.Type, &col.PrimaryKey, &col.References); err != nil {
			return nil, err
		}
		if n := len(schema.Tables); n == 0 || schema.Tables[n-1].Schema != nsp || schema.Tables[n-1].Name != table {
			schema.Tables = append(schema.Tables, db.Table{Schema: nsp, Name: table})
		}
		last := &schema.Tables[len(schema.Tables)-1]
		last.Columns = append(last.Columns, col)
	}
	return schema, rows.Err()
}
