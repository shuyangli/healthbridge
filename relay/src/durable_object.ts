// Cloudflare Durable Object wrapper around Mailbox.
//
// One DO instance per pair_id; the entry point in index.ts uses the pair
// query parameter as the DO name. The DO loads the mailbox from persistent
// storage on first use, processes a request, then persists any changes
// back to storage.
//
// Persistence layout (v2):
//
//   meta_v2                      → { nextSeq, pair }
//   job:<padded seq>             → StoredJob (one per pending job)
//   result:<jobId>:<padded page> → StoredResult (one per result page)
//
// One key per record means no single put ever exceeds the per-blob cap
// of MAX_BLOB_BYTES (1 MiB). Storing the whole snapshot under one key
// (the v1 layout) was fragile — once the mailbox accumulated a couple
// of near-cap blobs the combined snapshot exceeded the SQLite-backed
// DO row size limit and `state.storage.put` started throwing on every
// mutation. Those throws escaped DO.fetch as Cloudflare error 1101.
//
// On first load we migrate from the v1 `mailbox_snapshot_v1` key if it
// exists, then drop it on the next successful persist.
//
// This file is the only place that imports Workers types. The actual
// logic lives in mailbox.ts and handler.ts and is exercised by node tests.

/* eslint-disable @typescript-eslint/ban-ts-comment */
// @ts-nocheck — Workers globals are provided by the runtime, not by @types/node.

import { Mailbox } from "./mailbox.js";
import { handleRequest } from "./handler.js";

const LEGACY_SNAPSHOT_KEY = "mailbox_snapshot_v1";
const META_KEY = "meta_v2";
const JOB_PREFIX = "job:";
const RESULT_PREFIX = "result:";

function jobKey(seq: number): string {
  // 12 zero-padded digits keep keys lex-sortable so list() returns them
  // in the same order as the in-memory array.
  return `${JOB_PREFIX}${seq.toString().padStart(12, "0")}`;
}

function resultKey(jobId: string, pageIndex: number): string {
  return `${RESULT_PREFIX}${jobId}:${pageIndex.toString().padStart(6, "0")}`;
}

export class PairMailboxDO {
  private mailbox = new Mailbox();
  private loaded = false;

  constructor(private state: any, private env: unknown) {}

  private async ensureLoaded() {
    if (this.loaded) return;
    try {
      // Migration path: if a v1 snapshot key exists from a prior
      // deploy, restore from it once. The next successful persist
      // will rewrite the data in v2 layout and clean up the old key.
      const legacy = await this.state.storage.get(LEGACY_SNAPSHOT_KEY);
      if (legacy) {
        try {
          this.mailbox.restore(legacy as any);
        } catch {
          // Forward-compatible: discard corrupted snapshot rather than wedging.
        }
        this.loaded = true;
        return;
      }

      const meta = (await this.state.storage.get(META_KEY)) as
        | { nextSeq?: number; pair?: any }
        | undefined;
      const jobMap = await this.state.storage.list({ prefix: JOB_PREFIX });
      const resultMap = await this.state.storage.list({ prefix: RESULT_PREFIX });

      const jobs = Array.from(jobMap.values()).sort(
        (a: any, b: any) => a.seq - b.seq,
      );
      const results = Array.from(resultMap.values());

      this.mailbox.restore({
        jobs: jobs as any,
        results: results as any,
        nextSeq: meta?.nextSeq ?? 1,
        pair: meta?.pair ?? {},
      });
    } catch (err) {
      // Loading failure should NOT wedge the DO. Start fresh; the CLI's
      // local job mirror will re-enqueue anything in flight, and the iOS
      // app will re-drain on its next pull.
      console.error("PairMailboxDO: ensureLoaded failed:", err);
    }
    this.loaded = true;
  }

  /**
   * Write the current Mailbox state to per-record storage. Each
   * StoredJob and each StoredResult lives under its own key, so no
   * single `put` exceeds MAX_BLOB_BYTES. Anything in storage that is
   * NOT in the in-memory snapshot is deleted (this is how evictExpired
   * and revoke() propagate).
   */
  private async persist() {
    const snap = this.mailbox.snapshot();

    const desiredJobKeys = new Set<string>();
    for (const j of snap.jobs) {
      const k = jobKey(j.seq);
      desiredJobKeys.add(k);
      await this.state.storage.put(k, j);
    }

    const desiredResultKeys = new Set<string>();
    for (const r of snap.results) {
      const k = resultKey(r.jobId, r.pageIndex);
      desiredResultKeys.add(k);
      await this.state.storage.put(k, r);
    }

    // Garbage-collect any persisted records the in-memory snapshot
    // doesn't reference. Single-threaded DO execution means there are
    // no concurrent writes to race against here.
    const existingJobs = await this.state.storage.list({ prefix: JOB_PREFIX });
    for (const k of existingJobs.keys()) {
      if (!desiredJobKeys.has(k)) {
        await this.state.storage.delete(k);
      }
    }
    const existingResults = await this.state.storage.list({ prefix: RESULT_PREFIX });
    for (const k of existingResults.keys()) {
      if (!desiredResultKeys.has(k)) {
        await this.state.storage.delete(k);
      }
    }

    await this.state.storage.put(META_KEY, {
      nextSeq: snap.nextSeq,
      pair: snap.pair,
    });

    // One-time cleanup of the legacy snapshot key. delete() is a no-op
    // if the key doesn't exist, so this is cheap to repeat.
    await this.state.storage.delete(LEGACY_SNAPSHOT_KEY);
  }

  async fetch(request: Request): Promise<Response> {
    try {
      await this.ensureLoaded();
      const response = await handleRequest(request, this.mailbox);
      // Persist after every state-mutating method. Reads don't change state.
      if (request.method !== "GET") {
        try {
          await this.persist();
        } catch (err) {
          // Persistence failure does NOT fail the request — the
          // in-memory mailbox already has the update, and the next
          // successful persist will catch up. We log so wrangler tail
          // shows the underlying cause instead of a bare 1101.
          console.error("PairMailboxDO: persist failed:", err);
        }
      }
      return response;
    } catch (err) {
      // Defensive top-level catch. handleRequest already converts
      // every typed error into a JSON response, so the only way to
      // reach here is via storage.get / storage.list / a Workers
      // runtime exception. We surface those as structured 500s
      // instead of letting the runtime emit error 1101.
      console.error("PairMailboxDO: fetch failed:", err);
      const message = err instanceof Error ? err.message : String(err);
      return new Response(
        JSON.stringify({ code: "internal", message }),
        { status: 500, headers: { "content-type": "application/json" } },
      );
    }
  }
}
