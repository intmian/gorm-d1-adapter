package d1

import "net/http"

type workerExecutePayload struct {
	Mode      ExecuteMode `json:"mode"`
	SQL       string      `json:"sql"`
	Params    []any       `json:"params,omitempty"`
	RequestID string      `json:"requestId,omitempty"`
}

type workerBatchPayload struct {
	Statements []workerExecutePayload `json:"statements"`
	RequestID  string                 `json:"requestId,omitempty"`
}

type workerError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type workerErrorResponse struct {
	Success   bool         `json:"success"`
	RequestID string       `json:"requestId"`
	Error     *workerError `json:"error"`
}

func (r workerErrorResponse) apiError(statusCode int) error {
	apiErr := &WorkerAPIError{StatusCode: statusCode, RequestID: r.RequestID}
	if r.Error != nil {
		apiErr.Code = r.Error.Code
		apiErr.Message = r.Error.Message
	}
	if apiErr.Message == "" {
		if statusCode >= http.StatusBadRequest {
			apiErr.Message = http.StatusText(statusCode)
		} else {
			apiErr.Message = "worker request failed"
		}
	}
	return apiErr
}

type workerExecuteResponse struct {
	workerErrorResponse
	Result ResultSet `json:"result"`
}

type workerBatchResponse struct {
	workerErrorResponse
	Results []ResultSet `json:"results"`
}

type workerHealthResponse struct {
	OK        bool   `json:"ok"`
	RequestID string `json:"requestId"`
}
