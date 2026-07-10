package main

import (
	"errors"
	"testing"
	"time"

	d1 "github.com/intmian/gorm-d1-adapter"
)

func TestSummarizePercentilesFailuresAndMeta(t *testing.T) {
	report := summarize("query", []sample{
		{duration: 1 * time.Millisecond, meta: d1.ResultMeta{RowsRead: 1, ServedBy: "a"}},
		{duration: 2 * time.Millisecond, meta: d1.ResultMeta{RowsRead: 2, ServedBy: "b"}},
		{duration: 3 * time.Millisecond, meta: d1.ResultMeta{RowsRead: 3, ServedBy: "a"}},
		{duration: 4 * time.Millisecond, err: errors.New("failed")},
	})

	if report.Attempted != 4 || report.Succeeded != 3 || report.Failed != 1 || report.FailureRate != 0.25 {
		t.Fatalf("unexpected counts: %+v", report)
	}
	if report.P50MS != 2 || report.P95MS != 3 || report.P99MS != 3 {
		t.Fatalf("unexpected percentiles: %+v", report)
	}
	if report.D1Meta.RowsRead != 6 || len(report.D1Meta.ServedBy) != 2 || report.FirstError != "failed" {
		t.Fatalf("unexpected metadata: %+v", report)
	}
}

func TestPercentileEmpty(t *testing.T) {
	if got := percentile(nil, 0.95); got != 0 {
		t.Fatalf("expected zero, got %f", got)
	}
}
