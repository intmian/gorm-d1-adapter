import { ProxyError } from "./types";

const encoder = new TextEncoder();

export function authenticate(request: Request, expectedToken: string): void {
  if (expectedToken.length === 0) {
    throw new ProxyError(500, "misconfigured", "AUTH_TOKEN is not configured");
  }

  const authorization = request.headers.get("Authorization") ?? "";
  if (!authorization.startsWith("Bearer ")) {
    throw new ProxyError(401, "unauthorized", "valid bearer token required");
  }
  const suppliedToken = authorization.slice("Bearer ".length);
  if (!constantTimeEqual(suppliedToken, expectedToken)) {
    throw new ProxyError(401, "unauthorized", "valid bearer token required");
  }
}

function constantTimeEqual(left: string, right: string): boolean {
  const leftBytes = encoder.encode(left);
  const rightBytes = encoder.encode(right);
  const length = Math.max(leftBytes.length, rightBytes.length);
  let difference = leftBytes.length ^ rightBytes.length;

  for (let i = 0; i < length; i += 1) {
    difference |= (leftBytes[i] ?? 0) ^ (rightBytes[i] ?? 0);
  }
  return difference === 0;
}
