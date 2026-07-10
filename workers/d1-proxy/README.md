# D1 Worker Proxy

[中文文档](README.zh-CN.md) · [Main README](../../README.md)

This Worker is the recommended way to put D1 in production with `gorm-d1-adapter`. It exposes a narrow server-to-server API over a single D1 binding, so Go services outside Cloudflare can access D1 without ever touching an Account API Token.

The pattern is simple: the Account API Token stays inside Cloudflare. Application services only need this Worker's access token. REST mode remains available for compatibility, migration, and operational fallback.

```text
GET  /health
POST /v1/query
POST /v1/batch
```

Every route requires `Authorization: Bearer <AUTH_TOKEN>`. No CORS headers are set — browser clients are not supported.

## Configure and Deploy

1. Copy `wrangler.example.toml` to `wrangler.toml` (git-ignored), then replace the D1 database name and IDs:

```powershell
Copy-Item wrangler.example.toml wrangler.toml
```

2. Install dependencies and verify the project:

```powershell
npm ci
npm run check
npm test
npx wrangler deploy --dry-run --outdir dist
```

3. Store the authentication token as a Worker secret:

```powershell
npx wrangler secret put AUTH_TOKEN
```

4. Deploy:

```powershell
npm run deploy
```

Do not add `AUTH_TOKEN` to `wrangler.toml`. For local development, copy `.dev.vars.example` to `.dev.vars` and set a non-production token.

## Request Contract

Query or execute:

```json
{
  "mode": "query",
  "sql": "SELECT * FROM records WHERE id = ?",
  "params": [1],
  "requestId": "optional-correlation-id"
}
```

Batch:

```json
{
  "statements": [
    {"mode": "exec", "sql": "INSERT INTO records(id) VALUES(?)", "params": [1]},
    {"mode": "query", "sql": "SELECT id FROM records WHERE id = ?", "params": [1]}
  ],
  "requestId": "optional-correlation-id"
}
```

`query` uses `stmt.raw({ columnNames: true })`; `exec` uses `stmt.run()`; batch uses a single `env.DB.batch(...)` call and preserves statement order.

`d1b64:` is reserved by the proxy wire protocol for BLOB values. A text value starting with this prefix is interpreted as Base64-encoded binary. Callers that need to preserve such text verbatim must encode or escape it at the application layer.

## Safety Limits

Defaults are provided in `wrangler.example.toml` and copied into your local `wrangler.toml`.

| Variable | Default | Meaning |
|---|---:|---|
| `MAX_BODY_BYTES` | 1048576 | Maximum JSON request bytes |
| `MAX_SQL_LENGTH` | 100000 | Maximum characters per statement |
| `MAX_PARAMS` | 100 | Maximum parameters per statement |
| `MAX_BATCH_STATEMENTS` | 50 | Maximum statements per batch |

The SQL policy requires exactly one statement per request item and rejects `ATTACH` and `PRAGMA writable_schema`. The scanner skips string literals and comments before applying these rules.

Logs include the request ID, mode, latency, result metadata, and an SHA-256 SQL hash. Full SQL text and Bearer tokens are not logged.
