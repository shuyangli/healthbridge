// Cloudflare Durable Object wrapper around Mailbox.
//
// One DO instance per pair_id; the entry point in index.ts uses the pair
// query parameter as the DO name. The DO loads the mailbox from persistent
// storage on first use, processes a request, then schedules an asynchronous
// snapshot back to storage. We don't bother with per-request fsync because
// blob loss in a single-pair mailbox is recoverable: the CLI keeps a local
// mirror of every job it enqueues, and the iOS app re-drains on next launch.
//
// This file is the only place that imports Workers types. The actual logic
// lives in mailbox.ts and handler.ts and is exercised by node tests.

/* eslint-disable @typescript-eslint/ban-ts-comment */
// @ts-nocheck — Workers globals are provided by the runtime, not by @types/node.

import { Mailbox } from "./mailbox.js";
import { handleRequest } from "./handler.js";

const STORAGE_KEY = "mailbox_snapshot_v1";

export class PairMailboxDO {
  private mailbox = new Mailbox();
  private loaded = false;

  constructor(private state: any, private env: unknown) {}

  private async ensureLoaded() {
    if (this.loaded) return;
    const snap = await this.state.storage.get(STORAGE_KEY);
    if (snap) {
      try {
        this.mailbox.restore(snap);
      } catch {
        // Forward-compatible: discard corrupted snapshot rather than wedging.
      }
    }
    this.loaded = true;
  }

  private async persist() {
    await this.state.storage.put(STORAGE_KEY, this.mailbox.snapshot());
  }

  async fetch(request: Request): Promise<Response> {
    await this.ensureLoaded();
    const response = await handleRequest(request, this.mailbox);
    // Persist after every state-mutating method. Reads don't change state.
    if (request.method !== "GET") {
      await this.persist();
    }
    return response;
  }
}
