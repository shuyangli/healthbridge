import { describe, it, expect, beforeEach } from "vitest";
import {
  Mailbox,
  MailboxError,
  MailboxDeps,
  DEFAULT_JOB_TTL_MS,
  MAX_JOBS_PER_MAILBOX,
  MAX_BLOB_BYTES,
} from "./mailbox.js";

// Synthetic clock + waker so the tests are deterministic and never hit a
// real timer. `wait()` always returns immediately — long-poll behaviour is
// driven entirely by enqueue/postResult ordering.
/**
 * Helper that asserts a thrown MailboxError carries the expected code (and
 * optionally an http status). Vitest's `toThrowError(regex)` matches against
 * the message text, which is human-facing — we want to assert the stable
 * machine-readable code instead.
 */
function expectMailboxError(fn: () => unknown, code: string, httpStatus?: number) {
  try {
    fn();
  } catch (err) {
    expect(err).toBeInstanceOf(MailboxError);
    expect((err as MailboxError).code).toBe(code);
    if (httpStatus !== undefined) {
      expect((err as MailboxError).httpStatus).toBe(httpStatus);
    }
    return;
  }
  throw new Error(`expected MailboxError(${code}), got no throw`);
}

function fakeDeps(initial = 0): MailboxDeps & { advance(ms: number): void } {
  let now = initial;
  return {
    now: () => now,
    wait: (_ms: number, signal: Promise<void>) => signal.then(() => undefined).catch(() => undefined),
    advance(ms: number) {
      now += ms;
    },
  } as any;
}

describe("Mailbox.enqueueJob", () => {
  let deps: ReturnType<typeof fakeDeps>;
  let mb: Mailbox;
  beforeEach(() => {
    deps = fakeDeps(1_000);
    mb = new Mailbox(deps);
  });

  it("assigns monotonic seq numbers", () => {
    const a = mb.enqueueJob("job-a", "blob-a");
    const b = mb.enqueueJob("job-b", "blob-b");
    expect(a.seq).toBe(1);
    expect(b.seq).toBe(2);
    expect(mb.stats().nextSeq).toBe(3);
  });

  it("sets expiry from TTL", () => {
    const j = mb.enqueueJob("job", "blob", 5000);
    expect(j.enqueuedAt).toBe(1000);
    expect(j.expiresAt).toBe(6000);
  });

  it("rejects empty job_id", () => {
    expect(() => mb.enqueueJob("", "blob")).toThrowError(MailboxError);
  });

  it("rejects empty blob", () => {
    expect(() => mb.enqueueJob("job", "")).toThrowError(MailboxError);
  });

  it("rejects oversized blobs", () => {
    const huge = "x".repeat(MAX_BLOB_BYTES + 1);
    expect(() => mb.enqueueJob("job", huge)).toThrowError(MailboxError);
    expect(() => mb.enqueueJob("job", huge)).toThrow(/exceeds/);
  });

  it("rejects when mailbox is full", () => {
    for (let i = 0; i < MAX_JOBS_PER_MAILBOX; i++) {
      mb.enqueueJob(`job-${i}`, "blob");
    }
    try {
      mb.enqueueJob("overflow", "blob");
      throw new Error("expected throw");
    } catch (err) {
      expect(err).toBeInstanceOf(MailboxError);
      expect((err as MailboxError).code).toBe("mailbox_full");
      expect((err as MailboxError).httpStatus).toBe(429);
    }
  });
});

describe("Mailbox.pollJobs", () => {
  it("returns immediately when jobs are already pending", async () => {
    const mb = new Mailbox(fakeDeps());
    mb.enqueueJob("a", "blob-a");
    mb.enqueueJob("b", "blob-b");
    const res = await mb.pollJobs(0, 1000);
    expect(res.jobs.map((j) => j.jobId)).toEqual(["a", "b"]);
    expect(res.nextCursor).toBe(2);
  });

  it("filters by since cursor", async () => {
    const mb = new Mailbox(fakeDeps());
    mb.enqueueJob("a", "blob-a"); // seq 1
    mb.enqueueJob("b", "blob-b"); // seq 2
    const res = await mb.pollJobs(1, 1000);
    expect(res.jobs.map((j) => j.jobId)).toEqual(["b"]);
    expect(res.nextCursor).toBe(2);
  });

  it("blocks until a job arrives, then resolves", async () => {
    const mb = new Mailbox(fakeDeps());
    const pollPromise = mb.pollJobs(0, 60_000);
    // Enqueue happens after the poll started; the waker should fire.
    setTimeout(() => mb.enqueueJob("late", "blob-late"), 0);
    const res = await pollPromise;
    expect(res.jobs.map((j) => j.jobId)).toEqual(["late"]);
    expect(res.nextCursor).toBe(1);
  });

  it("returns empty with the original cursor on timeout (no jobs)", async () => {
    const mb = new Mailbox(fakeDeps());
    const res = await mb.pollJobs(5, 0);
    expect(res.jobs).toEqual([]);
    expect(res.nextCursor).toBe(5);
  });

  it("does not return expired jobs", async () => {
    const deps = fakeDeps(1000);
    const mb = new Mailbox(deps);
    mb.enqueueJob("ephemeral", "blob", 100);
    deps.advance(500);
    const res = await mb.pollJobs(0, 0);
    expect(res.jobs).toEqual([]);
  });
});

describe("Mailbox.postResult / pollResults", () => {
  it("returns posted pages immediately", async () => {
    const mb = new Mailbox(fakeDeps());
    mb.postResult("job-1", 0, "page-0");
    mb.postResult("job-1", 1, "page-1");
    const res = await mb.pollResults("job-1", 0);
    expect(res.results.map((r) => r.pageIndex)).toEqual([0, 1]);
  });

  it("does not surface other jobs' pages", async () => {
    const mb = new Mailbox(fakeDeps());
    mb.postResult("job-a", 0, "blob");
    mb.postResult("job-b", 0, "blob");
    const res = await mb.pollResults("job-a", 0);
    expect(res.results).toHaveLength(1);
    expect(res.results[0]!.jobId).toBe("job-a");
  });

  it("rejects duplicate (job_id, page_index) pairs", () => {
    const mb = new Mailbox(fakeDeps());
    mb.postResult("job", 0, "blob");
    expectMailboxError(() => mb.postResult("job", 0, "blob"), "duplicate_result_page", 409);
  });

  it("rejects negative page_index", () => {
    const mb = new Mailbox(fakeDeps());
    expectMailboxError(() => mb.postResult("job", -1, "blob"), "invalid_page_index");
  });

  it("blocks until a result arrives, then resolves", async () => {
    const mb = new Mailbox(fakeDeps());
    const pollPromise = mb.pollResults("job-x", 60_000);
    setTimeout(() => mb.postResult("job-x", 0, "ok"), 0);
    const res = await pollPromise;
    expect(res.results).toHaveLength(1);
    expect(res.results[0]!.blob).toBe("ok");
  });
});

describe("Mailbox.revoke", () => {
  it("drops jobs and results and resets seq", () => {
    const mb = new Mailbox(fakeDeps());
    mb.enqueueJob("a", "blob");
    mb.postResult("a", 0, "result");
    mb.revoke();
    expect(mb.stats()).toEqual({ pendingJobs: 0, pendingResults: 0, nextSeq: 1 });
  });

  it("wakes any pending pollers", async () => {
    const mb = new Mailbox(fakeDeps());
    const jobsPoll = mb.pollJobs(0, 60_000);
    const resultsPoll = mb.pollResults("nope", 60_000);
    mb.revoke();
    const [jobs, results] = await Promise.all([jobsPoll, resultsPoll]);
    expect(jobs.jobs).toEqual([]);
    expect(results.results).toEqual([]);
  });
});

describe("Mailbox.snapshot/restore", () => {
  it("preserves jobs, results, and the seq counter", () => {
    const a = new Mailbox(fakeDeps(2000));
    a.enqueueJob("j1", "blob1");
    a.enqueueJob("j2", "blob2");
    a.postResult("j1", 0, "result-1");
    const snap = a.snapshot();

    const b = new Mailbox(fakeDeps(2000));
    b.restore(snap);
    expect(b.stats().pendingJobs).toBe(2);
    expect(b.stats().pendingResults).toBe(1);
    // Next enqueue should continue the seq, not reset it.
    const next = b.enqueueJob("j3", "blob3");
    expect(next.seq).toBe(3);
  });
});

describe("Mailbox eviction", () => {
  it("uses default TTL of 7 days when none is given", () => {
    const deps = fakeDeps(0);
    const mb = new Mailbox(deps);
    const j = mb.enqueueJob("x", "blob");
    expect(j.expiresAt).toBe(DEFAULT_JOB_TTL_MS);
  });

  it("evictExpired is idempotent", () => {
    const deps = fakeDeps(1000);
    const mb = new Mailbox(deps);
    mb.enqueueJob("short", "blob", 100);
    deps.advance(500);
    mb.evictExpired();
    mb.evictExpired();
    expect(mb.stats().pendingJobs).toBe(0);
  });
});
