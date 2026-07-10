package d1

import (
	"errors"
	"fmt"
	"strings"
)

type parameterizedStatement struct {
	SQL    string `json:"sql"`
	Params []any  `json:"params,omitempty"`
}

type d1RespMessage struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type d1RespError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type d1RespResultInfo struct {
	Count      int `json:"count"`
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	TotalCount int `json:"total_count"`
}

type d1RespQueryResultMeta struct {
	ChangedDB   bool    `json:"changed_db"`
	Changes     int64   `json:"changes"`
	Duration    float64 `json:"duration"`
	LastRowID   int64   `json:"last_row_id"`
	RowsRead    int64   `json:"rows_read"`
	RowsWritten int64   `json:"rows_written"`
	ServedBy    string  `json:"served_by"`
	SizeAfter   int64   `json:"size_after"`
}

type d1RespQueryResults struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
}

type d1RespQueryResult struct {
	Meta    d1RespQueryResultMeta `json:"meta"`
	Results d1RespQueryResults    `json:"results"`
}

type d1Resp struct {
	Errors     []d1RespError        `json:"errors"`
	Messages   []d1RespMessage      `json:"messages"`
	Result     []*d1RespQueryResult `json:"result"`
	ResultInfo *d1RespResultInfo    `json:"result_info"`
	Success    bool                 `json:"success"`
}

type d1VerifyResp struct {
	Errors   []d1RespError   `json:"errors"`
	Messages []d1RespMessage `json:"messages"`
	Success  bool            `json:"success"`
}

func (r d1Resp) error() error {
	return responseError(r.Errors)
}

func (r d1VerifyResp) error() error {
	return responseError(r.Errors)
}

func responseError(apiErrors []d1RespError) error {
	errs := make([]error, 0, len(apiErrors))
	for _, e := range apiErrors {
		errs = append(errs, fmt.Errorf("[%d] %s", e.Code, e.Message))
	}
	if len(errs) == 0 {
		return errors.New("d1 api call failed")
	}
	return errors.New(strings.TrimSpace(errors.Join(errs...).Error()))
}

func (r d1RespQueryResult) toResultSet() ResultSet {
	return ResultSet{
		Meta: ResultMeta{
			ChangedDB:   r.Meta.ChangedDB,
			Changes:     r.Meta.Changes,
			Duration:    r.Meta.Duration,
			LastRowID:   r.Meta.LastRowID,
			RowsRead:    r.Meta.RowsRead,
			RowsWritten: r.Meta.RowsWritten,
			ServedBy:    r.Meta.ServedBy,
			SizeAfter:   r.Meta.SizeAfter,
		},
		Columns: append([]string(nil), r.Results.Columns...),
		Rows:    append([][]any(nil), r.Results.Rows...),
	}
}
