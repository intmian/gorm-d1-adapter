package d1

import "context"

type ExecuteMode string

const (
	ModeQuery ExecuteMode = "query"
	ModeExec  ExecuteMode = "exec"
)

type Executor interface {
	Execute(ctx context.Context, req ExecuteRequest) (ExecuteResult, error)
	Batch(ctx context.Context, req BatchRequest) (BatchResult, error)
	Verify(ctx context.Context) error
}

type ExecuteRequest struct {
	Mode      ExecuteMode `json:"mode"`
	SQL       string      `json:"sql"`
	Params    []any       `json:"params,omitempty"`
	RequestID string      `json:"requestId,omitempty"`
}

type ExecuteResult struct {
	ResultSet  ResultSet `json:"result"`
	RequestID  string    `json:"requestId,omitempty"`
	AuditLogID string    `json:"auditLogId,omitempty"`
}

type BatchRequest struct {
	Statements []ExecuteRequest `json:"statements"`
	RequestID  string           `json:"requestId,omitempty"`
}

type BatchResult struct {
	Results   []ExecuteResult `json:"results"`
	RequestID string          `json:"requestId,omitempty"`
}

type ResultSet struct {
	Meta    ResultMeta `json:"meta"`
	Columns []string   `json:"columns"`
	Rows    [][]any    `json:"rows"`
}

type ResultMeta struct {
	ChangedDB   bool    `json:"changed_db"`
	Changes     int64   `json:"changes"`
	Duration    float64 `json:"duration"`
	LastRowID   int64   `json:"last_row_id"`
	RowsRead    int64   `json:"rows_read"`
	RowsWritten int64   `json:"rows_written"`
	ServedBy    string  `json:"served_by"`
	SizeAfter   int64   `json:"size_after"`
}
