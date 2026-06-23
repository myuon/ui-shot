// Package provider abstracts image storage backends.
package provider

import (
	"context"
	"fmt"
	"strings"
)

// SetupOptions carries the resolved configuration for a setup run.
type SetupOptions struct {
	Provider       string
	ProjectID      string // GCS
	Bucket         string
	BaseURL        string
	Profile        string // S3
	AccountID      string // R2
	NonInteractive bool
}

// UploadOptions carries everything needed to upload one object.
type UploadOptions struct {
	Bucket      string
	BaseURL     string
	ObjectKey   string
	FilePath    string
	ContentType string
	// CacheControl is the Cache-Control header value to set on the object.
	CacheControl string
}

// Provider abstracts a storage backend.
type Provider interface {
	// Setup verifies credentials, ensures the bucket exists, and returns
	// the values the caller should persist (project/bucket/base-url).
	Setup(ctx context.Context, opts SetupOptions) (SetupResult, error)
	// Upload uploads the file and returns the public URL.
	Upload(ctx context.Context, opts UploadOptions) (string, error)
}

// SetupResult is the resolved configuration after a setup run.
type SetupResult struct {
	ProjectID string
	Bucket    string
	BaseURL   string
}

// ObjectKey builds the provider-agnostic object key.
//
//	PR:    <repo>/pr-<number>/<commit>/<name>.<ext>
//	Issue: <repo>/issue-<number>/<commit>/<name>.<ext>
//
// ext must include the leading dot.
func ObjectKey(repo, kind string, number int, commit, name, ext string) string {
	return strings.Join([]string{
		repo,
		fmt.Sprintf("%s-%d", kind, number),
		commit,
		name + ext,
	}, "/")
}

// BuildURL joins the base URL and object key.
func BuildURL(baseURL, objectKey string) string {
	return strings.TrimRight(baseURL, "/") + "/" + objectKey
}

// New returns the provider implementation for the given name. Unimplemented
// providers return a provider whose methods report "not implemented yet".
func New(name string) (Provider, error) {
	switch name {
	case "gcs":
		return &gcsProvider{}, nil
	case "s3":
		return &notImplementedProvider{name: "s3"}, nil
	case "r2":
		return &notImplementedProvider{name: "r2"}, nil
	case "":
		return nil, fmt.Errorf("no provider configured (run `uishot setup` or pass --provider)")
	default:
		return nil, fmt.Errorf("unknown provider %q (supported: gcs, s3, r2)", name)
	}
}

// notImplementedProvider is used for designed-but-unimplemented backends.
type notImplementedProvider struct{ name string }

func (p *notImplementedProvider) Setup(context.Context, SetupOptions) (SetupResult, error) {
	return SetupResult{}, fmt.Errorf("provider %q is not implemented yet", p.name)
}

func (p *notImplementedProvider) Upload(context.Context, UploadOptions) (string, error) {
	return "", fmt.Errorf("provider %q is not implemented yet", p.name)
}
