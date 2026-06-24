// Package provider abstracts image storage backends.
package provider

import (
	"context"
	"fmt"
	"strings"
)

// PublicPolicy controls how setup treats public-read access on the bucket.
type PublicPolicy int

const (
	// PublicAuto is the default: a newly created bucket is made public, but an
	// existing non-public bucket is only made public if ConfirmPublic returns
	// true (and is left private if ConfirmPublic is nil).
	PublicAuto PublicPolicy = iota
	// PublicForce makes the bucket public without confirmation (--public).
	PublicForce
	// PublicNever never grants public read (--no-public).
	PublicNever
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

	// PublicPolicy controls public-read configuration (GCS). Defaults to
	// PublicAuto (the zero value).
	PublicPolicy PublicPolicy
	// ConfirmPublic, if set, is called before making an *existing* bucket
	// public under PublicAuto. It must return true to proceed. It is never
	// called for a freshly created bucket nor under PublicForce/PublicNever.
	ConfirmPublic func() bool
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

	// BucketCreated is true when setup created the bucket during this run.
	BucketCreated bool
	// MadePublic is true when setup granted public read during this run.
	MadePublic bool
	// AlreadyPublic is true when the bucket was already publicly readable.
	AlreadyPublic bool
	// PublicSkipped is true when an existing non-public bucket was left
	// private (no confirmation / --no-public), so URLs may return 403.
	PublicSkipped bool
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
