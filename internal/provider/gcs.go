package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"cloud.google.com/go/iam"
	iampb "cloud.google.com/go/iam/apiv1/iampb"
	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
)

// publicReadRole is the IAM role that grants read-only access to objects.
const publicReadRole = "roles/storage.objectViewer"

// allUsers is the IAM member that represents anyone on the internet.
const allUsers = "allUsers"

// GCSBaseURLHost is the public host prefix for GCS objects. The default base
// URL is GCSBaseURLHost + "/" + bucket.
const GCSBaseURLHost = "https://storage.googleapis.com"

// DefaultGCSBaseURL returns the default public base URL for the given bucket.
func DefaultGCSBaseURL(bucket string) string {
	return GCSBaseURLHost + "/" + bucket
}

// gcsProvider uploads to Google Cloud Storage using ADC.
type gcsProvider struct{}

// newGCSClient creates a storage client. Authentication uses Application
// Default Credentials.
func newGCSClient(ctx context.Context) (*storage.Client, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not create GCS client (check Application Default Credentials, run `gcloud auth application-default login`): %w", err)
	}
	return client, nil
}

func (p *gcsProvider) Setup(ctx context.Context, opts SetupOptions) (SetupResult, error) {
	if opts.Bucket == "" {
		return SetupResult{}, errors.New("bucket name is required for GCS setup")
	}

	client, err := newGCSClient(ctx)
	if err != nil {
		return SetupResult{}, err
	}
	defer client.Close()

	bucket := client.Bucket(opts.Bucket)
	_, err = bucket.Attrs(ctx)
	switch {
	case err == nil:
		// Bucket already exists.
	case errors.Is(err, storage.ErrBucketNotExist):
		if opts.ProjectID == "" {
			return SetupResult{}, errors.New("GCP project id is required to create the bucket (pass --project or set UISHOT_GCS_PROJECT_ID)")
		}
		// Create the bucket with public access prevention left inherited so
		// that the public-read IAM binding below can take effect.
		attrs := &storage.BucketAttrs{
			PublicAccessPrevention: storage.PublicAccessPreventionInherited,
		}
		if cerr := bucket.Create(ctx, opts.ProjectID, attrs); cerr != nil {
			return SetupResult{}, fmt.Errorf("create bucket %q: %w", opts.Bucket, cerr)
		}
	default:
		return SetupResult{}, fmt.Errorf("check bucket %q: %w", opts.Bucket, err)
	}

	// Ensure the bucket is publicly readable so the returned URLs resolve
	// (otherwise objects return HTTP 403). Granting is idempotent.
	if err := ensurePublicRead(ctx, bucket.IAM().V3()); err != nil {
		return SetupResult{}, fmt.Errorf("grant public read on bucket %q: %w", opts.Bucket, err)
	}

	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = DefaultGCSBaseURL(opts.Bucket)
	}

	return SetupResult{
		ProjectID: opts.ProjectID,
		Bucket:    opts.Bucket,
		BaseURL:   baseURL,
	}, nil
}

func (p *gcsProvider) Upload(ctx context.Context, opts UploadOptions) (string, error) {
	if opts.Bucket == "" {
		return "", errors.New("bucket is not configured (run `uishot setup` or pass --bucket)")
	}

	f, err := os.Open(opts.FilePath)
	if err != nil {
		return "", fmt.Errorf("open file %s: %w", opts.FilePath, err)
	}
	defer f.Close()

	client, err := newGCSClient(ctx)
	if err != nil {
		return "", err
	}
	defer client.Close()

	obj := client.Bucket(opts.Bucket).Object(opts.ObjectKey)
	w := obj.NewWriter(ctx)
	w.ContentType = opts.ContentType
	w.CacheControl = opts.CacheControl

	if _, err := io.Copy(w, f); err != nil {
		_ = w.Close()
		return "", fmt.Errorf("upload to gs://%s/%s: %w", opts.Bucket, opts.ObjectKey, wrapGCSErr(err))
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("finalize upload to gs://%s/%s: %w", opts.Bucket, opts.ObjectKey, wrapGCSErr(err))
	}

	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = DefaultGCSBaseURL(opts.Bucket)
	}
	return BuildURL(baseURL, opts.ObjectKey), nil
}

// iamV3Handle is the subset of *iam.Handle3 used to manage the bucket policy.
// It is an interface so the public-read logic can be unit tested without GCS.
type iamV3Handle interface {
	Policy(ctx context.Context) (*iam.Policy3, error)
	SetPolicy(ctx context.Context, policy *iam.Policy3) error
}

// ensurePublicRead grants allUsers the object-viewer role on the bucket policy
// if it is not already granted. It is a no-op when the binding already exists.
func ensurePublicRead(ctx context.Context, h iamV3Handle) error {
	policy, err := h.Policy(ctx)
	if err != nil {
		return err
	}
	if !addPublicReadBinding(policy) {
		// Already public; nothing to change.
		return nil
	}
	return h.SetPolicy(ctx, policy)
}

// addPublicReadBinding adds an allUsers/roles/storage.objectViewer binding to
// the policy in place. It reports whether the policy was modified (false when
// the binding already exists).
func addPublicReadBinding(policy *iam.Policy3) bool {
	for _, b := range policy.Bindings {
		if b.Role != publicReadRole {
			continue
		}
		for _, m := range b.Members {
			if m == allUsers {
				return false
			}
		}
		b.Members = append(b.Members, allUsers)
		return true
	}
	policy.Bindings = append(policy.Bindings, &iampb.Binding{
		Role:    publicReadRole,
		Members: []string{allUsers},
	})
	return true
}

// wrapGCSErr adds a hint for common GCS API errors.
func wrapGCSErr(err error) error {
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		if apiErr.Code == 404 {
			return fmt.Errorf("%w (bucket may not exist; run `uishot setup`)", err)
		}
	}
	return err
}
