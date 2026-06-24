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
| `r2`     | Designed only — returns "not implemented yet" |

### GCS prerequisites

- `gcloud auth application-default login`, or set `GOOGLE_APPLICATION_CREDENTIALS`

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

### Object key

```
PR:    <repo>/pr-<number>/<commit>/<name>.<ext>
Issue: <repo>/issue-<number>/<commit>/<name>.<ext>
```

The URL is `base_url + "/" + object_key`. Uploaded objects get
`Cache-Control: public, max-age=31536000, immutable` and a `Content-Type`
derived from the extension.

## Configuration precedence

```
command-line flags > environment variables > global config
```

Environment variables: `UISHOT_PROVIDER`, `UISHOT_BUCKET`, `UISHOT_BASE_URL`,
`UISHOT_GCS_PROJECT_ID`, plus the standard AWS/R2 variables for future
providers.

## Development

```bash
go build ./...
go vet ./...
go test ./...
go build -o uishot ./cmd/uishot
```
