package cmd

import "testing"

func TestResolveTarget(t *testing.T) {
	tests := []struct {
		name     string
		pr       int
		issue    int
		wantKind string
		wantNum  int
		wantErr  bool
	}{
		{"pr only", 123, 0, "pr", 123, false},
		{"issue only", 0, 7, "issue", 7, false},
		{"both", 1, 2, "", 0, true},
		{"neither", 0, 0, "", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, num, err := resolveTarget(tt.pr, tt.issue)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if kind != tt.wantKind || num != tt.wantNum {
				t.Errorf("got (%q,%d), want (%q,%d)", kind, num, tt.wantKind, tt.wantNum)
			}
		})
	}
}

func TestFormatOutput(t *testing.T) {
	url := "https://storage.googleapis.com/ui-shot-assets/o/repo/pr-1/abc/x.png"
	if got := formatOutput(url, "x", false); got != url {
		t.Errorf("plain output = %q, want %q", got, url)
	}
	want := "![x](" + url + ")"
	if got := formatOutput(url, "x", true); got != want {
		t.Errorf("markdown output = %q, want %q", got, want)
	}
}
