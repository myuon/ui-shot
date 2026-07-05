# uishot

`uishot` is a Go CLI that uploads UI screenshots to an image store and prints a
URL (or Markdown) you can paste straight into a GitHub PR or Issue.

See [#1](https://github.com/myuon/ui-shot/issues/1) for the full spec.

## Install / Build

```bash
go install github.com/myuon/ui-shot/cmd/uishot@latest
```

This installs a binary named `uishot`.

To build locally:

```bash
go build -o uishot ./cmd/uishot
```

Requires Go 1.25+.

## Providers

| Provider | Status |
|----------|--------|
| `gcs`    | Implemented (Application Default Credentials) |
| `s3`     | Designed only — returns "not implemented yet" |
| `r2`     | Implemented (delegates to the `wrangler` CLI) |

### GCS prerequisites

- `gcloud auth application-default login`, or set `GOOGLE_APPLICATION_CREDENTIALS`

### R2 prerequisites

The R2 provider shells out to Cloudflare's official
[wrangler](https://developers.cloudflare.com/workers/wrangler/) CLI, so
credential management is delegated to wrangler and uishot never stores
Cloudflare credentials itself.

- Install wrangler: `npm install -g wrangler`
- Authenticate: `wrangler login` (OAuth), or set `CLOUDFLARE_API_TOKEN` to an
  API token with R2 edit permissions (for CI)
- Find your account id in the Cloudflare dashboard (**R2** page, or any zone
  overview) and pass it via `--account-id` / `UISHOT_R2_ACCOUNT_ID`; uishot
  exports it as `CLOUDFLARE_ACCOUNT_ID` when invoking wrangler. It can be
  omitted if your token only has access to a single account.
- A **public base URL for the bucket** is required and cannot be derived from
  the bucket name. Either:
  - enable the managed r2.dev domain (development use, rate-limited):
    `wrangler r2 bucket dev-url enable <bucket>` prints the
    `https://pub-<hash>.r2.dev` URL, or
  - attach a custom domain to the bucket in the Cloudflare dashboard
    (recommended for production; enables CDN caching).

  Pass that URL to setup via `--base-url` (or the interactive prompt).

## Usage

### Setup

Stores the global config at `~/.config/uishot/config.toml`
(`%APPDATA%\uishot\config.toml` on Windows).

```bash
uishot setup --provider gcs
# or fully non-interactive:
uishot setup --provider gcs \
  --project my-gcp-project \
  --bucket ui-shot-assets \
  --non-interactive
```

`setup` verifies ADC, decides the project/bucket/base-url, creates the bucket if
it does not exist, and saves the config.

For Cloudflare R2 (see [R2 prerequisites](#r2-prerequisites)):

```bash
uishot setup --provider r2 \
  --account-id <cloudflare-account-id> \
  --bucket ui-shot-assets \
  --base-url https://pub-xxxxxxxx.r2.dev \
  --non-interactive
```

R2 `setup` checks the wrangler CLI, creates the bucket if it does not exist
(`wrangler r2 bucket create`), and saves the config. The base URL must be the
bucket's public URL (r2.dev managed domain or custom domain); enabling it
automatically at setup time is tracked in
[#13](https://github.com/myuon/ui-shot/issues/13).

> [!IMPORTANT]
> Uploaded image URLs (`https://storage.googleapis.com/...`) are only
> accessible if the bucket grants `allUsers` the `roles/storage.objectViewer`
> role; otherwise they return HTTP 403. `setup` configures this **safely**:
>
> - **A bucket it creates** is made public read automatically (it leaves public
>   access prevention inherited and grants the `allUsers` binding). This is the
>   intended design for a dedicated asset bucket.
> - **An existing bucket** is **never** made public without your say-so. If it
>   is already public, nothing changes. If it is not, interactive `setup` asks
>   `Make it public? [y/N]` (default No), and `--non-interactive` leaves it
>   private and warns that URLs may return 403.
>
> Flags to control this explicitly:
>
> - `--public` — make the bucket public without asking.
> - `--no-public` — never grant public read (URLs may return 403).
>
> Do not point `--public` at a bucket holding private data: it becomes
> world-readable.

### Upload

```bash
uishot upload \
  --pr 123 \
  --name booking-detail \
  --file /tmp/booking-detail.png
# => https://storage.googleapis.com/ui-shot-assets/owner/repo/pr-123/<sha>/booking-detail.png

uishot upload --issue 45 --name detail --file shot.png --markdown
# => ![detail](https://storage.googleapis.com/...)
```

- `--pr` and `--issue` are mutually exclusive; exactly one is required.
- `--repo` defaults to `owner/repo` inferred from the git `origin` remote.
- `--commit` defaults to `git rev-parse HEAD`.
- Supported extensions: `.png .jpg .jpeg .webp`.

### Checking uploaded images

Open the URL printed by `upload` in a browser, or paste it into the PR/Issue.
To list what is already stored, query the bucket directly, e.g. for GCS:

```bash
gcloud storage ls gs://<bucket>/<owner>/<repo>/...
```

For R2, browse the bucket in the Cloudflare dashboard (**R2** → the bucket).

### Object key

```
PR:    <repo>/pr-<number>/<commit>/<name>.<ext>
Issue: <repo>/issue-<number>/<commit>/<name>.<ext>
```

The URL is `base_url + "/" + object_key`. Uploaded objects get
`Cache-Control: public, max-age=31536000, immutable` and a `Content-Type`
derived from the extension.

## MCP server (Cloudflare Workers + R2)

An MCP (Model Context Protocol) server that serves the same screenshots over
Streamable HTTP lives in [`workers/mcp-server/`](workers/mcp-server/). It runs
as a stateless Cloudflare Worker backed by the same R2 bucket and object-key
convention as the CLI (the CLI itself is not required), and exposes three
tools: `upload_screenshot`, `list_screenshots`, and `get_screenshot_url`.
Authentication is a static Bearer token.

See [workers/mcp-server/README.md](workers/mcp-server/README.md) for setup,
deploy steps, and MCP client configuration.

## Configuration precedence

```
command-line flags > environment variables > global config
```

Environment variables: `UISHOT_PROVIDER`, `UISHOT_BUCKET`, `UISHOT_BASE_URL`,
`UISHOT_GCS_PROJECT_ID`, `UISHOT_R2_ACCOUNT_ID`, plus the standard AWS
variables for future providers. R2 authentication itself is handled by
wrangler (`wrangler login` or `CLOUDFLARE_API_TOKEN`).

## Development

```bash
go build ./...
go vet ./...
go test ./...
go build -o uishot ./cmd/uishot
```
