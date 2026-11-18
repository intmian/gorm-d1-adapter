package gormd1

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	d1 "github.com/intmian/gorm-d1-adapter"
	"gorm.io/gorm"
)

// autoMigrateAgainst runs AutoMigrate for model against an existing table with
// the given declared column types and returns its error and the SQL it sent.
func autoMigrateAgainst(t *testing.T, model any, columns [][]any) (error, []string) {
	t.Helper()
	var sqls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
			return
		}
		var payload struct {
			SQL string `json:"sql"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		sqls = append(sqls, payload.SQL)
		result := map[string]any{"meta": map[string]any{}, "columns": []string{}, "rows": []any{}}
		switch {
		case strings.Contains(payload.SQL, "SELECT count(*) FROM sqlite_master"):
			result = map[string]any{"meta": map[string]any{}, "columns": []string{"count(*)"}, "rows": [][]any{{1}}}
		case strings.HasPrefix(payload.SQL, "PRAGMA table_info"):
			result = map[string]any{
				"meta":    map[string]any{},
				"columns": []string{"cid", "name", "type", "notnull", "dflt_value", "pk"},
				"rows":    columns,
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
	}))
	defer server.Close()

	db, err := gorm.Open(OpenConfig(d1.Config{
		Mode:           d1.ExecutorModeWorker,
		WorkerEndpoint: server.URL,
		WorkerToken:    "token",
	}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return db.Table("items").AutoMigrate(model), sqls
}

// Tables created by earlier D1 drivers declare bool columns as "string".
func TestAutoMigrateAcceptsLegacyStringBool(t *testing.T) {
	type item struct {
		ID      int64 `gorm:"primaryKey"`
		Deleted bool
	}
	err, sqls := autoMigrateAgainst(t, &item{}, [][]any{
		{0, "id", "integer", 0, nil, 1},
		{1, "deleted", "string", 0, nil, 0},
	})
	if err != nil {
		t.Fatalf("legacy bool column: %v", err)
	}
	for _, query := range sqls {
		if strings.HasPrefix(strings.ToUpper(query), "ALTER") {
			t.Fatalf("unexpected schema change: %s", query)
		}
	}
}

// Only the bool/"string" pair is accepted; other type changes stay rejected.
func TestAutoMigrateRejectsOtherTypeDifferences(t *testing.T) {
	type boolItem struct {
		ID    int64 `gorm:"primaryKey"`
		Value bool
	}
	type intItem struct {
		ID    int64 `gorm:"primaryKey"`
		Value int
	}
	cases := []struct {
		name     string
		model    any
		declared string
	}{
		{"bool declared as text", &boolItem{}, "text"},
		{"int declared as string", &intItem{}, "string"},
		{"int declared as text", &intItem{}, "text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err, _ := autoMigrateAgainst(t, tc.model, [][]any{
				{0, "id", "integer", 0, nil, 1},
				{1, "value", tc.declared, 0, nil, 0},
			})
			if !errors.Is(err, d1.ErrDestructiveMigration) {
				t.Fatalf("expected ErrDestructiveMigration, got %v", err)
			}
		})
	}
}

func TestMigratorUsesD1CompatibleMetadataQueries(t *testing.T) {
	var sqls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
			return
		}
		var payload struct {
			SQL string `json:"sql"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		sqls = append(sqls, payload.SQL)
		result := map[string]any{"meta": map[string]any{}, "columns": []string{}, "rows": []any{}}
		if strings.Contains(payload.SQL, "sqlite_master") && strings.Contains(payload.SQL, "SELECT name") {
			result = map[string]any{"meta": map[string]any{}, "columns": []string{"name"}, "rows": [][]any{{"alpha"}, {"beta"}}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
	}))
	defer server.Close()

	db, err := gorm.Open(OpenConfig(d1.Config{
		Mode:           d1.ExecutorModeWorker,
		WorkerEndpoint: server.URL,
		WorkerToken:    "token",
	}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}

	tables, err := db.Migrator().GetTables()
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 2 || tables[0] != "alpha" || tables[1] != "beta" {
		t.Fatalf("unexpected tables: %v", tables)
	}
	if err := db.Migrator().DropTable("children", "parents"); err != nil {
		t.Fatal(err)
	}
	for _, query := range sqls {
		if strings.Contains(strings.ToLower(query), "pragma foreign_keys") {
			t.Fatalf("migrator attempted unsupported foreign key toggle: %s", query)
		}
		if strings.Contains(strings.ToLower(query), "information_schema") {
			t.Fatalf("migrator used non-SQLite metadata query: %s", query)
		}
	}
}

func TestAutoMigrateWithDefaultIsIdempotent(t *testing.T) {
	type libraryNote struct {
		ID       int64 `gorm:"primaryKey"`
		Revision int   `gorm:"default:1"`
	}

	tableExists := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
			return
		}
		var payload struct {
			SQL string `json:"sql"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}

		result := map[string]any{"meta": map[string]any{}, "columns": []string{}, "rows": []any{}}
		switch {
		case strings.Contains(payload.SQL, "SELECT count(*) FROM sqlite_master"):
			count := 0
			if tableExists {
				count = 1
			}
			result = map[string]any{"meta": map[string]any{}, "columns": []string{"count(*)"}, "rows": [][]any{{count}}}
		case strings.HasPrefix(payload.SQL, "CREATE TABLE"):
			tableExists = true
		case strings.HasPrefix(payload.SQL, "PRAGMA table_info"):
			result = map[string]any{
				"meta":    map[string]any{},
				"columns": []string{"cid", "name", "type", "notnull", "dflt_value", "pk"},
				"rows": [][]any{
					{0, "id", "integer", 0, nil, 1},
					{1, "revision", "integer", 0, "1", 0},
				},
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
	}))
	defer server.Close()

	db, err := gorm.Open(OpenConfig(d1.Config{
		Mode:           d1.ExecutorModeWorker,
		WorkerEndpoint: server.URL,
		WorkerToken:    "token",
	}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		if err := db.Table("library_notes").AutoMigrate(&libraryNote{}); err != nil {
			t.Fatalf("AutoMigrate run %d failed: %v", i+1, err)
		}
	}

	columnTypes, err := db.Table("library_notes").Migrator().ColumnTypes(&libraryNote{})
	if err != nil {
		t.Fatal(err)
	}
	if len(columnTypes) != 2 {
		t.Fatalf("unexpected column count: %d", len(columnTypes))
	}
	if got := columnTypes[1].DatabaseTypeName(); got != "integer" {
		t.Fatalf("unexpected revision type: %q", got)
	}
	if value, ok := columnTypes[1].DefaultValue(); !ok || value != "1" {
		t.Fatalf("unexpected revision default: value=%q ok=%v", value, ok)
	}
}

// A column whose default differs from the model (added by an older model, or
// the tag was removed later) must not make AutoMigrate try to alter it.
func TestAutoMigrateIgnoresDefaultOnlyDifference(t *testing.T) {
	type withoutDefault struct {
		ID       int64 `gorm:"primaryKey"`
		Revision int
		Paused   bool
	}
	type withDefault struct {
		ID       int64 `gorm:"primaryKey"`
		Revision int   `gorm:"default:2"`
		Paused   bool  `gorm:"default:true"`
	}

	var sqls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
			return
		}
		var payload struct {
			SQL string `json:"sql"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		sqls = append(sqls, payload.SQL)
		result := map[string]any{"meta": map[string]any{}, "columns": []string{}, "rows": []any{}}
		switch {
		case strings.Contains(payload.SQL, "SELECT count(*) FROM sqlite_master"):
			result = map[string]any{"meta": map[string]any{}, "columns": []string{"count(*)"}, "rows": [][]any{{1}}}
		case strings.HasPrefix(payload.SQL, "PRAGMA table_info"):
			result = map[string]any{
				"meta":    map[string]any{},
				"columns": []string{"cid", "name", "type", "notnull", "dflt_value", "pk"},
				"rows": [][]any{
					{0, "id", "integer", 0, nil, 1},
					{1, "revision", "integer", 0, "1", 0},
					{2, "paused", "integer", 0, "false", 0},
				},
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
	}))
	defer server.Close()

	db, err := gorm.Open(OpenConfig(d1.Config{
		Mode:           d1.ExecutorModeWorker,
		WorkerEndpoint: server.URL,
		WorkerToken:    "token",
	}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Table("notes").AutoMigrate(&withoutDefault{}); err != nil {
		t.Fatalf("model without default: %v", err)
	}
	if err := db.Table("notes").AutoMigrate(&withDefault{}); err != nil {
		t.Fatalf("model with a different default: %v", err)
	}
	for _, query := range sqls {
		if strings.HasPrefix(strings.ToUpper(query), "ALTER") {
			t.Fatalf("unexpected schema change: %s", query)
		}
	}
}
