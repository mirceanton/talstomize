package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	tconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configdiff"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"

	"github.com/mirceanton/talstomize/internal/config"
	"github.com/mirceanton/talstomize/internal/talos"
)

func newDiffCommand() *cobra.Command {
	var (
		kustomizeDir string
		nodeFilter   string
	)

	cmd := &cobra.Command{
		Use:   "diff [-- talosctl flags]",
		Short: "Diff each node's rendered config against its running config",
		Long: "Render each node's machine config the same way `build` does, fetch its currently " +
			"running config with `talosctl get machineconfig`, and print the difference. Requires " +
			"talosctl to be installed and on PATH.\n\n" +
			"Anything after `--` is passed through to talosctl as-is, e.g.\n" +
			"  talstomize diff -k . -- --insecure\n\n" +
			"Exits 0 if every node matches, 1 if any node differs (or on error).",
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

			out := cmd.OutOrStdout()
			anyDiff := false

			for _, name := range names {
				node, _ := engine.Node(name)

				rendered, err := engine.RenderNode(name)
				if err != nil {
					return err
				}

				running, err := getRunningConfig(node.IP, passthrough, cmd.ErrOrStderr())
				if err != nil {
					return fmt.Errorf("node %q: fetching running config: %w", name, err)
				}

				diff, err := configdiff.DiffConfigs(running, rendered)
				if err != nil {
					return fmt.Errorf("node %q: diffing: %w", name, err)
				}

				if diff == "" {
					fmt.Fprintf(out, "==> %s (%s): no differences\n", name, node.IP)

					continue
				}

				anyDiff = true

				fmt.Fprintf(out, "==> %s (%s):\n%s\n", name, node.IP, diff)
			}

			if anyDiff {
				os.Exit(1)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&kustomizeDir, "kustomize", "k", "", "path to a directory containing talstomize.yaml (defaults to the current directory)")
	cmd.Flags().StringVar(&nodeFilter, "node", "", "only diff this node (by name)")

	return cmd
}

// getRunningConfig fetches and parses the machine config currently running
// on the node at ip, via `talosctl get machineconfig`. That command's `-o
// yaml` output wraps the config as a COSI resource - a "spec" field holding
// the actual v1alpha1 config as a nested YAML string - not the bare config
// itself, so it has to be unwrapped before it can be diffed against a
// rendered one.
func getRunningConfig(ip string, passthrough []string, stderr io.Writer) (tconfig.Provider, error) {
	args := append([]string{"get", "machineconfig", "--nodes", ip, "-o", "yaml"}, passthrough...)

	var stdout bytes.Buffer

	run := exec.Command("talosctl", args...)
	run.Stdout = &stdout
	run.Stderr = stderr

	if err := run.Run(); err != nil {
		return nil, fmt.Errorf("talosctl get machineconfig: %w", err)
	}

	var resource struct {
		Spec string `yaml:"spec"`
	}

	if err := yaml.NewDecoder(&stdout).Decode(&resource); err != nil {
		return nil, fmt.Errorf("parsing machineconfig resource: %w", err)
	}

	if resource.Spec == "" {
		return nil, fmt.Errorf("machineconfig resource has no spec")
	}

	provider, err := configloader.NewFromBytes([]byte(resource.Spec))
	if err != nil {
		return nil, fmt.Errorf("parsing running machine config: %w", err)
	}

	return provider, nil
}
