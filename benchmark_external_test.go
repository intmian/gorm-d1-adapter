package d1_test

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	d1 "github.com/intmian/gorm-d1-adapter"
	"github.com/intmian/gorm-d1-adapter/gormd1"
	_ "github.com/intmian/gorm-d1-adapter/stdlib"
	"gorm.io/gorm"
)

func BenchmarkGormOpen(b *testing.B) {
	cfg := integrationConfig(b)
	for i := 0; i < b.N; i++ {
		db, err := gorm.Open(gormd1.OpenConfig(cfg), &gorm.Config{})
		if err != nil {
			b.Fatal(err)
		}
		closeIntegrationGorm(b, db)
	}
}

func BenchmarkGormOpenAndPing(b *testing.B) {
	cfg := integrationConfig(b)
	for i := 0; i < b.N; i++ {
		db, err := gorm.Open(gormd1.OpenConfig(cfg), &gorm.Config{})
		if err != nil {
			b.Fatal(err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			b.Fatal(err)
		}
		if err := sqlDB.Ping(); err != nil {
			b.Fatal(err)
		}
		if err := sqlDB.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStdlibOpen(b *testing.B) {
	cfg := integrationConfig(b)
	dsn := d1.NewDSN(cfg)
	for i := 0; i < b.N; i++ {
		db, err := sql.Open(d1.DriverName, dsn)
		if err != nil {
			b.Fatal(err)
		}
		if err := db.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStdlibOpenAndPing(b *testing.B) {
	cfg := integrationConfig(b)
	dsn := d1.NewDSN(cfg)
	for i := 0; i < b.N; i++ {
		db, err := sql.Open(d1.DriverName, dsn)
		if err != nil {
			b.Fatal(err)
		}
		if err := db.Ping(); err != nil {
			b.Fatal(err)
		}
		if err := db.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGormSelectOne(b *testing.B) {
	db := openIntegrationGorm(b)
	defer closeIntegrationGorm(b, db)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var one int
		if err := db.Raw("SELECT 1").Scan(&one).Error; err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStdlibSelectOne(b *testing.B) {
	db, err := sql.Open(d1.DriverName, d1.NewDSN(integrationConfig(b)))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var one int
		if err := db.QueryRow("SELECT 1").Scan(&one); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGormParameterizedSelect(b *testing.B) {
	db := openIntegrationGorm(b)
	defer closeIntegrationGorm(b, db)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out struct {
			IntValue  int64
			TextValue string
		}
		if err := db.Raw("SELECT ? AS int_value, ? AS text_value", i, "benchmark").Scan(&out).Error; err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConcurrentSelectOne(b *testing.B) {
	db := openIntegrationGorm(b)
	defer closeIntegrationGorm(b, db)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			var one int
			if err := db.Raw("SELECT 1").Scan(&one).Error; err != nil {
				b.Error(err)
				return
			}
		}
	})
}

func BenchmarkExecutorBatchSelect(b *testing.B) {
	executor, err := d1.NewExecutor(integrationConfig(b))
	if err != nil {
		b.Fatal(err)
	}
	if err := executor.Verify(context.Background()); err != nil {
		b.Fatal(err)
	}
	req := d1.BatchRequest{Statements: []d1.ExecuteRequest{
		{Mode: d1.ModeQuery, SQL: "SELECT ? AS value", Params: []any{1}},
		{Mode: d1.ModeQuery, SQL: "SELECT ? AS value", Params: []any{2}},
	}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := executor.Batch(context.Background(), req); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAutoMigrateSmallTable(b *testing.B) {
	requireWrites(b)
	db := openIntegrationGorm(b)
	defer closeIntegrationGorm(b, db)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		table := uniqueTable(fmt.Sprintf("d1_adapter_migrate_%d", i))
		if err := db.Table(table).AutoMigrate(&integrationRow{}); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		_ = db.Exec("DROP TABLE IF EXISTS " + table).Error
		b.StartTimer()
	}
}

func BenchmarkInsertOne(b *testing.B) {
	requireWrites(b)
	db := openIntegrationGorm(b)
	defer closeIntegrationGorm(b, db)
	table := uniqueTable("d1_adapter_insert")
	mustCreateBenchmarkTable(b, db, table)
	defer dropBenchmarkTable(b, db, table)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := db.Exec("INSERT INTO "+table+" (id, name, flag, updated_at) VALUES (?, ?, ?, ?)", i+1, "insert", true, time.Now().UTC()).Error; err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUpdateOne(b *testing.B) {
	requireWrites(b)
	db := openIntegrationGorm(b)
	defer closeIntegrationGorm(b, db)
	table := uniqueTable("d1_adapter_update")
	mustCreateBenchmarkTable(b, db, table)
	defer dropBenchmarkTable(b, db, table)
	if err := db.Exec("INSERT INTO "+table+" (id, name) VALUES (?, ?)", 1, "before").Error; err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := db.Exec("UPDATE "+table+" SET name = ? WHERE id = ?", fmt.Sprintf("after_%d", i), 1).Error; err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDeleteOne(b *testing.B) {
	requireWrites(b)
	db := openIntegrationGorm(b)
	defer closeIntegrationGorm(b, db)
	table := uniqueTable("d1_adapter_delete")
	mustCreateBenchmarkTable(b, db, table)
	defer dropBenchmarkTable(b, db, table)
	for i := 0; i < b.N; i++ {
		if err := db.Exec("INSERT INTO "+table+" (id, name) VALUES (?, ?)", i+1, "delete").Error; err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := db.Exec("DELETE FROM "+table+" WHERE id = ?", i+1).Error; err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkInsertSelectUpdateDelete(b *testing.B) {
	requireWrites(b)
	db := openIntegrationGorm(b)
	defer closeIntegrationGorm(b, db)

	table := uniqueTable("d1_adapter_crud_bench")
	if err := db.Table(table).AutoMigrate(&integrationRow{}); err != nil {
		b.Fatal(err)
	}
	defer db.Exec("DROP TABLE IF EXISTS " + table)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rowID := int64(i + 1)
		row := integrationRow{ID: rowID, Name: "before", Flag: true, UpdatedAt: time.Now().UTC()}
		if err := db.Table(table).Create(&row).Error; err != nil {
			b.Fatal(err)
		}
		var got integrationRow
		if err := db.Table(table).Where("id = ?", rowID).First(&got).Error; err != nil {
			b.Fatal(err)
		}
		if err := db.Table(table).Where("id = ?", rowID).Update("name", "after").Error; err != nil {
			b.Fatal(err)
		}
		if err := db.Table(table).Delete(&integrationRow{}, rowID).Error; err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExecutorBatchCRUD(b *testing.B) {
	requireWrites(b)
	cfg := integrationConfig(b)
	db := openIntegrationGorm(b)
	defer closeIntegrationGorm(b, db)
	table := uniqueTable("d1_adapter_batch_crud")
	mustCreateBenchmarkTable(b, db, table)
	defer dropBenchmarkTable(b, db, table)

	executor, err := d1.NewExecutor(cfg)
	if err != nil {
		b.Fatal(err)
	}
	if err := executor.Verify(context.Background()); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rowID := i + 1
		req := d1.BatchRequest{Statements: []d1.ExecuteRequest{
			{Mode: d1.ModeExec, SQL: "INSERT INTO " + table + " (id, name) VALUES (?, ?)", Params: []any{rowID, "before"}},
			{Mode: d1.ModeExec, SQL: "UPDATE " + table + " SET name = ? WHERE id = ?", Params: []any{"after", rowID}},
			{Mode: d1.ModeQuery, SQL: "SELECT id, name FROM " + table + " WHERE id = ?", Params: []any{rowID}},
			{Mode: d1.ModeExec, SQL: "DELETE FROM " + table + " WHERE id = ?", Params: []any{rowID}},
		}}
		if _, err := executor.Batch(context.Background(), req); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFiveConcurrentOpenAndAutoMigrate(b *testing.B) {
	requireWrites(b)
	cfg := integrationConfig(b)

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		tables := make([]string, 5)
		for index := range tables {
			tables[index] = uniqueTable(fmt.Sprintf("d1_adapter_startup_%d", index))
		}
		b.StartTimer()

		var wg sync.WaitGroup
		errs := make(chan error, len(tables))
		for _, table := range tables {
			wg.Add(1)
			go func(table string) {
				defer wg.Done()
				db, err := gorm.Open(gormd1.OpenConfig(cfg), &gorm.Config{})
				if err != nil {
					errs <- err
					return
				}
				defer func() {
					sqlDB, dbErr := db.DB()
					if dbErr == nil {
						_ = sqlDB.Close()
					}
				}()
				if err := db.Table(table).AutoMigrate(&integrationRow{}); err != nil {
					errs <- err
				}
			}(table)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				b.Fatal(err)
			}
		}

		b.StopTimer()
		cleanupDB := openIntegrationGorm(b)
		for _, table := range tables {
			dropBenchmarkTable(b, cleanupDB, table)
		}
		closeIntegrationGorm(b, cleanupDB)
		b.StartTimer()
	}
}

func BenchmarkStdlibPreparedSelectOne(b *testing.B) {
	db, err := sql.Open(d1.DriverName, d1.NewDSN(integrationConfig(b)))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	stmt, err := db.Prepare("SELECT ?")
	if err != nil {
		b.Fatal(err)
	}
	defer stmt.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var value int
		if err := stmt.QueryRow(1).Scan(&value); err != nil {
			b.Fatal(err)
		}
	}
}

func mustCreateBenchmarkTable(tb testing.TB, db *gorm.DB, table string) {
	tb.Helper()
	if err := db.Exec("CREATE TABLE IF NOT EXISTS " + table + " (id INTEGER PRIMARY KEY, name TEXT, flag INTEGER, payload BLOB, updated_at DATETIME)").Error; err != nil {
		tb.Fatal(err)
	}
}

func dropBenchmarkTable(tb testing.TB, db *gorm.DB, table string) {
	tb.Helper()
	if err := db.Exec("DROP TABLE IF EXISTS " + table).Error; err != nil {
		tb.Logf("drop table %s failed: %v", table, err)
	}
}
