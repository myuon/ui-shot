package provider

import "testing"

func TestObjectKey(t *testing.T) {
	tests := []struct {
		name   string
		repo   string
		kind   string
		number int
		commit string
		key    string
		ext    string
		want   string
	}{
		{
			name:   "pr png",
			repo:   "my-org/my-app",
			kind:   "pr",
			number: 123,
			commit: "a1b2c3d4",
			key:    "booking-detail",
			ext:    ".png",
			want:   "my-org/my-app/pr-123/a1b2c3d4/booking-detail.png",
		},
		{
			name:   "issue jpeg",
			repo:   "owner/repo",
			kind:   "issue",
			number: 7,
			commit: "deadbeef",
			key:    "screen",
			ext:    ".jpeg",
			want:   "owner/repo/issue-7/deadbeef/screen.jpeg",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ObjectKey(tt.repo, tt.kind, tt.number, tt.commit, tt.key, tt.ext)
			if got != tt.want {
				t.Errorf("ObjectKey = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildURL(t *testing.T) {
	tests := []struct {
		base string
		key  string
		want string
	}{
		{
			base: "https://storage.googleapis.com/ui-shot-assets",
			key:  "my-org/my-app/pr-123/a1b2c3d4/booking-detail.png",
			want: "https://storage.googleapis.com/ui-shot-assets/my-org/my-app/pr-123/a1b2c3d4/booking-detail.png",
		},
		{
			base: "https://storage.googleapis.com/ui-shot-assets/",
			key:  "x.png",
			want: "https://storage.googleapis.com/ui-shot-assets/x.png",
		},
	}
	for _, tt := range tests {
		if got := BuildURL(tt.base, tt.key); got != tt.want {
			t.Errorf("BuildURL(%q,%q) = %q, want %q", tt.base, tt.key, got, tt.want)
		}
	}
}

func TestNewUnimplemented(t *testing.T) {
	for _, name := range []string{"s3", "r2"} {
		p, err := New(name)
		if err != nil {
			t.Fatalf("New(%q) unexpected error: %v", name, err)
		}
		if _, err := p.Upload(nil, UploadOptions{}); err == nil {
			t.Errorf("Upload for %q: expected not-implemented error", name)
		}
	}
}

func TestNewUnknownAndEmpty(t *testing.T) {
	if _, err := New("foo"); err == nil {
		t.Error("New(foo): expected error")
	}
	if _, err := New(""); err == nil {
		t.Error("New(\"\"): expected error")
	}
}
