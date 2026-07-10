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
