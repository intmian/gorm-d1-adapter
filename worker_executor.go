package d1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const maxWorkerResponseBytes = 64 << 20

type WorkerExecutor struct {
	cfg    Config
	client *http.Client
}

type WorkerAPIError struct {
	StatusCode int
	Code       string
	Message    string
	RequestID  string
}

func (e *WorkerAPIError) Error() string {
	if e == nil {
		return "d1: worker request failed"
	}
	if e.Code == "" {
		return fmt.Sprintf("d1: worker request failed (%d): %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("d1: worker request failed (%d, %s): %s", e.StatusCode, e.Code, e.Message)
}

func NewExecutor(cfg Config) (Executor, error) {
	cfg = cfg.WithDefaults()
	switch cfg.Mode {
	case ExecutorModeREST:
		return NewRestExecutor(cfg)
	case ExecutorModeWorker:
		return NewWorkerExecutor(cfg)
	default:
		return nil, ErrInvalidExecutorMode
	}
}

func NewWorkerExecutor(cfg Config) (*WorkerExecutor, error) {
	cfg = cfg.WithDefaults()
	if cfg.Mode != ExecutorModeWorker {
		return nil, ErrInvalidExecutorConfig
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	addTraceRedaction(cfg.WorkerToken)
	return &WorkerExecutor{
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}, nil
}

func (e *WorkerExecutor) Execute(ctx context.Context, req ExecuteRequest) (ExecuteResult, error) {
	if req.Mode != ModeExec && req.Mode != ModeQuery {
		return ExecuteResult{}, ErrInvalidMode
	}
	params, err := EncodeParams(req.Params)
	if err != nil {
		return ExecuteResult{}, err
	}
	payload := workerExecutePayload{
		Mode:      req.Mode,
		SQL:       req.SQL,
		Params:    params,
		RequestID: req.RequestID,
	}
	var response workerExecuteResponse
	if err := e.apiCall(ctx, http.MethodPost, "/v1/query", payload, &response); err != nil {
		return ExecuteResult{}, err
	}
	if !response.Success {
		return ExecuteResult{}, response.apiError(http.StatusOK)
	}
	return ExecuteResult{
		ResultSet: response.Result,
		RequestID: response.RequestID,
	}, nil
}

func (e *WorkerExecutor) Batch(ctx context.Context, req BatchRequest) (BatchResult, error) {
	statements := make([]workerExecutePayload, len(req.Statements))
	for i, statement := range req.Statements {
		if statement.Mode != ModeExec && statement.Mode != ModeQuery {
			return BatchResult{}, ErrInvalidMode
		}
		params, err := EncodeParams(statement.Params)
		if err != nil {
			return BatchResult{}, err
		}
		statements[i] = workerExecutePayload{
			Mode:   statement.Mode,
			SQL:    statement.SQL,
			Params: params,
		}
	}

	var response workerBatchResponse
	payload := workerBatchPayload{Statements: statements, RequestID: req.RequestID}
	if err := e.apiCall(ctx, http.MethodPost, "/v1/batch", payload, &response); err != nil {
		return BatchResult{}, err
	}
	if !response.Success {
		return BatchResult{}, response.apiError(http.StatusOK)
	}
	if len(response.Results) != len(req.Statements) {
		return BatchResult{}, ErrWorkerResultCountMismatch
	}
	results := make([]ExecuteResult, len(response.Results))
	for i := range response.Results {
		results[i] = ExecuteResult{ResultSet: response.Results[i], RequestID: response.RequestID}
	}
	return BatchResult{Results: results, RequestID: response.RequestID}, nil
}

func (e *WorkerExecutor) Verify(ctx context.Context) error {
	var response workerHealthResponse
	if err := e.apiCall(ctx, http.MethodGet, "/health", nil, &response); err != nil {
		return err
	}
	if !response.OK {
		return &WorkerAPIError{
			StatusCode: http.StatusOK,
			Code:       "unhealthy",
			Message:    "worker health check failed",
			RequestID:  response.RequestID,
		}
	}
	return nil
}

func (e *WorkerExecutor) apiCall(ctx context.Context, method, path string, payload, out any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, e.cfg.WorkerEndpoint+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.cfg.WorkerToken)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxWorkerResponseBytes+1))
	if err != nil {
		return err
	}
	if len(responseBody) > maxWorkerResponseBytes {
		return ErrWorkerResponseTooLarge
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failed workerErrorResponse
		if len(responseBody) > 0 && json.Unmarshal(responseBody, &failed) == nil {
			return failed.apiError(resp.StatusCode)
		}
		return &WorkerAPIError{
			StatusCode: resp.StatusCode,
			Message:    string(responseBody),
			RequestID:  resp.Header.Get("X-Request-ID"),
		}
	}
	if out == nil || len(responseBody) == 0 {
		return nil
	}
	return json.Unmarshal(responseBody, out)
}
