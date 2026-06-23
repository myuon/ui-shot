# ui-shot

`ui-shot` is a Go CLI that uploads UI screenshots to an image store and prints a
URL (or Markdown) you can paste straight into a GitHub PR or Issue.

See [#1](https://github.com/myuon/ui-shot/issues/1) for the full spec.

## Install / Build

```bash
go build -o ui-shot .
```

Requires Go 1.23+.

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

Stores the global config at `~/.config/ui-shot/config.toml`
(`%APPDATA%\ui-shot\config.toml` on Windows).

```bash
ui-shot setup --provider gcs
# or fully non-interactive:
ui-shot setup --provider gcs \
  --project my-gcp-project \
  --bucket ui-shot-assets \
  --non-interactive
```

`setup` verifies ADC, decides the project/bucket/base-url, creates the bucket if
it does not exist, and saves the config.

### Upload

```bash
ui-shot upload \
  --pr 123 \
  --name booking-detail \
  --file /tmp/booking-detail.png
# => https://storage.googleapis.com/ui-shot-assets/owner/repo/pr-123/<sha>/booking-detail.png

ui-shot upload --issue 45 --name detail --file shot.png --markdown
# => ![detail](https://storage.googleapis.com/...)
```

- `--pr` and `--issue` are mutually exclusive; exactly one is required.
- `--repo` defaults to `owner/repo` inferred from the git `origin` remote.
- `--commit` defaults to `git rev-parse HEAD`.
- Supported extensions: `.png .jpg .jpeg .webp`.

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

Environment variables: `UI_SHOT_PROVIDER`, `UI_SHOT_BUCKET`, `UI_SHOT_BASE_URL`,
`UI_SHOT_GCS_PROJECT_ID`, plus the standard AWS/R2 variables for future
providers.

## Development

```bash
go build ./...
go vet ./...
go test ./...
```
