package gormd1

import (
	"strings"
	"testing"

	d1 "github.com/intmian/gorm-d1-adapter"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

type dialectRow struct {
	ID   int64 `gorm:"primaryKey;autoIncrement"`
	Name string
}

func openDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(OpenConfig(d1.Config{
		Mode:           d1.ExecutorModeWorker,
		WorkerEndpoint: "https://worker.example",
		WorkerToken:    "token",
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSQLiteClauseBuilders(t *testing.T) {
	db := openDryRunDB(t)
	created := db.Create(&dialectRow{Name: "one"})
	if created.Error != nil {
		t.Fatal(created.Error)
	}
	if !strings.Contains(created.Statement.SQL.String(), "INSERT INTO `dialect_rows`") {
		t.Fatalf("unexpected insert SQL: %s", created.Statement.SQL.String())
	}

	offset := db.Offset(5).Find(&[]dialectRow{})
	if offset.Error != nil {
		t.Fatal(offset.Error)
	}
	if !strings.Contains(offset.Statement.SQL.String(), "LIMIT -1 OFFSET 5") {
		t.Fatalf("unexpected offset SQL: %s", offset.Statement.SQL.String())
	}

	locking := db.Clauses(clause.Locking{Strength: "UPDATE"}).Find(&[]dialectRow{})
	if locking.Error != nil {
		t.Fatal(locking.Error)
	}
	if strings.Contains(locking.Statement.SQL.String(), "FOR UPDATE") {
		t.Fatalf("SQLite locking clause was not removed: %s", locking.Statement.SQL.String())
	}
}

func TestSQLiteAutoIncrementDefaults(t *testing.T) {
	dialector := &Dialector{}
	field := &schema.Field{DataType: schema.Int, AutoIncrement: true, PrimaryKey: true}
	if got := dialector.DataTypeOf(field); got != "integer PRIMARY KEY AUTOINCREMENT" {
		t.Fatalf("unexpected auto increment type: %s", got)
	}
	if got := dialector.DefaultValueOf(field).(clause.Expr).SQL; got != "NULL" {
		t.Fatalf("unexpected auto increment default: %s", got)
	}
}
