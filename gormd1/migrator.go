package gormd1

import (
	"fmt"
	"strings"

	d1 "github.com/intmian/gorm-d1-adapter"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
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
