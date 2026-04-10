// The Mailbox is the relay's core data structure: a per-pair store of
// encrypted job blobs (waiting for the iOS app to drain) and result blobs
// (waiting for the CLI to fetch). It is intentionally pure TypeScript with
// no Workers/Cloudflare types so it can be unit-tested in plain Node.
//
// The Durable Object wrapper in `durable_object.ts` is the only place that
// touches the Workers runtime; everything observable about the relay's
// behaviour can be exercised from here.

/**
 * Default TTLs in milliseconds.
 *
 * In this model the "ack" for an inbound job is the iOS app posting a
 * result for it (postResult auto-prunes the inbound queue entry). If
 * no result has been posted within DEFAULT_JOB_TTL_MS the job is
 * considered abandoned — the iPhone is offline, the user revoked the
 * pair, the catalog type is no longer recognised, etc — and the
 * eviction sweep drops it. 24 hours is long enough to absorb a
 * weekend of background eviction without dropping anything legitimate
 * but short enough that a stale read job ("step_count for the last
 * 7 days as of yesterday morning") doesn't keep occupying a slot in
 * the 256-job mailbox cap.
 *
 * DEFAULT_RESULT_TTL_MS is the same — once a result has been posted
 * it sits at most this long before TTL eviction. The CLI's ack-via-
 * DELETE flow normally drops them much sooner.
 */
export const DEFAULT_JOB_TTL_MS = 24 * 60 * 60 * 1000; // 24 hours
export const DEFAULT_RESULT_TTL_MS = 24 * 60 * 60 * 1000; // 24 hours

/** Hard limits to keep a single mailbox bounded. */
export const MAX_JOBS_PER_MAILBOX = 256;
export const MAX_RESULT_PAGES_PER_JOB = 1024;
export const MAX_BLOB_BYTES = 1 * 1024 * 1024; // 1 MiB per blob

/**
 * Pairing state held by the relay during the X25519 exchange. Both sides
 * post their pubkey to /v1/pair; once both are present the relay generates
 * an auth_token that both sides retrieve and use as a Bearer credential
 * on every subsequent request.
 *
 * The relay never sees the session key derived from these pubkeys.
 */
export interface PairState {
  iosPub?: string;
  cliPub?: string;
  authToken?: string;
  /** ms timestamp of when the second pubkey was committed. */
  completedAt?: number;
  /** APNs device token (hex-encoded, posted by iOS after pairing). */
  deviceToken?: string;
  /** APNs environment: "development" or "production". */
  deviceTokenEnv?: string;
}

/**
 * Stored job blob. The relay never inspects `blob` — for M1 it's plaintext
 * base64; for M2+ it's XChaCha20-Poly1305 ciphertext.
 */
export interface StoredJob {
  /** Monotonic per-mailbox sequence number. CLI uses this as a cursor. */
  seq: number;
  /** Opaque ULID assigned by the CLI; used as the result key. */
  jobId: string;
  /** Base64 ciphertext (or plaintext in M1). */
  blob: string;
  /** Wall-clock ms when this job entered the mailbox. */
  enqueuedAt: number;
  /** Wall-clock ms after which the job is considered expired. */
  expiresAt: number;
}

/**
 * Stored result page; multiple pages may share a jobId.
 *
 * `persistent` controls whether the result survives a Durable Object
 * eviction. The mailbox always holds it in memory, but only persistent
 * results are written into the snapshot the DO flushes to storage.
 *
 *   - persistent=true  → write/profile results. Tiny blobs (a HealthKit
 *     UUID, a typed enum value). The CLI may legitimately come back
 *     after a delay to retrieve them, so we want them to survive an
 *     eviction. The CLI is expected to call `DELETE /v1/results` after
 *     successfully decoding so they're pruned promptly.
 *
 *   - persistent=false → read/sync results. Blobs can be large; the
 *     CLI is normally still long-polling at the other end (synchronous
 *     flow). We keep the blob in memory for as long as the DO instance
 *     is warm but never write it to storage. After eviction the result
 *     is simply gone — the CLI's pollResults sees no entries, the
 *     long-poll times out as `pending`, and the user re-runs the read.
 */
export interface StoredResult {
  jobId: string;
  pageIndex: number;
  blob: string;
  postedAt: number;
  expiresAt: number;
  persistent: boolean;
}

/** Auto-ephemeralize any blob larger than this regardless of the
 *  persistent flag. Belt-and-braces against future code paths that
 *  forget to set the flag, and against old iOS clients that don't
 *  know about it yet. 64 KiB easily covers a HealthKit UUID + JSON
 *  envelope and a typed profile value, but rejects anything that
 *  could plausibly accumulate into a snapshot-too-big crash. */
export const AUTO_EPHEMERAL_THRESHOLD = 64 * 1024;

/** Outcome of a long-poll for jobs. */
export interface JobsPollResult {
  jobs: StoredJob[];
  /** Next cursor the caller should pass on its next poll. */
  nextCursor: number;
}

/** Outcome of a long-poll for results. */
export interface ResultsPollResult {
  results: StoredResult[];
}

/**
 * Injectable clock + waker so unit tests can drive the mailbox without
 * `setTimeout`. In production this defaults to `Date.now` and a real timer.
 */
export interface MailboxDeps {
  now(): number;
  /** Sleep up to `ms` milliseconds, returning early if `signal` resolves. */
  wait(ms: number, signal: Promise<void>): Promise<void>;
}

export const realDeps: MailboxDeps = {
  now: () => Date.now(),
  wait: (ms, signal) =>
    new Promise<void>((resolve) => {
      const timer = setTimeout(resolve, ms);
      signal.then(() => {
        clearTimeout(timer);
        resolve();
      });
    }),
};

/** Errors the mailbox can throw. Mapped to HTTP responses by the wrapper. */
export class MailboxError extends Error {
  constructor(public code: string, message: string, public httpStatus = 400) {
    super(message);
    this.name = "MailboxError";
  }
}

/**
 * Mailbox holds the state for one pair_id. The Durable Object instance owns
 * exactly one of these.
 */
export class Mailbox {
  private nextSeq = 1;
  private jobs: StoredJob[] = [];
  private results: StoredResult[] = [];
  private pair: PairState = {};
  /** Resolvers waiting on new jobs (CLI side: iOS app long-poll). */
  private jobWaiters: Array<() => void> = [];
  /** Resolvers waiting on new results, keyed by jobId (CLI side). */
  private resultWaiters = new Map<string, Array<() => void>>();
  /** Resolvers waiting on pair completion (both sides during pairing). */
  private pairWaiters: Array<() => void> = [];

  constructor(private deps: MailboxDeps = realDeps) {}

  /**
   * Returns the persistable view of the mailbox state. NON-persistent
   * results are dropped entirely (the snapshot only contains
   * persistent ones); non-persistent ones live exclusively in memory
   * and vanish on a Durable Object eviction. This is what the DO
   * writes to storage on every mutation.
   *
   * Use `inMemorySnapshot()` for tests that need the full state
   * including the ephemeral entries the persistence path intentionally
   * drops.
   */
  snapshot(): {
    jobs: StoredJob[];
    results: StoredResult[];
    nextSeq: number;
    pair: PairState;
  } {
    return {
      jobs: structuredClone(this.jobs),
      results: structuredClone(this.results.filter((r) => r.persistent)),
      nextSeq: this.nextSeq,
      pair: structuredClone(this.pair),
    };
  }

  /** Full in-memory snapshot, ephemeral results included. Tests use
   *  this to assert on data that snapshot() intentionally drops. */
  inMemorySnapshot() {
    return {
      jobs: structuredClone(this.jobs),
      results: structuredClone(this.results),
      nextSeq: this.nextSeq,
      pair: structuredClone(this.pair),
    };
  }

  /** For tests / persistence — restore from a previous snapshot.
   *  Pre-v3 snapshots don't carry `persistent` on results; default
   *  to true so legacy data is treated as durable. */
  restore(snap: {
    jobs: StoredJob[];
    results: StoredResult[];
    nextSeq: number;
    pair?: PairState;
  }) {
    this.jobs = structuredClone(snap.jobs);
    this.results = snap.results.map((r) => ({
      ...structuredClone(r),
      persistent: (r as StoredResult).persistent ?? true,
    }));
    this.nextSeq = snap.nextSeq;
    this.pair = snap.pair ? structuredClone(snap.pair) : {};
  }

  // ---- Pairing -------------------------------------------------------

  /**
   * Commit one side's pubkey. Returns the current PairState (with the
   * auth_token set if both sides are now present). The same side cannot
   * post twice — that's a 409 to prevent an attacker from rotating a
   * legitimate pairing.
   */
  postPubkey(side: "ios" | "cli", pubkey: string, generateToken: () => string): PairState {
    if (typeof pubkey !== "string" || pubkey.length === 0) {
      throw new MailboxError("invalid_pubkey", "pubkey required");
    }
    if (side === "ios") {
      if (this.pair.iosPub && this.pair.iosPub !== pubkey) {
        throw new MailboxError("pair_locked", "ios pubkey already committed", 409);
      }
      this.pair.iosPub = pubkey;
    } else {
      if (this.pair.cliPub && this.pair.cliPub !== pubkey) {
        throw new MailboxError("pair_locked", "cli pubkey already committed", 409);
      }
      this.pair.cliPub = pubkey;
    }
    if (this.pair.iosPub && this.pair.cliPub && !this.pair.authToken) {
      this.pair.authToken = generateToken();
      this.pair.completedAt = this.deps.now();
      this.wakePairWaiters();
    }
    return structuredClone(this.pair);
  }

  /** Read the current pair state. Used by both sides to retrieve the auth_token. */
  getPair(): PairState {
    return structuredClone(this.pair);
  }

  /** Store the iOS device's APNs token so the relay can push on enqueue. */
  registerDeviceToken(token: string, env: string) {
    if (typeof token !== "string" || token.length === 0) {
      throw new MailboxError("invalid_token", "token required");
    }
    if (env !== "development" && env !== "production") {
      throw new MailboxError("invalid_env", "env must be 'development' or 'production'");
    }
    this.pair.deviceToken = token;
    this.pair.deviceTokenEnv = env;
  }

  /**
   * Long-poll for pair completion. Returns immediately if the auth_token
   * is already set; otherwise waits up to maxWaitMs for the second side
   * to commit its pubkey.
   */
  async pollPair(maxWaitMs: number): Promise<PairState> {
    if (this.pair.authToken || maxWaitMs <= 0) {
      return structuredClone(this.pair);
    }
    const wake = new Promise<void>((resolve) => {
      this.pairWaiters.push(resolve);
    });
    await this.deps.wait(maxWaitMs, wake);
    return structuredClone(this.pair);
  }

  private wakePairWaiters() {
    const waiters = this.pairWaiters;
    this.pairWaiters = [];
    for (const w of waiters) w();
  }

  /**
   * Append a job blob to the mailbox. Wakes any waiting job-pollers.
   */
  enqueueJob(jobId: string, blob: string, ttlMs = DEFAULT_JOB_TTL_MS): StoredJob {
    if (typeof jobId !== "string" || jobId.length === 0) {
      throw new MailboxError("invalid_job_id", "job_id required");
    }
    if (typeof blob !== "string" || blob.length === 0) {
      throw new MailboxError("invalid_blob", "blob required");
    }
    if (blob.length > MAX_BLOB_BYTES) {
      throw new MailboxError(
        "blob_too_large",
        `blob exceeds ${MAX_BLOB_BYTES} bytes`,
        413,
      );
    }
    this.evictExpired();
    if (this.jobs.length >= MAX_JOBS_PER_MAILBOX) {
      throw new MailboxError("mailbox_full", "too many pending jobs", 429);
    }
    const now = this.deps.now();
    const stored: StoredJob = {
      seq: this.nextSeq++,
      jobId,
      blob,
      enqueuedAt: now,
      expiresAt: now + ttlMs,
    };
    this.jobs.push(stored);
    this.wakeJobWaiters();
    return stored;
  }

  /**
   * Long-poll for jobs enqueued after `sinceMs` (Unix milliseconds).
   *
   * The cursor is wall-clock based rather than the relay-internal seq
   * counter so that storage migrations / DO state resets cannot
   * desynchronise the iOS-side cursor from the relay's view of the
   * world. After a migration the relay's seq might restart at 1 even
   * though the iOS app's last-drained seq was 308; under the
   * timestamp model the iOS app's lastDrainedMs is just "the wall-
   * clock time of the most recent job I drained", which is naturally
   * monotonic and resilient.
   *
   * Returns immediately if any newer jobs are already in the queue;
   * otherwise waits up to maxWaitMs.
   */
  async pollJobs(sinceMs: number, maxWaitMs: number): Promise<JobsPollResult> {
    this.evictExpired();
    const ready = this.jobsAfter(sinceMs);
    if (ready.length > 0) {
      return { jobs: ready, nextCursor: this.cursorOf(ready, sinceMs) };
    }
    if (maxWaitMs <= 0) {
      return { jobs: [], nextCursor: sinceMs };
    }
    const wakePromise = new Promise<void>((resolve) => {
      this.jobWaiters.push(resolve);
    });
    await this.deps.wait(maxWaitMs, wakePromise);
    this.evictExpired();
    const after = this.jobsAfter(sinceMs);
    return { jobs: after, nextCursor: this.cursorOf(after, sinceMs) };
  }

  private jobsAfter(sinceMs: number): StoredJob[] {
    return this.jobs.filter((j) => j.enqueuedAt > sinceMs);
  }

  private cursorOf(found: StoredJob[], fallback = 0): number {
    if (found.length === 0) return fallback;
    // The "next cursor" is the highest enqueuedAt we returned —
    // the next poll should pick up anything after that. Jobs are
    // appended to this.jobs in arrival order so the last one is
    // the most recent.
    return found[found.length - 1]!.enqueuedAt;
  }

  private wakeJobWaiters() {
    const waiters = this.jobWaiters;
    this.jobWaiters = [];
    for (const w of waiters) w();
  }

  /**
   * Append a result page for a previously-enqueued job. Wakes any
   * waiters for that jobId.
   *
   * `persistent` decides whether the result will be written into the
   * Durable Object snapshot:
   *
   *   - `true`  → small, durable. Used by write/profile results. Survives
   *               a DO eviction. Auto-downgraded to false (with a console
   *               warning) if the blob exceeds AUTO_EPHEMERAL_THRESHOLD,
   *               so an old client that always passes true can never
   *               crash the snapshot.
   *
   *   - `false` → in-memory only. Used by read/sync results. The blob
   *               lives in this.results until either the CLI polls and
   *               acks via DELETE /v1/results, the DO is evicted (in
   *               which case the result is gone and the CLI should
   *               re-issue), or the TTL eviction sweeps it.
   *
   * The default is `true` so that callers (or the over-the-wire body)
   * that don't pass the field continue to get the durable behaviour;
   * the iOS drain loop opts INTO ephemeral by passing false for read
   * paths.
   */
  postResult(
    jobId: string,
    pageIndex: number,
    blob: string,
    persistent: boolean = true,
    ttlMs = DEFAULT_RESULT_TTL_MS,
  ): StoredResult {
    if (typeof jobId !== "string" || jobId.length === 0) {
      throw new MailboxError("invalid_job_id", "job_id required");
    }
    if (!Number.isInteger(pageIndex) || pageIndex < 0) {
      throw new MailboxError("invalid_page_index", "page_index must be a non-negative integer");
    }
    if (typeof blob !== "string" || blob.length === 0) {
      throw new MailboxError("invalid_blob", "blob required");
    }
    if (blob.length > MAX_BLOB_BYTES) {
      throw new MailboxError("blob_too_large", `blob exceeds ${MAX_BLOB_BYTES} bytes`, 413);
    }
    // Auto-ephemeralize anything large enough to threaten snapshot
    // size, regardless of what the caller passed. Cheap belt-and-braces.
    let effectivePersistent = persistent;
    if (effectivePersistent && blob.length > AUTO_EPHEMERAL_THRESHOLD) {
      effectivePersistent = false;
    }
    this.evictExpired();
    const existingPages = this.results.filter((r) => r.jobId === jobId).length;
    if (existingPages >= MAX_RESULT_PAGES_PER_JOB) {
      throw new MailboxError("too_many_result_pages", "result page cap reached", 429);
    }
    if (this.results.some((r) => r.jobId === jobId && r.pageIndex === pageIndex)) {
      throw new MailboxError(
        "duplicate_result_page",
        `page ${pageIndex} of job ${jobId} already posted`,
        409,
      );
    }
    const now = this.deps.now();
    const stored: StoredResult = {
      jobId,
      pageIndex,
      blob,
      postedAt: now,
      expiresAt: now + ttlMs,
      persistent: effectivePersistent,
    };
    this.results.push(stored);
    // Auto-prune the inbound job entry: posting a result IS the
    // ack-of-drain for the inbound queue. The iOS app will never
    // re-execute a job whose result is already on the relay (its
    // drain cursor advanced past it), so leaving the entry in the
    // queue would just consume one of the 256 MAX_JOBS_PER_MAILBOX
    // slots and eventually 429. The cleanup is idempotent — if a
    // future page for the same jobId arrives, the inbound is
    // already gone and the filter is a no-op.
    this.jobs = this.jobs.filter((j) => j.jobId !== jobId);
    this.wakeResultWaiters(jobId);
    return stored;
  }

  /**
   * Long-poll for results of a specific jobId. Returns immediately if any
   * pages exist; otherwise waits up to maxWaitMs.
   */
  async pollResults(jobId: string, maxWaitMs: number): Promise<ResultsPollResult> {
    this.evictExpired();
    const ready = this.results.filter((r) => r.jobId === jobId);
    if (ready.length > 0) return { results: ready };
    if (maxWaitMs <= 0) return { results: [] };
    const wake = new Promise<void>((resolve) => {
      const list = this.resultWaiters.get(jobId) ?? [];
      list.push(resolve);
      this.resultWaiters.set(jobId, list);
    });
    await this.deps.wait(maxWaitMs, wake);
    this.evictExpired();
    return { results: this.results.filter((r) => r.jobId === jobId) };
  }

  private wakeResultWaiters(jobId: string) {
    const waiters = this.resultWaiters.get(jobId);
    if (!waiters) return;
    this.resultWaiters.delete(jobId);
    for (const w of waiters) w();
  }

  /**
   * Delete a single job (and any result pages associated with it) by
   * jobId. Returns true if anything was removed. Used by `DELETE
   * /v1/jobs?job_id=…` so the CLI can manually unwedge a poisoned job
   * (e.g. one whose result blob would exceed MAX_BLOB_BYTES) without
   * having to wait for the 7-day TTL eviction.
   */
  deleteJob(jobId: string): boolean {
    const beforeJobs = this.jobs.length;
    const beforeResults = this.results.length;
    this.jobs = this.jobs.filter((j) => j.jobId !== jobId);
    this.results = this.results.filter((r) => r.jobId !== jobId);
    // Wake any pollers blocked on results for this job so they observe
    // the disappearance instead of timing out.
    this.wakeResultWaiters(jobId);
    return this.jobs.length < beforeJobs || this.results.length < beforeResults;
  }

  /**
   * Delete only the result pages for a jobId, leaving the inbound job
   * itself in place. Used when the iOS app needs to retry posting a
   * result for the same job (rare; mostly here for symmetry with
   * deleteJob).
   */
  deleteResultsFor(jobId: string): boolean {
    const before = this.results.length;
    this.results = this.results.filter((r) => r.jobId !== jobId);
    this.wakeResultWaiters(jobId);
    return this.results.length < before;
  }

  /** Drop everything for this pair (used by DELETE /pair). */
  revoke() {
    this.jobs = [];
    this.results = [];
    this.nextSeq = 1;
    this.pair = {};
    // Wake any in-flight pollers so they observe the empty state.
    this.wakeJobWaiters();
    this.wakePairWaiters();
    for (const waiters of this.resultWaiters.values()) {
      for (const w of waiters) w();
    }
    this.resultWaiters.clear();
  }

  /** Evict any jobs/results past their expiry. Idempotent. */
  evictExpired() {
    const now = this.deps.now();
    this.jobs = this.jobs.filter((j) => j.expiresAt > now);
    this.results = this.results.filter((r) => r.expiresAt > now);
  }

  /** For diagnostics. */
  stats() {
    this.evictExpired();
    return {
      pendingJobs: this.jobs.length,
      pendingResults: this.results.length,
      nextSeq: this.nextSeq,
    };
  }
}
