package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/myuon/ui-shot/internal/config"
	"github.com/myuon/ui-shot/internal/gitutil"
	"github.com/myuon/ui-shot/internal/imageutil"
	"github.com/myuon/ui-shot/internal/provider"
)

type uploadFlags struct {
	pr       int
	issue    int
	name     string
	file     string
	repo     string
	commit   string
	markdown bool
	provider string
	bucket   string
	baseURL  string
}

func newUploadCmd() *cobra.Command {
	var f uploadFlags
	cmd := &cobra.Command{
		Use:   "upload",
		Short: "Upload an image and print its URL (or Markdown)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUpload(cmd.Context(), cmd, &f)
		},
	}
	cmd.Flags().IntVar(&f.pr, "pr", 0, "PR number (exclusive with --issue)")
	cmd.Flags().IntVar(&f.issue, "issue", 0, "Issue number (exclusive with --pr)")
	cmd.Flags().StringVar(&f.name, "name", "", "image name (without extension)")
	cmd.Flags().StringVar(&f.file, "file", "", "image file path")
	cmd.Flags().StringVar(&f.repo, "repo", "", "owner/repo (inferred from git remote if unset)")
	cmd.Flags().StringVar(&f.commit, "commit", "", "commit SHA (uses git HEAD if unset)")
	cmd.Flags().BoolVar(&f.markdown, "markdown", false, "output Markdown")
	cmd.Flags().StringVar(&f.provider, "provider", "", "provider (uses global config if unset)")
	cmd.Flags().StringVar(&f.bucket, "bucket", "", "bucket override")
	cmd.Flags().StringVar(&f.baseURL, "base-url", "", "base URL override")
	return cmd
}

func runUpload(ctx context.Context, cmd *cobra.Command, f *uploadFlags) error {
	// Validate --pr / --issue.
	kind, number, err := resolveTarget(f.pr, f.issue)
	if err != nil {
		return err
	}

	if f.name == "" {
		return errors.New("--name is required")
	}
	if f.file == "" {
		return errors.New("--file is required")
	}

	// File existence and extension.
	if _, err := os.Stat(f.file); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file does not exist: %s", f.file)
		}
		return fmt.Errorf("stat file %s: %w", f.file, err)
	}
	contentType, err := imageutil.ContentType(f.file)
	if err != nil {
		return err
	}
	ext := imageutil.Ext(f.file)

	// Load config and resolve effective settings.
	path, err := config.Path()
	if err != nil {
		return err
	}
	cfg, _, err := config.Load(path)
	if err != nil {
		return err
	}
	res := config.Resolve(cfg, f.provider, f.bucket, f.baseURL, "", "", "")
	if res.Provider == "" {
		return errors.New("no provider configured (run `ui-shot setup` or pass --provider)")
	}

	// Resolve repo / commit.
	repo := f.repo
	if repo == "" {
		repo, err = gitutil.Repo()
		if err != nil {
			return err
		}
	}
	commit := f.commit
	if commit == "" {
		commit, err = gitutil.Commit()
		if err != nil {
			return err
		}
	}

	objectKey := provider.ObjectKey(repo, kind, number, commit, f.name, ext)

	p, err := provider.New(res.Provider)
	if err != nil {
		return err
	}

	url, err := p.Upload(ctx, provider.UploadOptions{
		Bucket:       res.Bucket,
		BaseURL:      res.BaseURL,
		ObjectKey:    objectKey,
		FilePath:     f.file,
		ContentType:  contentType,
		CacheControl: imageutil.CacheControl,
	})
	if err != nil {
		return err
	}

	fmt.Fprintln(cmd.OutOrStdout(), formatOutput(url, f.name, f.markdown))
	return nil
}

// resolveTarget validates the --pr/--issue pair and returns the object-key
// kind ("pr" or "issue") and the number.
func resolveTarget(pr, issue int) (kind string, number int, err error) {
	switch {
	case pr > 0 && issue > 0:
		return "", 0, errors.New("--pr and --issue are mutually exclusive")
	case pr > 0:
		return "pr", pr, nil
	case issue > 0:
		return "issue", issue, nil
	default:
		return "", 0, errors.New("one of --pr or --issue is required")
	}
}

// formatOutput renders the upload result as a URL or Markdown image.
func formatOutput(url, name string, markdown bool) string {
	if markdown {
		return fmt.Sprintf("![%s](%s)", name, url)
	}
	return url
}
