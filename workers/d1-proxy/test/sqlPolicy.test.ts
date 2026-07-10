import { describe, expect, it } from "vitest";

import { enforceSQLPolicy } from "../src/sqlPolicy";
import { ProxyError } from "../src/types";

describe("enforceSQLPolicy", () => {
  it("allows one statement and ignores semicolons in strings and comments", () => {
    expect(() => enforceSQLPolicy("SELECT ';' AS value /* ; */; -- trailing ;", 1_000, "req")).not.toThrow();
    expect(() => enforceSQLPolicy("SELECT attach FROM records", 1_000, "req")).not.toThrow();
  });

  it("rejects multiple statements", () => {
    expectPolicyError("SELECT 1; DELETE FROM records", "multiple_statements");
  });

  it("rejects ATTACH and writable_schema pragma", () => {
    expectPolicyError("/* safe */ ATTACH DATABASE 'other' AS other", "forbidden_sql");
    expectPolicyError('PRAGMA main."writable_schema" = ON', "forbidden_sql");
    expectPolicyError("PRAGMA 'writable_schema' = ON", "forbidden_sql");
  });

  it("rejects malformed and oversized SQL", () => {
    expectPolicyError("SELECT 'unterminated", "invalid_sql");
    expectPolicyError("SELECT 123", "sql_too_large", 5);
  });
});

function expectPolicyError(sql: string, code: string, maxLength = 1_000): void {
  try {
    enforceSQLPolicy(sql, maxLength, "req");
    throw new Error("expected policy error");
  } catch (error) {
    expect(error).toBeInstanceOf(ProxyError);
    expect((error as ProxyError).code).toBe(code);
  }
}
