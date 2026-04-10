// APNs push sender for Cloudflare Workers.
//
// Signs an ES256 JWT using the Web Crypto API and sends an alert push
// with `content-available: 1` to the iOS device. The alert gets
// priority-10 delivery (immediate) while content-available wakes the
// app in the background to start draining. The JWT is cached for 50
// minutes (APNs rejects tokens older than 1 hour).
//
// All errors are swallowed and logged — push is best-effort and must
// never block or fail the enqueueJob path.

/** Secrets the DO needs from the Worker environment. */
export interface ApnsConfig {
  /** .p8 auth key contents (PEM body, no headers). */
  authKey: string;
  /** 10-char key ID from Apple Developer portal. */
  keyId: string;
  /** 10-char team ID from Apple Developer portal. */
  teamId: string;
  /** App bundle ID — the `apns-topic` header value. */
  bundleId: string;
}

interface CachedToken {
  cacheKey: string;
  jwt: string;
  expiresAt: number;
}

const JWT_LIFETIME_MS = 50 * 60 * 1000; // 50 minutes (APNs max is 60)
const APNS_ERROR_ENVIRONMENT_REASONS = new Set([
  "BadDeviceToken",
  "DeviceTokenNotForTopic",
  "BadEnvironment",
  "BadEnvironmentKeyInToken",
  "BadEnvironmentKeyIdInToken",
]);

let cachedToken: CachedToken | null = null;

export type PushStyle = "alert" | "silent";

/**
 * Send a push to the given APNs device token. "alert" uses priority
 * 10 with a visible notification; "silent" uses priority 5 with
 * content-available only. Both include `content-available: 1` so the
 * app wakes in the background. Best-effort: logs on failure but never
 * throws.
 */
export async function sendPush(
  deviceToken: string,
  env: "development" | "production",
  config: ApnsConfig,
  style: PushStyle = "alert",
): Promise<void> {
  try {
    const jwt = await getOrRefreshJwt(config);
    const primary = await postToApns(deviceToken, env, jwt, config, style);
    if (primary.response.ok) {
      console.log(`APNs push succeeded: ${primary.response.status} env=${env} style=${style}`);
      return;
    }

    const primaryLabel = `APNs push failed: status=${primary.response.status} env=${env} style=${style} reason=${primary.reason ?? "unknown"} body=${primary.body}`;
    const alternateEnv = env === "development" ? "production" : "development";
    if (!shouldRetryWithAlternateEnvironment(primary.response.status, primary.reason)) {
      console.error(primaryLabel);
      return;
    }

    console.warn(`${primaryLabel} — retrying with env=${alternateEnv}`);
    const fallback = await postToApns(deviceToken, alternateEnv, jwt, config, style);
    if (fallback.response.ok) {
      console.warn(
        `APNs push succeeded after env retry: original=${env} fallback=${alternateEnv} status=${fallback.response.status}`,
      );
      return;
    }

    console.error(
      `APNs push failed after env retry: original=${env} primary_status=${primary.response.status} primary_reason=${primary.reason ?? "unknown"} fallback_env=${alternateEnv} fallback_status=${fallback.response.status} fallback_reason=${fallback.reason ?? "unknown"} fallback_body=${fallback.body}`,
    );
  } catch (err) {
    console.error("APNs push error:", err);
  }
}

async function getOrRefreshJwt(config: ApnsConfig): Promise<string> {
  const now = Date.now();
  const cacheKey = jwtCacheKey(config);
  if (
    cachedToken &&
    cachedToken.cacheKey === cacheKey &&
    now < cachedToken.expiresAt
  ) {
    return cachedToken.jwt;
  }
  const jwt = await signJwt(config, now);
  cachedToken = { cacheKey, jwt, expiresAt: now + JWT_LIFETIME_MS };
  return jwt;
}

async function signJwt(config: ApnsConfig, nowMs: number): Promise<string> {
  const header = { alg: "ES256", kid: config.keyId };
  const payload = {
    iss: config.teamId,
    iat: Math.floor(nowMs / 1000),
  };

  const key = await importPrivateKey(config.authKey);

  const headerB64 = base64url(JSON.stringify(header));
  const payloadB64 = base64url(JSON.stringify(payload));
  const signingInput = `${headerB64}.${payloadB64}`;

  const signature = await crypto.subtle.sign(
    { name: "ECDSA", hash: "SHA-256" },
    key,
    new TextEncoder().encode(signingInput),
  );

  // Web Crypto returns the signature in DER-ish format (r||s, 64 bytes
  // for P-256). APNs expects raw r||s which is exactly what Web Crypto
  // gives us for ECDSA with the "raw" export — no DER unwrapping needed.
  return `${signingInput}.${base64url(signature)}`;
}

async function importPrivateKey(pem: string): Promise<any> {
  // Strip PEM headers/footers and whitespace.
  const cleaned = normalizePem(pem);
  const der = Uint8Array.from(atob(cleaned), (c) => c.charCodeAt(0));
  return crypto.subtle.importKey(
    "pkcs8",
    der,
    { name: "ECDSA", namedCurve: "P-256" },
    false,
    ["sign"],
  );
}

function jwtCacheKey(config: ApnsConfig): string {
  return [
    config.keyId,
    config.teamId,
    config.bundleId,
    normalizePem(config.authKey),
  ].join(":");
}

function normalizePem(pem: string): string {
  return pem
    .replace(/-----BEGIN PRIVATE KEY-----/g, "")
    .replace(/-----END PRIVATE KEY-----/g, "")
    .replace(/\s/g, "");
}

function hostForEnvironment(env: "development" | "production"): string {
  return env === "development"
    ? "https://api.sandbox.push.apple.com"
    : "https://api.push.apple.com";
}

async function postToApns(
  deviceToken: string,
  env: "development" | "production",
  jwt: string,
  config: ApnsConfig,
  style: PushStyle,
): Promise<{ response: Response; body: string; reason?: string }> {
  const isAlert = style === "alert";
  const aps: Record<string, unknown> = { "content-available": 1 };
  if (isAlert) {
    aps.alert = { title: "HealthBridge", body: "Your agent requested health data." };
    aps.sound = "default";
  }
  const response = await fetch(`${hostForEnvironment(env)}/3/device/${deviceToken}`, {
    method: "POST",
    headers: {
      authorization: `bearer ${jwt}`,
      "apns-topic": config.bundleId,
      "apns-push-type": isAlert ? "alert" : "background",
      "apns-priority": isAlert ? "10" : "5",
      "content-type": "application/json",
    },
    body: JSON.stringify({ aps }),
  });
  const body = await response.text().catch(() => "");
  return { response, body, reason: parseApnsReason(body) };
}

function parseApnsReason(body: string): string | undefined {
  if (!body) return undefined;
  try {
    const parsed = JSON.parse(body) as { reason?: unknown };
    return typeof parsed.reason === "string" ? parsed.reason : undefined;
  } catch {
    return undefined;
  }
}

function shouldRetryWithAlternateEnvironment(status: number, reason?: string): boolean {
  if (status !== 400 && status !== 403) {
    return false;
  }
  return reason !== undefined && APNS_ERROR_ENVIRONMENT_REASONS.has(reason);
}

export const __test = {
  getOrRefreshJwt,
  hostForEnvironment,
  parseApnsReason,
  shouldRetryWithAlternateEnvironment,
  resetCache() {
    cachedToken = null;
  },
};

function base64url(input: string | ArrayBuffer): string {
  let bytes: Uint8Array;
  if (typeof input === "string") {
    bytes = new TextEncoder().encode(input);
  } else {
    bytes = new Uint8Array(input);
  }
  // btoa works on latin1 strings; we need to go through binary.
  let binary = "";
  for (const b of bytes) {
    binary += String.fromCharCode(b);
  }
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
