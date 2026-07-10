import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { handleRequest } from "../src/handlers";
import type {
  D1BindValue,
  D1DatabaseLike,
  D1PreparedStatementLike,
  D1ResultLike,
  Env,
} from "../src/types";

class FakeStatement implements D1PreparedStatementLike {
  readonly sql: string;
  params: D1BindValue[] = [];

  constructor(sql: string) {
    this.sql = sql;
  }

  bind(...values: D1BindValue[]): D1PreparedStatementLike {
    this.params = values;
    return this;
  }

  async run(): Promise<D1ResultLike> {
    return {
      success: true,
      meta: { changes: 1, last_row_id: 7, rows_written: 1 },
      results: [],
    };
  }

	async raw(): Promise<unknown[][]> {
		return [["value", "payload"], [1, [1, 2, 3]]];
	}
}

class FakeDatabase implements D1DatabaseLike {
  readonly prepared: FakeStatement[] = [];

  prepare(sql: string): D1PreparedStatementLike {
    const statement = new FakeStatement(sql);
    this.prepared.push(statement);
    return statement;
  }

  async batch(statements: D1PreparedStatementLike[]): Promise<D1ResultLike[]> {
    return statements.map((_, index) =>
      index === 0
        ? { success: true, meta: { changes: 1, rows_written: 1 }, results: [] }
        : { success: true, meta: { rows_read: 1 }, results: [{ value: 42 }] },
    );
  }
}

describe("handleRequest", () => {
  let database: FakeDatabase;
  let env: Env;

  beforeEach(() => {
    database = new FakeDatabase();
    env = { DB: database, AUTH_TOKEN: "secret" };
    vi.spyOn(console, "info").mockImplementation(() => undefined);
    vi.spyOn(console, "error").mockImplementation(() => undefined);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("authenticates health checks", async () => {
    const unauthorized = await handleRequest(new Request("https://worker.example/health"), env);
    expect(unauthorized.status).toBe(401);

    const response = await handleRequest(authenticatedRequest("/health", { method: "GET" }), env);
    expect(response.status).toBe(200);
    expect(await response.json()).toMatchObject({ ok: true });
  });

  it("executes a query and translates blobs in both directions", async () => {
    const response = await handleRequest(
      authenticatedJSONRequest("/v1/query", {
        mode: "query",
        sql: "SELECT ?, ?",
        params: ["d1b64:AQID", true],
        requestId: "query-1",
      }),
      env,
    );

    expect(response.status).toBe(200);
    expect(response.headers.get("X-Request-ID")).toBe("query-1");
    expect(await response.json()).toEqual({
      success: true,
      requestId: "query-1",
      result: {
        meta: {
          changed_db: false,
          changes: 0,
          duration: 0,
          last_row_id: 0,
          rows_read: 0,
          rows_written: 0,
          served_by: "",
          size_after: 0,
        },
        columns: ["value", "payload"],
        rows: [[1, "d1b64:AQID"]],
      },
    });
    expect(database.prepared[0]?.params[0]).toBeInstanceOf(ArrayBuffer);
    expect(database.prepared[0]?.params[1]).toBe(1);
    const log = vi.mocked(console.info).mock.calls.map((call) => String(call[0])).join("\n");
    expect(log).toContain("sqlHash");
    expect(log).not.toContain("SELECT ?, ?");
    expect(log).not.toContain("d1b64:AQID");
  });

  it("runs a mixed batch in request order", async () => {
    const response = await handleRequest(
      authenticatedJSONRequest("/v1/batch", {
        requestId: "batch-1",
        statements: [
          { mode: "exec", sql: "INSERT INTO t(value) VALUES (?)", params: [1] },
          { mode: "query", sql: "SELECT value FROM t" },
        ],
      }),
      env,
    );
    const body = (await response.json()) as { results: Array<{ columns: string[]; rows: unknown[][] }> };

    expect(response.status).toBe(200);
    expect(body.results).toHaveLength(2);
    expect(body.results[0]).toMatchObject({ columns: [], rows: [] });
    expect(body.results[1]).toMatchObject({ columns: ["value"], rows: [[42]] });
    expect(database.prepared.map((statement) => statement.sql)).toEqual([
      "INSERT INTO t(value) VALUES (?)",
      "SELECT value FROM t",
    ]);
  });

  it("enforces auth, content type, body and SQL limits", async () => {
    const noJSON = await handleRequest(authenticatedRequest("/v1/query", { method: "POST", body: "{}" }), env);
    expect(noJSON.status).toBe(415);

    env.MAX_BODY_BYTES = "10";
    const tooLarge = await handleRequest(
      authenticatedJSONRequest("/v1/query", { mode: "query", sql: "SELECT 1" }),
      env,
    );
    expect(tooLarge.status).toBe(413);

    env.MAX_BODY_BYTES = "1000";
    const forbidden = await handleRequest(
      authenticatedJSONRequest("/v1/query", { mode: "exec", sql: "ATTACH DATABASE 'x' AS x" }),
      env,
    );
    expect(forbidden.status).toBe(403);
    expect(await forbidden.json()).toMatchObject({ error: { code: "forbidden_sql" } });
  });
});

function authenticatedRequest(path: string, init: RequestInit = {}): Request {
  const headers = new Headers(init.headers);
  headers.set("Authorization", "Bearer secret");
  return new Request(`https://worker.example${path}`, { ...init, headers });
}

function authenticatedJSONRequest(path: string, body: unknown): Request {
  return authenticatedRequest(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}
