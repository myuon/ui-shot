package cmd

import (
	"strings"
	"testing"

	"github.com/myuon/ui-shot/internal/provider"
)

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
