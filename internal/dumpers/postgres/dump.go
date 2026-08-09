package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log"
	"strings"
	"text/template"
)

type ColumnInfo struct {
	Name       string  `db:"column_name"`
	Type       string  `db:"data_type"`
	IsNullable string  `db:"is_nullable"`
	Default    *string `db:"column_default"`

	NotNull bool `db:"-"`
}

type Table struct {
	TableName string
	Columns   []*ColumnInfo
}

const (
	InformationSchema = "information_schema.columns"
)

const Query = `
SELECT
	column_name
	,data_type
	,is_nullable
	,column_default
FROM
	information_schema.columns
WHERE
	table_name = ?`

var createTableTmpl = `
CREATE TABLE {{.TableName}} (
{{- range $i, $col := .Columns}}
  {{$col.Name}} {{$col.Type}}{{if $col.NotNull}} NOT NULL{{end}}{{if $col.Default}} DEFAULT {{$col.Default}}{{end}}{{if lt (add $i 1) (len $.Columns)}},{{end}}
{{- end}}
);
`

// helpers used in the template
var funcMap = template.FuncMap{
	"join": func(strs []string, sep string) string {
		var out string
		for i, s := range strs {
			if i > 0 {
				out += sep
			}
			out += s
		}
		return out
	},
	"add": func(a, b int) int {
		return a + b
	},
	"len": func(x []*ColumnInfo) int {
		return len(x)
	},
}

func Dump(ctx context.Context, db *sql.DB, tableNames ...string) (string, error) {
	if len(tableNames) == 0 {
		return "", nil
	}

	var tables []*Table
	var err error
	var hasError bool

	for _, table := range tableNames {
		columns := []*ColumnInfo{}
		rows, err := db.QueryContext(ctx, Query, table)
		if err != nil {
			hasError = true
			log.Println("failed to get table schema information. error", err)
			continue
		}

		for rows.Next() {
			var tableInfo ColumnInfo

			if err := rows.Scan(
				&tableInfo.Name,
				&tableInfo.Type,
				&tableInfo.IsNullable,
				&tableInfo.Default,
			); err != nil {

				hasError = true
				log.Println("failed to scan table. error", err)
				continue
			}

			if strings.EqualFold(tableInfo.IsNullable, "NO") {
				tableInfo.NotNull = true
			}

			columns = append(columns, &tableInfo)
		}
		rows.Close()

		tables = append(tables, &Table{
			TableName: table,
			Columns:   columns,
		})
	}

	schemas := []string{}
	for _, table := range tables {
		ddl, err := GenerateDDL(table)
		if err != nil {
			hasError = true
			log.Println("failed to generate ddl. error", err)
			continue
		}

		schemas = append(schemas, ddl)
	}

	if hasError {
		err = errors.New("some operations failed")
	}

	return strings.Join(schemas, "\n"), err
}

func GenerateDDL(t *Table) (string, error) {
	tmpl, err := template.New("createTable").Funcs(funcMap).Parse(createTableTmpl)
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	err = tmpl.Execute(&out, t)
	if err != nil {
		return "", err
	}

	return out.String(), nil
}
