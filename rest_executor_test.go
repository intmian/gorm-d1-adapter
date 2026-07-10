package d1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRestExecutorVerifyAccountTokenAndExecute(t *testing.T) {
	var sawAuth bool
	var sawSQL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization") == "Bearer token"
		switch r.URL.Path {
		case "/accounts/account/tokens/verify":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"result": map[string]any{
					"id":     "token-id",
					"status": "active",
				},
			})
		case "/accounts/account/d1/database/12345678-1234-1234-1234-123456789012/raw":
			var req parameterizedStatement
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			sawSQL = req.SQL
			_ = json.NewEncoder(w).Encode(d1Resp{
				Success: true,
				Result: []*d1RespQueryResult{{
					Meta: d1RespQueryResultMeta{Changes: 1, LastRowID: 9},
					Results: d1RespQueryResults{
						Columns: []string{"value"},
						Rows:    [][]any{{float64(1)}},
					},
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	executor, err := NewRestExecutor(Config{
		AccountID:  "account",
		APIToken:   "token",
		DatabaseID: "12345678-1234-1234-1234-123456789012",
		BaseURL:    server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err := executor.Execute(context.Background(), ExecuteRequest{Mode: ModeQuery, SQL: "SELECT 1"})
	if err != nil {
		t.Fatal(err)
	}
	if !sawAuth {
		t.Fatal("authorization header was not sent")
	}
	if sawSQL != "SELECT 1" {
		t.Fatalf("unexpected sql: %s", sawSQL)
	}
	if result.ResultSet.Meta.Changes != 1 || result.ResultSet.Meta.LastRowID != 9 || len(result.ResultSet.Rows) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestRestExecutorEmptyResultDoesNotPanic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(d1Resp{Success: true})
	}))
	defer server.Close()

	executor, err := NewRestExecutor(Config{
		AccountID:  "account",
		APIToken:   "token",
		DatabaseID: "12345678-1234-1234-1234-123456789012",
		BaseURL:    server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.Execute(context.Background(), ExecuteRequest{Mode: ModeQuery, SQL: "SELECT 1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ResultSet.Rows) != 0 || len(result.ResultSet.Columns) != 0 {
		t.Fatalf("expected empty result, got %+v", result)
	}
}

func TestRestExecutorBatchUsesSingleBatchRequest(t *testing.T) {
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		var req struct {
			Batch []parameterizedStatement `json:"batch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if len(req.Batch) != 2 || req.Batch[0].SQL != "INSERT INTO t(id) VALUES (?)" || req.Batch[1].SQL != "SELECT id FROM t" {
			t.Errorf("unexpected batch request: %+v", req)
		}
		_ = json.NewEncoder(w).Encode(d1Resp{
			Success: true,
			Result: []*d1RespQueryResult{
				{Meta: d1RespQueryResultMeta{Changes: 1, LastRowID: 1}},
				{Results: d1RespQueryResults{Columns: []string{"id"}, Rows: [][]any{{float64(1)}}}},
			},
		})
	}))
	defer server.Close()

	executor, err := NewRestExecutor(Config{
		AccountID:  "account",
		APIToken:   "token",
		DatabaseID: "12345678-1234-1234-1234-123456789012",
		BaseURL:    server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.Batch(context.Background(), BatchRequest{Statements: []ExecuteRequest{
		{Mode: ModeExec, SQL: "INSERT INTO t(id) VALUES (?)", Params: []any{1}},
		{Mode: ModeQuery, SQL: "SELECT id FROM t"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if requestCount != 1 {
		t.Fatalf("expected one HTTP request, got %d", requestCount)
	}
	if len(result.Results) != 2 || result.Results[0].ResultSet.Meta.Changes != 1 || len(result.Results[1].ResultSet.Rows) != 1 {
		t.Fatalf("unexpected batch result: %+v", result)
	}
}
