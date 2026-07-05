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
	key := strings.Join(args[:3], " ")
	res, ok := f.results[key]
	if !ok {
		return "", nil
	}
	return res.out, res.err
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

	t.Run("missing base url hints at r2.dev", func(t *testing.T) {
		_, err := p.Setup(context.Background(), SetupOptions{Bucket: "assets"})
		if err == nil {
			t.Fatal("expected error for missing base URL")
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
			"r2 bucket info":   {err: errors.New("exit status 1")},
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
			"r2 bucket info":   {err: errors.New("exit status 1")},
			"r2 bucket create": {out: "Authentication error [code: 10000]", err: errors.New("exit status 1")},
		}}
		_, err := newTestR2Provider(f).Setup(context.Background(), opts)
		if err == nil || !strings.Contains(err.Error(), "Authentication error") {
			t.Errorf("err = %v, want error containing wrangler output", err)
		}
	})
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
		if err == nil || !strings.Contains(err.Error(), "open file") {
			t.Errorf("err = %v, want open-file error", err)
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
		{"The specified bucket does not exist. [code: 10006]", false},
		{"Authentication error [code: 10000]", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isBucketAlreadyExists(tt.out); got != tt.want {
			t.Errorf("isBucketAlreadyExists(%q) = %v, want %v", tt.out, got, tt.want)
		}
	}
}
