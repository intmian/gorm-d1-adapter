package gormd1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	d1 "github.com/intmian/gorm-d1-adapter"
	"gorm.io/gorm"
)

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
