package d1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type RestExecutor struct {
	cfg    Config
	client *http.Client
}

func NewRestExecutor(cfg Config) (*RestExecutor, error) {
	cfg = cfg.WithDefaults()
	if cfg.Mode != ExecutorModeREST {
		return nil, ErrInvalidExecutorConfig
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.APIToken != "" {
		addTraceRedaction(cfg.APIToken)
	}
	return &RestExecutor{
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}, nil
}

func (e *RestExecutor) Execute(ctx context.Context, req ExecuteRequest) (ExecuteResult, error) {
	if req.Mode != "" && req.Mode != ModeExec && req.Mode != ModeQuery {
		return ExecuteResult{}, ErrInvalidMode
	}
	params, err := EncodeParams(req.Params)
	if err != nil {
		return ExecuteResult{}, err
	}
	body, err := json.Marshal(parameterizedStatement{SQL: req.SQL, Params: params})
	if err != nil {
		return ExecuteResult{}, err
	}

	var resp d1Resp
	auditLogID, err := e.apiCall(ctx, "POST", e.queryEndpoint(), body, &resp)
	if err != nil {
		return ExecuteResult{}, err
	}
	if !resp.Success {
		return ExecuteResult{}, resp.error()
	}
	result := ExecuteResult{AuditLogID: auditLogID}
	if len(resp.Result) == 0 || resp.Result[0] == nil {
		result.ResultSet = ResultSet{}
		return result, nil
	}
	result.ResultSet = resp.Result[0].toResultSet()
	return result, nil
}

func (e *RestExecutor) Batch(ctx context.Context, req BatchRequest) (BatchResult, error) {
	statements := make([]parameterizedStatement, len(req.Statements))
	for i, statement := range req.Statements {
		if statement.Mode != "" && statement.Mode != ModeExec && statement.Mode != ModeQuery {
			return BatchResult{}, ErrInvalidMode
		}
		params, err := EncodeParams(statement.Params)
		if err != nil {
			return BatchResult{}, err
		}
		statements[i] = parameterizedStatement{SQL: statement.SQL, Params: params}
	}
	body, err := json.Marshal(parameterizedBatch{Batch: statements})
	if err != nil {
		return BatchResult{}, err
	}

	var resp d1Resp
	auditLogID, err := e.apiCall(ctx, http.MethodPost, e.queryEndpoint(), body, &resp)
	if err != nil {
		return BatchResult{}, err
	}
	if !resp.Success {
		return BatchResult{}, resp.error()
	}
	if len(resp.Result) != len(req.Statements) {
		return BatchResult{}, ErrRESTResultCountMismatch
	}
	results := make([]ExecuteResult, len(resp.Result))
	for i, result := range resp.Result {
		if result == nil {
			return BatchResult{}, ErrRESTResultCountMismatch
		}
		results[i] = ExecuteResult{ResultSet: result.toResultSet(), AuditLogID: auditLogID}
	}
	return BatchResult{Results: results, RequestID: req.RequestID}, nil
}

func (e *RestExecutor) Verify(ctx context.Context) error {
	var resp d1VerifyResp
	_, err := e.apiCall(ctx, "GET", e.tokenEndpoint(), nil, &resp)
	if err != nil {
		return err
	}
	if !resp.Success && len(resp.Errors) > 0 {
		return resp.error()
	}
	return nil
}

func (e *RestExecutor) queryEndpoint() string {
	return fmt.Sprintf("/accounts/%s/d1/database/%s/raw", e.cfg.AccountID, e.cfg.DatabaseID)
}

func (e *RestExecutor) tokenEndpoint() string {
	// Config.APIToken is an account-owned token. User-owned tokens use a
	// different Cloudflare endpoint and are not interchangeable here.
	return fmt.Sprintf("/accounts/%s/tokens/verify", e.cfg.AccountID)
}

func (e *RestExecutor) apiCall(ctx context.Context, method, endpoint string, reqBody []byte, out any) (auditLogID string, err error) {
	if endpoint == "" {
		return "", ErrInvalidAPI
	}
	var body io.Reader
	if reqBody != nil {
		body = bytes.NewReader(reqBody)
	}
	req, err := http.NewRequestWithContext(ctx, method, e.cfg.BaseURL+endpoint, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.cfg.APIToken)

	resp, err := e.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.Header.Get("cf-auditlog-id"), fmt.Errorf("http status: %d, body: %s", resp.StatusCode, respBody)
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return resp.Header.Get("cf-auditlog-id"), err
		}
	}
	return resp.Header.Get("cf-auditlog-id"), nil
}
