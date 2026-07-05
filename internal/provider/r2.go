package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// wranglerBin is the Cloudflare CLI binary the R2 provider delegates to.
const wranglerBin = "wrangler"

// r2Provider uploads to Cloudflare R2 by delegating to the official wrangler
// CLI, so credential management stays with wrangler (`wrangler login` OAuth
// token or the CLOUDFLARE_API_TOKEN environment variable). uishot itself never
// stores Cloudflare credentials.
type r2Provider struct {
	// lookPath and run are indirections over exec so tests can stub the
	// wrangler invocations.
	lookPath func(file string) (string, error)
	// run executes wrangler with the given args and returns its combined
	// output. accountID, when non-empty, is exported as CLOUDFLARE_ACCOUNT_ID
	// so wrangler operates on the right account without prompting.
	run func(ctx context.Context, accountID string, args ...string) (output string, err error)
}

// newR2Provider returns an r2Provider that shells out to the real wrangler.
func newR2Provider() *r2Provider {
	return &r2Provider{
		lookPath: exec.LookPath,
		run:      runWrangler,
	}
}

// runWrangler executes wrangler with CLOUDFLARE_ACCOUNT_ID set (when provided)
// and returns the combined stdout/stderr output.
func runWrangler(ctx context.Context, accountID string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, wranglerBin, args...)
	cmd.Env = os.Environ()
	if accountID != "" {
		cmd.Env = append(cmd.Env, "CLOUDFLARE_ACCOUNT_ID="+accountID)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// checkWrangler verifies the wrangler CLI is installed.
func (p *r2Provider) checkWrangler() error {
	if _, err := p.lookPath(wranglerBin); err != nil {
		return errors.New("wrangler CLI not found (install it with `npm install -g wrangler`, then run `wrangler login`)")
	}
	return nil
}

// r2BaseURLHint explains how to obtain a public URL for an R2 bucket, since it
// cannot be derived from the bucket name (unlike GCS).
func r2BaseURLHint(bucket string) string {
	if bucket == "" {
		bucket = "<bucket>"
	}
	return fmt.Sprintf("R2 has no derivable public URL: enable the r2.dev managed domain with `wrangler r2 bucket dev-url enable %s` (development use) or attach a custom domain in the Cloudflare dashboard, then pass the URL via --base-url", bucket)
}

func (p *r2Provider) Setup(ctx context.Context, opts SetupOptions) (SetupResult, error) {
	if opts.Bucket == "" {
		return SetupResult{}, errors.New("bucket name is required for R2 setup")
	}
	if opts.BaseURL == "" {
		return SetupResult{}, fmt.Errorf("base URL is required for R2 setup; %s", r2BaseURLHint(opts.Bucket))
	}
	if err := p.checkWrangler(); err != nil {
		return SetupResult{}, err
	}

	created := false
	if out, err := p.run(ctx, opts.AccountID, "r2", "bucket", "info", opts.Bucket); err != nil {
		// Only fall through to create when the bucket is genuinely missing;
		// surfacing other failures (auth, network) as a check error keeps the
		// root cause visible and mirrors the GCS check/create error split.
		if !isBucketNotFound(out) {
			return SetupResult{}, fmt.Errorf("check bucket %q with wrangler: %w\n%s", opts.Bucket, err, strings.TrimSpace(out))
		}
		cout, cerr := p.run(ctx, opts.AccountID, "r2", "bucket", "create", opts.Bucket)
		switch {
		case cerr == nil:
			created = true
		case isBucketAlreadyExists(cout):
			// Someone created it in the meantime: treat the existing bucket
			// as success.
		default:
			return SetupResult{}, fmt.Errorf("create bucket %q with wrangler: %w\n%s", opts.Bucket, cerr, strings.TrimSpace(cout))
		}
	}

	return SetupResult{
		Bucket:        opts.Bucket,
		BaseURL:       opts.BaseURL,
		BucketCreated: created,
	}, nil
}

func (p *r2Provider) Upload(ctx context.Context, opts UploadOptions) (string, error) {
	if opts.Bucket == "" {
		return "", errors.New("bucket is not configured (run `uishot setup` or pass --bucket)")
	}
	if opts.BaseURL == "" {
		return "", fmt.Errorf("base URL is not configured (run `uishot setup` or pass --base-url); %s", r2BaseURLHint(opts.Bucket))
	}
	if err := p.checkWrangler(); err != nil {
		return "", err
	}
	if _, err := os.Stat(opts.FilePath); err != nil {
		return "", fmt.Errorf("stat file %s: %w", opts.FilePath, err)
	}

	args := []string{
		"r2", "object", "put", opts.Bucket + "/" + opts.ObjectKey,
		"--file", opts.FilePath,
		"--remote",
	}
	if opts.ContentType != "" {
		args = append(args, "--content-type", opts.ContentType)
	}
	if opts.CacheControl != "" {
		args = append(args, "--cache-control", opts.CacheControl)
	}
	if out, err := p.run(ctx, opts.AccountID, args...); err != nil {
		return "", fmt.Errorf("upload to r2://%s/%s with wrangler: %w\n%s", opts.Bucket, opts.ObjectKey, err, strings.TrimSpace(out))
	}

	return BuildURL(opts.BaseURL, opts.ObjectKey), nil
}

// isBucketAlreadyExists reports whether wrangler's output indicates the bucket
// already exists (Cloudflare API error code 10004). The code match is anchored
// on wrangler's "[code: 10004]" rendering so that unrelated digits (ray ids,
// account ids) never false-positive.
func isBucketAlreadyExists(out string) bool {
	lower := strings.ToLower(out)
	return strings.Contains(lower, "already exists") || strings.Contains(lower, "code: 10004")
}

// isBucketNotFound reports whether wrangler's output indicates the bucket does
// not exist (Cloudflare API error code 10006).
func isBucketNotFound(out string) bool {
	lower := strings.ToLower(out)
	return strings.Contains(lower, "does not exist") || strings.Contains(lower, "code: 10006")
}
