import { handleRequest } from "./handlers";
import type { Env } from "./types";

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    return handleRequest(request, env);
  },
};
