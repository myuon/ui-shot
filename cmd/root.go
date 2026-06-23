// Package cmd implements the ui-shot CLI commands.
package cmd

import (
	"github.com/spf13/cobra"
)

// NewRootCmd builds the root cobra command.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "ui-shot",
		Short:         "Upload UI screenshots and return a URL/Markdown for GitHub PR/Issue",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newSetupCmd())
	root.AddCommand(newUploadCmd())
	return root
}
