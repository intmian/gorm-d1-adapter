package gormd1

import (
	"database/sql"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	d1 "github.com/intmian/gorm-d1-adapter"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

const (
	boolColumnType = "integer"
	legacyBoolType = "string"
)

type Migrator struct {
	migrator.Migrator
}

func (dialector *Dialector) Migrator(db *gorm.DB) gorm.Migrator {
	return Migrator{Migrator: migrator.Migrator{Config: migrator.Config{
		DB:                          db,
		Dialector:                   dialector,
		CreateIndexAfterCreateTable: true,
	}}}
}

// RunWithoutForeignKey is retained for source compatibility. D1 cannot turn
// off foreign key enforcement across requests, so fc runs with enforcement on.
func (m Migrator) RunWithoutForeignKey(fc func() error) error {
	return fc()
}

func (m Migrator) HasTable(value any) bool {
	var count int
	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		return m.DB.Raw("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", stmt.Table).Row().Scan(&count)
	})
	return count > 0
}

func (m Migrator) GetTables() (tableList []string, err error) {
	err = m.DB.Raw("SELECT name FROM sqlite_master WHERE type = ?", "table").Scan(&tableList).Error
	return tableList, err
}

// ColumnTypes reads declared schema metadata instead of relying on
// database/sql rows metadata. D1 query results only contain column names, so
// the generic GORM implementation cannot report types or default values.
func (m Migrator) ColumnTypes(value any) (columnTypes []gorm.ColumnType, err error) {
	err = m.RunWithValue(value, func(stmt *gorm.Statement) error {
		rows, err := m.DB.Raw("PRAGMA table_info(?)", clause.Table{Name: stmt.Table}).Rows()
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var (
				cid          int
				name         string
				declaredType string
				notNull      int
				defaultValue sql.NullString
				primaryKey   int
			)
			if err := rows.Scan(&cid, &name, &declaredType, &notNull, &defaultValue, &primaryKey); err != nil {
				return err
			}

			dataType, length, precision, scale := parseDeclaredType(declaredType)
			columnType := migrator.ColumnType{
				NameValue:         sql.NullString{String: name, Valid: true},
				DataTypeValue:     sql.NullString{String: dataType, Valid: true},
				ColumnTypeValue:   sql.NullString{String: declaredType, Valid: declaredType != ""},
				PrimaryKeyValue:   sql.NullBool{Bool: primaryKey > 0, Valid: true},
				LengthValue:       sql.NullInt64{Valid: true},
				DecimalSizeValue:  sql.NullInt64{Valid: true},
				ScaleValue:        sql.NullInt64{Valid: true},
				NullableValue:     sql.NullBool{Bool: notNull == 0, Valid: true},
				ScanTypeValue:     reflect.TypeOf((*any)(nil)).Elem(),
				DefaultValueValue: normalizeDefaultValue(defaultValue),
			}
			if length >= 0 {
				columnType.LengthValue = sql.NullInt64{Int64: length, Valid: true}
			}
			if precision >= 0 {
				columnType.DecimalSizeValue = sql.NullInt64{Int64: precision, Valid: true}
				columnType.ScaleValue = sql.NullInt64{Int64: scale, Valid: true}
			}
			columnTypes = append(columnTypes, columnType)
		}
		return rows.Err()
	})
	return columnTypes, err
}

func parseDeclaredType(declaredType string) (dataType string, length, precision, scale int64) {
	declaredType = strings.TrimSpace(declaredType)
	dataType = declaredType
	length, precision, scale = -1, -1, -1

	open := strings.LastIndexByte(declaredType, '(')
	if open < 0 || !strings.HasSuffix(declaredType, ")") {
		return dataType, length, precision, scale
	}

	args := strings.Split(declaredType[open+1:len(declaredType)-1], ",")
	first, err := strconv.ParseInt(strings.TrimSpace(args[0]), 10, 64)
	if err != nil {
		return dataType, length, precision, scale
	}
	dataType = strings.TrimSpace(declaredType[:open])
	if len(args) == 1 {
		return dataType, first, precision, scale
	}
	if len(args) == 2 {
		second, err := strconv.ParseInt(strings.TrimSpace(args[1]), 10, 64)
		if err == nil {
			precision, scale = first, second
		}
	}
	return dataType, length, precision, scale
}

func normalizeDefaultValue(value sql.NullString) sql.NullString {
	if !value.Valid || strings.EqualFold(strings.TrimSpace(value.String), "NULL") {
		return sql.NullString{}
	}

	value.String = strings.TrimSpace(value.String)
	if len(value.String) >= 2 {
		quote := value.String[0]
		if (quote == '\'' || quote == '"') && value.String[len(value.String)-1] == quote {
			value.String = value.String[1 : len(value.String)-1]
			doubled := string([]byte{quote, quote})
			value.String = strings.ReplaceAll(value.String, doubled, string(quote))
		}
	}
	return value
}

func (m Migrator) DropTable(values ...any) error {
	values = m.ReorderModels(values, false)
	tx := m.DB.Session(&gorm.Session{})
	for i := len(values) - 1; i >= 0; i-- {
		if err := m.RunWithValue(values[i], func(stmt *gorm.Statement) error {
			return tx.Exec("DROP TABLE IF EXISTS ?", clause.Table{Name: stmt.Table}).Error
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m Migrator) HasColumn(value any, name string) bool {
	var count int
	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema != nil {
			if field := stmt.Schema.LookUpField(name); field != nil {
				name = field.DBName
			}
		}
		if name != "" {
			m.DB.Raw(
				"SELECT count(*) FROM sqlite_master WHERE type = ? AND tbl_name = ? AND (sql LIKE ? OR sql LIKE ? OR sql LIKE ? OR sql LIKE ? OR sql LIKE ?)",
				"table", stmt.Table, `%"`+name+`" %`, `%`+name+` %`, "%`"+name+"`%", "%["+name+"]%", "%\t"+name+"\t%",
			).Row().Scan(&count)
		}
		return nil
	})
	return count > 0
}

// MigrateColumn compares an existing column with the model the same way GORM
// does, with two exceptions for differences SQLite cannot change without
// rebuilding the table, which this adapter refuses to do:
//
//   - The default value. Defaults still apply when a column is added; on
//     existing columns they are left as is.
//   - Bool columns declared as "string". Earlier D1 drivers created bool columns
//     with that type, and those tables are still in use.
func (m Migrator) MigrateColumn(value any, field *schema.Field, columnType gorm.ColumnType) error {
	return m.Migrator.MigrateColumn(value, field, existingColumn{baseColumn: columnType, field: field})
}

// baseColumn is embedded under its own name because gorm.ColumnType has a
// ColumnType method, which an embedded field of that name would shadow.
type baseColumn = gorm.ColumnType

// existingColumn adjusts what GORM's MigrateColumn sees for an existing column,
// so the differences accepted above do not count as schema changes.
type existingColumn struct {
	baseColumn
	field *schema.Field
}

func (c existingColumn) DefaultValue() (string, bool) {
	f := c.field
	return f.DefaultValue, f.HasDefaultValue && (f.DefaultValueInterface != nil || !strings.EqualFold(f.DefaultValue, "NULL"))
}

func (c existingColumn) DatabaseTypeName() string {
	name := c.baseColumn.DatabaseTypeName()
	if c.field.DataType == schema.Bool && strings.EqualFold(strings.TrimSpace(name), legacyBoolType) {
		return boolColumnType
	}
	return name
}

func (m Migrator) AlterColumn(any, string) error {
	return d1.ErrDestructiveMigration
}

func (m Migrator) DropColumn(any, string) error {
	return d1.ErrDestructiveMigration
}

func (m Migrator) CreateConstraint(any, string) error {
	return d1.ErrConstraintsNotImplemented
}

func (m Migrator) DropConstraint(any, string) error {
	return d1.ErrConstraintsNotImplemented
}

func (m Migrator) HasConstraint(value any, name string) bool {
	var count int64
	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		m.DB.Raw(
			"SELECT count(*) FROM sqlite_master WHERE type = ? AND tbl_name = ? AND (sql LIKE ? OR sql LIKE ? OR sql LIKE ?)",
			"table", stmt.Table, `%CONSTRAINT "`+name+`" %`, `%CONSTRAINT `+name+` %`, "%CONSTRAINT `"+name+"`%",
		).Row().Scan(&count)
		return nil
	})
	return count > 0
}

func (m Migrator) CurrentDatabase() (name string) {
	var null any
	m.DB.Raw("PRAGMA database_list").Row().Scan(&null, &name, &null)
	return name
}

func (m Migrator) BuildIndexOptions(opts []schema.IndexOption, stmt *gorm.Statement) (results []any) {
	for _, opt := range opts {
		str := stmt.Quote(opt.DBName)
		if opt.Expression != "" {
			str = opt.Expression
		}
		if opt.Collate != "" {
			str += " COLLATE " + opt.Collate
		}
		if opt.Sort != "" {
			str += " " + opt.Sort
		}
		results = append(results, clause.Expr{SQL: str})
	}
	return results
}

func (m Migrator) CreateIndex(value any, name string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema == nil {
			return fmt.Errorf("failed to create index with name %v", name)
		}
		idx := stmt.Schema.LookIndex(name)
		if idx == nil {
			return fmt.Errorf("failed to create index with name %v", name)
		}
		opts := m.BuildIndexOptions(idx.Fields, stmt)
		values := []any{clause.Column{Name: idx.Name}, clause.Table{Name: stmt.Table}, opts}

		createIndexSQL := "CREATE "
		if idx.Class != "" {
			createIndexSQL += idx.Class + " "
		}
		createIndexSQL += "INDEX ?"
		if idx.Type != "" {
			createIndexSQL += " USING " + idx.Type
		}
		createIndexSQL += " ON ??"
		if idx.Where != "" {
			createIndexSQL += " WHERE " + idx.Where
		}
		return m.DB.Exec(createIndexSQL, values...).Error
	})
}

func (m Migrator) HasIndex(value any, name string) bool {
	var count int
	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema != nil {
			if idx := stmt.Schema.LookIndex(name); idx != nil {
				name = idx.Name
			}
		}
		if name != "" {
			m.DB.Raw("SELECT count(*) FROM sqlite_master WHERE type = ? AND tbl_name = ? AND name = ?", "index", stmt.Table, name).Row().Scan(&count)
		}
		return nil
	})
	return count > 0
}

func (m Migrator) RenameIndex(value any, oldName, newName string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		var sql string
		m.DB.Raw("SELECT sql FROM sqlite_master WHERE type = ? AND tbl_name = ? AND name = ?", "index", stmt.Table, oldName).Row().Scan(&sql)
		if sql == "" {
			return fmt.Errorf("failed to find index with name %v", oldName)
		}
		if err := m.DB.Exec(strings.Replace(sql, oldName, newName, 1)).Error; err != nil {
			return err
		}
		return m.DB.Exec("DROP INDEX ?", clause.Column{Name: oldName}).Error
	})
}

func (m Migrator) DropIndex(value any, name string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema != nil {
			if idx := stmt.Schema.LookIndex(name); idx != nil {
				name = idx.Name
			}
		}
		return m.DB.Exec("DROP INDEX ?", clause.Column{Name: name}).Error
	})
}

var _ gorm.Migrator = (*Migrator)(nil)
