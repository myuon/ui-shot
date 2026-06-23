package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
)

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
			return SetupResult{}, errors.New("GCP project id is required to create the bucket (pass --project or set UI_SHOT_GCS_PROJECT_ID)")
		}
		if cerr := bucket.Create(ctx, opts.ProjectID, nil); cerr != nil {
			return SetupResult{}, fmt.Errorf("create bucket %q: %w", opts.Bucket, cerr)
		}
	default:
		return SetupResult{}, fmt.Errorf("check bucket %q: %w", opts.Bucket, err)
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
		return "", errors.New("bucket is not configured (run `ui-shot setup` or pass --bucket)")
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

// wrapGCSErr adds a hint for common GCS API errors.
func wrapGCSErr(err error) error {
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		if apiErr.Code == 404 {
			return fmt.Errorf("%w (bucket may not exist; run `ui-shot setup`)", err)
		}
	}
	return err
}
