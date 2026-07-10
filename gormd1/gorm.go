package gormd1

import (
	"database/sql"
	"strings"

	d1 "github.com/intmian/gorm-d1-adapter"
	_ "github.com/intmian/gorm-d1-adapter/stdlib"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

type Dialector struct {
	dsn  string
	Conn gorm.ConnPool
}

func Open(dsn string) gorm.Dialector {
	return &Dialector{dsn: dsn}
}

func OpenConfig(cfg d1.Config) gorm.Dialector {
	return &Dialector{dsn: d1.NewDSN(cfg)}
}

func (dialector *Dialector) Name() string {
	return d1.DriverName
}

func (dialector *Dialector) Initialize(db *gorm.DB) error {
	db.Config.SkipDefaultTransaction = true
	callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{LastInsertIDReversed: true})

	if dialector.Conn != nil {
		db.ConnPool = dialector.Conn
		return nil
	}

	conn, err := sql.Open(d1.DriverName, dialector.dsn)
	if err != nil {
		return err
	}
	db.ConnPool = conn
	return nil
}

func (dialector *Dialector) DataTypeOf(field *schema.Field) string {
	switch field.DataType {
	case schema.Bool:
		return "integer"
	case schema.Int, schema.Uint:
		if field.AutoIncrement && !field.PrimaryKey {
			return "integer PRIMARY KEY AUTOINCREMENT"
		}
		return "integer"
	case schema.Float:
		return "real"
	case schema.String:
		return "text"
	case schema.Time:
		return "datetime"
	case schema.Bytes:
		return "blob"
	default:
		return string(field.DataType)
	}
}

func (dialector *Dialector) DefaultValueOf(*schema.Field) clause.Expression {
	return clause.Expr{SQL: "DEFAULT"}
}

func (dialector *Dialector) BindVarTo(writer clause.Writer, _ *gorm.Statement, _ any) {
	writer.WriteByte('?')
}

func (dialector *Dialector) QuoteTo(writer clause.Writer, str string) {
	for idx, part := range strings.Split(str, ".") {
		if idx > 0 {
			writer.WriteByte('.')
		}
		writer.WriteByte('`')
		writer.WriteString(strings.ReplaceAll(part, "`", "``"))
		writer.WriteByte('`')
	}
}

func (dialector *Dialector) Explain(sql string, vars ...any) string {
	return logger.ExplainSQL(sql, nil, `"`, vars...)
}

var _ gorm.Dialector = (*Dialector)(nil)
