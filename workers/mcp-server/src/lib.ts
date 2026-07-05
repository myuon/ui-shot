/**
 * Pure helper functions for the ui-shot MCP server.
 * Extracted here so they can be unit-tested without a Workers runtime.
 */

/** Supported image MIME types and their file extensions. */
export const SUPPORTED_TYPES = {
  "image/png": ".png",
  "image/jpeg": ".jpeg",
  "image/webp": ".webp",
  "image/gif": ".gif",
} as const satisfies Record<string, string>;

export type SupportedMimeType = keyof typeof SUPPORTED_TYPES;

/** Maximum allowed decoded image size (10 MiB). */
export const MAX_IMAGE_BYTES = 10 * 1024 * 1024;

/**
 * Build the R2 object key following the same convention as the Go CLI's ObjectKey().
 *
 *   PR:    <repo>/pr-<number>/<commit>/<name>.<ext>
 *   Issue: <repo>/issue-<number>/<commit>/<name>.<ext>
 *
 * ext must include the leading dot (e.g. ".png").
 */
export function buildObjectKey(
  repo: string,
  kind: "pr" | "issue",
  number: number,
  commit: string,
  name: string,
  ext: string,
): string {
  return `${repo}/${kind}-${number}/${commit}/${name}${ext}`;
}

/**
 * Build the public URL for an object key.
 * Strips any trailing slash from baseUrl before joining.
 */
export function buildPublicUrl(baseUrl: string, objectKey: string): string {
  return `${baseUrl.replace(/\/+$/, "")}/${objectKey}`;
}

/**
 * Detect the MIME type from a base64-encoded image by inspecting magic bytes.
 * Returns null when the format cannot be identified.
 */
export function detectMimeType(bytes: Uint8Array): SupportedMimeType | null {
  if (bytes.length < 4) return null;

  // PNG: 89 50 4E 47
  if (
    bytes[0] === 0x89 &&
    bytes[1] === 0x50 &&
    bytes[2] === 0x4e &&
    bytes[3] === 0x47
  ) {
    return "image/png";
  }

  // JPEG: FF D8
  if (bytes[0] === 0xff && bytes[1] === 0xd8) {
    return "image/jpeg";
  }

  // WebP: 52 49 46 46 ... 57 45 42 50
  if (
    bytes[0] === 0x52 &&
    bytes[1] === 0x49 &&
    bytes[2] === 0x46 &&
    bytes[3] === 0x46 &&
    bytes.length >= 12 &&
    bytes[8] === 0x57 &&
    bytes[9] === 0x45 &&
    bytes[10] === 0x42 &&
    bytes[11] === 0x50
  ) {
    return "image/webp";
  }

  // GIF: 47 49 46 38
  if (
    bytes[0] === 0x47 &&
    bytes[1] === 0x49 &&
    bytes[2] === 0x46 &&
    bytes[3] === 0x38
  ) {
    return "image/gif";
  }

  return null;
}

/**
 * Decode and validate a base64-encoded image.
 * Returns the decoded bytes and detected MIME type,
 * or throws a descriptive Error on failure.
 */
export function decodeAndValidateImage(
  base64Data: string,
  contentTypeHint?: string,
): { bytes: Uint8Array; mimeType: SupportedMimeType; ext: string } {
  // Strip optional data URL prefix (data:image/png;base64,...)
  const raw = base64Data.replace(/^data:[^;]+;base64,/, "");

  let bytes: Uint8Array;
  try {
    const binary = atob(raw);
    bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) {
      bytes[i] = binary.charCodeAt(i);
    }
  } catch {
    throw new Error("Invalid base64 data: could not decode");
  }

  if (bytes.length === 0) {
    throw new Error("Image data is empty");
  }

  if (bytes.length > MAX_IMAGE_BYTES) {
    throw new Error(
      `Image too large: ${bytes.length} bytes exceeds the 10 MiB limit`,
    );
  }

  // Detect MIME type from magic bytes; fall back to hint if provided.
  let mimeType: SupportedMimeType | null = detectMimeType(bytes);
  if (!mimeType) {
    if (
      contentTypeHint &&
      Object.keys(SUPPORTED_TYPES).includes(contentTypeHint)
    ) {
      mimeType = contentTypeHint as SupportedMimeType;
    } else {
      throw new Error(
        "Cannot detect image format from data; only PNG, JPEG, WebP, and GIF are supported",
      );
    }
  }

  const ext = SUPPORTED_TYPES[mimeType];
  return { bytes, mimeType, ext };
}

/**
 * Perform a constant-time comparison of two strings.
 * Returns true when both strings are equal.
 *
 * Uses HMAC-SHA256 signatures so the comparison length is always 32 bytes,
 * which prevents timing leaks on inputs of differing lengths.
 * This works in both Cloudflare Workers and Node.js (Web Crypto API).
 */
export async function timingSafeEqual(a: string, b: string): Promise<boolean> {
  const enc = new TextEncoder();
  const aBytes = enc.encode(a);
  const bBytes = enc.encode(b);

  // Use a fixed key material — the goal is constant-time comparison, not
  // cryptographic secrecy of the key material itself.
  const keyMaterial = enc.encode("ui-shot-mcp-compare");
  const baseKey = await crypto.subtle.importKey(
    "raw",
    keyMaterial,
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"],
  );

  const [sigA, sigB] = await Promise.all([
    crypto.subtle.sign("HMAC", baseKey, aBytes),
    crypto.subtle.sign("HMAC", baseKey, bBytes),
  ]);

  // Both signatures are 32 bytes, so we can do a simple byte-by-byte XOR
  // which runs in constant time regardless of where they first differ.
  const viewA = new Uint8Array(sigA);
  const viewB = new Uint8Array(sigB);
  let diff = 0;
  for (let i = 0; i < viewA.length; i++) {
    diff |= (viewA[i] ?? 0) ^ (viewB[i] ?? 0);
  }
  return diff === 0;
}

/**
 * Check the Authorization header against the expected bearer token.
 * Returns true when the token matches.
 */
export async function checkBearerToken(
  authHeader: string | null,
  expectedToken: string,
): Promise<boolean> {
  if (!authHeader) return false;
  const prefix = "Bearer ";
  if (!authHeader.startsWith(prefix)) return false;
  const provided = authHeader.slice(prefix.length);
  return timingSafeEqual(provided, expectedToken);
}
