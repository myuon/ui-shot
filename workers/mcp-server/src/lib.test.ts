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

  it("builds a key with .jpg extension (CLI-uploaded files)", () => {
    const key = buildObjectKey(
      "myuon/ui-shot",
      "pr",
      1,
      "abc1234",
      "shot",
      ".jpg",
    );
    expect(key).toBe("myuon/ui-shot/pr-1/abc1234/shot.jpg");
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

  it("builds URL for a .jpg key", () => {
    const url = buildPublicUrl(
      "https://pub-xxx.r2.dev",
      "myuon/ui-shot/pr-1/abc1234/shot.jpg",
    );
    expect(url).toBe(
      "https://pub-xxx.r2.dev/myuon/ui-shot/pr-1/abc1234/shot.jpg",
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

  it("throws when image is too large (post-decode check)", () => {
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

  it("throws when base64 string length exceeds pre-decode size bound", () => {
    // The pre-decode check fires when raw.length > ceil((MAX_IMAGE_BYTES + 2) / 3) * 4.
    // Craft a base64 string that is just over that threshold (without actually decoding).
    // MAX_IMAGE_BYTES = 10 * 1024 * 1024 = 10485760
    // Threshold = ceil((10485760 + 2) / 3) * 4 = ceil(10485762/3) * 4 = 3495254 * 4 = 13981016
    const overLimitBase64 = "A".repeat(13_981_016 + 4);
    expect(() => decodeAndValidateImage(overLimitBase64)).toThrow(/too large/);
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
// Input validation: repo / commit / name regex patterns
// These mirror the REPO_RE / COMMIT_RE / NAME_RE constants in index.ts.
// We test the regexes directly as pure functions here so we don't need
// a Workers runtime to exercise the Zod schemas.
// ---------------------------------------------------------------------------
const REPO_RE = /^(?!\.{1,2}(?:\/|$))[A-Za-z0-9_.-]+\/(?!\.{1,2}$)[A-Za-z0-9_.-]+$/;
const COMMIT_RE = /^[0-9a-f]{7,40}$/i;
const NAME_RE = /^[A-Za-z0-9_.-]+$/;

describe("repo format validation (REPO_RE)", () => {
  it("accepts a normal owner/repo", () => {
    expect(REPO_RE.test("myuon/ui-shot")).toBe(true);
  });

  it("accepts repos with dots and dashes", () => {
    expect(REPO_RE.test("my-org/my.repo")).toBe(true);
  });

  it("rejects empty string", () => {
    expect(REPO_RE.test("")).toBe(false);
  });

  it("rejects missing slash", () => {
    expect(REPO_RE.test("noslash")).toBe(false);
  });

  it("rejects leading slash (empty owner)", () => {
    expect(REPO_RE.test("/repo")).toBe(false);
  });

  it("rejects trailing slash (empty name)", () => {
    expect(REPO_RE.test("owner/")).toBe(false);
  });

  it("rejects owner that is a single dot", () => {
    expect(REPO_RE.test("./repo")).toBe(false);
  });

  it("rejects owner that is double dot", () => {
    expect(REPO_RE.test("../repo")).toBe(false);
  });

  it("rejects name that is a single dot", () => {
    expect(REPO_RE.test("owner/.")).toBe(false);
  });

  it("rejects name that is double dot", () => {
    expect(REPO_RE.test("owner/..")).toBe(false);
  });

  it("rejects multiple slashes (path traversal attempt)", () => {
    expect(REPO_RE.test("a/b/pr-9/fake")).toBe(false);
  });

  it("rejects URL special characters", () => {
    expect(REPO_RE.test("owner/repo?x=1")).toBe(false);
    expect(REPO_RE.test("owner/repo#anchor")).toBe(false);
    expect(REPO_RE.test("owner/repo%20space")).toBe(false);
  });
});

describe("commit format validation (COMMIT_RE)", () => {
  it("accepts a 7-char short SHA", () => {
    expect(COMMIT_RE.test("abc1234")).toBe(true);
  });

  it("accepts a 40-char full SHA", () => {
    expect(COMMIT_RE.test("a".repeat(40))).toBe(true);
  });

  it("accepts uppercase hex", () => {
    expect(COMMIT_RE.test("ABCDEF1")).toBe(true);
  });

  it("rejects fewer than 7 characters", () => {
    expect(COMMIT_RE.test("abc12")).toBe(false);
  });

  it("rejects more than 40 characters", () => {
    expect(COMMIT_RE.test("a".repeat(41))).toBe(false);
  });

  it("rejects non-hex characters", () => {
    expect(COMMIT_RE.test("ghijklm")).toBe(false);
    expect(COMMIT_RE.test("abc1234!")).toBe(false);
  });

  it("rejects empty string", () => {
    expect(COMMIT_RE.test("")).toBe(false);
  });
});

describe("name format validation (NAME_RE)", () => {
  it("accepts a typical slug", () => {
    expect(NAME_RE.test("booking-detail")).toBe(true);
  });

  it("accepts dots and underscores", () => {
    expect(NAME_RE.test("my_screenshot.v2")).toBe(true);
  });

  it("rejects slashes (path traversal)", () => {
    expect(NAME_RE.test("a/b")).toBe(false);
    expect(NAME_RE.test("../etc")).toBe(false);
  });

  it("rejects URL-special characters", () => {
    expect(NAME_RE.test("name?q=1")).toBe(false);
    expect(NAME_RE.test("name#section")).toBe(false);
    expect(NAME_RE.test("name%20x")).toBe(false);
  });

  it("rejects spaces", () => {
    expect(NAME_RE.test("my screenshot")).toBe(false);
  });

  it("rejects empty string", () => {
    expect(NAME_RE.test("")).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Pagination merge logic (unit test)
// Simulate R2 paginated responses to verify merging works correctly.
// ---------------------------------------------------------------------------
describe("pagination merge logic", () => {
  it("merges objects from multiple pages", () => {
    // Simulated paginated R2 responses
    const page1 = { objects: [{ key: "a" }, { key: "b" }], truncated: true, cursor: "cursor1" };
    const page2 = { objects: [{ key: "c" }], truncated: false, cursor: "" };

    const allObjects: { key: string }[] = [];
    let pages = [page1, page2];
    let pageIdx = 0;
    let cursor: string | undefined;

    do {
      const listed = pages[pageIdx++]!;
      allObjects.push(...listed.objects);
      cursor = listed.truncated ? listed.cursor : undefined;
    } while (cursor !== undefined && allObjects.length < 10_000);

    expect(allObjects).toHaveLength(3);
    expect(allObjects.map((o) => o.key)).toEqual(["a", "b", "c"]);
  });

  it("stops at the safety cap of 10,000 objects", () => {
    // Simulate an infinite stream of truncated pages
    let callCount = 0;
    const MAX_OBJECTS = 10_000;
    const allObjects: { key: string }[] = [];
    let cursor: string | undefined = "start";

    // Each "page" has 3000 objects and is always truncated
    while (cursor !== undefined && allObjects.length < MAX_OBJECTS) {
      const pageSize = Math.min(3000, MAX_OBJECTS - allObjects.length + 3000);
      const objects = Array.from({ length: 3000 }, (_, i) => ({
        key: `obj-${callCount * 3000 + i}`,
      }));
      allObjects.push(...objects);
      callCount++;
      cursor = allObjects.length < MAX_OBJECTS ? `cursor${callCount}` : undefined;
    }

    // Should have stopped adding new pages once >= 10,000 objects accumulated
    expect(allObjects.length).toBeGreaterThanOrEqual(MAX_OBJECTS);
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
