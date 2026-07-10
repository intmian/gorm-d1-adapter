# gorm-d1-adapter

`gorm-d1-adapter` 是一个面向 Cloudflare D1 的 Go 数据库适配器，让应用可以通过 GORM 或 `database/sql` 访问 D1。

## 功能

- 支持 GORM 和 `database/sql`。
- 支持 Cloudflare REST API 与 Worker Proxy 两种访问模式。
- 提供查询、写入、迁移和批处理能力。
- 统一常用数据类型在 Go 与 D1 之间的转换。
- REST 与 Worker 模式可以通过配置切换，便于迁移和回滚。

## 项目结构

```text
gorm-d1-adapter/
├── config.go / executor.go              配置与公共执行协议
├── driver.go / connection.go            database/sql 驱动实现
├── rest_executor.go / rest_protocol.go  Cloudflare REST 客户端
├── worker_executor.go / worker_protocol.go
│                                         Worker Proxy 客户端
├── codec.go / trace.go / errors.go       公共支撑能力
├── gormd1/                               GORM 接入与迁移
├── stdlib/                               database/sql 驱动注册
├── workers/d1-proxy/                     Cloudflare Worker Proxy
├── cmd/d1bench/                          性能测试工具
└── scripts/                              验证与 benchmark 脚本
```

## 接入

```bash
go get github.com/intmian/gorm-d1-adapter
```

应用创建 `d1.Config`，选择 REST 或 Worker 模式，然后通过 `gormd1.OpenConfig` 接入 GORM：

```go
cfg := d1.Config{
    Mode: d1.ExecutorModeWorker,
    WorkerEndpoint: "https://example.workers.dev",
    WorkerToken: "<token>",
}

db, err := gorm.Open(gormd1.OpenConfig(cfg), &gorm.Config{})
```

REST 模式使用 Cloudflare Account、API Token 和 D1 Database ID；Worker 模式使用已部署的 Worker Endpoint 和访问 Token。业务代码不需要因模式切换而改变。

Worker 部署说明见 [`workers/d1-proxy/README.md`](workers/d1-proxy/README.md)，性能测试口径和结果见 [`BENCHMARK.md`](BENCHMARK.md)。
