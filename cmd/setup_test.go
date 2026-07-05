package cmd

import (
	"strings"
	"testing"

	"github.com/myuon/ui-shot/internal/config"
	"github.com/myuon/ui-shot/internal/provider"
)

func TestGCSBaseURLDefault(t *testing.T) {
	// Regression for issue #7: with an existing config for bucketA, re-running
	// setup with --bucket bucketB (no --base-url) must derive the base URL from
	// bucketB rather than keeping the stale bucketA base URL.
	t.Run("changed bucket without explicit base url re-derives from bucket", func(t *testing.T) {
		res := config.Resolved{
			Bucket:          "bucketB",
			BaseURL:         "https://storage.googleapis.com/bucketA",
			ConfigBucket:    "bucketA",
			ConfigBaseURL:   "https://storage.googleapis.com/bucketA",
			BaseURLExplicit: false,
		}
		got := gcsBaseURLDefault(res, "bucketB")
		want := "https://storage.googleapis.com/bucketB"
		if got != want {
			t.Errorf("gcsBaseURLDefault() = %q, want %q", got, want)
		}
	})

	// Regression for the PR #8 review finding: a custom config base_url that is
	// not derivable from the bucket (e.g. a CDN host) must be preserved when the
	// bucket is unchanged, instead of being overwritten with the GCS default.
	t.Run("unchanged bucket preserves custom config base url", func(t *testing.T) {
		res := config.Resolved{
			Bucket:          "bucketA",
			BaseURL:         "https://cdn.example.com",
			ConfigBucket:    "bucketA",
			ConfigBaseURL:   "https://cdn.example.com",
			BaseURLExplicit: false,
		}
		got := gcsBaseURLDefault(res, "bucketA")
		want := "https://cdn.example.com"
		if got != want {
			t.Errorf("gcsBaseURLDefault() = %q, want %q (custom base URL must be preserved)", got, want)
		}
	})

	t.Run("explicit base url is honored", func(t *testing.T) {
		res := config.Resolved{
			Bucket:          "bucketB",
			BaseURL:         "https://cdn.example.com",
			ConfigBucket:    "bucketA",
			ConfigBaseURL:   "https://storage.googleapis.com/bucketA",
			BaseURLExplicit: true,
		}
		got := gcsBaseURLDefault(res, "bucketB")
		want := "https://cdn.example.com"
		if got != want {
			t.Errorf("gcsBaseURLDefault() = %q, want %q", got, want)
		}
	})

	t.Run("empty config base url derives from bucket", func(t *testing.T) {
		res := config.Resolved{Bucket: "fresh", BaseURL: "", BaseURLExplicit: false}
		got := gcsBaseURLDefault(res, "fresh")
		want := "https://storage.googleapis.com/fresh"
		if got != want {
			t.Errorf("gcsBaseURLDefault() = %q, want %q", got, want)
		}
	})
}

func TestR2BaseURLDefault(t *testing.T) {
	// Unlike GCS, an R2 public URL cannot be derived from the bucket name, so
	// a changed bucket must fall back to empty (forcing user input) instead of
	// keeping a stale base URL pointing at the previous bucket (issue #7).
	t.Run("changed bucket without explicit base url is empty", func(t *testing.T) {
		res := config.Resolved{
			Bucket:          "bucketB",
			BaseURL:         "https://pub-a.r2.dev",
			ConfigBucket:    "bucketA",
			ConfigBaseURL:   "https://pub-a.r2.dev",
			BaseURLExplicit: false,
		}
		if got := r2BaseURLDefault(res, "bucketB"); got != "" {
			t.Errorf("r2BaseURLDefault() = %q, want empty (stale base URL must not be reused)", got)
		}
	})

	t.Run("unchanged bucket preserves config base url", func(t *testing.T) {
		res := config.Resolved{
			Bucket:          "bucketA",
			BaseURL:         "https://pub-a.r2.dev",
			ConfigBucket:    "bucketA",
			ConfigBaseURL:   "https://pub-a.r2.dev",
			BaseURLExplicit: false,
		}
		if got := r2BaseURLDefault(res, "bucketA"); got != "https://pub-a.r2.dev" {
			t.Errorf("r2BaseURLDefault() = %q, want config base URL", got)
		}
	})

	t.Run("explicit base url is honored", func(t *testing.T) {
		res := config.Resolved{
			Bucket:          "bucketB",
			BaseURL:         "https://shots.example.com",
			ConfigBucket:    "bucketA",
			ConfigBaseURL:   "https://pub-a.r2.dev",
			BaseURLExplicit: true,
		}
		if got := r2BaseURLDefault(res, "bucketB"); got != "https://shots.example.com" {
			t.Errorf("r2BaseURLDefault() = %q, want explicit base URL", got)
		}
	})

	t.Run("no saved config yields empty default", func(t *testing.T) {
		res := config.Resolved{Bucket: "fresh"}
		if got := r2BaseURLDefault(res, "fresh"); got != "" {
			t.Errorf("r2BaseURLDefault() = %q, want empty", got)
		}
	})
}

// TestSetupBaseURLDefaultRealPath exercises the real code path
// config.Resolve() -> gcsBaseURLDefault() that setupGCS uses, so the
// regression is covered end-to-end rather than against a hand-built Resolved.
func TestSetupBaseURLDefaultRealPath(t *testing.T) {
	newCfg := func() *config.Config {
		cfg := config.Default()
		cfg.Provider = "gcs"
		cfg.GCS.Bucket = "bucketA"
		cfg.GCS.BaseURL = "https://storage.googleapis.com/bucketA"
		return cfg
	}

	clearEnv := func(t *testing.T) {
		t.Helper()
		t.Setenv("UISHOT_PROVIDER", "")
		t.Setenv("UISHOT_BUCKET", "")
		t.Setenv("UISHOT_BASE_URL", "")
		t.Setenv("UISHOT_GCS_PROJECT_ID", "")
	}

	// (a) issue #7: config bucketA + base_urlA, --bucket bucketB (no --base-url)
	// => base URL re-derived from bucketB.
	t.Run("changed bucket re-derives base url", func(t *testing.T) {
		clearEnv(t)
		res := config.Resolve(newCfg(), "", "bucketB", "", "", "", "")
		got := gcsBaseURLDefault(res, res.Bucket)
		want := "https://storage.googleapis.com/bucketB"
		if got != want {
			t.Errorf("base URL = %q, want %q", got, want)
		}
	})

	// (b) PR review regression: config has a custom (non-derivable) base_url and
	// the bucket is left unchanged => the custom base_url is preserved.
	t.Run("unchanged bucket preserves custom base url", func(t *testing.T) {
		clearEnv(t)
		cfg := newCfg()
		cfg.GCS.BaseURL = "https://cdn.example.com"
		res := config.Resolve(cfg, "", "", "", "", "", "")
		got := gcsBaseURLDefault(res, res.Bucket)
		want := "https://cdn.example.com"
		if got != want {
			t.Errorf("base URL = %q, want %q (custom base URL must survive a no-op setup)", got, want)
		}
	})

	// (c) explicit --base-url wins regardless of bucket change.
	t.Run("explicit base url flag wins", func(t *testing.T) {
		clearEnv(t)
		res := config.Resolve(newCfg(), "", "bucketB", "https://explicit.example.com", "", "", "")
		got := gcsBaseURLDefault(res, res.Bucket)
		want := "https://explicit.example.com"
		if got != want {
			t.Errorf("base URL = %q, want %q", got, want)
		}
	})
}

func TestPublicPolicy(t *testing.T) {
	tests := []struct {
		name  string
		flags setupFlags
		want  provider.PublicPolicy
	}{
		{"default", setupFlags{}, provider.PublicAuto},
		{"public", setupFlags{public: true}, provider.PublicForce},
		{"no-public", setupFlags{noPublic: true}, provider.PublicNever},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.flags.publicPolicy(); got != tt.want {
				t.Errorf("publicPolicy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReportPublicState(t *testing.T) {
	tests := []struct {
		name       string
		result     provider.SetupResult
		wantSubstr string
		notWant    string
	}{
		{
			name:       "created and made public",
			result:     provider.SetupResult{BucketCreated: true, MadePublic: true},
			wantSubstr: "created and configured for public read",
		},
		{
			name:       "existing made public",
			result:     provider.SetupResult{MadePublic: true},
			wantSubstr: "Granted public read",
		},
		{
			name:       "already public",
			result:     provider.SetupResult{AlreadyPublic: true},
			wantSubstr: "already publicly readable",
		},
		{
			name:       "skipped warns about 403",
			result:     provider.SetupResult{PublicSkipped: true},
			wantSubstr: "HTTP 403",
		},
		{
			name:    "no public state prints nothing",
			result:  provider.SetupResult{},
			notWant: "public",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sb strings.Builder
			reportPublicState(&sb, tt.result)
			got := sb.String()
			if tt.wantSubstr != "" && !strings.Contains(got, tt.wantSubstr) {
				t.Errorf("output %q does not contain %q", got, tt.wantSubstr)
			}
			if tt.notWant != "" && strings.Contains(strings.ToLower(got), tt.notWant) {
				t.Errorf("output %q unexpectedly contains %q", got, tt.notWant)
			}
		})
	}
}

func TestReportR2PublicState(t *testing.T) {
	const cfgPath = "/home/user/.config/uishot/config.toml"
	tests := []struct {
		name       string
		result     provider.SetupResult
		wantSubstr string
		notWant    string
	}{
		{
			name:       "created and made public",
			result:     provider.SetupResult{Bucket: "assets", BucketCreated: true, MadePublic: true},
			wantSubstr: "created and r2.dev",
		},
		{
			name:       "existing made public",
			result:     provider.SetupResult{Bucket: "assets", MadePublic: true, BaseURL: "https://pub-x.r2.dev"},
			wantSubstr: "r2.dev public domain enabled",
		},
		{
			name:       "already public",
			result:     provider.SetupResult{Bucket: "assets", AlreadyPublic: true, BaseURL: "https://pub-x.r2.dev"},
			wantSubstr: "already enabled",
		},
		{
			name:   "skipped with no base url warns and shows config path",
			result: provider.SetupResult{Bucket: "assets", PublicSkipped: true, BaseURL: ""},
			// Must mention the config file path (not a hardcoded default).
			wantSubstr: cfgPath,
		},
		{
			name: "skipped but base url was filled in: no warning",
			// PublicSkipped=true but BaseURL was set via manual prompt fallback.
			result:  provider.SetupResult{Bucket: "assets", PublicSkipped: true, BaseURL: "https://custom.example.com"},
			notWant: "was not configured automatically",
		},
		{
			name:       "explicit base url with no public state: note printed",
			result:     provider.SetupResult{Bucket: "assets", BaseURL: "https://shots.example.com"},
			wantSubstr: "only work if",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sb strings.Builder
			reportR2PublicState(&sb, tt.result, cfgPath)
			got := sb.String()
			if tt.wantSubstr != "" && !strings.Contains(got, tt.wantSubstr) {
				t.Errorf("output %q does not contain %q", got, tt.wantSubstr)
			}
			if tt.notWant != "" && strings.Contains(got, tt.notWant) {
				t.Errorf("output %q unexpectedly contains %q", got, tt.notWant)
			}
		})
	}
}
