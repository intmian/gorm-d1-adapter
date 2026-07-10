import type { D1BindValue, D1ResultLike, ExecuteMode, ResultMeta, ResultSet } from "./types";
import { ProxyError } from "./types";

export const BLOB_PREFIX = "d1b64:";

export function decodeParams(params: unknown[], requestId: string): D1BindValue[] {
  return params.map((value) => decodeParam(value, requestId));
}

export function resultSetFromRaw(raw: unknown[][]): ResultSet {
  if (raw.length === 0) {
    return { meta: emptyMeta(), columns: [], rows: [] };
  }
  const header = raw[0];
  if (!Array.isArray(header) || !header.every((column) => typeof column === "string")) {
    throw new Error("D1 raw result did not include a valid column header");
  }
  return {
    meta: emptyMeta(),
    columns: [...header],
    rows: raw.slice(1).map((row) => row.map(encodeResultValue)),
  };
}

export function resultSetFromD1Result(result: D1ResultLike, mode: ExecuteMode): ResultSet {
  assertD1Success(result);
  const meta = normalizeMeta(result.meta);
  if (mode === "exec") {
    return { meta, columns: [], rows: [] };
  }

  const records = result.results ?? [];
  if (records.length === 0) {
    return { meta, columns: [], rows: [] };
  }
  const first = records[0];
  if (first === undefined || first === null || Array.isArray(first) || typeof first !== "object") {
    throw new Error("D1 batch query returned malformed rows");
  }
  const columns = Object.keys(first);
  const rows = records.map((record) => columns.map((column) => encodeResultValue(record[column])));
  return { meta, columns, rows };
}

export function assertD1Success(result: D1ResultLike): void {
  if (result.success === false) {
    throw new Error(result.error ?? "D1 statement failed");
  }
}

function decodeParam(value: unknown, requestId: string): D1BindValue {
  if (value === null || typeof value === "number") {
    if (typeof value === "number" && !Number.isFinite(value)) {
      throw new ProxyError(400, "invalid_params", "numeric params must be finite", requestId);
    }
    return value;
  }
  if (typeof value === "boolean") {
    return value ? 1 : 0;
  }
  if (typeof value !== "string") {
    throw new ProxyError(400, "invalid_params", "params must contain only scalar D1 values", requestId);
  }
  if (!value.startsWith(BLOB_PREFIX)) {
    return value;
  }

  const encoded = value.slice(BLOB_PREFIX.length);
  if (encoded.length % 4 !== 0 || !/^[A-Za-z0-9+/]*={0,2}$/.test(encoded)) {
    throw new ProxyError(400, "invalid_params", "blob param is not valid base64", requestId);
  }
  try {
    const binary = atob(encoded);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i += 1) {
      bytes[i] = binary.charCodeAt(i);
    }
    return bytes.buffer;
  } catch {
    throw new ProxyError(400, "invalid_params", "blob param is not valid base64", requestId);
  }
}

function encodeResultValue(value: unknown): unknown {
	if (value instanceof ArrayBuffer) {
		return BLOB_PREFIX + bytesToBase64(new Uint8Array(value));
	}
	if (ArrayBuffer.isView(value)) {
		return BLOB_PREFIX + bytesToBase64(new Uint8Array(value.buffer, value.byteOffset, value.byteLength));
	}
	if (Array.isArray(value) && value.every((byte) => Number.isInteger(byte) && byte >= 0 && byte <= 255)) {
		return BLOB_PREFIX + bytesToBase64(Uint8Array.from(value as number[]));
	}
	if (typeof value === "bigint") {
    return value.toString();
  }
  return value ?? null;
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = "";
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary);
}

function normalizeMeta(meta: Partial<ResultMeta> | undefined): ResultMeta {
  return {
    changed_db: meta?.changed_db ?? false,
    changes: finiteNumber(meta?.changes),
    duration: finiteNumber(meta?.duration),
    last_row_id: finiteNumber(meta?.last_row_id),
    rows_read: finiteNumber(meta?.rows_read),
    rows_written: finiteNumber(meta?.rows_written),
    served_by: typeof meta?.served_by === "string" ? meta.served_by : "",
    size_after: finiteNumber(meta?.size_after),
  };
}

function finiteNumber(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function emptyMeta(): ResultMeta {
  return normalizeMeta(undefined);
}
