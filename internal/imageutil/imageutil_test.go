package imageutil

import "testing"

func TestContentType(t *testing.T) {
	tests := []struct {
		path    string
		want    string
		wantErr bool
	}{
		{"a.png", "image/png", false},
		{"a.PNG", "image/png", false},
		{"a.jpg", "image/jpeg", false},
		{"a.jpeg", "image/jpeg", false},
		{"a.webp", "image/webp", false},
		{"a.gif", "", true},
		{"noext", "", true},
		{"a.svg", "", true},
	}
	for _, tt := range tests {
		got, err := ContentType(tt.path)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ContentType(%q): expected error", tt.path)
			}
			continue
		}
		if err != nil {
			t.Errorf("ContentType(%q): unexpected error %v", tt.path, err)
		}
		if got != tt.want {
			t.Errorf("ContentType(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestIsSupported(t *testing.T) {
	if !IsSupported("x.JPG") {
		t.Error("expected x.JPG supported")
	}
	if IsSupported("x.bmp") {
		t.Error("expected x.bmp unsupported")
	}
}
