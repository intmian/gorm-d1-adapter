export type ExecuteMode = "query" | "exec";
export type D1BindValue = null | string | number | ArrayBuffer;

export interface StatementRequest {
  mode: ExecuteMode;
  sql: string;
  params?: unknown[];
}

export interface ExecuteRequest extends StatementRequest {
  requestId?: string;
}

export interface BatchRequest {
  statements: StatementRequest[];
  requestId?: string;
}

export interface ResultMeta {
  changed_db: boolean;
  changes: number;
  duration: number;
  last_row_id: number;
  rows_read: number;
  rows_written: number;
  served_by: string;
  size_after: number;
}

export interface ResultSet {
  meta: ResultMeta;
  columns: string[];
  rows: unknown[][];
}

export interface D1ResultLike {
  success?: boolean;
  error?: string;
  meta?: Partial<ResultMeta>;
  results?: Array<Record<string, unknown>>;
}

export interface D1PreparedStatementLike {
  bind(...values: D1BindValue[]): D1PreparedStatementLike;
  run(): Promise<D1ResultLike>;
  raw(options: { columnNames: true }): Promise<unknown[][]>;
}

export interface D1DatabaseLike {
  prepare(sql: string): D1PreparedStatementLike;
  batch(statements: D1PreparedStatementLike[]): Promise<D1ResultLike[]>;
}

export interface Env {
  DB: D1DatabaseLike;
  AUTH_TOKEN: string;
  MAX_BODY_BYTES?: string;
  MAX_SQL_LENGTH?: string;
  MAX_PARAMS?: string;
  MAX_BATCH_STATEMENTS?: string;
}

export interface Limits {
  maxBodyBytes: number;
  maxSQLLength: number;
  maxParams: number;
  maxBatchStatements: number;
}

export class ProxyError extends Error {
  readonly status: number;
  readonly code: string;
  readonly requestId?: string;

  constructor(status: number, code: string, message: string, requestId?: string) {
    super(message);
    this.name = "ProxyError";
    this.status = status;
    this.code = code;
    if (requestId !== undefined) {
      this.requestId = requestId;
    }
  }
}
