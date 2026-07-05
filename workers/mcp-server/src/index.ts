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

/** Shared Zod schema for repo/PR-or-issue identification. */
const repoSchema = z.object({
  repo: z
    .string()
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
          .min(1)
          .describe("Full commit SHA (e.g. from git rev-parse HEAD)"),
        name: z
          .string()
          .min(1)
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
        "Returns a JSON array of objects with key and url fields.",
      inputSchema: {
        ...repoSchema.shape,
      },
    },
    async ({ repo, pr, issue }) => {
      const { kind, number } = resolveKind(pr, issue);

      const prefix = `${repo}/${kind}-${number}/`;
      const listed = await env.SCREENSHOTS.list({ prefix });

      const results = listed.objects.map((obj) => ({
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
          .min(1)
          .describe("Full commit SHA"),
        name: z
          .string()
          .min(1)
          .describe('Image name without extension (e.g. "booking-detail")'),
        ext: z
          .enum([".png", ".jpeg", ".webp", ".gif"])
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
