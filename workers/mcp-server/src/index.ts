/**
 * ui-shot MCP server — Cloudflare Workers (stateless, Streamable HTTP transport)
 *
 * Tools:
 *   upload_screenshot   — base64 image → R2.put → public URL
 *   list_screenshots    — R2.list by repo/PR(or issue) prefix → keys + URLs
 *   get_screenshot_url  — derive public URL from key components (no R2 access)
 *
 * Auth: static Bearer token stored as Cloudflare secret MCP_AUTH_TOKEN.
 */

import { createMcpHandler } from "agents/mcp";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import {
  buildObjectKey,
  buildPublicUrl,
  checkBearerToken,
  decodeAndValidateImage,
} from "./lib.js";

/** Cloudflare Workers environment bindings. */
export interface Env {
  /** R2 bucket bound as SCREENSHOTS in wrangler.jsonc. */
  SCREENSHOTS: R2Bucket;
  /**
   * Public base URL for the R2 bucket (r2.dev domain or custom domain).
   * Set via wrangler.jsonc vars.PUBLIC_BASE_URL.
   */
  PUBLIC_BASE_URL: string;
  /**
   * Static Bearer token for authentication.
   * Set via: wrangler secret put MCP_AUTH_TOKEN
   */
  MCP_AUTH_TOKEN: string;
}

/**
 * Regex for a valid GitHub "owner/repo" slug.
 * Each segment: alphanumeric, dot, dash, underscore — but not a pure-dot segment (. or ..).
 */
const REPO_RE = /^(?!\.{1,2}(?:\/|$))[A-Za-z0-9_.-]+\/(?!\.{1,2}$)[A-Za-z0-9_.-]+$/;

/** Regex for a git commit SHA (7–40 hex digits, case-insensitive). */
const COMMIT_RE = /^[0-9a-f]{7,40}$/i;

/**
 * Regex for a safe image name slug.
 * Allows alphanumerics, dots, dashes, and underscores — no slashes, no .., no URL-special chars.
 */
const NAME_RE = /^[A-Za-z0-9_.-]+$/;

/** Shared Zod schema for repo/PR-or-issue identification. */
const repoSchema = z.object({
  repo: z
    .string()
    .regex(REPO_RE, 'Expected "owner/repo" with alphanumeric/dot/dash/underscore segments')
    .describe(
      'Repository in "owner/repo" format (e.g. "myuon/ui-shot")',
    ),
  pr: z
    .number()
    .int()
    .positive()
    .optional()
    .describe("PR number (mutually exclusive with issue)"),
  issue: z
    .number()
    .int()
    .positive()
    .optional()
    .describe("Issue number (mutually exclusive with pr)"),
});

/** Resolve pr/issue to kind + number, or throw. */
function resolveKind(pr: number | undefined, issue: number | undefined) {
  if (pr !== undefined && issue !== undefined) {
    throw new Error("Specify either pr or issue, not both");
  }
  if (pr !== undefined) return { kind: "pr" as const, number: pr };
  if (issue !== undefined) return { kind: "issue" as const, number: issue };
  throw new Error("Either pr or issue must be specified");
}

/** Create a fresh McpServer instance (must be per-request per agents SDK 1.26+). */
function createServer(env: Env): McpServer {
  const server = new McpServer({
    name: "ui-shot",
    version: "0.1.0",
  });

  // ------------------------------------------------------------------ //
  // Tool: upload_screenshot                                              //
  // ------------------------------------------------------------------ //
  server.registerTool(
    "upload_screenshot",
    {
      description:
        "Upload a base64-encoded screenshot to R2 and return its public URL. " +
        "Accepts PNG, JPEG, WebP, and GIF. Maximum size is 10 MiB decoded.",
      inputSchema: {
        ...repoSchema.shape,
        commit: z
          .string()
          .regex(COMMIT_RE, "Expected a git SHA (7–40 hex digits)")
          .describe("Full commit SHA (e.g. from git rev-parse HEAD)"),
        name: z
          .string()
          .regex(NAME_RE, "Alphanumeric, dot, dash, underscore only — no slashes or URL-special chars")
          .describe('Image name without extension (e.g. "booking-detail")'),
        data: z
          .string()
          .min(1)
          .describe(
            "Base64-encoded image data. Optional data URL prefix (data:image/...;base64,...) is stripped automatically.",
          ),
        content_type: z
          .string()
          .optional()
          .describe(
            'Optional MIME type hint (e.g. "image/png"). Used as fallback when format cannot be detected from magic bytes.',
          ),
      },
    },
    async ({ repo, pr, issue, commit, name, data, content_type }) => {
      const { kind, number } = resolveKind(pr, issue);

      const { bytes, mimeType, ext } = decodeAndValidateImage(
        data,
        content_type,
      );

      const key = buildObjectKey(repo, kind, number, commit, name, ext);

      await env.SCREENSHOTS.put(key, bytes, {
        httpMetadata: {
          contentType: mimeType,
          cacheControl: "public, max-age=31536000, immutable",
        },
      });

      const url = buildPublicUrl(env.PUBLIC_BASE_URL, key);
      return {
        content: [{ type: "text", text: url }],
      };
    },
  );

  // ------------------------------------------------------------------ //
  // Tool: list_screenshots                                               //
  // ------------------------------------------------------------------ //
  server.registerTool(
    "list_screenshots",
    {
      description:
        "List all screenshots stored for a given repository and PR or issue. " +
        "Returns a JSON array of objects with key and url fields. " +
        "Paginates R2 list results automatically; capped at 10,000 objects total.",
      inputSchema: {
        ...repoSchema.shape,
      },
    },
    async ({ repo, pr, issue }) => {
      const { kind, number } = resolveKind(pr, issue);

      const prefix = `${repo}/${kind}-${number}/`;

      // R2.list() returns at most 1000 objects per page; paginate until done.
      // Safety cap: stop after 10,000 objects to bound memory and latency.
      const MAX_OBJECTS = 10_000;
      const allObjects: R2Object[] = [];
      let cursor: string | undefined;

      do {
        const listed = await env.SCREENSHOTS.list({
          prefix,
          ...(cursor ? { cursor } : {}),
        });
        allObjects.push(...listed.objects);
        cursor = listed.truncated ? listed.cursor : undefined;
      } while (cursor !== undefined && allObjects.length < MAX_OBJECTS);

      const results = allObjects.map((obj) => ({
        key: obj.key,
        url: buildPublicUrl(env.PUBLIC_BASE_URL, obj.key),
      }));

      return {
        content: [
          {
            type: "text",
            text: JSON.stringify(results, null, 2),
          },
        ],
      };
    },
  );

  // ------------------------------------------------------------------ //
  // Tool: get_screenshot_url                                             //
  // ------------------------------------------------------------------ //
  server.registerTool(
    "get_screenshot_url",
    {
      description:
        "Derive the public URL for a screenshot from its key components without accessing R2. " +
        "Useful when you already know the repo, PR/issue, commit, name, and extension.",
      inputSchema: {
        ...repoSchema.shape,
        commit: z
          .string()
          .regex(COMMIT_RE, "Expected a git SHA (7–40 hex digits)")
          .describe("Full commit SHA"),
        name: z
          .string()
          .regex(NAME_RE, "Alphanumeric, dot, dash, underscore only — no slashes or URL-special chars")
          .describe('Image name without extension (e.g. "booking-detail")'),
        ext: z
          .enum([".png", ".jpg", ".jpeg", ".webp", ".gif"])
          .default(".png")
          .describe("File extension including the leading dot"),
      },
    },
    async ({ repo, pr, issue, commit, name, ext }) => {
      const { kind, number } = resolveKind(pr, issue);
      const key = buildObjectKey(repo, kind, number, commit, name, ext);
      const url = buildPublicUrl(env.PUBLIC_BASE_URL, key);
      return {
        content: [{ type: "text", text: url }],
      };
    },
  );

  return server;
}

export default {
  async fetch(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
    // ------------------------------------------------------------------
    // Authentication: Bearer token check (constant-time comparison)
    // ------------------------------------------------------------------
    if (!env.MCP_AUTH_TOKEN) {
      // Fail closed: if the secret is not set, refuse all requests.
      return new Response("Server misconfigured: MCP_AUTH_TOKEN is not set", {
        status: 500,
      });
    }

    const authorized = await checkBearerToken(
      request.headers.get("Authorization"),
      env.MCP_AUTH_TOKEN,
    );

    if (!authorized) {
      return new Response("Unauthorized", {
        status: 401,
        headers: {
          "WWW-Authenticate": 'Bearer realm="ui-shot-mcp"',
        },
      });
    }

    // ------------------------------------------------------------------
    // MCP handler — new McpServer instance per request (SDK requirement)
    // ------------------------------------------------------------------
    const server = createServer(env);
    return createMcpHandler(server)(request, env, ctx);
  },
} satisfies ExportedHandler<Env>;
