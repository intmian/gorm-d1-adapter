# D1 Worker Proxy

[中文文档](README.zh-CN.md) · [Main README](../../README.md)

This is the project's recommended primary production deployment mode: the Worker exposes a narrow server-to-server API over a single D1 binding for Go services running outside Cloudflare.

Keep the Account API Token in the Cloudflare control plane rather than configuring it in application services; those services only need this Worker's access token. REST mode remains available for compatibility, migration, and operational fallback.

```text
GET  /health
POST /v1/query
POST /v1/batch
```

Every route requires `Authorization: Bearer <AUTH_TOKEN>`. No CORS headers are emitted because browser clients are not supported.

## Configure and Deploy

1. Copy `wrangler.example.toml` to the ignored local file `wrangler.toml`, then replace the D1 database name and IDs:

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

4. Deploy the Worker:

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

`query` uses `stmt.raw({ columnNames: true })`; `exec` uses `stmt.run()`; batch uses one `env.DB.batch(...)` call and preserves statement order.

`d1b64:` is reserved by the proxy wire protocol for BLOB values. A text value beginning with this prefix is interpreted as a Base64-encoded BLOB; callers that need to preserve such text verbatim must encode or escape it at the application layer.

## Safety Limits

Defaults are provided in `wrangler.example.toml` and copied into the local `wrangler.toml`.

| Variable | Default | Meaning |
|---|---:|---|
| `MAX_BODY_BYTES` | 1048576 | Maximum JSON request bytes |
| `MAX_SQL_LENGTH` | 100000 | Maximum characters per statement |
| `MAX_PARAMS` | 100 | Maximum parameters per statement |
| `MAX_BATCH_STATEMENTS` | 50 | Maximum statements per batch |

The SQL policy requires exactly one statement per request item and rejects `ATTACH` and `PRAGMA writable_schema`. It scans outside strings and comments before applying these rules.

Logs contain the request ID, mode, latency, result metadata, and SHA-256 SQL hash. Full SQL text and Bearer tokens are not logged.
