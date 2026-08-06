// Package cli implements the talstomize command-line interface.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is set at build time via -ldflags.
var version = "dev"

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "talstomize",
		Short:         "Kustomize-style config generation and rollout for Talos Linux clusters",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newBuildCommand())
	root.AddCommand(newApplyCommand())
	root.AddCommand(newVersionCommand())

	return root
}

// Execute runs the talstomize CLI and exits the process on error.
func Execute() {
	if err := newRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
