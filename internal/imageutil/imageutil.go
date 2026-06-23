// Package imageutil provides extension and MIME helpers for supported images.
package imageutil

import (
	"fmt"
	"path/filepath"
	"strings"
)

// CacheControl is the Cache-Control header set on every uploaded object.
const CacheControl = "public, max-age=31536000, immutable"

// contentTypes maps supported lowercase extensions (with leading dot) to MIME.
var contentTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
}

// Ext returns the lowercase extension (with leading dot) of the given path.
func Ext(path string) string {
	return strings.ToLower(filepath.Ext(path))
}

// ContentType returns the MIME type for the file's extension. It returns an
// error for unsupported extensions.
func ContentType(path string) (string, error) {
	ext := Ext(path)
	ct, ok := contentTypes[ext]
	if !ok {
		return "", fmt.Errorf("unsupported file extension %q (allowed: .png .jpg .jpeg .webp)", ext)
	}
	return ct, nil
}

// IsSupported reports whether the file extension is supported.
func IsSupported(path string) bool {
	_, ok := contentTypes[Ext(path)]
	return ok
}
