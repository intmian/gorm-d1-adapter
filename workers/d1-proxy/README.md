# D1 Worker Proxy

This Worker exposes a narrow server-to-server API over a single D1 binding:

```text
GET  /health
POST /v1/query
POST /v1/batch
```

Every route requires `Authorization: Bearer <AUTH_TOKEN>`. No CORS headers are emitted because browser clients are not supported.

## Configure

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

4. Deploy:

```powershell
npm run deploy
```

Do not add `AUTH_TOKEN` to `wrangler.toml`. For local development, copy `.dev.vars.example` to `.dev.vars` and set a non-production token.

## Request Contract

Query or exec:

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

`query` uses `stmt.raw({ columnNames: true })`. `exec` uses `stmt.run()`. Batch uses one `env.DB.batch(...)` call and preserves statement order.

## Safety Limits

Defaults are provided in `wrangler.example.toml` and copied into the local `wrangler.toml`:

| Variable | Default | Meaning |
|---|---:|---|
| `MAX_BODY_BYTES` | 1048576 | Maximum JSON request bytes |
| `MAX_SQL_LENGTH` | 100000 | Maximum characters per statement |
| `MAX_PARAMS` | 100 | Maximum params per statement |
| `MAX_BATCH_STATEMENTS` | 50 | Maximum statements per batch |

The SQL policy requires exactly one statement per request item and rejects `ATTACH` and `PRAGMA writable_schema`. It scans outside strings and comments before applying these rules.

Logs contain the request ID, mode, latency, result metadata, and SHA-256 SQL hash. Full SQL text and Bearer tokens are not logged.
