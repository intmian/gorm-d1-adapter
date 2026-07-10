import { authenticate } from "./auth";
import { assertD1Success, decodeParams, resultSetFromD1Result, resultSetFromRaw } from "./compat";
import { enforceSQLPolicy } from "./sqlPolicy";
import type {
  BatchRequest,
  D1PreparedStatementLike,
  Env,
  ExecuteMode,
  ExecuteRequest,
  Limits,
  ResultSet,
  StatementRequest,
} from "./types";
import { ProxyError } from "./types";

const DEFAULT_LIMITS: Limits = {
  maxBodyBytes: 1_048_576,
  maxSQLLength: 100_000,
  maxParams: 100,
  maxBatchStatements: 50,
};
const REQUEST_ID_PATTERN = /^[A-Za-z0-9._:-]{1,128}$/;

export async function handleRequest(request: Request, env: Env): Promise<Response> {
  let requestId = requestIdFromHeader(request) ?? crypto.randomUUID();
  try {
    authenticate(request, env.AUTH_TOKEN ?? "");
    if (env.DB === undefined || env.DB === null) {
      throw new ProxyError(503, "misconfigured", "DB binding is not configured", requestId);
    }

    const url = new URL(request.url);
    if (url.pathname === "/health") {
      ensureMethod(request, "GET", requestId);
      return jsonResponse(200, { ok: true, requestId }, requestId);
    }
    if (url.pathname === "/v1/query") {
      ensureMethod(request, "POST", requestId);
      const limits = limitsFromEnv(env);
      const payload = await readJSON<ExecuteRequest>(request, limits.maxBodyBytes, requestId);
      requestId = resolveRequestId(payload.requestId, requestId);
      const statement = validateStatement(payload, limits, requestId);
      const result = await executeOne(env, statement, requestId);
      return jsonResponse(200, { success: true, requestId, result }, requestId);
    }
    if (url.pathname === "/v1/batch") {
      ensureMethod(request, "POST", requestId);
      const limits = limitsFromEnv(env);
      const payload = await readJSON<BatchRequest>(request, limits.maxBodyBytes, requestId);
      requestId = resolveRequestId(payload.requestId, requestId);
      const statements = validateBatch(payload, limits, requestId);
      const results = await executeBatch(env, statements, requestId);
      return jsonResponse(200, { success: true, requestId, results }, requestId);
    }
    throw new ProxyError(404, "not_found", "route not found", requestId);
  } catch (error) {
    const proxyError = normalizeError(error, requestId);
    requestId = proxyError.requestId ?? requestId;
    console.error(JSON.stringify({
      event: "d1_proxy_error",
      requestId,
      code: proxyError.code,
      status: proxyError.status,
    }));
    const headers = proxyError.status === 401 ? { "WWW-Authenticate": "Bearer" } : undefined;
    return jsonResponse(
      proxyError.status,
      {
        success: false,
        requestId,
        error: { code: proxyError.code, message: proxyError.message },
      },
      requestId,
      headers,
    );
  }
}

async function executeOne(env: Env, statement: StatementRequest, requestId: string): Promise<ResultSet> {
  const sqlHash = await hashSQL(statement.sql);
  const started = performance.now();
  try {
    const prepared = env.DB.prepare(statement.sql).bind(...decodeParams(statement.params ?? [], requestId));
    let result: ResultSet;
    if (statement.mode === "query") {
      result = resultSetFromRaw(await prepared.raw({ columnNames: true }));
    } else {
      const executed = await prepared.run();
      assertD1Success(executed);
      result = resultSetFromD1Result(executed, "exec");
    }
    logStatement(requestId, statement.mode, sqlHash, performance.now() - started, true, result);
    return result;
  } catch (error) {
    logStatement(requestId, statement.mode, sqlHash, performance.now() - started, false);
    if (error instanceof ProxyError) {
      throw error;
    }
    throw new ProxyError(502, "d1_error", errorMessage(error), requestId);
  }
}

async function executeBatch(env: Env, statements: StatementRequest[], requestId: string): Promise<ResultSet[]> {
  const started = performance.now();
  const hashes = await Promise.all(statements.map((statement) => hashSQL(statement.sql)));

  try {
    const prepared: D1PreparedStatementLike[] = statements.map((statement) =>
      env.DB.prepare(statement.sql).bind(...decodeParams(statement.params ?? [], requestId)),
    );
    const rawResults = await env.DB.batch(prepared);
    if (rawResults.length !== statements.length) {
      throw new Error("D1 batch result count does not match request");
    }
    const results = rawResults.map((result, index) => {
      const statement = statements[index];
      if (statement === undefined) {
        throw new Error("D1 batch result has no matching statement");
      }
      return resultSetFromD1Result(result, statement.mode);
    });
    console.info(JSON.stringify({
      event: "d1_batch",
      requestId,
      sqlHashes: hashes,
      statementCount: statements.length,
      durationMs: performance.now() - started,
      success: true,
    }));
    return results;
  } catch (error) {
    console.info(JSON.stringify({
      event: "d1_batch",
      requestId,
      sqlHashes: hashes,
      statementCount: statements.length,
      durationMs: performance.now() - started,
      success: false,
    }));
    if (error instanceof ProxyError) {
      throw error;
    }
    throw new ProxyError(502, "d1_error", errorMessage(error), requestId);
  }
}

function validateStatement(value: unknown, limits: Limits, requestId: string): StatementRequest {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    throw new ProxyError(400, "invalid_request", "request body must be an object", requestId);
  }
  const record = value as Partial<StatementRequest>;
  if (record.mode !== "query" && record.mode !== "exec") {
    throw new ProxyError(400, "invalid_mode", "mode must be query or exec", requestId);
  }
  if (typeof record.sql !== "string") {
    throw new ProxyError(400, "invalid_sql", "sql must be a string", requestId);
  }
  const params = record.params ?? [];
  if (!Array.isArray(params)) {
    throw new ProxyError(400, "invalid_params", "params must be an array", requestId);
  }
  if (params.length > limits.maxParams) {
    throw new ProxyError(413, "too_many_params", `params exceeds ${limits.maxParams} values`, requestId);
  }
  enforceSQLPolicy(record.sql, limits.maxSQLLength, requestId);
  return { mode: record.mode, sql: record.sql, params };
}

function validateBatch(value: unknown, limits: Limits, requestId: string): StatementRequest[] {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    throw new ProxyError(400, "invalid_request", "request body must be an object", requestId);
  }
  const statements = (value as Partial<BatchRequest>).statements;
  if (!Array.isArray(statements) || statements.length === 0) {
    throw new ProxyError(400, "invalid_batch", "statements must be a non-empty array", requestId);
  }
  if (statements.length > limits.maxBatchStatements) {
    throw new ProxyError(
      413,
      "batch_too_large",
      `batch exceeds ${limits.maxBatchStatements} statements`,
      requestId,
    );
  }
  return statements.map((statement) => validateStatement(statement, limits, requestId));
}

async function readJSON<T>(request: Request, maxBodyBytes: number, requestId: string): Promise<T> {
  const contentType = request.headers.get("Content-Type") ?? "";
  if (!contentType.toLowerCase().startsWith("application/json")) {
    throw new ProxyError(415, "unsupported_media_type", "Content-Type must be application/json", requestId);
  }
  const contentLength = Number(request.headers.get("Content-Length"));
  if (Number.isFinite(contentLength) && contentLength > maxBodyBytes) {
    throw new ProxyError(413, "body_too_large", `request body exceeds ${maxBodyBytes} bytes`, requestId);
  }
  const body = await request.arrayBuffer();
  if (body.byteLength > maxBodyBytes) {
    throw new ProxyError(413, "body_too_large", `request body exceeds ${maxBodyBytes} bytes`, requestId);
  }
  try {
    return JSON.parse(new TextDecoder().decode(body)) as T;
  } catch {
    throw new ProxyError(400, "invalid_json", "request body must contain valid JSON", requestId);
  }
}

function ensureMethod(request: Request, expected: string, requestId: string): void {
  if (request.method !== expected) {
    throw new ProxyError(405, "method_not_allowed", `${request.method} is not allowed for this route`, requestId);
  }
}

function resolveRequestId(value: unknown, fallback: string): string {
  if (value === undefined || value === null || value === "") {
    return fallback;
  }
  if (typeof value !== "string" || !REQUEST_ID_PATTERN.test(value)) {
    throw new ProxyError(400, "invalid_request_id", "requestId has an invalid format", fallback);
  }
  return value;
}

function requestIdFromHeader(request: Request): string | undefined {
  const value = request.headers.get("X-Request-ID");
  return value !== null && REQUEST_ID_PATTERN.test(value) ? value : undefined;
}

function limitsFromEnv(env: Env): Limits {
  return {
    maxBodyBytes: positiveInteger(env.MAX_BODY_BYTES, DEFAULT_LIMITS.maxBodyBytes),
    maxSQLLength: positiveInteger(env.MAX_SQL_LENGTH, DEFAULT_LIMITS.maxSQLLength),
    maxParams: positiveInteger(env.MAX_PARAMS, DEFAULT_LIMITS.maxParams),
    maxBatchStatements: positiveInteger(env.MAX_BATCH_STATEMENTS, DEFAULT_LIMITS.maxBatchStatements),
  };
}

function positiveInteger(value: string | undefined, fallback: number): number {
  if (value === undefined || !/^\d+$/.test(value)) {
    return fallback;
  }
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : fallback;
}

async function hashSQL(sql: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(sql));
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

function logStatement(
  requestId: string,
  mode: ExecuteMode,
  sqlHash: string,
  durationMs: number,
  success: boolean,
  result?: ResultSet,
): void {
  console.info(JSON.stringify({
    event: "d1_statement",
    requestId,
    mode,
    sqlHash,
    durationMs,
    success,
    meta: result?.meta,
  }));
}

function normalizeError(error: unknown, requestId: string): ProxyError {
  if (error instanceof ProxyError) {
    return error;
  }
  return new ProxyError(500, "internal_error", "internal worker error", requestId);
}

function errorMessage(error: unknown): string {
  return error instanceof Error && error.message.length > 0 ? error.message : "D1 request failed";
}

function jsonResponse(
  status: number,
  body: unknown,
  requestId: string,
  extraHeaders?: Record<string, string>,
): Response {
  const headers = new Headers(extraHeaders);
  headers.set("Cache-Control", "no-store");
  headers.set("Content-Type", "application/json; charset=utf-8");
  headers.set("X-Request-ID", requestId);
  return new Response(JSON.stringify(body), { status, headers });
}
