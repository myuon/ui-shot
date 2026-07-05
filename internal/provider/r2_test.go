package provider

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeWrangler records wrangler invocations and replies per subcommand.
type fakeWrangler struct {
	calls [][]string
	// results maps the first three args joined by space (e.g. "r2 bucket info")
	// to a canned result.
	results map[string]fakeResult
	// accountIDs records the accountID passed to each call.
	accountIDs []string
}

type fakeResult struct {
	out string
	err error
}

func (f *fakeWrangler) run(_ context.Context, accountID string, args ...string) (string, error) {
	f.calls = append(f.calls, args)
	f.accountIDs = append(f.accountIDs, accountID)
	// Try progressively shorter prefixes so tests can key on "r2 bucket
	// dev-url enable" (4 args) as well as "r2 bucket info" (3 args).
	for n := min(len(args), 4); n >= 1; n-- {
		key := strings.Join(args[:n], " ")
		if res, ok := f.results[key]; ok {
			return res.out, res.err
		}
	}
	return "", nil
}

func newTestR2Provider(f *fakeWrangler) *r2Provider {
	return &r2Provider{
		lookPath: func(string) (string, error) { return "/usr/bin/wrangler", nil },
		run:      f.run,
	}
}

func writeTempFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(path, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestR2SetupValidation(t *testing.T) {
	p := newTestR2Provider(&fakeWrangler{})

	t.Run("missing bucket", func(t *testing.T) {
		_, err := p.Setup(context.Background(), SetupOptions{BaseURL: "https://pub-x.r2.dev"})
		if err == nil || !strings.Contains(err.Error(), "bucket name is required") {
			t.Errorf("err = %v, want bucket-required error", err)
		}
	})

	t.Run("missing base url with no policy sets PublicSkipped", func(t *testing.T) {
		// With no explicit base URL and the default PublicAuto policy (zero
		// value) and no ConfirmPublic callback, the provider cannot determine
		// the public URL and returns PublicSkipped so the cmd layer can fall
		// back to a manual prompt.
		res, err := p.Setup(context.Background(), SetupOptions{Bucket: "assets"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.PublicSkipped {
			t.Errorf("PublicSkipped = false, want true when no base URL and no public policy")
		}
		if res.BaseURL != "" {
			t.Errorf("BaseURL = %q, want empty for PublicSkipped result", res.BaseURL)
		}
	})

	t.Run("missing base url with --no-public errors with hint", func(t *testing.T) {
		_, err := p.Setup(context.Background(), SetupOptions{Bucket: "assets", PublicPolicy: PublicNever})
		if err == nil {
			t.Fatal("expected error for missing base URL with --no-public")
		}
		if !strings.Contains(err.Error(), "dev-url enable assets") {
			t.Errorf("err = %v, want hint mentioning `wrangler r2 bucket dev-url enable assets`", err)
		}
	})

	t.Run("wrangler not installed", func(t *testing.T) {
		p := &r2Provider{
			lookPath: func(string) (string, error) { return "", errors.New("not found") },
		}
		_, err := p.Setup(context.Background(), SetupOptions{Bucket: "assets", BaseURL: "https://pub-x.r2.dev"})
		if err == nil || !strings.Contains(err.Error(), "wrangler CLI not found") {
			t.Errorf("err = %v, want wrangler-not-found error", err)
		}
	})
}

func TestR2SetupBucketLifecycle(t *testing.T) {
	opts := SetupOptions{
		Provider:  "r2",
		AccountID: "acc123",
		Bucket:    "assets",
		BaseURL:   "https://pub-x.r2.dev",
	}

	t.Run("existing bucket is not re-created", func(t *testing.T) {
		f := &fakeWrangler{results: map[string]fakeResult{}} // info succeeds
		res, err := newTestR2Provider(f).Setup(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		if res.BucketCreated {
			t.Error("BucketCreated = true for an existing bucket, want false")
		}
		if res.Bucket != "assets" || res.BaseURL != "https://pub-x.r2.dev" {
			t.Errorf("result = %+v", res)
		}
		if len(f.calls) != 1 {
			t.Fatalf("calls = %v, want a single bucket info call", f.calls)
		}
		if f.accountIDs[0] != "acc123" {
			t.Errorf("accountID = %q, want acc123", f.accountIDs[0])
		}
	})

	t.Run("missing bucket is created", func(t *testing.T) {
		f := &fakeWrangler{results: map[string]fakeResult{
			"r2 bucket info": {out: "The specified bucket does not exist. [code: 10006]", err: errors.New("exit status 1")},
		}}
		res, err := newTestR2Provider(f).Setup(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		if !res.BucketCreated {
			t.Error("BucketCreated = false, want true")
		}
		if len(f.calls) != 2 || f.calls[1][2] != "create" {
			t.Errorf("calls = %v, want info then create", f.calls)
		}
	})

	t.Run("already-exists create error is handled gracefully", func(t *testing.T) {
		f := &fakeWrangler{results: map[string]fakeResult{
			"r2 bucket info":   {out: "The specified bucket does not exist. [code: 10006]", err: errors.New("exit status 1")},
			"r2 bucket create": {out: "The bucket you tried to create already exists. [code: 10004]", err: errors.New("exit status 1")},
		}}
		res, err := newTestR2Provider(f).Setup(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		if res.BucketCreated {
			t.Error("BucketCreated = true when bucket already existed, want false")
		}
	})

	t.Run("create failure surfaces wrangler output", func(t *testing.T) {
		f := &fakeWrangler{results: map[string]fakeResult{
			"r2 bucket info":   {out: "The specified bucket does not exist. [code: 10006]", err: errors.New("exit status 1")},
			"r2 bucket create": {out: "Authentication error [code: 10000]", err: errors.New("exit status 1")},
		}}
		_, err := newTestR2Provider(f).Setup(context.Background(), opts)
		if err == nil || !strings.Contains(err.Error(), "Authentication error") {
			t.Errorf("err = %v, want error containing wrangler output", err)
		}
		if err != nil && !strings.Contains(err.Error(), "create bucket") {
			t.Errorf("err = %v, want create-bucket framing", err)
		}
	})

	t.Run("info failure other than missing bucket is surfaced as check error", func(t *testing.T) {
		f := &fakeWrangler{results: map[string]fakeResult{
			"r2 bucket info": {out: "Authentication error [code: 10000]", err: errors.New("exit status 1")},
		}}
		_, err := newTestR2Provider(f).Setup(context.Background(), opts)
		if err == nil || !strings.Contains(err.Error(), "check bucket") || !strings.Contains(err.Error(), "Authentication error") {
			t.Errorf("err = %v, want check-bucket error containing wrangler output", err)
		}
		if len(f.calls) != 1 {
			t.Errorf("calls = %v, want no create attempt after a non-missing-bucket info failure", f.calls)
		}
	})
}

// fakeEnableOut is sample output from `wrangler r2 bucket dev-url enable
// assets --force` that contains the pub-<hash>.r2.dev URL.
const fakeEnableOut = "Enabling public access for bucket 'assets'...\n✨ Public access enabled at 'https://pub-abc123def456.r2.dev'.\n"

// fakeGetOut is sample output from `wrangler r2 bucket dev-url get assets`
// when the dev URL is already enabled.
const fakeGetOut = "Public access is enabled at 'https://pub-abc123def456.r2.dev'.\n"

// fakeGetDisabledOut is sample output from `wrangler r2 bucket dev-url get`
// when public access is not yet enabled.
const fakeGetDisabledOut = "Public access via the r2.dev URL is disabled.\n"

func TestR2SetupDevURL(t *testing.T) {
	baseOpts := SetupOptions{
		Provider:  "r2",
		AccountID: "acc123",
		Bucket:    "assets",
		// BaseURL intentionally empty so the r2.dev flow is exercised.
	}

	t.Run("PublicForce enables dev URL and parses URL", func(t *testing.T) {
		f := &fakeWrangler{results: map[string]fakeResult{
			"r2 bucket dev-url enable": {out: fakeEnableOut},
		}}
		opts := baseOpts
		opts.PublicPolicy = PublicForce
		res, err := newTestR2Provider(f).Setup(context.Background(), opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := "https://pub-abc123def456.r2.dev"; res.BaseURL != want {
			t.Errorf("BaseURL = %q, want %q", res.BaseURL, want)
		}
		if !res.MadePublic {
			t.Error("MadePublic = false, want true")
		}
		// Verify --force was passed so wrangler does not prompt.
		got := strings.Join(f.calls[len(f.calls)-1], " ")
		if !strings.Contains(got, "--force") {
			t.Errorf("wrangler args %q missing --force", got)
		}
	})

	t.Run("PublicAuto and ConfirmPublic=true enables dev URL for existing bucket", func(t *testing.T) {
		f := &fakeWrangler{results: map[string]fakeResult{
			"r2 bucket dev-url enable": {out: fakeEnableOut},
		}}
		opts := baseOpts
		opts.PublicPolicy = PublicAuto
		opts.ConfirmPublic = func() bool { return true }
		res, err := newTestR2Provider(f).Setup(context.Background(), opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := "https://pub-abc123def456.r2.dev"; res.BaseURL != want {
			t.Errorf("BaseURL = %q, want %q", res.BaseURL, want)
		}
		if !res.MadePublic {
			t.Error("MadePublic = false, want true")
		}
	})

	t.Run("PublicAuto ConfirmPublic=true fallback to get when already enabled", func(t *testing.T) {
		// enable fails (already enabled scenario) → get succeeds.
		f := &fakeWrangler{results: map[string]fakeResult{
			"r2 bucket dev-url enable": {out: "some error", err: errors.New("exit status 1")},
			"r2 bucket dev-url get":    {out: fakeGetOut},
		}}
		opts := baseOpts
		opts.PublicPolicy = PublicAuto
		opts.ConfirmPublic = func() bool { return true }
		res, err := newTestR2Provider(f).Setup(context.Background(), opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := "https://pub-abc123def456.r2.dev"; res.BaseURL != want {
			t.Errorf("BaseURL = %q, want %q", res.BaseURL, want)
		}
		if !res.AlreadyPublic {
			t.Error("AlreadyPublic = false, want true (got URL from dev-url get)")
		}
	})

	t.Run("opt-out: ConfirmPublic=false sets PublicSkipped with empty BaseURL", func(t *testing.T) {
		f := &fakeWrangler{results: map[string]fakeResult{}}
		opts := baseOpts
		opts.PublicPolicy = PublicAuto
		opts.ConfirmPublic = func() bool { return false }
		res, err := newTestR2Provider(f).Setup(context.Background(), opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.PublicSkipped {
			t.Error("PublicSkipped = false, want true when user declines r2.dev")
		}
		if res.BaseURL != "" {
			t.Errorf("BaseURL = %q, want empty after opt-out", res.BaseURL)
		}
	})

	t.Run("explicit base URL bypasses r2.dev flow", func(t *testing.T) {
		// No dev-url calls should be made when BaseURL is already set.
		f := &fakeWrangler{results: map[string]fakeResult{}}
		opts := baseOpts
		opts.BaseURL = "https://shots.example.com"
		res, err := newTestR2Provider(f).Setup(context.Background(), opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.BaseURL != "https://shots.example.com" {
			t.Errorf("BaseURL = %q, want explicit URL", res.BaseURL)
		}
		// Only the bucket-info call should have been made.
		for _, call := range f.calls {
			if strings.Contains(strings.Join(call, " "), "dev-url") {
				t.Errorf("unexpected dev-url call: %v", call)
			}
		}
	})

	t.Run("newly created bucket enables dev URL automatically (PublicAuto)", func(t *testing.T) {
		f := &fakeWrangler{results: map[string]fakeResult{
			"r2 bucket info":           {out: "The specified bucket does not exist. [code: 10006]", err: errors.New("exit status 1")},
			"r2 bucket dev-url enable": {out: fakeEnableOut},
		}}
		opts := baseOpts
		opts.PublicPolicy = PublicAuto
		// No ConfirmPublic — a freshly created bucket is made public automatically.
		res, err := newTestR2Provider(f).Setup(context.Background(), opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.BucketCreated {
			t.Error("BucketCreated = false, want true")
		}
		if !res.MadePublic {
			t.Error("MadePublic = false, want true for newly created bucket")
		}
		if want := "https://pub-abc123def456.r2.dev"; res.BaseURL != want {
			t.Errorf("BaseURL = %q, want %q", res.BaseURL, want)
		}
	})

	t.Run("dev URL enable wrangler failure surfaces error", func(t *testing.T) {
		f := &fakeWrangler{results: map[string]fakeResult{
			"r2 bucket dev-url enable": {out: "Authentication error [code: 10000]", err: errors.New("exit status 1")},
		}}
		opts := baseOpts
		opts.PublicPolicy = PublicForce
		_, err := newTestR2Provider(f).Setup(context.Background(), opts)
		if err == nil || !strings.Contains(err.Error(), "Authentication error") {
			t.Errorf("err = %v, want error containing wrangler output", err)
		}
		if err != nil && !strings.Contains(err.Error(), "enable r2.dev URL") {
			t.Errorf("err = %v, want enable-r2.dev-URL framing", err)
		}
	})

	t.Run("dev URL enable URL parse failure surfaces error", func(t *testing.T) {
		f := &fakeWrangler{results: map[string]fakeResult{
			"r2 bucket dev-url enable": {out: "Enabling public access for bucket 'assets'...\nDone.\n"},
		}}
		opts := baseOpts
		opts.PublicPolicy = PublicForce
		_, err := newTestR2Provider(f).Setup(context.Background(), opts)
		if err == nil || !strings.Contains(err.Error(), "could not parse pub-") {
			t.Errorf("err = %v, want parse-failure error", err)
		}
	})
}

func TestR2DevURLPattern(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// enable output
		{fakeEnableOut, "https://pub-abc123def456.r2.dev"},
		// get output
		{fakeGetOut, "https://pub-abc123def456.r2.dev"},
		// no URL
		{fakeGetDisabledOut, ""},
		{"", ""},
		// ensures the pattern does not match non-pub- URLs
		{"Public access at 'https://example.r2.dev'.", ""},
	}
	for _, tt := range tests {
		got := r2DevURLPattern.FindString(tt.input)
		if got != tt.want {
			t.Errorf("r2DevURLPattern.FindString(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestR2Upload(t *testing.T) {
	file := writeTempFile(t)
	opts := UploadOptions{
		Bucket:       "assets",
		BaseURL:      "https://pub-x.r2.dev",
		ObjectKey:    "owner/repo/pr-1/sha/shot.png",
		FilePath:     file,
		ContentType:  "image/png",
		CacheControl: "public, max-age=31536000, immutable",
		AccountID:    "acc123",
	}

	t.Run("builds wrangler command and URL", func(t *testing.T) {
		f := &fakeWrangler{results: map[string]fakeResult{}}
		url, err := newTestR2Provider(f).Upload(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		if want := "https://pub-x.r2.dev/owner/repo/pr-1/sha/shot.png"; url != want {
			t.Errorf("url = %q, want %q", url, want)
		}
		if len(f.calls) != 1 {
			t.Fatalf("calls = %v, want one object put call", f.calls)
		}
		got := strings.Join(f.calls[0], " ")
		for _, want := range []string{
			"r2 object put assets/owner/repo/pr-1/sha/shot.png",
			"--file " + file,
			"--remote",
			"--content-type image/png",
			"--cache-control public, max-age=31536000, immutable",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("wrangler args %q missing %q", got, want)
			}
		}
		if f.accountIDs[0] != "acc123" {
			t.Errorf("accountID = %q, want acc123", f.accountIDs[0])
		}
	})

	t.Run("missing bucket", func(t *testing.T) {
		o := opts
		o.Bucket = ""
		_, err := newTestR2Provider(&fakeWrangler{}).Upload(context.Background(), o)
		if err == nil || !strings.Contains(err.Error(), "bucket is not configured") {
			t.Errorf("err = %v, want bucket-not-configured error", err)
		}
	})

	t.Run("missing base url hints at r2.dev", func(t *testing.T) {
		o := opts
		o.BaseURL = ""
		_, err := newTestR2Provider(&fakeWrangler{}).Upload(context.Background(), o)
		if err == nil || !strings.Contains(err.Error(), "dev-url enable assets") {
			t.Errorf("err = %v, want base-url hint", err)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		o := opts
		o.FilePath = filepath.Join(t.TempDir(), "missing.png")
		_, err := newTestR2Provider(&fakeWrangler{}).Upload(context.Background(), o)
		if err == nil || !strings.Contains(err.Error(), "stat file") {
			t.Errorf("err = %v, want stat-file error", err)
		}
	})

	t.Run("wrangler failure surfaces output", func(t *testing.T) {
		f := &fakeWrangler{results: map[string]fakeResult{
			"r2 object put": {out: "Authentication error [code: 10000]", err: errors.New("exit status 1")},
		}}
		_, err := newTestR2Provider(f).Upload(context.Background(), opts)
		if err == nil || !strings.Contains(err.Error(), "Authentication error") {
			t.Errorf("err = %v, want error containing wrangler output", err)
		}
	})
}

func TestIsBucketAlreadyExists(t *testing.T) {
	tests := []struct {
		out  string
		want bool
	}{
		{"The bucket you tried to create already exists. [code: 10004]", true},
		{"bucket Already Exists", true},
		{"[code: 10004]", true},
		{"[Code: 10004]", true},
		{"The specified bucket does not exist. [code: 10006]", false},
		{"Authentication error [code: 10000]", false},
		// Bare digits elsewhere in the output (ray ids, account ids, ...)
		// must not be mistaken for the already-exists error code.
		{"Authentication error [code: 10000] (ray id: 7f2a10004bce)", false},
		{"account 9310004ab: create failed", false},
		{"10004", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isBucketAlreadyExists(tt.out); got != tt.want {
			t.Errorf("isBucketAlreadyExists(%q) = %v, want %v", tt.out, got, tt.want)
		}
	}
}

func TestIsBucketNotFound(t *testing.T) {
	tests := []struct {
		out  string
		want bool
	}{
		{"The specified bucket does not exist. [code: 10006]", true},
		{"[code: 10006]", true},
		{"bucket Does Not Exist", true},
		{"The bucket you tried to create already exists. [code: 10004]", false},
		{"Authentication error [code: 10000] (ray id: 7f2a10006bce)", false},
		{"10006", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isBucketNotFound(tt.out); got != tt.want {
			t.Errorf("isBucketNotFound(%q) = %v, want %v", tt.out, got, tt.want)
		}
	}
}
