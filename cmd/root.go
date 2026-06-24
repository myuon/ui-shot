// Package cmd implements the uishot CLI commands.
package cmd

import (
	"github.com/spf13/cobra"
)

// NewRootCmd builds the root cobra command.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "uishot",
		Short: "Upload UI screenshots and return a URL/Markdown for GitHub PR/Issue",
		Long: `uishot uploads a UI screenshot to an image store and prints a URL (or
Markdown) you can paste straight into a GitHub PR or Issue.

Typical workflow: run setup once, then upload per screenshot.
See the README for full details.`,
		Example: `  # 1. Configure once (GCS provider)
  uishot setup --provider gcs

  # 2. Upload a screenshot for PR #123
  uishot upload --pr 123 --name booking-detail --file ./shot.png`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newSetupCmd())
	root.AddCommand(newUploadCmd())
	return root
}
