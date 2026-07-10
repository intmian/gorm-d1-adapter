# Benchmarks

[中文文档](BENCHMARK.zh-CN.md) · [Main README](README.md)

## Reporting Policy

All published performance results must come from a test environment in the same geographic region as the target deployment. Local Windows runs are for development checks only — don't use them for performance tables, release decisions, or external claims.

The official comparison holds these conditions fixed:

1. Request origin and deployment target: same region.
2. Runtime: `golang:1.25` Docker image on Ubuntu Linux x86_64.
3. Database: the same Cloudflare D1 database for the Legacy Adapter, REST, and Worker.
4. Percentile suite: 100 samples per operation, concurrency 8, writes enabled.
5. Go benchmark suite: `-benchmem -benchtime 10x`.
6. Correctness suites must pass with zero sampled failures and zero temporary tables remaining after cleanup.

Official run ID: `20260710-162100`. The consolidated result is below. Raw artifacts live in the git-ignored `runs/` directory and are not part of the source distribution.

## Verification Status

Date: 2026-07-10

- REST correctness: PASS
- Worker correctness: PASS
- Worker batch rollback: PASS
- Legacy Adapter correctness: PASS
- Sampled operations: REST 1100/1100, Worker 1100/1100
- Standard Go benchmarks: REST 17/17, Worker 17/17
- Legacy Adapter Go benchmarks: 15/15
- Remaining temporary D1 tables: 0
- Remaining remote containers and temp files: 0

## REST vs Worker Percentiles

| Operation | REST p50 ms | Worker p50 ms | p50 ratio | REST p95 ms | Worker p95 ms | p95 ratio |
|---|---:|---:|---:|---:|---:|---:|
| verify | 208.99 | 10.66 | 19.60x | 244.56 | 12.15 | 20.12x |
| select_one | 106.53 | 63.20 | 1.69x | 127.07 | 74.56 | 1.70x |
| parameterized_select | 102.45 | 61.31 | 1.67x | 117.91 | 68.74 | 1.72x |
| batch_select_two | 203.78 | 62.08 | 3.28x | 251.49 | 69.64 | 3.61x |
| concurrent_select_one | 134.35 | 71.52 | 1.88x | 189.00 | 92.30 | 2.05x |
| insert_one | 104.85 | 65.14 | 1.61x | 125.24 | 74.17 | 1.69x |
| update_one | 106.70 | 64.00 | 1.67x | 144.58 | 70.44 | 2.05x |
| delete_one | 111.65 | 64.03 | 1.74x | 151.76 | 71.75 | 2.12x |
| crud_cycle | 430.65 | 254.54 | 1.69x | 497.50 | 281.40 | 1.77x |
| batch_crud | 428.84 | 64.79 | 6.62x | 546.44 | 88.83 | 6.15x |
| auto_migrate | 216.77 | 127.15 | 1.70x | 251.48 | 144.66 | 1.74x |

Single-statement p50 improves 1.61x to 1.88x. Combining two reads into one Worker batch improves p50 by 3.28x. Atomic batch CRUD improves p50 by 6.62x. The `verify` result is not a SQL latency comparison — REST calls the Cloudflare control plane's token verification endpoint, while Worker calls its edge health endpoint.

## Standard Go Benchmarks

| Benchmark | REST ms/op | Worker ms/op | Ratio |
|---|---:|---:|---:|
| GormOpen | 213.52 | 11.93 | 17.89x |
| GormOpenAndPing | 212.86 | 11.38 | 18.71x |
| StdlibOpenAndPing | 211.45 | 11.29 | 18.73x |
| GormSelectOne | 116.30 | 64.58 | 1.80x |
| StdlibSelectOne | 130.03 | 61.70 | 2.11x |
| GormParameterizedSelect | 102.93 | 64.93 | 1.59x |
| ConcurrentSelectOne | 67.16 | 31.43 | 2.14x |
| ExecutorBatchSelect | 209.60 | 63.15 | 3.32x |
| InsertOne | 121.51 | 70.65 | 1.72x |
| UpdateOne | 117.53 | 71.03 | 1.65x |
| DeleteOne | 132.25 | 81.70 | 1.62x |
| InsertSelectUpdateDelete | 449.36 | 260.36 | 1.73x |
| ExecutorBatchCRUD | 450.56 | 71.65 | 6.29x |
| AutoMigrateSmallTable | 234.10 | 129.11 | 1.81x |
| FiveConcurrentOpenAndAutoMigrate | 541.64 | 155.46 | 3.48x |
| StdlibPreparedSelectOne | 108.14 | 61.34 | 1.76x |

`StdlibOpen` is intentionally excluded — it only allocates a local `database/sql` handle and performs no network operation.

## Legacy Adapter, REST, and Worker

These results use matching benchmark workloads from the same region, D1 database, Docker runtime, and `10x` setting.

| Comparable benchmark | Legacy ms/op | REST ms/op | Worker ms/op | REST/Worker | Legacy/Worker |
|---|---:|---:|---:|---:|---:|
| GormOpen | 199.80 | 213.52 | 11.93 | 17.89x | 16.74x |
| GormOpenAndPing | 204.63 | 212.86 | 11.38 | 18.71x | 17.98x |
| StdlibOpenAndPing | 204.39 | 211.45 | 11.29 | 18.73x | 18.10x |
| GormSelectOne | 114.36 | 116.30 | 64.58 | 1.80x | 1.77x |
| StdlibSelectOne | 114.31 | 130.03 | 61.70 | 2.11x | 1.85x |
| GormParameterizedSelect | 94.31 | 102.93 | 64.93 | 1.59x | 1.45x |
| ConcurrentSelectOne | 53.15 | 67.16 | 31.43 | 2.14x | 1.69x |
| InsertOne | 93.86 | 121.51 | 70.65 | 1.72x | 1.33x |
| UpdateOne | 104.89 | 117.53 | 71.03 | 1.65x | 1.48x |
| DeleteOne | 102.65 | 132.25 | 81.70 | 1.62x | 1.26x |
| StdlibPreparedSelectOne | 82.74 | 108.14 | 61.34 | 1.76x | 1.35x |

These same-name workloads changed between harness versions and are directional, not strict transport comparisons:

| Benchmark | Legacy ms/op | Worker ms/op | Ratio | Workload difference |
|---|---:|---:|---:|---|
| AutoMigrateSmallTable | 173.62 | 129.11 | 1.34x | Current model adds a BLOB field |
| InsertSelectUpdateDelete | 356.59 | 260.36 | 1.37x | Legacy uses raw SQL; current adapter uses GORM model APIs |
| FiveConcurrentOpenAndAutoMigrate | 437.70 | 155.46 | 2.82x | Legacy uses five models; current adapter uses one model shape |

The Legacy Adapter has no true batch equivalent. Comparing its regular CRUD at 356.59 ms/op with Worker `ExecutorBatchCRUD` at 71.65 ms/op gives a 4.98x architecture-level improvement, but batching changes both request shape and atomicity — it's not an apples-to-apples comparison.

## Running Checks

The PowerShell runner can execute correctness and exploratory performance checks from a developer machine:

```powershell
powershell -ExecutionPolicy Bypass -File scripts/benchmark.ps1 -Mode both -Samples 30 -Concurrency 8 -Full
```

Local latency output is not an official benchmark. A formal benchmark must run the same suite from the same region as the target deployment and archive its raw JSON and Go benchmark logs under `runs/benchmark-<timestamp>/`.

The runner never writes secret values. Missing credentials produce a `blocked` or `partial` report that only lists missing variable names.

## Interpretation Rules

1. Only compare transports when region, D1 database, sample count, concurrency, write mode, and test window all match.
2. Always include correctness, failure count, and cleanup results alongside latency data.
3. Don't mix percentile-runner values with Go `ms/op` values in one ratio — they come from different measurement frameworks.
4. If workloads changed between harness versions, label it explicitly.
5. Require at least three successful same-region runs before publishing stable performance claims. The current run is valid performance data, not a stability guarantee.
