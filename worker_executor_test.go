package d1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestWorkerExecutorVerifyExecuteAndBatch(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer worker-token" {
			t.Errorf("unexpected authorization: %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/health":
			if r.Method != http.MethodGet {
				t.Errorf("unexpected health method: %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "requestId": "health-id"})
		case "/v1/query":
			var req workerExecutePayload
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			if req.Mode != ModeQuery || req.SQL != "SELECT ?, ?" || req.RequestID != "query-id" {
				t.Errorf("unexpected query request: %+v", req)
			}
			wantParams := []any{"d1b64:AQID", float64(1)}
			if !reflect.DeepEqual(req.Params, wantParams) {
				t.Errorf("unexpected encoded params: %#v", req.Params)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success":   true,
				"requestId": "query-id",
				"result": map[string]any{
					"meta": map[string]any{
						"changes":      2,
						"last_row_id":  17,
						"rows_read":    3,
						"rows_written": 4,
					},
					"columns": []string{"blob", "flag"},
					"rows":    [][]any{{"d1b64:AQID", 1}},
				},
			})
		case "/v1/batch":
			var req workerBatchPayload
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			if req.RequestID != "batch-id" || len(req.Statements) != 2 || req.Statements[0].Mode != ModeExec || req.Statements[1].Mode != ModeQuery {
				t.Errorf("unexpected batch request: %+v", req)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success":   true,
				"requestId": "batch-id",
				"results": []any{
					map[string]any{"meta": map[string]any{"changes": 1}, "columns": []string{}, "rows": []any{}},
					map[string]any{"meta": map[string]any{"rows_read": 1}, "columns": []string{"value"}, "rows": [][]any{{1}}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	executor, err := NewWorkerExecutor(Config{
		Mode:           ExecutorModeWorker,
		WorkerEndpoint: server.URL + "/",
		WorkerToken:    "worker-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	result, err := executor.Execute(context.Background(), ExecuteRequest{
		Mode:      ModeQuery,
		SQL:       "SELECT ?, ?",
		Params:    []any{[]byte{1, 2, 3}, true},
		RequestID: "query-id",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RequestID != "query-id" || result.ResultSet.Meta.LastRowID != 17 || result.ResultSet.Meta.RowsWritten != 4 || len(result.ResultSet.Rows) != 1 {
		t.Fatalf("unexpected query result: %+v", result)
	}

	batch, err := executor.Batch(context.Background(), BatchRequest{
		RequestID: "batch-id",
		Statements: []ExecuteRequest{
			{Mode: ModeExec, SQL: "INSERT INTO t(value) VALUES (?)", Params: []any{1}},
			{Mode: ModeQuery, SQL: "SELECT value FROM t"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if batch.RequestID != "batch-id" || len(batch.Results) != 2 || batch.Results[0].ResultSet.Meta.Changes != 1 || batch.Results[1].ResultSet.Columns[0] != "value" {
		t.Fatalf("unexpected batch result: %+v", batch)
	}
	if !reflect.DeepEqual(paths, []string{"/health", "/v1/query", "/v1/batch"}) {
		t.Fatalf("unexpected paths: %v", paths)
	}
}

func TestWorkerExecutorReturnsStructuredAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success":   false,
			"requestId": "request-42",
			"error": map[string]any{
				"code":    "forbidden_sql",
				"message": "statement is forbidden",
			},
		})
	}))
	defer server.Close()

	executor, err := NewWorkerExecutor(Config{Mode: ExecutorModeWorker, WorkerEndpoint: server.URL, WorkerToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = executor.Execute(context.Background(), ExecuteRequest{Mode: ModeExec, SQL: "ATTACH DATABASE 'x' AS x"})
	var apiErr *WorkerAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected WorkerAPIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusForbidden || apiErr.Code != "forbidden_sql" || apiErr.RequestID != "request-42" {
		t.Fatalf("unexpected api error: %+v", apiErr)
	}
}

func TestWorkerExecutorRejectsInvalidModeAndBatchCount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "results": []any{}})
	}))
	defer server.Close()

	executor, err := NewWorkerExecutor(Config{Mode: ExecutorModeWorker, WorkerEndpoint: server.URL, WorkerToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Execute(context.Background(), ExecuteRequest{SQL: "SELECT 1"}); !errors.Is(err, ErrInvalidMode) {
		t.Fatalf("expected invalid mode, got %v", err)
	}
	_, err = executor.Batch(context.Background(), BatchRequest{Statements: []ExecuteRequest{{Mode: ModeExec, SQL: "SELECT 1"}}})
	if !errors.Is(err, ErrWorkerResultCountMismatch) {
		t.Fatalf("expected result count mismatch, got %v", err)
	}
}

func TestNewExecutorSelectsConfiguredMode(t *testing.T) {
	rest, err := NewExecutor(Config{DatabaseID: "12345678-1234-1234-1234-123456789012"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rest.(*RestExecutor); !ok {
		t.Fatalf("expected RestExecutor, got %T", rest)
	}

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	worker, err := NewExecutor(Config{Mode: ExecutorModeWorker, WorkerEndpoint: server.URL, WorkerToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := worker.(*WorkerExecutor); !ok {
		t.Fatalf("expected WorkerExecutor, got %T", worker)
	}
}
