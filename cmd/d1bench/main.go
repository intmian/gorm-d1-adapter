package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	d1 "github.com/intmian/gorm-d1-adapter"
	"github.com/intmian/gorm-d1-adapter/gormd1"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type benchmarkReport struct {
	Status        string            `json:"status"`
	CreatedAt     time.Time         `json:"createdAt"`
	GoVersion     string            `json:"goVersion"`
	Mode          d1.ExecutorMode   `json:"mode"`
	Samples       int               `json:"samples"`
	Concurrency   int               `json:"concurrency"`
	IncludeWrites bool              `json:"includeWrites"`
	Operations    []operationReport `json:"operations"`
}

type operationReport struct {
	Name        string         `json:"name"`
	Attempted   int            `json:"attempted"`
	Succeeded   int            `json:"succeeded"`
	Failed      int            `json:"failed"`
	FailureRate float64        `json:"failureRate"`
	MinMS       float64        `json:"minMs"`
	MeanMS      float64        `json:"meanMs"`
	P50MS       float64        `json:"p50Ms"`
	P95MS       float64        `json:"p95Ms"`
	P99MS       float64        `json:"p99Ms"`
	MaxMS       float64        `json:"maxMs"`
	D1Meta      aggregatedMeta `json:"d1Meta"`
	FirstError  string         `json:"firstError,omitempty"`
}

type aggregatedMeta struct {
	Changes       int64    `json:"changes"`
	Duration      float64  `json:"duration"`
	RowsRead      int64    `json:"rowsRead"`
	RowsWritten   int64    `json:"rowsWritten"`
	ServedBy      []string `json:"servedBy,omitempty"`
	LastSizeAfter int64    `json:"lastSizeAfter"`
}

type sample struct {
	duration time.Duration
	meta     d1.ResultMeta
	err      error
}

type sampleFunc func(index int) (d1.ResultMeta, error)

type benchRow struct {
	ID        int64 `gorm:"primaryKey"`
	Name      string
	Flag      bool
	UpdatedAt time.Time
}

func main() {
	modeFlag := flag.String("mode", envOr("D1_EXECUTOR_MODE", string(d1.ExecutorModeREST)), "executor mode: rest or worker")
	samplesFlag := flag.Int("samples", 20, "samples per operation")
	concurrencyFlag := flag.Int("concurrency", 4, "parallel workers for the concurrent query operation")
	writesFlag := flag.Bool("writes", false, "include write and migration operations")
	outputFlag := flag.String("out", "", "optional JSON output path")
	flag.Parse()

	if *samplesFlag <= 0 || *concurrencyFlag <= 0 {
		fmt.Fprintln(os.Stderr, "samples and concurrency must be positive")
		os.Exit(2)
	}
	mode := d1.ExecutorMode(strings.ToLower(*modeFlag))
	cfg, err := configFromEnv(mode)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	report, err := runBenchmark(cfg, *samplesFlag, *concurrencyFlag, *writesFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *outputFlag != "" {
		if err := os.MkdirAll(filepath.Dir(*outputFlag), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := os.WriteFile(*outputFlag, append(encoded, '\n'), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	fmt.Println(string(encoded))
	if report.Status != "complete" {
		os.Exit(1)
	}
}

func runBenchmark(cfg d1.Config, sampleCount, concurrency int, includeWrites bool) (benchmarkReport, error) {
	executor, err := d1.NewExecutor(cfg)
	if err != nil {
		return benchmarkReport{}, err
	}
	if err := executor.Verify(context.Background()); err != nil {
		return benchmarkReport{}, fmt.Errorf("initial verify failed: %w", err)
	}

	report := benchmarkReport{
		Status:        "complete",
		CreatedAt:     time.Now().UTC(),
		GoVersion:     runtime.Version(),
		Mode:          cfg.Mode,
		Samples:       sampleCount,
		Concurrency:   concurrency,
		IncludeWrites: includeWrites,
	}
	ctx := context.Background()
	report.Operations = append(report.Operations,
		measureOperation("verify", sampleCount, 1, func(int) (d1.ResultMeta, error) {
			one, err := d1.NewExecutor(cfg)
			if err != nil {
				return d1.ResultMeta{}, err
			}
			return d1.ResultMeta{}, one.Verify(ctx)
		}),
		measureOperation("select_one", sampleCount, 1, func(int) (d1.ResultMeta, error) {
			result, err := executor.Execute(ctx, d1.ExecuteRequest{Mode: d1.ModeQuery, SQL: "SELECT 1 AS value"})
			if err != nil {
				return d1.ResultMeta{}, err
			}
			if err := expectSingleInt(result.ResultSet, 1); err != nil {
				return result.ResultSet.Meta, err
			}
			return result.ResultSet.Meta, nil
		}),
		measureOperation("parameterized_select", sampleCount, 1, func(index int) (d1.ResultMeta, error) {
			result, err := executor.Execute(ctx, d1.ExecuteRequest{
				Mode:   d1.ModeQuery,
				SQL:    "SELECT ? AS value, ? AS label",
				Params: []any{index, "benchmark"},
			})
			if err != nil {
				return d1.ResultMeta{}, err
			}
			if len(result.ResultSet.Rows) != 1 || len(result.ResultSet.Columns) != 2 {
				return result.ResultSet.Meta, errors.New("unexpected parameterized query result")
			}
			return result.ResultSet.Meta, nil
		}),
		measureOperation("batch_select_two", sampleCount, 1, func(int) (d1.ResultMeta, error) {
			result, err := executor.Batch(ctx, d1.BatchRequest{Statements: []d1.ExecuteRequest{
				{Mode: d1.ModeQuery, SQL: "SELECT 1 AS value"},
				{Mode: d1.ModeQuery, SQL: "SELECT 2 AS value"},
			}})
			if err != nil {
				return d1.ResultMeta{}, err
			}
			if len(result.Results) != 2 {
				return d1.ResultMeta{}, errors.New("unexpected batch result count")
			}
			if err := expectSingleInt(result.Results[0].ResultSet, 1); err != nil {
				return combineResultMeta(result.Results), err
			}
			if err := expectSingleInt(result.Results[1].ResultSet, 2); err != nil {
				return combineResultMeta(result.Results), err
			}
			return combineResultMeta(result.Results), nil
		}),
		measureOperation("concurrent_select_one", sampleCount, concurrency, func(int) (d1.ResultMeta, error) {
			result, err := executor.Execute(ctx, d1.ExecuteRequest{Mode: d1.ModeQuery, SQL: "SELECT 1 AS value"})
			if err != nil {
				return d1.ResultMeta{}, err
			}
			return result.ResultSet.Meta, expectSingleInt(result.ResultSet, 1)
		}),
	)

	if includeWrites {
		writeReports, err := measureWrites(ctx, cfg, executor, sampleCount)
		if err != nil {
			return benchmarkReport{}, err
		}
		report.Operations = append(report.Operations, writeReports...)
	}
	for _, operation := range report.Operations {
		if operation.Failed > 0 {
			report.Status = "failed"
			break
		}
	}
	return report, nil
}

func measureWrites(ctx context.Context, cfg d1.Config, executor d1.Executor, sampleCount int) ([]operationReport, error) {
	table := fmt.Sprintf("d1_adapter_bench_%d", time.Now().UnixNano())
	createSQL := "CREATE TABLE " + table + " (id INTEGER PRIMARY KEY, name TEXT, flag INTEGER, updated_at DATETIME)"
	if _, err := executor.Execute(ctx, d1.ExecuteRequest{Mode: d1.ModeExec, SQL: createSQL}); err != nil {
		return nil, fmt.Errorf("create benchmark table: %w", err)
	}
	defer func() {
		_, _ = executor.Execute(ctx, d1.ExecuteRequest{Mode: d1.ModeExec, SQL: "DROP TABLE IF EXISTS " + table})
	}()

	reports := []operationReport{
		measureOperation("insert_one", sampleCount, 1, func(index int) (d1.ResultMeta, error) {
			result, err := executor.Execute(ctx, d1.ExecuteRequest{
				Mode:   d1.ModeExec,
				SQL:    "INSERT INTO " + table + " (id, name, flag, updated_at) VALUES (?, ?, ?, ?)",
				Params: []any{index + 1, "before", true, time.Now().UTC()},
			})
			return result.ResultSet.Meta, err
		}),
		measureOperation("update_one", sampleCount, 1, func(index int) (d1.ResultMeta, error) {
			result, err := executor.Execute(ctx, d1.ExecuteRequest{
				Mode:   d1.ModeExec,
				SQL:    "UPDATE " + table + " SET name = ? WHERE id = ?",
				Params: []any{"after", index + 1},
			})
			return result.ResultSet.Meta, err
		}),
		measureOperation("delete_one", sampleCount, 1, func(index int) (d1.ResultMeta, error) {
			result, err := executor.Execute(ctx, d1.ExecuteRequest{
				Mode:   d1.ModeExec,
				SQL:    "DELETE FROM " + table + " WHERE id = ?",
				Params: []any{index + 1},
			})
			return result.ResultSet.Meta, err
		}),
		measureOperation("crud_cycle", sampleCount, 1, func(index int) (d1.ResultMeta, error) {
			rowID := sampleCount + index + 1
			var meta d1.ResultMeta
			for _, request := range []d1.ExecuteRequest{
				{Mode: d1.ModeExec, SQL: "INSERT INTO " + table + " (id, name) VALUES (?, ?)", Params: []any{rowID, "before"}},
				{Mode: d1.ModeQuery, SQL: "SELECT id, name FROM " + table + " WHERE id = ?", Params: []any{rowID}},
				{Mode: d1.ModeExec, SQL: "UPDATE " + table + " SET name = ? WHERE id = ?", Params: []any{"after", rowID}},
				{Mode: d1.ModeExec, SQL: "DELETE FROM " + table + " WHERE id = ?", Params: []any{rowID}},
			} {
				result, err := executor.Execute(ctx, request)
				meta = mergeMeta(meta, result.ResultSet.Meta)
				if err != nil {
					return meta, err
				}
			}
			return meta, nil
		}),
		measureOperation("batch_crud", sampleCount, 1, func(index int) (d1.ResultMeta, error) {
			rowID := sampleCount*2 + index + 1
			result, err := executor.Batch(ctx, d1.BatchRequest{Statements: []d1.ExecuteRequest{
				{Mode: d1.ModeExec, SQL: "INSERT INTO " + table + " (id, name) VALUES (?, ?)", Params: []any{rowID, "before"}},
				{Mode: d1.ModeExec, SQL: "UPDATE " + table + " SET name = ? WHERE id = ?", Params: []any{"after", rowID}},
				{Mode: d1.ModeQuery, SQL: "SELECT id, name FROM " + table + " WHERE id = ?", Params: []any{rowID}},
				{Mode: d1.ModeExec, SQL: "DELETE FROM " + table + " WHERE id = ?", Params: []any{rowID}},
			}})
			return combineResultMeta(result.Results), err
		}),
	}

	db, err := gorm.Open(gormd1.OpenConfig(cfg), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, fmt.Errorf("open gorm for migration benchmark: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	defer sqlDB.Close()
	migrationTables := make([]string, 0, sampleCount)
	reports = append(reports, measureOperation("auto_migrate", sampleCount, 1, func(index int) (d1.ResultMeta, error) {
		migrationTable := fmt.Sprintf("%s_migrate_%d", table, index)
		migrationTables = append(migrationTables, migrationTable)
		return d1.ResultMeta{}, db.Table(migrationTable).AutoMigrate(&benchRow{})
	}))
	for _, migrationTable := range migrationTables {
		_ = db.Exec("DROP TABLE IF EXISTS " + migrationTable).Error
	}
	return reports, nil
}

func measureOperation(name string, count, concurrency int, fn sampleFunc) operationReport {
	samples := make([]sample, 0, count)
	if concurrency <= 1 {
		for index := 0; index < count; index++ {
			started := time.Now()
			meta, err := fn(index)
			samples = append(samples, sample{duration: time.Since(started), meta: meta, err: err})
		}
		return summarize(name, samples)
	}

	jobs := make(chan int)
	results := make(chan sample, count)
	workers := min(concurrency, count)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				started := time.Now()
				meta, err := fn(index)
				results <- sample{duration: time.Since(started), meta: meta, err: err}
			}
		}()
	}
	go func() {
		for index := 0; index < count; index++ {
			jobs <- index
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	for result := range results {
		samples = append(samples, result)
	}
	return summarize(name, samples)
}

func summarize(name string, samples []sample) operationReport {
	report := operationReport{Name: name, Attempted: len(samples)}
	durations := make([]float64, 0, len(samples))
	servedBy := map[string]struct{}{}
	for _, one := range samples {
		if one.err != nil {
			report.Failed++
			if report.FirstError == "" {
				report.FirstError = one.err.Error()
			}
			continue
		}
		report.Succeeded++
		durations = append(durations, float64(one.duration)/float64(time.Millisecond))
		report.D1Meta.Changes += one.meta.Changes
		report.D1Meta.Duration += one.meta.Duration
		report.D1Meta.RowsRead += one.meta.RowsRead
		report.D1Meta.RowsWritten += one.meta.RowsWritten
		report.D1Meta.LastSizeAfter = one.meta.SizeAfter
		if one.meta.ServedBy != "" {
			servedBy[one.meta.ServedBy] = struct{}{}
		}
	}
	if report.Attempted > 0 {
		report.FailureRate = float64(report.Failed) / float64(report.Attempted)
	}
	for value := range servedBy {
		report.D1Meta.ServedBy = append(report.D1Meta.ServedBy, value)
	}
	sort.Strings(report.D1Meta.ServedBy)
	if len(durations) == 0 {
		return report
	}
	sort.Float64s(durations)
	var total float64
	for _, duration := range durations {
		total += duration
	}
	report.MinMS = durations[0]
	report.MeanMS = total / float64(len(durations))
	report.P50MS = percentile(durations, 0.50)
	report.P95MS = percentile(durations, 0.95)
	report.P99MS = percentile(durations, 0.99)
	report.MaxMS = durations[len(durations)-1]
	return report
}

func percentile(sortedValues []float64, quantile float64) float64 {
	if len(sortedValues) == 0 {
		return 0
	}
	index := int(math.Ceil(quantile*float64(len(sortedValues)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sortedValues) {
		index = len(sortedValues) - 1
	}
	return sortedValues[index]
}

func configFromEnv(mode d1.ExecutorMode) (d1.Config, error) {
	switch mode {
	case d1.ExecutorModeREST:
		cfg := d1.Config{
			Mode:       mode,
			AccountID:  firstEnv("D1_ACCOUNT_ID", "ACCOUNT_ID"),
			APIToken:   firstEnv("D1_API_TOKEN", "API_TOKEN"),
			DatabaseID: firstEnv("D1_DATABASE_ID", "DATABASE_ID"),
			BaseURL:    os.Getenv("D1_REST_BASE_URL"),
		}
		if cfg.AccountID == "" || cfg.APIToken == "" || cfg.DatabaseID == "" {
			return d1.Config{}, errors.New("REST mode requires D1_ACCOUNT_ID, D1_API_TOKEN and D1_DATABASE_ID")
		}
		return cfg, cfg.Validate()
	case d1.ExecutorModeWorker:
		cfg := d1.Config{
			Mode:           mode,
			WorkerEndpoint: os.Getenv("D1_WORKER_ENDPOINT"),
			WorkerToken:    os.Getenv("D1_WORKER_TOKEN"),
		}
		if cfg.WorkerEndpoint == "" || cfg.WorkerToken == "" {
			return d1.Config{}, errors.New("worker mode requires D1_WORKER_ENDPOINT and D1_WORKER_TOKEN")
		}
		return cfg, cfg.Validate()
	default:
		return d1.Config{}, fmt.Errorf("unsupported mode %q", mode)
	}
}

func expectSingleInt(result d1.ResultSet, want int64) error {
	if len(result.Rows) != 1 || len(result.Rows[0]) == 0 {
		return fmt.Errorf("expected one row, got %d", len(result.Rows))
	}
	got, err := asInt64(result.Rows[0][0])
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("expected %d, got %d", want, got)
	}
	return nil
}

func asInt64(value any) (int64, error) {
	switch typed := value.(type) {
	case int:
		return int64(typed), nil
	case int64:
		return typed, nil
	case float64:
		return int64(typed), nil
	case json.Number:
		return typed.Int64()
	case string:
		return strconv.ParseInt(typed, 10, 64)
	default:
		return 0, fmt.Errorf("expected integer result, got %T", value)
	}
}

func combineResultMeta(results []d1.ExecuteResult) d1.ResultMeta {
	var combined d1.ResultMeta
	for _, result := range results {
		combined = mergeMeta(combined, result.ResultSet.Meta)
	}
	return combined
}

func mergeMeta(left, right d1.ResultMeta) d1.ResultMeta {
	left.ChangedDB = left.ChangedDB || right.ChangedDB
	left.Changes += right.Changes
	left.Duration += right.Duration
	left.RowsRead += right.RowsRead
	left.RowsWritten += right.RowsWritten
	if right.LastRowID != 0 {
		left.LastRowID = right.LastRowID
	}
	if right.ServedBy != "" {
		left.ServedBy = right.ServedBy
	}
	if right.SizeAfter != 0 {
		left.SizeAfter = right.SizeAfter
	}
	return left
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
