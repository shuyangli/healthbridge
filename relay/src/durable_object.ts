// Cloudflare Durable Object wrapper around Mailbox.
//
// One DO instance per pair_id; the entry point in index.ts uses the
// pair query parameter as the DO name. The DO loads the mailbox from
// persistent storage on first use, processes a request, then persists
// the (small) updated snapshot back to storage.
//
// Persistence layout (v3): one key per pair, `snapshot_v3`, holding a
// structuredClone-friendly snapshot of:
//
//   - all pending jobs (small — encrypted job requests)
//   - the pair state (pubkeys + auth_token)
//   - PERSISTENT result pages only (writes / profiles — small UUIDs and
//     enum values that should survive a DO eviction)
//   - nextSeq
//
// Read and sync results are EPHEMERAL: they live in Mailbox.results in
// memory, are never written into the snapshot, and the CLI is expected
// to ack them via DELETE /v1/results so they're pruned promptly. The
// snapshot therefore stays small no matter how many reads pass through.
//
// On first load we migrate from any older layout we recognise — v2's
// per-record meta_v2 + job:* + result:* keys, and v1's
// `mailbox_snapshot_v1` single key — then drop the old keys.
//
// A 1-hour repeating alarm wakes the DO to evictExpired() and re-snapshot
// even when no requests are coming in, so a paired-but-quiet device
// doesn't hold stale data past its TTL.

/* eslint-disable @typescript-eslint/ban-ts-comment */
// @ts-nocheck — Workers globals are provided by the runtime, not by @types/node.

import { Mailbox } from "./mailbox.js";
import { handleRequest } from "./handler.js";
import { sendSilentPush, type ApnsConfig } from "./apns.js";

const SNAPSHOT_KEY = "snapshot_v3";
const LEGACY_V1_KEY = "mailbox_snapshot_v1";
const META_V2_KEY = "meta_v2";
const JOB_V2_PREFIX = "job:";
const RESULT_V2_PREFIX = "result:";

const ALARM_INTERVAL_MS = 60 * 60 * 1000; // hourly TTL sweep

export class PairMailboxDO {
  private mailbox = new Mailbox();
  private loaded = false;

  constructor(private state: any, private env: any) {}

  private async ensureLoaded() {
    if (this.loaded) return;
    try {
      // Try the current snapshot first.
      const snap = await this.state.storage.get(SNAPSHOT_KEY);
      if (snap) {
        try {
          this.mailbox.restore(snap);
        } catch {
          // Forward-compatible: discard a corrupt snapshot rather than wedging.
        }
        this.loaded = true;
        await this.ensureAlarmScheduled();
        return;
      }

      // v2 migration: per-record keys (meta_v2 + job:<seq> + result:<jobId>:<page>).
      const metaV2 = (await this.state.storage.get(META_V2_KEY)) as
        | { nextSeq?: number; pair?: any }
        | undefined;
      if (metaV2) {
        const jobMap = await this.state.storage.list({ prefix: JOB_V2_PREFIX });
        const resultMap = await this.state.storage.list({ prefix: RESULT_V2_PREFIX });
        const jobs = Array.from(jobMap.values()).sort(
          (a: any, b: any) => a.seq - b.seq,
        );
        const results = Array.from(resultMap.values());
        this.mailbox.restore({
          jobs: jobs as any,
          results: results as any,
          nextSeq: metaV2.nextSeq ?? 1,
          pair: metaV2.pair ?? {},
        });
        // Persist in v3 format and drop the old keys.
        await this.persist();
        for (const k of jobMap.keys()) await this.state.storage.delete(k);
        for (const k of resultMap.keys()) await this.state.storage.delete(k);
        await this.state.storage.delete(META_V2_KEY);
        this.loaded = true;
        await this.ensureAlarmScheduled();
        return;
      }

      // v1 migration: single-key legacy snapshot.
      const legacy = await this.state.storage.get(LEGACY_V1_KEY);
      if (legacy) {
        try {
          this.mailbox.restore(legacy as any);
        } catch {
          // Forward-compatible: discard corruption.
        }
        await this.persist();
        await this.state.storage.delete(LEGACY_V1_KEY);
      }
    } catch (err) {
      // Loading must NEVER wedge the DO. Start fresh; the CLI's local
      // mirror will re-enqueue anything in flight, and the iOS app will
      // re-drain.
      console.error("PairMailboxDO: ensureLoaded failed:", err);
    }
    this.loaded = true;
    await this.ensureAlarmScheduled();
  }

  /**
   * Snapshot the in-memory mailbox to a single DO storage key. Only
   * persistent results are written (the snapshot's filter() drops
   * ephemeral entries entirely), so this stays small no matter how
   * many reads have passed through.
   */
  private async persist() {
    const snap = this.mailbox.snapshot();
    await this.state.storage.put(SNAPSHOT_KEY, snap);
  }

  /**
   * Fire-and-forget silent push to the iOS device. Called after a
   * successful job enqueue. If any APNs secret is missing, or no
   * device token is registered, this is a no-op.
   */
  private maybeSendPush() {
    const pair = this.mailbox.getPair();
    if (!pair.deviceToken || !pair.deviceTokenEnv) {
      console.log("maybeSendPush: no device token registered — skipping");
      return;
    }
    const config = this.apnsConfig();
    if (!config) {
      console.log("maybeSendPush: APNs config incomplete — skipping");
      return;
    }
    console.log(`maybeSendPush: sending to ${pair.deviceToken.slice(0, 8)}… env=${pair.deviceTokenEnv}`);
    // waitUntil keeps the DO alive for the outbound fetch without
    // blocking the response to the CLI.
    this.state.waitUntil(
      sendSilentPush(
        pair.deviceToken,
        pair.deviceTokenEnv as "development" | "production",
        config,
      ),
    );
  }

  private apnsConfig(): ApnsConfig | null {
    const authKey = (this.env.APNS_AUTH_KEY as string | undefined)?.trim();
    const keyId = (this.env.APNS_KEY_ID as string | undefined)?.trim();
    const teamId = (this.env.APNS_TEAM_ID as string | undefined)?.trim();
    const bundleId = (this.env.APNS_BUNDLE_ID as string | undefined)?.trim();
    if (!authKey || !keyId || !teamId || !bundleId) {
      console.log(`maybeSendPush: missing secrets — key=${!!authKey} keyId=${!!keyId} teamId=${!!teamId} bundleId=${!!bundleId}`);
      return null;
    }
    return { authKey, keyId, teamId, bundleId };
  }

  private async ensureAlarmScheduled() {
    try {
      const current = await this.state.storage.getAlarm();
      if (current === null || current === undefined) {
        await this.state.storage.setAlarm(Date.now() + ALARM_INTERVAL_MS);
      }
    } catch (err) {
      // setAlarm is best-effort; we still evict on every fetch.
      console.error("PairMailboxDO: setAlarm failed:", err);
    }
  }

  /**
   * Cloudflare Workers calls this when the alarm fires. We sweep
   * expired jobs/results and re-arm. The DO instance is only spun up
   * for the duration of this method, so the in-memory mailbox is
   * (re)loaded from storage first.
   */
  async alarm(): Promise<void> {
    try {
      await this.ensureLoaded();
      const before = this.mailbox.stats();
      this.mailbox.evictExpired();
      const after = this.mailbox.stats();
      if (
        after.pendingJobs !== before.pendingJobs ||
        after.pendingResults !== before.pendingResults
      ) {
        await this.persist();
      }
    } catch (err) {
      console.error("PairMailboxDO: alarm sweep failed:", err);
    } finally {
      try {
        await this.state.storage.setAlarm(Date.now() + ALARM_INTERVAL_MS);
      } catch (err) {
        console.error("PairMailboxDO: alarm reschedule failed:", err);
      }
    }
  }

  async fetch(request: Request): Promise<Response> {
    try {
      await this.ensureLoaded();
      const url = new URL(request.url);
      const isJobEnqueue =
        request.method === "POST" && url.pathname === "/v1/jobs";

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

      // Fire a silent push after a successful job enqueue so the iOS
      // app wakes immediately instead of waiting for its next long-poll.
      // Best-effort: failures are logged but never block the response.
      if (isJobEnqueue && response.status === 201) {
        this.maybeSendPush();
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
