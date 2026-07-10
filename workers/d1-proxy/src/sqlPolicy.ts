import { ProxyError } from "./types";

interface Token {
  value: string;
  quoted: boolean;
}

type ScannerState = "normal" | "single" | "double" | "backtick" | "bracket" | "line-comment" | "block-comment";

export function enforceSQLPolicy(sql: string, maxLength: number, requestId: string): void {
  if (sql.trim().length === 0) {
    throw new ProxyError(400, "invalid_sql", "sql must not be empty", requestId);
  }
  if (sql.length > maxLength) {
    throw new ProxyError(413, "sql_too_large", `sql exceeds ${maxLength} characters`, requestId);
  }

  const inspection = inspectSQL(sql, requestId);
  if (inspection.statementCount !== 1) {
    throw new ProxyError(400, "multiple_statements", "exactly one SQL statement is required", requestId);
  }

  const first = inspection.tokens[0];
  if (first !== undefined && !first.quoted && first.value === "attach") {
    throw new ProxyError(403, "forbidden_sql", "ATTACH is not allowed", requestId);
  }
  if (
    first !== undefined &&
    !first.quoted &&
    first.value === "pragma" &&
    inspection.tokens.slice(1).some((token) => token.value === "writable_schema")
  ) {
    throw new ProxyError(403, "forbidden_sql", "PRAGMA writable_schema is not allowed", requestId);
  }
}

function inspectSQL(sql: string, requestId: string): { statementCount: number; tokens: Token[] } {
  let state: ScannerState = "normal";
  let token = "";
  let quotedToken = "";
  let hasStatementContent = false;
  let statementCount = 0;
  const tokens: Token[] = [];

  const flushToken = (): void => {
    if (token.length > 0) {
      tokens.push({ value: token.toLowerCase(), quoted: false });
      token = "";
    }
  };
  const flushQuotedToken = (): void => {
    if (/^[A-Za-z_][A-Za-z0-9_]*$/.test(quotedToken)) {
      tokens.push({ value: quotedToken.toLowerCase(), quoted: true });
    }
    quotedToken = "";
  };

  for (let i = 0; i < sql.length; i += 1) {
    const char = sql[i] ?? "";
    const next = sql[i + 1] ?? "";

    if (state === "line-comment") {
      if (char === "\n" || char === "\r") {
        state = "normal";
      }
      continue;
    }
    if (state === "block-comment") {
      if (char === "*" && next === "/") {
        state = "normal";
        i += 1;
      }
      continue;
    }
    if (state === "single") {
      if (char === "'" && next === "'") {
        quotedToken += "'";
        i += 1;
      } else if (char === "'") {
        flushQuotedToken();
        state = "normal";
      } else {
        quotedToken += char;
      }
      continue;
    }
    if (state === "double" || state === "backtick") {
      const delimiter = state === "double" ? '"' : "`";
      if (char === delimiter && next === delimiter) {
        quotedToken += delimiter;
        i += 1;
      } else if (char === delimiter) {
        flushQuotedToken();
        state = "normal";
      } else {
        quotedToken += char;
      }
      continue;
    }
    if (state === "bracket") {
      if (char === "]" && next === "]") {
        quotedToken += "]";
        i += 1;
      } else if (char === "]") {
        flushQuotedToken();
        state = "normal";
      } else {
        quotedToken += char;
      }
      continue;
    }

    if (char === "-" && next === "-") {
      flushToken();
      state = "line-comment";
      i += 1;
      continue;
    }
    if (char === "/" && next === "*") {
      flushToken();
      state = "block-comment";
      i += 1;
      continue;
    }
    if (char === "'") {
      flushToken();
      hasStatementContent = true;
      quotedToken = "";
      state = "single";
      continue;
    }
    if (char === '"' || char === "`" || char === "[") {
      flushToken();
      hasStatementContent = true;
      quotedToken = "";
      state = char === '"' ? "double" : char === "`" ? "backtick" : "bracket";
      continue;
    }
    if (char === ";") {
      flushToken();
      if (hasStatementContent) {
        statementCount += 1;
        hasStatementContent = false;
      }
      continue;
    }
    if (/[A-Za-z0-9_]/.test(char)) {
      token += char;
      hasStatementContent = true;
      continue;
    }
    flushToken();
    if (!/\s/.test(char)) {
      hasStatementContent = true;
    }
  }

  if (state !== "normal" && state !== "line-comment") {
    throw new ProxyError(400, "invalid_sql", "sql contains an unterminated quote or comment", requestId);
  }
  flushToken();
  if (hasStatementContent) {
    statementCount += 1;
  }
  return { statementCount, tokens };
}
