package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mirceanton/talstomize/internal/config"
	"github.com/mirceanton/talstomize/internal/talos"
)

func newApplyCommand() *cobra.Command {
	var (
		kustomizeDir string
		nodeFilter   string
	)

	cmd := &cobra.Command{
		Use:   "apply [-- talosctl flags]",
		Short: "Render and apply Talos machine configs to their nodes",
		Long: "Render each node's machine config and apply it with `talosctl apply-config`, " +
			"talking to the node's IP directly. Requires talosctl to be installed and on PATH.\n\n" +
			"Anything after `--` is passed through to talosctl as-is, e.g.\n" +
			"  talstomize apply -k . -- --insecure --dry-run",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			passthrough, err := passthroughArgs(cmd, args)
			if err != nil {
				return err
			}

			path := kustomizeDir
			if path == "" {
				path = "."
			}

			cfg, err := config.Load(path)
			if err != nil {
				return err
			}

			engine, err := talos.NewEngine(cfg)
			if err != nil {
				return err
			}

			names := engine.NodeNames(nodeFilter)
			if len(names) == 0 {
				return fmt.Errorf("no matching nodes")
			}

			tmpDir, err := os.MkdirTemp("", "talstomize-apply-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(tmpDir)

			for _, name := range names {
				node, _ := engine.Node(name)

				rendered, err := engine.RenderNode(name)
				if err != nil {
					return err
				}

				encoded, err := rendered.Bytes()
				if err != nil {
					return fmt.Errorf("encoding config for node %q: %w", name, err)
				}

				dest := filepath.Join(tmpDir, name+".yaml")
				if err := os.WriteFile(dest, encoded, 0o600); err != nil {
					return err
				}

				talosctlArgs := append([]string{"apply-config", "--nodes", node.IP, "--file", dest}, passthrough...)

				fmt.Fprintf(cmd.OutOrStdout(), "==> applying config to %s (%s)\n", name, node.IP)

				run := exec.Command("talosctl", talosctlArgs...)
				run.Stdout = cmd.OutOrStdout()
				run.Stderr = cmd.ErrOrStderr()
				run.Stdin = os.Stdin

				if err := run.Run(); err != nil {
					return fmt.Errorf("applying config to node %q (%s): %w", name, node.IP, err)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&kustomizeDir, "kustomize", "k", "", "path to a directory containing talstomize.yaml (defaults to the current directory)")
	cmd.Flags().StringVar(&nodeFilter, "node", "", "only apply to this node (by name)")

	return cmd
}

// passthroughArgs returns the arguments given after "--", erroring if any
// positional arguments were given before it (apply takes none of its own).
func passthroughArgs(cmd *cobra.Command, args []string) ([]string, error) {
	dash := cmd.ArgsLenAtDash()

	if dash < 0 {
		if len(args) > 0 {
			return nil, fmt.Errorf("unexpected arguments %v (did you mean `-- %v` to pass them to talosctl?)", args, args)
		}

		return nil, nil
	}

	if dash > 0 {
		return nil, fmt.Errorf("unexpected arguments before --: %v", args[:dash])
	}

	return args[dash:], nil
}
