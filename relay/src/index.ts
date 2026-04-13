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

    // Gate pairing endpoints with a relay-level secret. If RELAY_SECRET
    // is set, POST and GET /v1/pair require a matching X-Relay-Secret
    // header. This prevents strangers from creating new pairs on your
    // relay — once paired, the per-pair Bearer token handles auth.
    if (url.pathname === "/v1/pair") {
      const expected = env.RELAY_SECRET;
      if (expected) {
        const got = request.headers.get("x-relay-secret");
        if (!got) {
          return jsonResponse(401, { code: "missing_relay_secret", message: "X-Relay-Secret header required" });
        }
        if (!constantTimeEquals(got, expected)) {
          return jsonResponse(403, { code: "bad_relay_secret", message: "X-Relay-Secret does not match" });
        }
      }
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
    const response = await stub.fetch(request);
    console.log(`${request.method} ${url.pathname} pair=${pair} → ${response.status}`);
    return response;
  },
};

/** Constant-time string comparison to prevent timing attacks. */
function constantTimeEquals(a: string, b: string): boolean {
  if (a.length !== b.length) return false;
  const encoder = new TextEncoder();
  const ab = encoder.encode(a);
  const bb = encoder.encode(b);
  let diff = 0;
  for (let i = 0; i < ab.length; i++) {
    diff |= ab[i] ^ bb[i];
  }
  return diff === 0;
}

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
