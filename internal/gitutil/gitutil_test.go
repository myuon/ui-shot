package gitutil

import "testing"

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"https://github.com/owner/repo.git", "owner/repo"},
		{"https://github.com/owner/repo", "owner/repo"},
		{"git@github.com:owner/repo.git", "owner/repo"},
		{"git@github.com:owner/repo", "owner/repo"},
		{"ssh://git@github.com/owner/repo.git", "owner/repo"},
		{"ssh://git@github.com:22/owner/repo.git", "owner/repo"},
		{"https://gitlab.example.com/group/sub/repo.git", "sub/repo"},
		{"git://github.com/owner/repo.git", "owner/repo"},
		{"", ""},
		{"not-a-url", ""},
	}
	for _, tt := range tests {
		if got := ParseRemoteURL(tt.in); got != tt.want {
			t.Errorf("ParseRemoteURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
