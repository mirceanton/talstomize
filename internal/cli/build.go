package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mirceanton/talstomize/internal/config"
	"github.com/mirceanton/talstomize/internal/talos"
)

func newBuildCommand() *cobra.Command {
	var outputDir string

	cmd := &cobra.Command{
		Use:   "build [path]",
		Short: "Render per-node Talos machine configs",
		Long: "Render per-node Talos machine configs from a talstomize.yaml, applying role and " +
			"per-node patches, and write one file per node (named <node>.yaml) to the output " +
			"directory, plus a talosctl client config (named \"talosconfig\").",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) == 1 {
				path = args[0]
			}

			cfg, err := config.Load(path)
			if err != nil {
				return err
			}

			engine, err := talos.NewEngine(cfg)
			if err != nil {
				return err
			}

			dir := outputDir
			if dir == "" {
				dir = filepath.Join(cfg.Dir(), "_out")
			}

			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("creating output directory: %w", err)
			}

			out := cmd.OutOrStdout()

			for _, name := range engine.NodeNames("") {
				rendered, err := engine.RenderNode(name)
				if err != nil {
					return err
				}

				encoded, err := rendered.Bytes()
				if err != nil {
					return fmt.Errorf("encoding config for node %q: %w", name, err)
				}

				dest := filepath.Join(dir, name+".yaml")
				if err := os.WriteFile(dest, encoded, 0o600); err != nil {
					return fmt.Errorf("writing %s: %w", dest, err)
				}

				fmt.Fprintf(out, "wrote %s\n", dest)
			}

			talosconfig, err := engine.Talosconfig()
			if err != nil {
				return fmt.Errorf("generating talosconfig: %w", err)
			}

			encoded, err := talosconfig.Bytes()
			if err != nil {
				return fmt.Errorf("encoding talosconfig: %w", err)
			}

			dest := filepath.Join(dir, "talosconfig")
			if err := os.WriteFile(dest, encoded, 0o600); err != nil {
				return fmt.Errorf("writing %s: %w", dest, err)
			}

			fmt.Fprintf(out, "wrote %s\n", dest)

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputDir, "output", "o", "", "directory to write per-node configs to (defaults to \"_out\" next to the talstomize.yaml)")

	return cmd
}
