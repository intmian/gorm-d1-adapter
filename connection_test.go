package d1

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"
)

type mockExecutor struct {
	verifyErr error
	exec      func(context.Context, ExecuteRequest) (ExecuteResult, error)
}

func (m *mockExecutor) Verify(context.Context) error {
	return m.verifyErr
}

func (m *mockExecutor) Execute(ctx context.Context, req ExecuteRequest) (ExecuteResult, error) {
	if m.exec != nil {
		return m.exec(ctx, req)
	}
	return ExecuteResult{ResultSet: ResultSet{
		Meta:    ResultMeta{Changes: 1, LastRowID: 7},
		Columns: []string{"value"},
		Rows:    [][]any{{float64(1)}},
	}}, nil
}

func (m *mockExecutor) Batch(ctx context.Context, req BatchRequest) (BatchResult, error) {
	out := BatchResult{}
	for _, statement := range req.Statements {
		result, err := m.Execute(ctx, statement)
		out.Results = append(out.Results, result)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

func TestConnectionQueryExecAndTransaction(t *testing.T) {
	conn, err := OpenWithExecutor(Config{}, &mockExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	rows, err := conn.QueryContext(context.Background(), "SELECT 1", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if got := rows.Columns(); len(got) != 1 || got[0] != "value" {
		t.Fatalf("unexpected columns: %v", got)
	}

	result, err := conn.ExecContext(context.Background(), "UPDATE t SET a = 1", nil)
	if err != nil {
		t.Fatal(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		t.Fatal(err)
	}
	if affected != 1 {
		t.Fatalf("unexpected affected rows: %d", affected)
	}

	if _, err := conn.Begin(); !errors.Is(err, ErrUnsupportedTransaction) {
		t.Fatalf("expected unsupported transaction, got %v", err)
	}
}

func TestConnectionContextCancellation(t *testing.T) {
	conn, err := OpenWithExecutor(Config{}, &mockExecutor{
		exec: func(ctx context.Context, req ExecuteRequest) (ExecuteResult, error) {
			<-ctx.Done()
			return ExecuteResult{}, ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = conn.QueryContext(ctx, "SELECT 1", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestRowsDecodeValues(t *testing.T) {
	now := time.Date(2026, 7, 9, 1, 2, 3, 4, time.UTC)
	encodedBytes, _ := EncodeParam([]byte{1, 2, 3})
	rows := newRows(ResultSet{
		Columns: []string{"id", "flag", "updated_at", "payload"},
		Rows: [][]any{{
			float64(42),
			float64(1),
			now.Format(time.RFC3339Nano),
			encodedBytes,
		}},
	})

	dest := make([]driver.Value, 4)
	if err := rows.Next(dest); err != nil {
		t.Fatal(err)
	}
	if dest[0] != int64(42) || dest[1] != true || !dest[2].(time.Time).Equal(now) || string(dest[3].([]byte)) != string([]byte{1, 2, 3}) {
		t.Fatalf("unexpected decoded values: %#v", dest)
	}
}
