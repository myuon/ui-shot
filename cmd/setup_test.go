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
	t.Run("new bucket without explicit base url re-derives from bucket", func(t *testing.T) {
		res := config.Resolved{
			Bucket:          "bucketB",
			BaseURL:         "https://storage.googleapis.com/bucketA",
			BaseURLExplicit: false,
		}
		got := gcsBaseURLDefault(res, "bucketB")
		want := "https://storage.googleapis.com/bucketB"
		if got != want {
			t.Errorf("gcsBaseURLDefault() = %q, want %q", got, want)
		}
	})

	t.Run("explicit base url is honored", func(t *testing.T) {
		res := config.Resolved{
			Bucket:          "bucketB",
			BaseURL:         "https://cdn.example.com",
			BaseURLExplicit: true,
		}
		got := gcsBaseURLDefault(res, "bucketB")
		want := "https://cdn.example.com"
		if got != want {
			t.Errorf("gcsBaseURLDefault() = %q, want %q", got, want)
		}
	})

	t.Run("empty base url derives from bucket", func(t *testing.T) {
		res := config.Resolved{Bucket: "fresh", BaseURL: "", BaseURLExplicit: false}
		got := gcsBaseURLDefault(res, "fresh")
		want := "https://storage.googleapis.com/fresh"
		if got != want {
			t.Errorf("gcsBaseURLDefault() = %q, want %q", got, want)
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
