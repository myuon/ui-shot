package cmd

import (
	"bufio"
	"context"
	"fmt"
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
	return cmd
}

const defaultGCSBucket = "ui-shot-assets"

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

	baseURL := res.BaseURL
	if baseURL == "" {
		baseURL = provider.DefaultGCSBaseURL(bucket)
	}
	baseURL = promptDefault(cmd, f.nonInteractive, "Base URL", baseURL)

	fmt.Fprintln(cmd.OutOrStdout(), "Note: this bucket will be configured for public read (allUsers: roles/storage.objectViewer) so the uploaded image URLs are accessible.")

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
	})
	if err != nil {
		return err
	}

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
