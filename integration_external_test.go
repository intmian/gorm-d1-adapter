package d1_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	d1 "github.com/intmian/gorm-d1-adapter"
	"github.com/intmian/gorm-d1-adapter/gormd1"
	_ "github.com/intmian/gorm-d1-adapter/stdlib"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type integrationRow struct {
	ID        int64 `gorm:"primaryKey"`
	Name      string
	Flag      bool
	Payload   []byte
	UpdatedAt time.Time
}

func integrationConfig(tb testing.TB) d1.Config {
	tb.Helper()
	mode := strings.ToLower(os.Getenv("D1_EXECUTOR_MODE"))
	if mode == "" {
		if firstEnv("D1_WORKER_ENDPOINT") != "" && firstEnv("D1_WORKER_TOKEN") != "" && firstEnv("D1_ACCOUNT_ID", "ACCOUNT_ID") == "" {
			mode = string(d1.ExecutorModeWorker)
		} else {
			mode = string(d1.ExecutorModeREST)
		}
	}

	if mode == string(d1.ExecutorModeWorker) {
		endpoint := firstEnv("D1_WORKER_ENDPOINT")
		token := firstEnv("D1_WORKER_TOKEN")
		if endpoint == "" || token == "" {
			tb.Skip("worker mode requires D1_WORKER_ENDPOINT and D1_WORKER_TOKEN")
		}
		return d1.Config{
			Mode:           d1.ExecutorModeWorker,
			WorkerEndpoint: endpoint,
			WorkerToken:    token,
		}
	}
	if mode != string(d1.ExecutorModeREST) {
		tb.Fatalf("unsupported D1_EXECUTOR_MODE %q", mode)
	}

	accountID := firstEnv("D1_ACCOUNT_ID", "ACCOUNT_ID")
	apiToken := firstEnv("D1_API_TOKEN", "API_TOKEN")
	databaseID := firstEnv("D1_DATABASE_ID", "DATABASE_ID")
	if accountID == "" || apiToken == "" || databaseID == "" {
		tb.Skip("REST mode requires D1_ACCOUNT_ID, D1_API_TOKEN and D1_DATABASE_ID")
	}
	return d1.Config{
		Mode:       d1.ExecutorModeREST,
		AccountID:  accountID,
		APIToken:   apiToken,
		DatabaseID: databaseID,
		BaseURL:    firstEnv("D1_REST_BASE_URL"),
	}
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

func requireWrites(tb testing.TB) {
	tb.Helper()
	if os.Getenv("D1_INCLUDE_WRITES") != "1" {
		tb.Skip("set D1_INCLUDE_WRITES=1 to run write tests")
	}
}

func openIntegrationGorm(tb testing.TB) *gorm.DB {
	tb.Helper()
	db, err := gorm.Open(gormd1.OpenConfig(integrationConfig(tb)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		tb.Fatal(err)
	}
	return db
}

func closeIntegrationGorm(tb testing.TB, db *gorm.DB) {
	tb.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		tb.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		tb.Fatal(err)
	}
}

func uniqueTable(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func TestIntegrationStdlibQuery(t *testing.T) {
	cfg := integrationConfig(t)
	db, err := sql.Open(d1.DriverName, d1.NewDSN(cfg))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var one int
	if err := db.QueryRow("SELECT 1").Scan(&one); err != nil {
		t.Fatal(err)
	}
	if one != 1 {
		t.Fatalf("expected 1, got %d", one)
	}
}

func TestIntegrationExecutorBatchQuery(t *testing.T) {
	executor, err := d1.NewExecutor(integrationConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err := executor.Batch(context.Background(), d1.BatchRequest{
		Statements: []d1.ExecuteRequest{
			{Mode: d1.ModeQuery, SQL: "SELECT ? AS value", Params: []any{1}},
			{Mode: d1.ModeQuery, SQL: "SELECT ? AS value", Params: []any{2}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 2 || len(result.Results[0].ResultSet.Rows) != 1 || len(result.Results[1].ResultSet.Rows) != 1 {
		t.Fatalf("unexpected batch result: %+v", result)
	}
}

func TestIntegrationWorkerBatchRollsBackOnFailure(t *testing.T) {
	requireWrites(t)
	cfg := integrationConfig(t)
	if cfg.Mode != d1.ExecutorModeWorker {
		t.Skip("atomic batch rollback is specific to the Worker executor")
	}
	executor, err := d1.NewExecutor(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	table := uniqueTable("d1_adapter_batch_rollback")
	if _, err := executor.Execute(context.Background(), d1.ExecuteRequest{
		Mode: d1.ModeExec,
		SQL:  "CREATE TABLE " + table + " (id INTEGER PRIMARY KEY)",
	}); err != nil {
		t.Fatal(err)
	}
	defer executor.Execute(context.Background(), d1.ExecuteRequest{Mode: d1.ModeExec, SQL: "DROP TABLE IF EXISTS " + table})

	_, err = executor.Batch(context.Background(), d1.BatchRequest{Statements: []d1.ExecuteRequest{
		{Mode: d1.ModeExec, SQL: "INSERT INTO " + table + " (id) VALUES (?)", Params: []any{1}},
		{Mode: d1.ModeExec, SQL: "INSERT INTO d1_adapter_table_that_does_not_exist (id) VALUES (?)", Params: []any{1}},
	}})
	if err == nil {
		t.Fatal("expected batch failure")
	}

	result, err := executor.Execute(context.Background(), d1.ExecuteRequest{
		Mode:   d1.ModeQuery,
		SQL:    "SELECT COUNT(*) AS value FROM " + table + " WHERE id = ?",
		Params: []any{1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ResultSet.Rows) != 1 || len(result.ResultSet.Rows[0]) != 1 || result.ResultSet.Rows[0][0] != float64(0) {
		t.Fatalf("failed Worker batch was not rolled back: %+v", result.ResultSet)
	}
}

func TestIntegrationGormQueryAndEmptyResult(t *testing.T) {
	db := openIntegrationGorm(t)
	defer closeIntegrationGorm(t, db)

	var one int
	if err := db.Raw("SELECT 1").Scan(&one).Error; err != nil {
		t.Fatal(err)
	}
	if one != 1 {
		t.Fatalf("expected 1, got %d", one)
	}

	var rows []struct{ Value int }
	if err := db.Raw("SELECT 1 AS value WHERE 1 = 0").Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected no rows, got %d", len(rows))
	}
}

func TestIntegrationGormCRUDAndUnsupportedTransaction(t *testing.T) {
	requireWrites(t)

	db := openIntegrationGorm(t)
	defer closeIntegrationGorm(t, db)

	table := uniqueTable("d1_adapter_crud")
	if err := db.Table(table).AutoMigrate(&integrationRow{}); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP TABLE IF EXISTS " + table)

	payload := []byte{1, 2, 3, 4}
	row := integrationRow{ID: 1, Name: "before", Flag: true, Payload: payload, UpdatedAt: time.Now().UTC()}
	if err := db.Table(table).Create(&row).Error; err != nil {
		t.Fatal(err)
	}

	var got integrationRow
	if err := db.Table(table).Where("id = ?", 1).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.Name != "before" || !got.Flag || string(got.Payload) != string(payload) {
		t.Fatalf("unexpected row: %+v", got)
	}

	txCalled := false
	err := db.Transaction(func(tx *gorm.DB) error {
		txCalled = true
		return nil
	})
	if !errors.Is(err, d1.ErrUnsupportedTransaction) {
		t.Fatalf("expected unsupported transaction, got %v", err)
	}
	if txCalled {
		t.Fatal("transaction callback should not run when Begin fails")
	}
}

func TestIntegrationGormDialectMetadataAndForeignKeyDrop(t *testing.T) {
	requireWrites(t)

	db := openIntegrationGorm(t)
	defer closeIntegrationGorm(t, db)

	parentTable := uniqueTable("d1_adapter_parent")
	childTable := uniqueTable("d1_adapter_child")
	defer db.Exec("DROP TABLE IF EXISTS " + childTable)
	defer db.Exec("DROP TABLE IF EXISTS " + parentTable)

	if err := db.Exec("CREATE TABLE " + parentTable + " (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE " + childTable + " (id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL REFERENCES " + parentTable + "(id))").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO " + parentTable + " (id) VALUES (1), (2)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO " + childTable + " (id, parent_id) VALUES (1, 1)").Error; err != nil {
		t.Fatal(err)
	}

	var rows []struct{ ID int64 }
	if err := db.Table(parentTable).Select("id").Order("id").Offset(1).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != 2 {
		t.Fatalf("unexpected offset rows: %+v", rows)
	}

	tables, err := db.Migrator().GetTables()
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(tables, parentTable) || !containsString(tables, childTable) {
		t.Fatalf("migrator table list is missing test tables: %v", tables)
	}

	if err := db.Migrator().DropTable(parentTable); err == nil {
		t.Fatal("expected foreign key enforcement when dropping a referenced parent table")
	}
	if err := db.Migrator().DropTable(parentTable, childTable); err != nil {
		t.Fatal(err)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
