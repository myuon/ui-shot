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
	attrs, err := bucket.Attrs(ctx)
	created := false
	switch {
	case err == nil:
		// Bucket already exists.
	case errors.Is(err, storage.ErrBucketNotExist):
		if opts.ProjectID == "" {
			return SetupResult{}, errors.New("GCP project id is required to create the bucket (pass --project or set UISHOT_GCS_PROJECT_ID)")
		}
		// Create the bucket with public access prevention left inherited so
		// that the public-read IAM binding below can take effect.
		newAttrs := &storage.BucketAttrs{
			PublicAccessPrevention: storage.PublicAccessPreventionInherited,
		}
		if cerr := bucket.Create(ctx, opts.ProjectID, newAttrs); cerr != nil {
			return SetupResult{}, fmt.Errorf("create bucket %q: %w", opts.Bucket, cerr)
		}
		created = true
		if a, aerr := bucket.Attrs(ctx); aerr == nil {
			attrs = a
		}
	default:
		return SetupResult{}, fmt.Errorf("check bucket %q: %w", opts.Bucket, err)
	}

	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = DefaultGCSBaseURL(opts.Bucket)
	}
	res := SetupResult{
		ProjectID:     opts.ProjectID,
		Bucket:        opts.Bucket,
		BaseURL:       baseURL,
		BucketCreated: created,
	}

	if err := p.configurePublicRead(ctx, bucket, attrs, created, opts, &res); err != nil {
		if created {
			return SetupResult{}, fmt.Errorf("bucket %q was created but configuring public read failed: %w", opts.Bucket, err)
		}
		return SetupResult{}, fmt.Errorf("configure public read on bucket %q: %w", opts.Bucket, err)
	}

	return res, nil
}

// configurePublicRead decides, based on the public policy and whether the
// bucket was freshly created, whether to grant public read. It is safe by
// default: an existing non-public bucket is never made public without an
// explicit confirmation (or --public). Results are reported via res.
func (p *gcsProvider) configurePublicRead(ctx context.Context, bucket *storage.BucketHandle, attrs *storage.BucketAttrs, created bool, opts SetupOptions, res *SetupResult) error {
	handle := bucket.IAM().V3()
	policy, err := handle.Policy(ctx)
	if err != nil {
		return err
	}
	if hasPublicReadBinding(policy) {
		res.AlreadyPublic = true
		return nil
	}

	// Explicit --no-public: never grant, leave private.
	if opts.PublicPolicy == PublicNever {
		res.PublicSkipped = true
		return nil
	}

	// Refuse early with a clear message if public access prevention is
	// enforced, since granting allUsers would fail with an opaque API error.
	if attrs != nil && attrs.PublicAccessPrevention == storage.PublicAccessPreventionEnforced {
		return errors.New("bucket has public access prevention enforced; set it to inherited to allow public read")
	}

	// Decide whether we're allowed to make this bucket public.
	switch {
	case created:
		// Freshly created asset bucket: public read is the intended default.
	case opts.PublicPolicy == PublicForce:
		// Explicit --public on an existing bucket.
	case opts.ConfirmPublic != nil && opts.ConfirmPublic():
		// User confirmed interactively.
	default:
		// Existing, non-public bucket with no explicit go-ahead: leave private.
		res.PublicSkipped = true
		return nil
	}

	if !addPublicReadBinding(policy) {
		res.AlreadyPublic = true
		return nil
	}
	if err := handle.SetPolicy(ctx, policy); err != nil {
		return err
	}
	res.MadePublic = true
	return nil
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

// hasPublicReadBinding reports whether the policy already grants allUsers the
// object-viewer role via an *unconditional* binding (Condition == nil). A
// conditional grant does not make objects unconditionally world-readable, so it
// is not treated as "already public".
func hasPublicReadBinding(policy *iam.Policy3) bool {
	for _, b := range policy.Bindings {
		if b.Role != publicReadRole || b.Condition != nil {
			continue
		}
		for _, m := range b.Members {
			if m == allUsers {
				return true
			}
		}
	}
	return false
}

// addPublicReadBinding adds an allUsers/roles/storage.objectViewer binding to
// the policy in place. It only considers/extends *unconditional* bindings
// (Condition == nil): conditional roles/storage.objectViewer bindings are left
// untouched, and a new unconditional binding is added if none exists. It
// reports whether the policy was modified (false when allUsers is already
// granted unconditionally).
func addPublicReadBinding(policy *iam.Policy3) bool {
	for _, b := range policy.Bindings {
		if b.Role != publicReadRole || b.Condition != nil {
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
