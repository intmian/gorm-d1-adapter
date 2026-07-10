# 性能测试

[English](BENCHMARK.md) · [主 README](README.zh-CN.md)

## 报告规范

所有公开的性能结果必须来自与目标部署位于同一区域的测试环境。本地 Windows 运行仅用于开发检查，不能放进性能表格、发布决策或对外声明。

正式对比需满足以下条件：

1. 请求来源与目标部署处于同一区域。
2. 运行时：Ubuntu Linux x86_64 上的 `golang:1.25` Docker 镜像。
3. Legacy Adapter、REST 与 Worker 使用同一个 Cloudflare D1 数据库。
4. 百分位测试套件：每个操作 100 次采样、并发 8、包含写入。
5. Go 基准测试套件：`-benchmem -benchtime 10x`。
6. 正确性测试必须通过，采样失败数为 0，清理后临时表数量为 0。

正式运行 ID：`20260710-162100`。汇总结果如下。原始文件保存在 git-ignored 的 `runs/` 目录，不属于源码发布内容。

## 验证状态

日期：2026-07-10

- REST 正确性：PASS
- Worker 正确性：PASS
- Worker batch 回滚：PASS
- Legacy Adapter 正确性：PASS
- 采样操作：REST 1100/1100，Worker 1100/1100
- 标准 Go 基准：REST 17/17，Worker 17/17
- Legacy Adapter Go 基准：15/15
- 剩余临时 D1 表：0
- 剩余远程容器与临时文件：0

## REST 与 Worker 百分位

| 操作 | REST p50 ms | Worker p50 ms | p50 比率 | REST p95 ms | Worker p95 ms | p95 比率 |
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

单语句 p50 提升 1.61x 至 1.88x。两次读取合并为一次 Worker batch，p50 提升 3.28x。原子 batch CRUD 提升 6.62x。`verify` 不是 SQL 延迟对比——REST 调用 Cloudflare 控制面的 token 校验接口，Worker 调用边缘 health 接口，二者不可直接比较。

## 标准 Go 基准

| 基准 | REST ms/op | Worker ms/op | 比率 |
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

`StdlibOpen` 被有意排除，因为它只分配本地 `database/sql` 句柄，不执行网络操作。

## Legacy Adapter、REST 与 Worker

以下严格对比使用匹配的基准工作负载、同一区域来源、同一个 D1 数据库、相同 Docker 运行时和 `10x` 设置。

| 可比基准 | Legacy ms/op | REST ms/op | Worker ms/op | REST/Worker | Legacy/Worker |
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

以下同名工作负载在不同测试版本间有变化，仅供参考，不是严格的传输对比：

| 基准 | Legacy ms/op | Worker ms/op | 比率 | 工作负载差异 |
|---|---:|---:|---:|---|
| AutoMigrateSmallTable | 173.62 | 129.11 | 1.34x | 当前模型新增 BLOB 字段 |
| InsertSelectUpdateDelete | 356.59 | 260.36 | 1.37x | Legacy 使用直接 SQL；当前适配器使用 GORM 模型 API |
| FiveConcurrentOpenAndAutoMigrate | 437.70 | 155.46 | 2.82x | Legacy 使用五个模型；当前适配器使用一个模型形状 |

Legacy Adapter 没有真正的 batch 等价物。将其普通 CRUD 的 356.59 ms/op 与 Worker `ExecutorBatchCRUD` 的 71.65 ms/op 对比，差距 4.98×，但 batch 同时改变了请求形式和原子性——这是架构升级，不是公平对比。

## 运行检查

PowerShell 脚本可在开发机上执行正确性与探索性性能检查：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/benchmark.ps1 -Mode both -Samples 30 -Concurrency 8 -Full
```

本地延迟结果不是正式基准。正式基准必须在与目标部署同一区域运行相同套件，并将原始 JSON 和 Go 基准日志归档至 `runs/benchmark-<timestamp>/`。

脚本不会写出 secret 值。缺少凭据时会生成 `blocked` 或 `partial` 报告，只列出缺失的变量名。

## 解读规则

1. 仅当区域、D1 数据库、样本数、并发、写入模式和测试窗口全部一致时，才能对比传输方式。
2. 每份延迟报告都必须附带正确性、失败数和清理结果。
3. 不要将百分位测试结果与 Go `ms/op` 结果混在同一个比率中——测量框架不同，数值不可直接类比。
4. 如果工作负载在不同版本间有差异，必须明确标注。
5. 对外发布稳定性能结论前，需要至少三次成功的同区域运行。当前结果是有效的性能数据，不是稳定性承诺。
