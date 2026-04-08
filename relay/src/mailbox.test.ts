import { describe, it, expect, beforeEach } from "vitest";
import {
  Mailbox,
  MailboxError,
  MailboxDeps,
  DEFAULT_JOB_TTL_MS,
  MAX_JOBS_PER_MAILBOX,
  MAX_BLOB_BYTES,
  AUTO_EPHEMERAL_THRESHOLD,
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
    const deps = fakeDeps(1000);
    const mb = new Mailbox(deps);
    mb.enqueueJob("a", "blob-a"); // enqueuedAt = 1000
    deps.advance(1);
    mb.enqueueJob("b", "blob-b"); // enqueuedAt = 1001
    const res = await mb.pollJobs(0, 1000);
    expect(res.jobs.map((j) => j.jobId)).toEqual(["a", "b"]);
    expect(res.nextCursor).toBe(1001);
  });

  it("filters by sinceMs cursor", async () => {
    const deps = fakeDeps(1000);
    const mb = new Mailbox(deps);
    mb.enqueueJob("a", "blob-a"); // enqueuedAt = 1000
    deps.advance(1);
    mb.enqueueJob("b", "blob-b"); // enqueuedAt = 1001
    const res = await mb.pollJobs(1000, 1000);
    expect(res.jobs.map((j) => j.jobId)).toEqual(["b"]);
    expect(res.nextCursor).toBe(1001);
  });

  it("blocks until a job arrives, then resolves", async () => {
    const deps = fakeDeps(1000);
    const mb = new Mailbox(deps);
    const pollPromise = mb.pollJobs(500, 60_000);
    // Enqueue happens after the poll started; the waker should fire.
    setTimeout(() => mb.enqueueJob("late", "blob-late"), 0);
    const res = await pollPromise;
    expect(res.jobs.map((j) => j.jobId)).toEqual(["late"]);
    expect(res.nextCursor).toBe(1000);
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

describe("Mailbox.postPubkey / pollPair", () => {
  let counter = 0;
  const fakeToken = () => `token-${++counter}`;

  beforeEach(() => {
    counter = 0;
  });

  it("returns auth_token only after both sides commit", () => {
    const mb = new Mailbox(fakeDeps());
    let state = mb.postPubkey("ios", "ios-pub", fakeToken);
    expect(state.iosPub).toBe("ios-pub");
    expect(state.cliPub).toBeUndefined();
    expect(state.authToken).toBeUndefined();

    state = mb.postPubkey("cli", "cli-pub", fakeToken);
    expect(state.cliPub).toBe("cli-pub");
    expect(state.authToken).toBe("token-1");
  });

  it("rejects a different pubkey for the same side once committed", () => {
    const mb = new Mailbox(fakeDeps());
    mb.postPubkey("ios", "ios-pub", fakeToken);
    expectMailboxError(() => mb.postPubkey("ios", "ios-other", fakeToken), "pair_locked", 409);
  });

  it("posting the identical pubkey twice is idempotent", () => {
    const mb = new Mailbox(fakeDeps());
    mb.postPubkey("ios", "ios-pub", fakeToken);
    expect(() => mb.postPubkey("ios", "ios-pub", fakeToken)).not.toThrow();
  });

  it("does not regenerate auth_token after completion", () => {
    const mb = new Mailbox(fakeDeps());
    mb.postPubkey("ios", "ios-pub", fakeToken);
    mb.postPubkey("cli", "cli-pub", fakeToken);
    const before = mb.getPair().authToken;
    // Re-posting same pubkeys should not mint a fresh token.
    mb.postPubkey("ios", "ios-pub", fakeToken);
    expect(mb.getPair().authToken).toBe(before);
  });

  it("pollPair blocks until both sides have posted", async () => {
    const mb = new Mailbox(fakeDeps());
    mb.postPubkey("ios", "ios-pub", fakeToken);
    const promise = mb.pollPair(60_000);
    setTimeout(() => mb.postPubkey("cli", "cli-pub", fakeToken), 0);
    const state = await promise;
    expect(state.authToken).toBe("token-1");
  });

  it("pollPair returns immediately if already complete", async () => {
    const mb = new Mailbox(fakeDeps());
    mb.postPubkey("ios", "ios-pub", fakeToken);
    mb.postPubkey("cli", "cli-pub", fakeToken);
    const state = await mb.pollPair(0);
    expect(state.authToken).toBe("token-1");
  });

  it("revoke wipes pair state", () => {
    const mb = new Mailbox(fakeDeps());
    mb.postPubkey("ios", "ios-pub", fakeToken);
    mb.postPubkey("cli", "cli-pub", fakeToken);
    mb.revoke();
    expect(mb.getPair()).toEqual({});
  });
});

describe("Mailbox.snapshot/restore", () => {
  it("preserves jobs, results, and the seq counter", () => {
    const a = new Mailbox(fakeDeps(2000));
    a.enqueueJob("j1", "blob1");
    a.enqueueJob("j2", "blob2");
    // postResult auto-prunes the matching inbound entry, so j1 is
    // gone from the pending queue after this line.
    a.postResult("j1", 0, "result-1");
    const snap = a.snapshot();

    const b = new Mailbox(fakeDeps(2000));
    b.restore(snap);
    expect(b.stats().pendingJobs).toBe(1);
    expect(b.stats().pendingResults).toBe(1);
    // Next enqueue should continue the seq, not reset it. j1's seq
    // was 1, j2's was 2, so the next one is 3.
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

describe("Mailbox.deleteJob", () => {
  it("removes any result pages for the jobId and reports true", () => {
    const mb = new Mailbox(fakeDeps(0));
    mb.enqueueJob("alpha", "blob-a");
    mb.enqueueJob("beta", "blob-b");
    // postResult auto-prunes inbound entries, so alpha and beta are
    // already gone from pendingJobs by the time we call deleteJob.
    // What's left to delete are the result pages.
    mb.postResult("alpha", 0, "result-a-0");
    mb.postResult("alpha", 1, "result-a-1");
    mb.postResult("beta", 0, "result-b");
    expect(mb.stats().pendingJobs).toBe(0);

    const removed = mb.deleteJob("alpha");
    expect(removed).toBe(true);
    expect(mb.stats().pendingJobs).toBe(0);
    expect(mb.stats().pendingResults).toBe(1);
  });

  it("returns false when nothing matches the jobId", () => {
    const mb = new Mailbox(fakeDeps(0));
    mb.enqueueJob("alpha", "blob-a");
    expect(mb.deleteJob("not-a-job")).toBe(false);
    expect(mb.stats().pendingJobs).toBe(1);
  });

  it("wakes any pollers waiting on the deleted jobId", async () => {
    const mb = new Mailbox(fakeDeps(0));
    mb.enqueueJob("ghost", "blob");
    // Start a poll for ghost's results, then delete the job. The
    // poller should observe an empty result set rather than waiting
    // out the long-poll window.
    const pollPromise = mb.pollResults("ghost", 1000);
    mb.deleteJob("ghost");
    const result = await pollPromise;
    expect(result.results).toEqual([]);
  });
});

describe("Mailbox.deleteResultsFor", () => {
  it("removes only the result pages, not any inbound job", () => {
    const mb = new Mailbox(fakeDeps(0));
    // Use postResult for an orphan jobId so the inbound queue is
    // unaffected (postResult auto-prunes any matching inbound).
    mb.enqueueJob("untouched", "blob-x");
    mb.postResult("alpha", 0, "result");
    expect(mb.stats().pendingJobs).toBe(1);
    expect(mb.stats().pendingResults).toBe(1);

    expect(mb.deleteResultsFor("alpha")).toBe(true);
    expect(mb.stats().pendingJobs).toBe(1);
    expect(mb.stats().pendingResults).toBe(0);
  });

  it("returns false when no results match", () => {
    const mb = new Mailbox(fakeDeps(0));
    expect(mb.deleteResultsFor("ghost")).toBe(false);
  });
});

describe("Mailbox.postResult prunes inbound jobs", () => {
  it("removes the matching inbound job entry on post", () => {
    const mb = new Mailbox(fakeDeps(0));
    mb.enqueueJob("alpha", "blob-a");
    mb.enqueueJob("beta", "blob-b");
    expect(mb.stats().pendingJobs).toBe(2);

    mb.postResult("alpha", 0, "result", true);
    expect(mb.stats().pendingJobs).toBe(1);
    expect(mb.stats().pendingResults).toBe(1);
  });

  it("is a no-op when the inbound job is already gone (multi-page case)", () => {
    const mb = new Mailbox(fakeDeps(0));
    mb.enqueueJob("alpha", "blob-a");
    mb.postResult("alpha", 0, "page-0", false);
    expect(mb.stats().pendingJobs).toBe(0);
    // Second page for the same jobId — inbound already pruned, no throw.
    mb.postResult("alpha", 1, "page-1", false);
    expect(mb.stats().pendingJobs).toBe(0);
    expect(mb.stats().pendingResults).toBe(2);
  });

  it("posting a result for an unknown jobId still works (orphan result)", () => {
    const mb = new Mailbox(fakeDeps(0));
    // Edge case: relay accepts the result even though no inbound
    // entry exists (e.g. inbound was pruned by an admin).
    mb.postResult("ghost", 0, "blob", true);
    expect(mb.stats().pendingJobs).toBe(0);
    expect(mb.stats().pendingResults).toBe(1);
  });
});

describe("Mailbox.postResult persistent flag", () => {
  it("defaults to persistent=true when not passed", () => {
    const mb = new Mailbox(fakeDeps(0));
    const r = mb.postResult("a", 0, "small");
    expect(r.persistent).toBe(true);
  });

  it("respects an explicit persistent=false", () => {
    const mb = new Mailbox(fakeDeps(0));
    const r = mb.postResult("a", 0, "ephemeral", false);
    expect(r.persistent).toBe(false);
  });

  it("auto-downgrades to ephemeral when blob exceeds AUTO_EPHEMERAL_THRESHOLD even if persistent=true", () => {
    const mb = new Mailbox(fakeDeps(0));
    const big = "x".repeat(AUTO_EPHEMERAL_THRESHOLD + 1);
    const r = mb.postResult("big", 0, big, true);
    expect(r.persistent).toBe(false);
  });

  it("keeps persistent=true for blobs at exactly the threshold", () => {
    const mb = new Mailbox(fakeDeps(0));
    const just = "x".repeat(AUTO_EPHEMERAL_THRESHOLD);
    const r = mb.postResult("ok", 0, just, true);
    expect(r.persistent).toBe(true);
  });
});

describe("Mailbox.snapshot persistence filter", () => {
  it("drops ephemeral results entirely from the snapshot", () => {
    const mb = new Mailbox(fakeDeps(0));
    mb.postResult("write-1", 0, "uuid-1", true);
    mb.postResult("read-1", 0, "huge", false);
    mb.postResult("write-2", 0, "uuid-2", true);

    const snap = mb.snapshot();
    expect(snap.results).toHaveLength(2);
    expect(snap.results.map((r) => r.jobId).sort()).toEqual(["write-1", "write-2"]);
    // Persistent ones keep their blob.
    for (const r of snap.results) {
      expect(r.blob).not.toBe("");
      expect(r.persistent).toBe(true);
    }
  });

  it("inMemorySnapshot still returns ephemeral results for tests", () => {
    const mb = new Mailbox(fakeDeps(0));
    mb.postResult("read-1", 0, "huge", false);
    const inMem = mb.inMemorySnapshot();
    expect(inMem.results).toHaveLength(1);
    expect(inMem.results[0].persistent).toBe(false);
    expect(inMem.results[0].blob).toBe("huge");
  });

  it("restoring a snapshot then snapshotting again is stable", () => {
    const a = new Mailbox(fakeDeps(0));
    a.enqueueJob("j1", "blob-j1");
    a.postResult("j1", 0, "uuid", true);
    a.postResult("ephemeral-job", 0, "data", false);
    const snap = a.snapshot();
    expect(snap.results).toHaveLength(1);

    const b = new Mailbox(fakeDeps(2000));
    b.restore(snap);
    expect(b.stats().pendingResults).toBe(1);
    // Re-snapshotting drops nothing further.
    expect(b.snapshot().results).toHaveLength(1);
  });

  it("legacy snapshot without persistent field is restored as durable", () => {
    const mb = new Mailbox(fakeDeps(0));
    // Simulate a v2-format snapshot: result with no `persistent` field.
    mb.restore({
      jobs: [],
      results: [
        {
          jobId: "old",
          pageIndex: 0,
          blob: "blob",
          postedAt: 0,
          expiresAt: 1_000_000,
        } as any,
      ],
      nextSeq: 1,
    });
    const snap = mb.snapshot();
    expect(snap.results).toHaveLength(1);
    expect(snap.results[0].persistent).toBe(true);
  });
});
