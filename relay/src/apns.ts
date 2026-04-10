// APNs silent-push sender for Cloudflare Workers.
//
// Signs an ES256 JWT using the Web Crypto API and sends a
// `content-available: 1` push to the iOS device. The JWT is cached
// for 50 minutes (APNs rejects tokens older than 1 hour).
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
  jwt: string;
  expiresAt: number;
}

const JWT_LIFETIME_MS = 50 * 60 * 1000; // 50 minutes (APNs max is 60)

let cachedToken: CachedToken | null = null;

/**
 * Send a silent push to the given APNs device token. Best-effort:
 * logs on failure but never throws.
 */
export async function sendSilentPush(
  deviceToken: string,
  env: "development" | "production",
  config: ApnsConfig,
): Promise<void> {
  try {
    const host =
      env === "development"
        ? "https://api.sandbox.push.apple.com"
        : "https://api.push.apple.com";

    const jwt = await getOrRefreshJwt(config);

    const response = await fetch(`${host}/3/device/${deviceToken}`, {
      method: "POST",
      headers: {
        authorization: `bearer ${jwt}`,
        "apns-topic": config.bundleId,
        "apns-push-type": "background",
        // Priority 5 = "send when convenient" — the right level for
        // silent push. Priority 10 would require an alert.
        "apns-priority": "5",
        "content-type": "application/json",
      },
      body: JSON.stringify({ aps: { "content-available": 1 } }),
    });

    if (!response.ok) {
      const body = await response.text().catch(() => "");
      console.error(
        `APNs push failed: ${response.status} ${body}`,
      );
    }
  } catch (err) {
    console.error("APNs push error:", err);
  }
}

async function getOrRefreshJwt(config: ApnsConfig): Promise<string> {
  const now = Date.now();
  if (cachedToken && now < cachedToken.expiresAt) {
    return cachedToken.jwt;
  }
  const jwt = await signJwt(config, now);
  cachedToken = { jwt, expiresAt: now + JWT_LIFETIME_MS };
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

async function importPrivateKey(pem: string): Promise<CryptoKey> {
  // Strip PEM headers/footers and whitespace.
  const cleaned = pem
    .replace(/-----BEGIN PRIVATE KEY-----/g, "")
    .replace(/-----END PRIVATE KEY-----/g, "")
    .replace(/\s/g, "");
  const der = Uint8Array.from(atob(cleaned), (c) => c.charCodeAt(0));
  return crypto.subtle.importKey(
    "pkcs8",
    der,
    { name: "ECDSA", namedCurve: "P-256" },
    false,
    ["sign"],
  );
}

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
