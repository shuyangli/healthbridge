import { afterEach, describe, expect, it, vi } from "vitest";
import { sendPush, type ApnsConfig, __test } from "./apns.js";

const CONFIG: ApnsConfig = {
  authKey: [
    "-----BEGIN PRIVATE KEY-----",
    "MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgeeLF1MBBa49VzgeO",
    "6wr+yleGoQ5MNooGKTj5zjcOzoihRANCAASMrRvNfEncrorpuG0KMmWuSF6OodXj",
    "Iiw2K3qaQw2x+qqMli+FgddX+qMBP2jPXVxbKl3g827rkuAwUkS33IY+",
    "-----END PRIVATE KEY-----",
  ].join("\n"),
  keyId: "ABCDEFGHIJ",
  teamId: "TEAMID1234",
  bundleId: "li.shuyang.healthbridge",
};

describe("apns JWT cache", () => {
  afterEach(() => {
    __test.resetCache();
    vi.restoreAllMocks();
  });

  it("refreshes the cached JWT when the APNs config changes", async () => {
    const importKey = vi
      .spyOn(globalThis.crypto.subtle, "importKey")
      .mockResolvedValue({} as any);
    const sign = vi
      .spyOn(globalThis.crypto.subtle, "sign")
      .mockResolvedValue(new Uint8Array(64).buffer);

    const first = await __test.getOrRefreshJwt(CONFIG);
    const second = await __test.getOrRefreshJwt({
      ...CONFIG,
      keyId: "ZZZZZZZZZZ",
    });

    expect(first).not.toBe(second);
    expect(importKey).toHaveBeenCalledTimes(2);
    expect(sign).toHaveBeenCalledTimes(2);
  });
});

describe("sendPush", () => {
  afterEach(() => {
    __test.resetCache();
    vi.restoreAllMocks();
  });

  it("retries once against the other APNs environment on env mismatch errors", async () => {
    vi.spyOn(globalThis.crypto.subtle, "importKey").mockResolvedValue({} as any);
    vi.spyOn(globalThis.crypto.subtle, "sign").mockResolvedValue(new Uint8Array(64).buffer);
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ reason: "BadEnvironmentKeyInToken" }), {
          status: 403,
          headers: { "content-type": "application/json" },
        }),
      )
      .mockResolvedValueOnce(new Response("", { status: 200 }));

    await sendPush("deadbeef", "development", CONFIG);

    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(fetchMock.mock.calls[0]?.[0]).toBe(
      `${__test.hostForEnvironment("development")}/3/device/deadbeef`,
    );
    expect(fetchMock.mock.calls[1]?.[0]).toBe(
      `${__test.hostForEnvironment("production")}/3/device/deadbeef`,
    );
  });
});
