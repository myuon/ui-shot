package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/myuon/ui-shot/internal/config"
	"github.com/myuon/ui-shot/internal/provider"
)

type setupFlags struct {
	provider       string
	bucket         string
	baseURL        string
	project        string
	profile        string
	accountID      string
	nonInteractive bool
	public         bool
	noPublic       bool
}

func newSetupCmd() *cobra.Command {
	var f setupFlags
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure uishot and save the global config",
		Long: `Configure the storage provider (bucket, base URL, project) and save it to
the global config at ~/.config/uishot/config.toml.

GCS requires Application Default Credentials: run
"gcloud auth application-default login" or set GOOGLE_APPLICATION_CREDENTIALS.
Prompts for any value not passed as a flag (skip prompts with --non-interactive).`,
		Example: `  # Interactive
  uishot setup --provider gcs

  # Non-interactive
  uishot setup --provider gcs --project my-gcp-project \
    --bucket ui-shot-assets --non-interactive`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSetup(cmd.Context(), cmd, f)
		},
	}
	cmd.Flags().StringVar(&f.provider, "provider", "", "provider: gcs, s3, r2")
	cmd.Flags().StringVar(&f.bucket, "bucket", "", "bucket name")
	cmd.Flags().StringVar(&f.baseURL, "base-url", "", "public URL base")
	cmd.Flags().StringVar(&f.project, "project", "", "GCS GCP project id")
	cmd.Flags().StringVar(&f.profile, "profile", "", "S3 AWS profile")
	cmd.Flags().StringVar(&f.accountID, "account-id", "", "R2 Cloudflare account id")
	cmd.Flags().BoolVar(&f.nonInteractive, "non-interactive", false, "do not prompt for input")
	cmd.Flags().BoolVar(&f.public, "public", false, "make the bucket publicly readable without confirmation (GCS)")
	cmd.Flags().BoolVar(&f.noPublic, "no-public", false, "do not grant public read on the bucket (GCS)")
	cmd.MarkFlagsMutuallyExclusive("public", "no-public")
	return cmd
}

// publicPolicy maps the --public/--no-public flags to a provider.PublicPolicy.
func (f setupFlags) publicPolicy() provider.PublicPolicy {
	switch {
	case f.noPublic:
		return provider.PublicNever
	case f.public:
		return provider.PublicForce
	default:
		return provider.PublicAuto
	}
}

const defaultGCSBucket = "ui-shot-assets"

// gcsBaseURLDefault returns the base URL to present as the setup default for
// the final (post-prompt) bucket.
//
//   - An explicitly provided base URL (--base-url or UISHOT_BASE_URL) is
//     honored as-is (highest precedence, unchanged behavior).
//   - Otherwise, if the final bucket is unchanged from the saved config and the
//     config has a non-empty base URL, that value is preserved. This keeps a
//     custom base URL (e.g. a CDN host not derivable from the bucket) intact.
//   - Otherwise (the bucket changed, or there is no saved base URL) the base URL
//     is derived from the final bucket so it always tracks the bucket and never
//     points at a previously configured one (issue #7).
func gcsBaseURLDefault(res config.Resolved, bucket string) string {
	if res.BaseURLExplicit {
		return res.BaseURL
	}
	if bucket == res.ConfigBucket && res.ConfigBaseURL != "" {
		return res.ConfigBaseURL
	}
	return provider.DefaultGCSBaseURL(bucket)
}

func runSetup(ctx context.Context, cmd *cobra.Command, f setupFlags) error {
	path, err := config.Path()
	if err != nil {
		return err
	}
	cfg, _, err := config.Load(path)
	if err != nil {
		return err
	}

	res := config.Resolve(cfg, f.provider, f.bucket, f.baseURL, f.project, f.profile, f.accountID)
	if res.Provider == "" {
		res.Provider = promptDefault(cmd, f.nonInteractive, "Provider", "gcs")
	}

	switch res.Provider {
	case "gcs":
		return setupGCS(ctx, cmd, f, cfg, path, res)
	case "s3", "r2":
		return fmt.Errorf("provider %q is not implemented yet", res.Provider)
	default:
		return fmt.Errorf("unknown provider %q (supported: gcs, s3, r2)", res.Provider)
	}
}

func setupGCS(ctx context.Context, cmd *cobra.Command, f setupFlags, cfg *config.Config, path string, res config.Resolved) error {
	prov := res.Provider

	// Determine project candidate: --project / env / gcloud config.
	projectID := res.ProjectID
	if projectID == "" {
		projectID = gcloudProject()
	}
	projectID = promptDefault(cmd, f.nonInteractive, "GCP project", projectID)

	bucket := res.Bucket
	if bucket == "" {
		bucket = defaultGCSBucket
	}
	bucket = promptDefault(cmd, f.nonInteractive, "Bucket name", bucket)

	// Resolve the base URL default. When the user did not explicitly pass a
	// base URL (via --base-url or UISHOT_BASE_URL), derive it from the final
	// bucket so that changing the bucket also updates the base URL. Reusing a
	// stale config base_url here would leave the saved base URL pointing at the
	// previous bucket (issue #7).
	baseURL := gcsBaseURLDefault(res, bucket)
	baseURL = promptDefault(cmd, f.nonInteractive, "Base URL", baseURL)

	out := cmd.OutOrStdout()
	policy := f.publicPolicy()

	// confirmPublic is only consulted by the provider for an *existing*,
	// non-public bucket under the default (auto) policy. In non-interactive
	// mode we never confirm: leave it private rather than risk exposing data.
	var confirmPublic func() bool
	if !f.nonInteractive && policy == provider.PublicAuto {
		confirmPublic = func() bool {
			return promptYesNo(cmd, "This existing bucket is not publicly readable. Make it public (grant allUsers roles/storage.objectViewer)?")
		}
	}

	p, err := provider.New(prov)
	if err != nil {
		return err
	}
	result, err := p.Setup(ctx, provider.SetupOptions{
		Provider:       prov,
		ProjectID:      projectID,
		Bucket:         bucket,
		BaseURL:        baseURL,
		NonInteractive: f.nonInteractive,
		PublicPolicy:   policy,
		ConfirmPublic:  confirmPublic,
	})
	if err != nil {
		return err
	}

	reportPublicState(out, result)

	cfg.Version = 1
	cfg.Provider = prov
	if cfg.Defaults.RepoPrefixMode == "" {
		cfg.Defaults.RepoPrefixMode = "git_remote"
	}
	// For an existing bucket no project id is required, so result.ProjectID
	// may be empty; keep any previously saved value rather than blanking it.
	if result.ProjectID != "" {
		cfg.GCS.ProjectID = result.ProjectID
	}
	cfg.GCS.Bucket = result.Bucket
	cfg.GCS.BaseURL = result.BaseURL

	if err := config.Save(path, cfg); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Saved config to %s\n", path)
	return nil
}

// reportPublicState prints a message describing the bucket's public-read state
// after setup, so the user knows whether the returned URLs will be accessible.
func reportPublicState(out io.Writer, r provider.SetupResult) {
	switch {
	case r.MadePublic && r.BucketCreated:
		fmt.Fprintln(out, "Bucket created and configured for public read (allUsers: roles/storage.objectViewer). Uploaded image URLs are publicly accessible.")
	case r.MadePublic:
		fmt.Fprintln(out, "Granted public read on the bucket (allUsers: roles/storage.objectViewer). Uploaded image URLs are publicly accessible.")
	case r.AlreadyPublic:
		fmt.Fprintln(out, "Bucket is already publicly readable. Uploaded image URLs are accessible.")
	case r.PublicSkipped:
		fmt.Fprintln(out, "Warning: the bucket is NOT publicly readable, so uploaded image URLs may return HTTP 403.")
		fmt.Fprintln(out, "To make it public later, run `uishot setup --public` or grant allUsers roles/storage.objectViewer on the bucket.")
	}
}

// promptYesNo asks a yes/no question, defaulting to No (safe choice). It is
// only called in interactive mode.
func promptYesNo(cmd *cobra.Command, question string) bool {
	fmt.Fprintf(cmd.OutOrStdout(), "%s [y/N]: ", question)
	reader := bufio.NewReader(cmd.InOrStdin())
	line, _ := reader.ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// gcloudProject returns `gcloud config get-value project` or "".
func gcloudProject() string {
	out, err := exec.Command("gcloud", "config", "get-value", "project").Output()
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(out))
	if v == "(unset)" {
		return ""
	}
	return v
}

// promptDefault asks for input with a default. In non-interactive mode it
// returns def unchanged.
func promptDefault(cmd *cobra.Command, nonInteractive bool, label, def string) string {
	if nonInteractive {
		return def
	}
	out := cmd.OutOrStdout()
	if def != "" {
		fmt.Fprintf(out, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(out, "%s: ", label)
	}
	reader := bufio.NewReader(cmd.InOrStdin())
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}
