// The Mailbox is the relay's core data structure: a per-pair store of
// encrypted job blobs (waiting for the iOS app to drain) and result blobs
// (waiting for the CLI to fetch). It is intentionally pure TypeScript with
// no Workers/Cloudflare types so it can be unit-tested in plain Node.
//
// The Durable Object wrapper in `durable_object.ts` is the only place that
// touches the Workers runtime; everything observable about the relay's
// behaviour can be exercised from here.

/** Default TTLs in milliseconds. Match the design doc. */
export const DEFAULT_JOB_TTL_MS = 7 * 24 * 60 * 60 * 1000; // 7 days
export const DEFAULT_RESULT_TTL_MS = 24 * 60 * 60 * 1000; // 24 hours

/** Hard limits to keep a single mailbox bounded. */
export const MAX_JOBS_PER_MAILBOX = 256;
export const MAX_RESULT_PAGES_PER_JOB = 1024;
export const MAX_BLOB_BYTES = 1 * 1024 * 1024; // 1 MiB per blob

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

/** Stored result page; multiple pages may share a jobId. */
export interface StoredResult {
  jobId: string;
  pageIndex: number;
  blob: string;
  postedAt: number;
  expiresAt: number;
}

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
  /** Resolvers waiting on new jobs (CLI side: iOS app long-poll). */
  private jobWaiters: Array<() => void> = [];
  /** Resolvers waiting on new results, keyed by jobId (CLI side). */
  private resultWaiters = new Map<string, Array<() => void>>();

  constructor(private deps: MailboxDeps = realDeps) {}

  /** For tests / persistence. */
  snapshot(): { jobs: StoredJob[]; results: StoredResult[]; nextSeq: number } {
    return {
      jobs: structuredClone(this.jobs),
      results: structuredClone(this.results),
      nextSeq: this.nextSeq,
    };
  }

  /** For tests / persistence — restore from a previous snapshot. */
  restore(snap: { jobs: StoredJob[]; results: StoredResult[]; nextSeq: number }) {
    this.jobs = structuredClone(snap.jobs);
    this.results = structuredClone(snap.results);
    this.nextSeq = snap.nextSeq;
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
   * Long-poll for jobs whose seq > since. Returns immediately if any are
   * already pending; otherwise waits up to maxWaitMs.
   */
  async pollJobs(since: number, maxWaitMs: number): Promise<JobsPollResult> {
    this.evictExpired();
    const ready = this.jobsAfter(since);
    if (ready.length > 0) {
      return { jobs: ready, nextCursor: this.cursorOf(ready) };
    }
    if (maxWaitMs <= 0) {
      return { jobs: [], nextCursor: since };
    }
    const wakePromise = new Promise<void>((resolve) => {
      this.jobWaiters.push(resolve);
    });
    await this.deps.wait(maxWaitMs, wakePromise);
    this.evictExpired();
    const after = this.jobsAfter(since);
    return { jobs: after, nextCursor: this.cursorOf(after, since) };
  }

  private jobsAfter(since: number): StoredJob[] {
    return this.jobs.filter((j) => j.seq > since);
  }

  private cursorOf(found: StoredJob[], fallback = 0): number {
    if (found.length === 0) return fallback;
    return found[found.length - 1]!.seq;
  }

  private wakeJobWaiters() {
    const waiters = this.jobWaiters;
    this.jobWaiters = [];
    for (const w of waiters) w();
  }

  /**
   * Append a result page for a previously-enqueued job. Wakes any waiters
   * for that jobId.
   */
  postResult(
    jobId: string,
    pageIndex: number,
    blob: string,
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
    };
    this.results.push(stored);
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

  /** Drop everything for this pair (used by DELETE /pair). */
  revoke() {
    this.jobs = [];
    this.results = [];
    this.nextSeq = 1;
    // Wake any in-flight pollers so they observe the empty state.
    this.wakeJobWaiters();
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
