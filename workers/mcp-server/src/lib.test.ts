import { describe, it, expect } from "vitest";
import {
  buildObjectKey,
  buildPublicUrl,
  detectMimeType,
  decodeAndValidateImage,
  checkBearerToken,
  MAX_IMAGE_BYTES,
} from "./lib.js";

// ---------------------------------------------------------------------------
// buildObjectKey
// ---------------------------------------------------------------------------
describe("buildObjectKey", () => {
  it("builds a PR key", () => {
    const key = buildObjectKey(
      "myuon/ui-shot",
      "pr",
      42,
      "abc1234def",
      "booking-detail",
      ".png",
    );
    expect(key).toBe("myuon/ui-shot/pr-42/abc1234def/booking-detail.png");
  });

  it("builds an issue key", () => {
    const key = buildObjectKey(
      "myuon/ui-shot",
      "issue",
      7,
      "deadbeef",
      "screenshot",
      ".webp",
    );
    expect(key).toBe("myuon/ui-shot/issue-7/deadbeef/screenshot.webp");
  });

  it("matches the Go ObjectKey convention exactly", () => {
    // Reference: internal/provider/provider.go ObjectKey()
    // PR:    <repo>/pr-<number>/<commit>/<name>.<ext>
    const key = buildObjectKey(
      "owner/repo",
      "pr",
      123,
      "sha",
      "name",
      ".jpeg",
    );
    expect(key).toBe("owner/repo/pr-123/sha/name.jpeg");
  });
});

// ---------------------------------------------------------------------------
// buildPublicUrl
// ---------------------------------------------------------------------------
describe("buildPublicUrl", () => {
  it("joins base URL and key with a slash", () => {
    const url = buildPublicUrl(
      "https://pub-xxx.r2.dev",
      "myuon/ui-shot/pr-1/sha/detail.png",
    );
    expect(url).toBe(
      "https://pub-xxx.r2.dev/myuon/ui-shot/pr-1/sha/detail.png",
    );
  });

  it("strips trailing slashes from the base URL", () => {
    const url = buildPublicUrl(
      "https://pub-xxx.r2.dev///",
      "myuon/ui-shot/pr-1/sha/detail.png",
    );
    expect(url).toBe(
      "https://pub-xxx.r2.dev/myuon/ui-shot/pr-1/sha/detail.png",
    );
  });
});

// ---------------------------------------------------------------------------
// detectMimeType
// ---------------------------------------------------------------------------
describe("detectMimeType", () => {
  it("detects PNG from magic bytes", () => {
    const bytes = new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
    expect(detectMimeType(bytes)).toBe("image/png");
  });

  it("detects JPEG from magic bytes", () => {
    const bytes = new Uint8Array([0xff, 0xd8, 0xff, 0xe0]);
    expect(detectMimeType(bytes)).toBe("image/jpeg");
  });

  it("detects WebP from magic bytes", () => {
    const bytes = new Uint8Array([
      0x52, 0x49, 0x46, 0x46, // RIFF
      0x00, 0x00, 0x00, 0x00, // file size (placeholder)
      0x57, 0x45, 0x42, 0x50, // WEBP
    ]);
    expect(detectMimeType(bytes)).toBe("image/webp");
  });

  it("detects GIF from magic bytes", () => {
    const bytes = new Uint8Array([0x47, 0x49, 0x46, 0x38, 0x39, 0x61]);
    expect(detectMimeType(bytes)).toBe("image/gif");
  });

  it("returns null for unknown formats", () => {
    const bytes = new Uint8Array([0x00, 0x01, 0x02, 0x03]);
    expect(detectMimeType(bytes)).toBeNull();
  });

  it("returns null for too-short input", () => {
    const bytes = new Uint8Array([0x89, 0x50]);
    expect(detectMimeType(bytes)).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// decodeAndValidateImage
// ---------------------------------------------------------------------------
describe("decodeAndValidateImage", () => {
  // Minimal valid PNG (1x1 pixel, from standard PNG spec)
  const minimalPngBase64 =
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==";

  it("decodes a valid PNG and returns correct mime type and ext", () => {
    const result = decodeAndValidateImage(minimalPngBase64);
    expect(result.mimeType).toBe("image/png");
    expect(result.ext).toBe(".png");
    expect(result.bytes.length).toBeGreaterThan(0);
  });

  it("strips data URL prefix automatically", () => {
    const withPrefix = `data:image/png;base64,${minimalPngBase64}`;
    const result = decodeAndValidateImage(withPrefix);
    expect(result.mimeType).toBe("image/png");
  });

  it("throws on invalid base64", () => {
    expect(() => decodeAndValidateImage("!!! not base64 !!!")).toThrow(
      /Invalid base64|Cannot detect/,
    );
  });

  it("throws when image is too large", () => {
    // Create base64 of MAX_IMAGE_BYTES + 1 zero bytes
    const bigArray = new Uint8Array(MAX_IMAGE_BYTES + 1);
    // Use PNG magic bytes so detection succeeds
    bigArray[0] = 0x89;
    bigArray[1] = 0x50;
    bigArray[2] = 0x4e;
    bigArray[3] = 0x47;
    let binary = "";
    bigArray.forEach((b) => { binary += String.fromCharCode(b); });
    const bigBase64 = btoa(binary);
    expect(() => decodeAndValidateImage(bigBase64)).toThrow(/too large/);
  });

  it("uses content_type hint when magic bytes are unrecognized", () => {
    // 4 null bytes — unrecognizable, but hint says jpeg
    const unknownBytes = new Uint8Array([0x00, 0x00, 0x00, 0x00]);
    let binary = "";
    unknownBytes.forEach((b) => { binary += String.fromCharCode(b); });
    const b64 = btoa(binary);
    const result = decodeAndValidateImage(b64, "image/jpeg");
    expect(result.mimeType).toBe("image/jpeg");
    expect(result.ext).toBe(".jpeg");
  });

  it("throws when bytes are unrecognizable and no hint", () => {
    const unknownBytes = new Uint8Array([0x00, 0x00, 0x00, 0x00]);
    let binary = "";
    unknownBytes.forEach((b) => { binary += String.fromCharCode(b); });
    const b64 = btoa(binary);
    expect(() => decodeAndValidateImage(b64)).toThrow(/Cannot detect/);
  });
});

// ---------------------------------------------------------------------------
// checkBearerToken
// ---------------------------------------------------------------------------
describe("checkBearerToken", () => {
  it("returns true for a matching token", async () => {
    const result = await checkBearerToken("Bearer secret-token", "secret-token");
    expect(result).toBe(true);
  });

  it("returns false for a wrong token", async () => {
    const result = await checkBearerToken("Bearer wrong-token", "secret-token");
    expect(result).toBe(false);
  });

  it("returns false when header is null", async () => {
    const result = await checkBearerToken(null, "secret-token");
    expect(result).toBe(false);
  });

  it("returns false when scheme is not Bearer", async () => {
    const result = await checkBearerToken(
      "Basic c2VjcmV0LXRva2Vu",
      "secret-token",
    );
    expect(result).toBe(false);
  });

  it("returns false for an empty token", async () => {
    const result = await checkBearerToken("Bearer ", "secret-token");
    expect(result).toBe(false);
  });
});
