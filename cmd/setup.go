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
R2 delegates to the wrangler CLI: install it with "npm install -g wrangler"
and authenticate with "wrangler login" (or set CLOUDFLARE_API_TOKEN).
Prompts for any value not passed as a flag (skip prompts with --non-interactive).`,
		Example: `  # Interactive
  uishot setup --provider gcs

  # Non-interactive (GCS)
  uishot setup --provider gcs --project my-gcp-project \
    --bucket ui-shot-assets --non-interactive

  # Cloudflare R2 — interactive: prompts whether to enable r2.dev public domain
  uishot setup --provider r2 --account-id <cloudflare-account-id>

  # Cloudflare R2 — non-interactive with r2.dev auto-enabled (development only,
  # rate-limited; use --base-url for a custom domain in production)
  uishot setup --provider r2 --public \
    --account-id <cloudflare-account-id> \
    --bucket ui-shot-assets --non-interactive

  # Cloudflare R2 — non-interactive with a custom domain (production)
  uishot setup --provider r2 \
    --account-id <cloudflare-account-id> \
    --bucket ui-shot-assets \
    --base-url https://shots.example.com --non-interactive`,
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
	cmd.Flags().BoolVar(&f.public, "public", false, "GCS: grant public read without confirmation; R2: enable r2.dev managed domain without prompting (development only, rate-limited)")
	cmd.Flags().BoolVar(&f.noPublic, "no-public", false, "GCS: never grant public read; R2: skip r2.dev (requires --base-url)")
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

// defaultBucket is the bucket name suggested by setup for every provider.
const defaultBucket = "ui-shot-assets"

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
	case "r2":
		return setupR2(ctx, cmd, f, cfg, path, res)
	case "s3":
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
		bucket = defaultBucket
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

// r2BaseURLDefault returns the base URL to present as the setup default for
// the final (post-prompt) bucket. Unlike GCS, an R2 public URL (an r2.dev
// managed domain or a custom domain) cannot be derived from the bucket name,
// so there is no derived fallback:
//
//   - An explicitly provided base URL (--base-url or UISHOT_BASE_URL) is
//     honored as-is (highest precedence).
//   - Otherwise, if the final bucket is unchanged from the saved config and the
//     config has a non-empty base URL, that value is preserved.
//   - Otherwise (the bucket changed, or there is no saved base URL) the default
//     is empty and the user must supply the bucket's public URL, so a stale
//     base URL never points at a different bucket (issue #7).
func r2BaseURLDefault(res config.Resolved, bucket string) string {
	if res.BaseURLExplicit {
		return res.BaseURL
	}
	if bucket == res.ConfigBucket && res.ConfigBaseURL != "" {
		return res.ConfigBaseURL
	}
	return ""
}

func setupR2(ctx context.Context, cmd *cobra.Command, f setupFlags, cfg *config.Config, path string, res config.Resolved) error {
	prov := res.Provider

	accountID := promptDefault(cmd, f.nonInteractive, "Cloudflare account id", res.AccountID)

	bucket := res.Bucket
	if bucket == "" {
		bucket = defaultBucket
	}
	bucket = promptDefault(cmd, f.nonInteractive, "Bucket name", bucket)

	// R2 public URLs cannot be derived from the bucket name (unlike GCS). The
	// strategy depends on the --public / --no-public flags and whether a base
	// URL is already known:
	//
	//   Explicit base URL (--base-url or env): use it directly; skip r2.dev.
	//   --public:   provider enables the r2.dev domain unconditionally.
	//   --no-public: provider skips r2.dev (PublicSkipped); fall back to manual
	//                base-URL prompt if no URL was supplied.
	//   default:    provider checks if r2.dev is already enabled (no prompt);
	//               if not, interactive setup asks whether to enable it;
	//               if the user declines, fall back to a manual prompt below.
	baseURL := r2BaseURLDefault(res, bucket)
	policy := f.publicPolicy()

	// If an explicit base URL is available (from --base-url, env, or the saved
	// config for the unchanged bucket), present it as the default; it bypasses
	// the r2.dev flow in the provider.
	if baseURL != "" {
		baseURL = promptDefault(cmd, f.nonInteractive, "Base URL (public bucket URL, e.g. https://pub-xxxxxxxx.r2.dev)", baseURL)
	}

	// confirmPublic is passed to the provider for existing buckets under
	// PublicAuto. For a newly created bucket the provider enables r2.dev
	// automatically without asking (matching GCS's "created = auto-public"
	// behaviour). The callback here decides whether to enable r2.dev for an
	// *existing* bucket when the user has not passed --public.
	//
	// If the user declines, the provider sets PublicSkipped and returns an
	// empty BaseURL; we then fall back to a manual base-URL prompt below.
	var confirmPublic func() bool
	if !f.nonInteractive && policy == provider.PublicAuto && baseURL == "" {
		confirmPublic = func() bool {
			return promptYesNo(cmd, "Enable the r2.dev managed public domain for this bucket? (rate-limited; intended for development — use a custom domain for production)")
		}
	}

	p, err := provider.New(prov)
	if err != nil {
		return err
	}
	result, err := p.Setup(ctx, provider.SetupOptions{
		Provider:       prov,
		AccountID:      accountID,
		Bucket:         bucket,
		BaseURL:        baseURL,
		NonInteractive: f.nonInteractive,
		PublicPolicy:   policy,
		ConfirmPublic:  confirmPublic,
	})
	if err != nil {
		return err
	}

	// If the provider could not determine the base URL automatically (user
	// declined r2.dev or --no-public was set), fall back to a manual prompt.
	// In non-interactive mode, leave the base URL empty and warn; uploads will
	// fail until the user configures it.
	if result.PublicSkipped && result.BaseURL == "" {
		result.BaseURL = promptDefault(cmd, f.nonInteractive, "Base URL (public bucket URL, e.g. https://pub-xxxxxxxx.r2.dev — or set later via `uishot setup --base-url`)", "")
	}

	out := cmd.OutOrStdout()
	reportR2PublicState(out, result, path)

	cfg.Version = 1
	cfg.Provider = prov
	if cfg.Defaults.RepoPrefixMode == "" {
		cfg.Defaults.RepoPrefixMode = "git_remote"
	}
	// account id may be empty when wrangler can pick the account itself (a
	// single-account token); keep any previously saved value in that case,
	// mirroring how setupGCS keeps a previously saved project id. Clearing a
	// saved account id requires editing config.toml by hand.
	if accountID != "" {
		cfg.R2.AccountID = accountID
	}
	cfg.R2.Bucket = result.Bucket
	cfg.R2.BaseURL = result.BaseURL

	if err := config.Save(path, cfg); err != nil {
		return err
	}
	fmt.Fprintf(out, "Saved config to %s\n", path)
	return nil
}

// reportR2PublicState prints a message describing the R2 bucket's public
// access state after setup. path is the config file path used in the warning
// message so we never hardcode the default location.
func reportR2PublicState(out io.Writer, r provider.SetupResult, path string) {
	switch {
	case r.MadePublic && r.BucketCreated:
		fmt.Fprintf(out, "Bucket %q created and r2.dev public domain enabled. Uploaded image URLs are publicly accessible.\n", r.Bucket)
		fmt.Fprintln(out, "Note: r2.dev is rate-limited and intended for development use. For production, attach a custom domain in the Cloudflare dashboard.")
	case r.MadePublic:
		fmt.Fprintf(out, "r2.dev public domain enabled for bucket %q. Uploaded image URLs are publicly accessible at %s\n", r.Bucket, r.BaseURL)
		fmt.Fprintln(out, "Note: r2.dev is rate-limited and intended for development use. For production, attach a custom domain in the Cloudflare dashboard.")
	case r.AlreadyPublic:
		fmt.Fprintf(out, "r2.dev public domain is already enabled for bucket %q. Uploaded image URLs are accessible at %s\n", r.Bucket, r.BaseURL)
	case r.PublicSkipped && r.BaseURL == "":
		// r2.dev was skipped and no base URL was supplied — warn the user.
		fmt.Fprintf(out, "Warning: the base URL for bucket %q was not configured automatically. Uploaded URLs may not be accessible until you:\n", r.Bucket)
		fmt.Fprintf(out, "  - run `wrangler r2 bucket dev-url enable %s` (development, rate-limited), or\n", r.Bucket)
		fmt.Fprintln(out, "  - attach a custom domain in the Cloudflare dashboard (recommended for production),")
		fmt.Fprintf(out, "  then update base_url in %s.\n", path)
	default:
		if r.BaseURL != "" {
			fmt.Fprintf(out, "Note: uploaded URLs only work if %q serves bucket %q publicly.\n", r.BaseURL, r.Bucket)
		}
	}
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
