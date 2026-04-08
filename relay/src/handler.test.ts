import { describe, it, expect } from "vitest";
import { Mailbox, MailboxDeps } from "./mailbox.js";
import { handleRequest } from "./handler.js";

function fakeDeps(): MailboxDeps {
  return {
    now: () => 1_000,
    wait: (_ms, signal) => signal.then(() => undefined).catch(() => undefined),
  };
}

/** newAuthedMailbox returns a Mailbox already past the pairing step — both
 * pubkeys committed, an auth_token minted. The test helpers below default
 * to using this so they don't have to thread auth through every call. */
function newMailbox() {
  const mb = new Mailbox(fakeDeps());
  // Pre-pair so the auth check passes; tests that want a fresh, unpaired
  // mailbox can use newUnpairedMailbox.
  let counter = 0;
  mb.postPubkey("ios", "ios-pub", () => `test-token-${++counter}`);
  mb.postPubkey("cli", "cli-pub", () => `test-token-${++counter}`);
  return mb;
}

function newUnpairedMailbox() {
  return new Mailbox(fakeDeps());
}

const TEST_TOKEN = "test-token-1";
const URL_BASE = "https://relay.example.com";

async function call(
  method: string,
  path: string,
  body?: unknown,
  mailbox = newMailbox(),
  token: string | null = TEST_TOKEN,
): Promise<{ status: number; body: any; mailbox: Mailbox }> {
  const init: RequestInit = { method };
  const headers: Record<string, string> = {};
  if (body !== undefined) {
    init.body = JSON.stringify(body);
    headers["content-type"] = "application/json";
  }
  if (token) {
    headers["authorization"] = `Bearer ${token}`;
  }
  init.headers = headers;
  const res = await handleRequest(new Request(`${URL_BASE}${path}`, init), mailbox, {
    longPollMs: 0,
  });
  const text = await res.text();
  const json = text ? JSON.parse(text) : null;
  return { status: res.status, body: json, mailbox };
}

describe("handleRequest routing", () => {
  it("GET /v1/health returns ok", async () => {
    const { status, body } = await call("GET", "/v1/health");
    expect(status).toBe(200);
    expect(body).toEqual({ ok: true });
  });

  it("returns 404 for unknown paths", async () => {
    const { status, body } = await call("GET", "/v1/nope");
    expect(status).toBe(404);
    expect(body.code).toBe("not_found");
  });
});

describe("POST /v1/jobs", () => {
  it("enqueues a valid job", async () => {
    const { status, body, mailbox } = await call("POST", "/v1/jobs", {
      job_id: "job-1",
      blob: "ciphertext",
    });
    expect(status).toBe(201);
    expect(body.seq).toBe(1);
    expect(body.job_id).toBe("job-1");
    expect(mailbox.stats().pendingJobs).toBe(1);
  });

  it("rejects missing job_id", async () => {
    const { status, body } = await call("POST", "/v1/jobs", { blob: "x" });
    expect(status).toBe(400);
    expect(body.code).toBe("missing_job_id");
  });

  it("rejects missing blob", async () => {
    const { status, body } = await call("POST", "/v1/jobs", { job_id: "x" });
    expect(status).toBe(400);
    expect(body.code).toBe("missing_blob");
  });

  it("rejects invalid JSON", async () => {
    const req = new Request(`${URL_BASE}/v1/jobs`, {
      method: "POST",
      body: "{not json",
      headers: { authorization: `Bearer ${TEST_TOKEN}` },
    });
    const res = await handleRequest(req, newMailbox(), { longPollMs: 0 });
    expect(res.status).toBe(400);
  });
});

describe("GET /v1/jobs (long-poll)", () => {
  it("returns enqueued jobs since cursor 0", async () => {
    const mb = newMailbox();
    mb.enqueueJob("a", "blob-a");
    mb.enqueueJob("b", "blob-b");
    const { status, body } = await call("GET", "/v1/jobs", undefined, mb);
    expect(status).toBe(200);
    expect(body.jobs).toHaveLength(2);
    expect(body.next_cursor).toBe(2);
  });

  it("filters by since parameter", async () => {
    const mb = newMailbox();
    mb.enqueueJob("a", "blob-a");
    mb.enqueueJob("b", "blob-b");
    const { body } = await call("GET", "/v1/jobs?since=1", undefined, mb);
    expect(body.jobs.map((j: any) => j.job_id)).toEqual(["b"]);
  });

  it("returns empty with original cursor when nothing pending", async () => {
    const { body } = await call("GET", "/v1/jobs?since=42");
    expect(body.jobs).toEqual([]);
    expect(body.next_cursor).toBe(42);
  });
});

describe("POST /v1/results", () => {
  it("accepts a result page", async () => {
    const mb = newMailbox();
    const { status, body } = await call(
      "POST",
      "/v1/results",
      { job_id: "j", page_index: 0, blob: "result-blob" },
      mb,
    );
    expect(status).toBe(201);
    expect(body.job_id).toBe("j");
    expect(body.page_index).toBe(0);
    expect(mb.stats().pendingResults).toBe(1);
  });

  it("rejects negative page_index", async () => {
    const { status, body } = await call("POST", "/v1/results", {
      job_id: "j",
      page_index: -1,
      blob: "x",
    });
    expect(status).toBe(400);
    expect(body.code).toBe("invalid_page_index");
  });

  it("rejects duplicate page_index for the same job", async () => {
    const mb = newMailbox();
    await call("POST", "/v1/results", { job_id: "j", page_index: 0, blob: "a" }, mb);
    const { status, body } = await call(
      "POST",
      "/v1/results",
      { job_id: "j", page_index: 0, blob: "b" },
      mb,
    );
    expect(status).toBe(409);
    expect(body.code).toBe("duplicate_result_page");
  });
});

describe("GET /v1/results (long-poll)", () => {
  it("returns posted pages for the requested job", async () => {
    const mb = newMailbox();
    mb.postResult("job-1", 0, "page-0");
    mb.postResult("job-1", 1, "page-1");
    mb.postResult("other", 0, "ignored");
    const { body } = await call("GET", "/v1/results?job_id=job-1", undefined, mb);
    expect(body.results).toHaveLength(2);
    expect(body.results.map((r: any) => r.page_index)).toEqual([0, 1]);
  });

  it("requires job_id", async () => {
    const { status, body } = await call("GET", "/v1/results");
    expect(status).toBe(400);
    expect(body.code).toBe("missing_job_id");
  });
});

describe("POST /v1/pair", () => {
  it("commits ios pubkey first, then cli pubkey, returning auth_token", async () => {
    const mb = newUnpairedMailbox();
    let res = await call("POST", "/v1/pair", { side: "ios", pubkey: "ios-pub" }, mb, null);
    expect(res.status).toBe(201);
    expect(res.body.ios_pub).toBe("ios-pub");
    expect(res.body.auth_token).toBeNull();

    res = await call("POST", "/v1/pair", { side: "cli", pubkey: "cli-pub" }, mb, null);
    expect(res.status).toBe(201);
    expect(res.body.cli_pub).toBe("cli-pub");
    expect(typeof res.body.auth_token).toBe("string");
    expect(res.body.auth_token.length).toBe(64); // 32 bytes hex
  });

  it("rejects an invalid side", async () => {
    const mb = newUnpairedMailbox();
    const { status, body } = await call("POST", "/v1/pair", { side: "watch", pubkey: "p" }, mb, null);
    expect(status).toBe(400);
    expect(body.code).toBe("invalid_side");
  });

  it("rejects a different pubkey for an already-committed side", async () => {
    const mb = newUnpairedMailbox();
    await call("POST", "/v1/pair", { side: "ios", pubkey: "ios-1" }, mb, null);
    const { status, body } = await call("POST", "/v1/pair", { side: "ios", pubkey: "ios-2" }, mb, null);
    expect(status).toBe(409);
    expect(body.code).toBe("pair_locked");
  });
});

describe("GET /v1/pair", () => {
  it("returns the current state without long-poll", async () => {
    const mb = newUnpairedMailbox();
    await call("POST", "/v1/pair", { side: "ios", pubkey: "ios-pub" }, mb, null);
    const { status, body } = await call("GET", "/v1/pair", undefined, mb, null);
    expect(status).toBe(200);
    expect(body.ios_pub).toBe("ios-pub");
    expect(body.cli_pub).toBeNull();
    expect(body.auth_token).toBeNull();
  });
});

describe("auth", () => {
  it("returns 401 with no Authorization header on a protected endpoint", async () => {
    const mb = newMailbox();
    const { status, body } = await call("POST", "/v1/jobs", { job_id: "j", blob: "b" }, mb, null);
    expect(status).toBe(401);
    expect(body.code).toBe("missing_auth");
  });

  it("returns 403 with a wrong token", async () => {
    const mb = newMailbox();
    const { status, body } = await call("POST", "/v1/jobs", { job_id: "j", blob: "b" }, mb, "wrong-token");
    expect(status).toBe(403);
    expect(body.code).toBe("bad_auth");
  });

  it("returns 401 if pairing has not completed", async () => {
    const mb = newUnpairedMailbox();
    const { status, body } = await call("POST", "/v1/jobs", { job_id: "j", blob: "b" }, mb, "any");
    expect(status).toBe(401);
    expect(body.code).toBe("pair_incomplete");
  });
});

describe("DELETE /v1/pair", () => {
  it("revokes the mailbox", async () => {
    const mb = newMailbox();
    mb.enqueueJob("a", "blob");
    mb.postResult("a", 0, "result");
    const { status, body } = await call("DELETE", "/v1/pair", undefined, mb);
    expect(status).toBe(200);
    expect(body.ok).toBe(true);
    expect(mb.stats()).toEqual({ pendingJobs: 0, pendingResults: 0, nextSeq: 1 });
  });
});

describe("POST /v1/results persistent flag", () => {
  it("defaults to persistent=true when the field is omitted", async () => {
    const mb = newMailbox();
    mb.enqueueJob("a", "blob");
    const { status, body, mailbox } = await call(
      "POST",
      "/v1/results",
      { job_id: "a", page_index: 0, blob: "uuid" },
      mb,
    );
    expect(status).toBe(201);
    expect(body.persistent).toBe(true);
    expect(mailbox.snapshot().results).toHaveLength(1);
  });

  it("honours persistent=false and drops the result from snapshot", async () => {
    const mb = newMailbox();
    mb.enqueueJob("a", "blob");
    const { status, body, mailbox } = await call(
      "POST",
      "/v1/results",
      { job_id: "a", page_index: 0, blob: "ciphertext", persistent: false },
      mb,
    );
    expect(status).toBe(201);
    expect(body.persistent).toBe(false);
    expect(mailbox.snapshot().results).toHaveLength(0);
    // The in-memory mailbox still has it for the CLI poll.
    expect(mailbox.inMemorySnapshot().results).toHaveLength(1);
  });

  it("rejects a non-boolean persistent field with invalid_persistent", async () => {
    const mb = newMailbox();
    mb.enqueueJob("a", "blob");
    const { status, body } = await call(
      "POST",
      "/v1/results",
      { job_id: "a", page_index: 0, blob: "x", persistent: "yes" },
      mb,
    );
    expect(status).toBe(400);
    expect(body.code).toBe("invalid_persistent");
  });
});

describe("DELETE /v1/jobs", () => {
  it("removes the job and its result pages, returns removed=true", async () => {
    const mb = newMailbox();
    mb.enqueueJob("alpha", "blob-a");
    mb.enqueueJob("beta", "blob-b");
    mb.postResult("alpha", 0, "result-a");
    const { status, body } = await call("DELETE", "/v1/jobs?job_id=alpha", undefined, mb);
    expect(status).toBe(200);
    expect(body).toEqual({ ok: true, removed: true });
    expect(mb.stats().pendingJobs).toBe(1);
    expect(mb.stats().pendingResults).toBe(0);
  });

  it("returns removed=false when nothing matches", async () => {
    const mb = newMailbox();
    mb.enqueueJob("alpha", "blob-a");
    const { status, body } = await call("DELETE", "/v1/jobs?job_id=ghost", undefined, mb);
    expect(status).toBe(200);
    expect(body).toEqual({ ok: true, removed: false });
    expect(mb.stats().pendingJobs).toBe(1);
  });

  it("rejects DELETE without a job_id", async () => {
    const { status, body } = await call("DELETE", "/v1/jobs", undefined);
    expect(status).toBe(400);
    expect(body.code).toBe("missing_job_id");
  });
});

describe("DELETE /v1/results", () => {
  it("removes only the result pages and leaves any unrelated inbound jobs alone", async () => {
    const mb = newMailbox();
    // Note: postResult auto-prunes the inbound entry for the
    // matching jobId. Use an orphan jobId for the result so the
    // unrelated inbound job below stays visible.
    mb.enqueueJob("untouched", "blob-x");
    mb.postResult("orphan", 0, "result");
    expect(mb.stats().pendingJobs).toBe(1);
    expect(mb.stats().pendingResults).toBe(1);

    const { status, body } = await call("DELETE", "/v1/results?job_id=orphan", undefined, mb);
    expect(status).toBe(200);
    expect(body).toEqual({ ok: true, removed: true });
    expect(mb.stats().pendingJobs).toBe(1);
    expect(mb.stats().pendingResults).toBe(0);
  });

  it("rejects DELETE without a job_id", async () => {
    const { status, body } = await call("DELETE", "/v1/results", undefined);
    expect(status).toBe(400);
    expect(body.code).toBe("missing_job_id");
  });
});
