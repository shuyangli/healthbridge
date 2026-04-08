// HTTP routing layer that turns fetch requests into Mailbox calls.
//
// This is split from durable_object.ts so that the routing logic can be
// unit-tested with a plain Mailbox instance and a synthetic Request, without
// pulling in any Workers/Cloudflare types.
//
// The contract:
//
//   POST   /v1/jobs?pair=<id>           body: { job_id, blob }                       enqueue a job
//   GET    /v1/jobs?pair=<id>&since=N   long-poll for jobs whose seq > N
//   POST   /v1/results?pair=<id>        body: { job_id, page_index, blob }           post a result page
//   GET    /v1/results?pair=<id>&job_id=<j>   long-poll for any result pages for job
//   DELETE /v1/pair?pair=<id>           wipe a pair
//   GET    /v1/health                   simple liveness ping
//
// All bodies are JSON. All errors are JSON: { code, message }.
//
// `pair` is the random ULID established at pairing. The Worker entry point
// (index.ts) is responsible for routing each pair to its own Durable Object;
// this handler operates on a single Mailbox.

import { Mailbox, MailboxError, MAX_BLOB_BYTES } from "./mailbox.js";

/** How long a long-poll waits before returning empty. Tunable for tests. */
export const DEFAULT_LONG_POLL_MS = 25_000;

export interface HandleOptions {
  /** Override the long-poll wait window. Tests pass a small value. */
  longPollMs?: number;
}

export async function handleRequest(
  request: Request,
  mailbox: Mailbox,
  opts: HandleOptions = {},
): Promise<Response> {
  const longPollMs = opts.longPollMs ?? DEFAULT_LONG_POLL_MS;
  const url = new URL(request.url);
  const path = url.pathname;
  const method = request.method.toUpperCase();

  try {
    // Public endpoints — no auth.
    if (path === "/v1/health" && method === "GET") {
      return jsonResponse(200, { ok: true });
    }
    if (path === "/v1/pair" && method === "POST") {
      return await postPair(request, mailbox);
    }
    if (path === "/v1/pair" && method === "GET") {
      return await getPair(url, mailbox, longPollMs);
    }

    // Everything else requires the per-pair Bearer token issued at pairing.
    const token = bearerToken(request);
    const expected = mailbox.getPair().authToken;
    if (!expected) {
      return errorResponse(401, "pair_incomplete", "this pair has no auth token yet — finish pairing first");
    }
    if (!token) {
      return errorResponse(401, "missing_auth", "Authorization: Bearer <auth_token> required");
    }
    if (!constantTimeEquals(token, expected)) {
      return errorResponse(403, "bad_auth", "auth token does not match this pair");
    }

    if (path === "/v1/jobs" && method === "POST") {
      return await postJob(request, mailbox);
    }
    if (path === "/v1/jobs" && method === "GET") {
      return await getJobs(url, mailbox, longPollMs);
    }
    if (path === "/v1/jobs" && method === "DELETE") {
      const jobId = url.searchParams.get("job_id");
      if (!jobId) {
        throw new BadRequestError("missing_job_id", "job_id query parameter required");
      }
      const removed = mailbox.deleteJob(jobId);
      return jsonResponse(200, { ok: true, removed });
    }
    if (path === "/v1/results" && method === "POST") {
      return await postResult(request, mailbox);
    }
    if (path === "/v1/results" && method === "GET") {
      return await getResults(url, mailbox, longPollMs);
    }
    if (path === "/v1/results" && method === "DELETE") {
      const jobId = url.searchParams.get("job_id");
      if (!jobId) {
        throw new BadRequestError("missing_job_id", "job_id query parameter required");
      }
      const removed = mailbox.deleteResultsFor(jobId);
      return jsonResponse(200, { ok: true, removed });
    }
    if (path === "/v1/pair" && method === "DELETE") {
      mailbox.revoke();
      return jsonResponse(200, { ok: true });
    }
    return errorResponse(404, "not_found", `${method} ${path}`);
  } catch (err) {
    if (err instanceof MailboxError) {
      return errorResponse(err.httpStatus, err.code, err.message);
    }
    if (err instanceof BadRequestError) {
      return errorResponse(400, err.code, err.message);
    }
    return errorResponse(500, "internal", (err as Error).message ?? "unknown");
  }
}

class BadRequestError extends Error {
  constructor(public code: string, message: string) {
    super(message);
  }
}

async function postJob(request: Request, mailbox: Mailbox): Promise<Response> {
  const body = await readJson<{ job_id?: unknown; blob?: unknown }>(request);
  const jobId = requireString(body.job_id, "job_id");
  const blob = requireString(body.blob, "blob");
  const stored = mailbox.enqueueJob(jobId, blob);
  return jsonResponse(201, {
    job_id: stored.jobId,
    seq: stored.seq,
    enqueued_at: stored.enqueuedAt,
    expires_at: stored.expiresAt,
  });
}

async function getJobs(
  url: URL,
  mailbox: Mailbox,
  longPollMs: number,
): Promise<Response> {
  // Cursor is a Unix-millis timestamp. The legacy `since` query
  // param (relay-internal seq counter) is no longer accepted — see
  // mailbox.ts pollJobs for the rationale.
  const sinceMs = parseIntParam(url, "since_ms", 0);
  const wait = parseIntParam(url, "wait_ms", longPollMs);
  const result = await mailbox.pollJobs(sinceMs, Math.min(wait, longPollMs));
  return jsonResponse(200, {
    jobs: result.jobs.map((j) => ({
      seq: j.seq,
      job_id: j.jobId,
      blob: j.blob,
      enqueued_at: j.enqueuedAt,
      expires_at: j.expiresAt,
    })),
    next_cursor_ms: result.nextCursor,
  });
}

async function postResult(request: Request, mailbox: Mailbox): Promise<Response> {
  const body = await readJson<{
    job_id?: unknown;
    page_index?: unknown;
    blob?: unknown;
    persistent?: unknown;
  }>(request);
  const jobId = requireString(body.job_id, "job_id");
  const pageIndex = requireInt(body.page_index, "page_index");
  const blob = requireString(body.blob, "blob");
  // `persistent` is optional so legacy clients keep working — they get
  // the durable default. New clients explicitly opt OUT for read/sync.
  let persistent = true;
  if (body.persistent !== undefined) {
    if (typeof body.persistent !== "boolean") {
      throw new BadRequestError("invalid_persistent", "persistent must be a boolean");
    }
    persistent = body.persistent;
  }
  const stored = mailbox.postResult(jobId, pageIndex, blob, persistent);
  return jsonResponse(201, {
    job_id: stored.jobId,
    page_index: stored.pageIndex,
    posted_at: stored.postedAt,
    expires_at: stored.expiresAt,
    persistent: stored.persistent,
  });
}

async function getResults(
  url: URL,
  mailbox: Mailbox,
  longPollMs: number,
): Promise<Response> {
  const jobId = url.searchParams.get("job_id");
  if (!jobId) {
    throw new BadRequestError("missing_job_id", "job_id query parameter required");
  }
  const wait = parseIntParam(url, "wait_ms", longPollMs);
  const result = await mailbox.pollResults(jobId, Math.min(wait, longPollMs));
  return jsonResponse(200, {
    results: result.results.map((r) => ({
      job_id: r.jobId,
      page_index: r.pageIndex,
      blob: r.blob,
      posted_at: r.postedAt,
      expires_at: r.expiresAt,
    })),
  });
}

async function postPair(request: Request, mailbox: Mailbox): Promise<Response> {
  const body = await readJson<{ side?: unknown; pubkey?: unknown }>(request);
  const side = requireString(body.side, "side");
  if (side !== "ios" && side !== "cli") {
    throw new BadRequestError("invalid_side", "side must be 'ios' or 'cli'");
  }
  const pubkey = requireString(body.pubkey, "pubkey");
  const state = mailbox.postPubkey(side, pubkey, generateAuthToken);
  return jsonResponse(201, {
    ios_pub: state.iosPub ?? null,
    cli_pub: state.cliPub ?? null,
    auth_token: state.authToken ?? null,
    completed_at: state.completedAt ?? null,
  });
}

async function getPair(url: URL, mailbox: Mailbox, longPollMs: number): Promise<Response> {
  const wait = parseIntParam(url, "wait_ms", 0);
  const state = await mailbox.pollPair(Math.min(wait, longPollMs));
  return jsonResponse(200, {
    ios_pub: state.iosPub ?? null,
    cli_pub: state.cliPub ?? null,
    auth_token: state.authToken ?? null,
    completed_at: state.completedAt ?? null,
  });
}

/**
 * generateAuthToken returns a 32-byte random Bearer token rendered as
 * lowercase hex. The default uses Web Crypto, which exists on both
 * Cloudflare Workers and modern Node — but tests inject their own to keep
 * outputs deterministic.
 */
export function generateAuthToken(): string {
  const buf = new Uint8Array(32);
  crypto.getRandomValues(buf);
  return Array.from(buf, (b) => b.toString(16).padStart(2, "0")).join("");
}

async function readJson<T>(request: Request): Promise<T> {
  // Cap body size to MAX_BLOB_BYTES + a slack envelope (2 KiB) of JSON keys.
  const maxBytes = MAX_BLOB_BYTES + 2048;
  const text = await request.text();
  if (text.length > maxBytes) {
    throw new BadRequestError("body_too_large", `body exceeds ${maxBytes} bytes`);
  }
  try {
    return JSON.parse(text) as T;
  } catch {
    throw new BadRequestError("invalid_json", "request body is not valid JSON");
  }
}

function requireString(v: unknown, field: string): string {
  if (typeof v !== "string" || v.length === 0) {
    throw new BadRequestError(`missing_${field}`, `${field} required`);
  }
  return v;
}

function requireInt(v: unknown, field: string): number {
  if (typeof v !== "number" || !Number.isInteger(v) || v < 0) {
    throw new BadRequestError(`invalid_${field}`, `${field} must be a non-negative integer`);
  }
  return v;
}

function parseIntParam(url: URL, name: string, fallback: number): number {
  const raw = url.searchParams.get(name);
  if (raw === null) return fallback;
  const n = Number(raw);
  if (!Number.isInteger(n) || n < 0) {
    throw new BadRequestError(`invalid_${name}`, `${name} must be a non-negative integer`);
  }
  return n;
}

function bearerToken(request: Request): string | null {
  const header = request.headers.get("authorization") ?? request.headers.get("Authorization");
  if (!header) return null;
  const match = header.match(/^Bearer\s+(.+)$/i);
  return match ? match[1]!.trim() : null;
}

/** constant-time string comparison; the runtime crypto.subtle helpers
 * accept ArrayBuffers only, so we do a small hand-rolled compare. */
function constantTimeEquals(a: string, b: string): boolean {
  if (a.length !== b.length) return false;
  let acc = 0;
  for (let i = 0; i < a.length; i++) {
    acc |= a.charCodeAt(i) ^ b.charCodeAt(i);
  }
  return acc === 0;
}

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

function errorResponse(status: number, code: string, message: string): Response {
  return jsonResponse(status, { code, message });
}
