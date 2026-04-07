// Cloudflare Worker entry point.
//
// Routes inbound requests to the per-pair Durable Object identified by the
// `pair` query parameter. Every endpoint requires a `pair` parameter; if it
// is missing the request is rejected here so the DO never has to handle
// "no pair" cases.
//
// Auth: M1 has no auth at all (the relay is dev-only). M2 introduces
// per-pair HMAC tokens that this layer validates before forwarding.

/* eslint-disable @typescript-eslint/ban-ts-comment */
// @ts-nocheck — Workers globals provided by runtime.

export { PairMailboxDO } from "./durable_object.js";

export default {
  async fetch(request: Request, env: any): Promise<Response> {
    const url = new URL(request.url);

    if (url.pathname === "/v1/health") {
      return jsonResponse(200, { ok: true, version: "m1" });
    }

    const pair = url.searchParams.get("pair");
    if (!pair) {
      return jsonResponse(400, { code: "missing_pair", message: "pair query parameter required" });
    }
    if (!isValidPairId(pair)) {
      return jsonResponse(400, { code: "invalid_pair", message: "pair must be a 26-char ULID" });
    }

    const id = env.PAIR_DO.idFromName(pair);
    const stub = env.PAIR_DO.get(id);
    return stub.fetch(request);
  },
};

const ULID_REGEX = /^[0-7][0-9A-HJKMNP-TV-Z]{25}$/;

export function isValidPairId(s: string): boolean {
  return ULID_REGEX.test(s);
}

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}
