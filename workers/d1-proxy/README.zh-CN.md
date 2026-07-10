# D1 Worker Proxy

[English](README.md) · [主 README](../../README.zh-CN.md)

这是本项目推荐的生产主力部署模式：该 Worker 在单个 D1 binding 上暴露受限的服务端 API，供 Cloudflare 之外的 Go 服务访问。

将 Account API Token 留在 Cloudflare 控制面，不要把它配置到业务服务；业务服务只需持有此 Worker 的访问 token。REST 模式仍可作为兼容、迁移和运维回退通道。

```text
GET  /health
POST /v1/query
POST /v1/batch
```

所有路由都需要 `Authorization: Bearer <AUTH_TOKEN>`。由于不支持浏览器客户端，因此不会输出 CORS 响应头。

## 配置与部署

1. 将 `wrangler.example.toml` 复制为被 Git 忽略的本地文件 `wrangler.toml`，然后替换 D1 数据库名称和 ID：

```powershell
Copy-Item wrangler.example.toml wrangler.toml
```

2. 安装依赖并验证项目：

```powershell
npm ci
npm run check
npm test
npx wrangler deploy --dry-run --outdir dist
```

3. 将认证 token 保存为 Worker secret：

```powershell
npx wrangler secret put AUTH_TOKEN
```

4. 部署 Worker：

```powershell
npm run deploy
```

不要将 `AUTH_TOKEN` 写入 `wrangler.toml`。本地开发时，将 `.dev.vars.example` 复制为 `.dev.vars`，并设置非生产 token。

## 请求协议

查询或执行：

```json
{
  "mode": "query",
  "sql": "SELECT * FROM records WHERE id = ?",
  "params": [1],
  "requestId": "optional-correlation-id"
}
```

批处理：

```json
{
  "statements": [
    {"mode": "exec", "sql": "INSERT INTO records(id) VALUES(?)", "params": [1]},
    {"mode": "query", "sql": "SELECT id FROM records WHERE id = ?", "params": [1]}
  ],
  "requestId": "optional-correlation-id"
}
```

`query` 使用 `stmt.raw({ columnNames: true })`，`exec` 使用 `stmt.run()`；batch 通过单次 `env.DB.batch(...)` 调用执行并保持语句顺序。

`d1b64:` 是 BLOB 值在线协议中的保留前缀。以此开头的文本会被解释为 Base64 编码的 BLOB；如需原样保存此类文本，调用方必须在应用层自行编码或转义。

## 安全限制

默认值在 `wrangler.example.toml` 中提供，并复制到本地 `wrangler.toml`。

| 变量 | 默认值 | 含义 |
|---|---:|---|
| `MAX_BODY_BYTES` | 1048576 | 最大 JSON 请求字节数 |
| `MAX_SQL_LENGTH` | 100000 | 每条语句的最大字符数 |
| `MAX_PARAMS` | 100 | 每条语句的最大参数数 |
| `MAX_BATCH_STATEMENTS` | 50 | 每个 batch 的最大语句数 |

SQL 策略要求每个请求项恰好包含一条语句，并拒绝 `ATTACH` 和 `PRAGMA writable_schema`。扫描会跳过字符串和注释中的内容。

日志包含请求 ID、模式、延迟、结果元数据和 SHA-256 SQL 哈希；不会记录完整 SQL 文本或 Bearer token。
