package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	tconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"

	"github.com/mirceanton/talstomize/internal/config"
	"github.com/mirceanton/talstomize/internal/talos"
)

func newDiffCommand() *cobra.Command {
	var (
		file       string
		nodeFilter string
		output     string
	)

	cmd := &cobra.Command{
		Use:   "diff [-- talosctl flags]",
		Short: "Diff each node's rendered config against its running config",
		Long: "Render each node's machine config the same way `build` does, and compare it " +
			"against live cluster state: the running machine config, the node's actual Talos OS " +
			"version, its installed system extensions and kernel args (if a schematic is " +
			"configured), and the cluster's running Kubernetes version. Requires talosctl to be " +
			"installed and on PATH.\n\n" +
			"This only detects drift - it never upgrades anything. A version bump in " +
			"talstomize.yaml (talosVersion, kubernetesVersion, or a schematic change) only takes " +
			"effect once you actually run `talosctl upgrade`/`upgrade-k8s` yourself; `diff` is how " +
			"you find out that's still pending after `apply`.\n\n" +
			"--output/-o controls how results are presented: \"plain\" (default) prints " +
			"human-readable text; \"diff\" prints a single unified diff covering every node, " +
			"suitable for redirecting to a .diff file and opening in an editor (e.g. " +
			"`talstomize diff -o diff > cluster.diff`); \"pretty\" opens an interactive terminal " +
			"viewer with one tab per node.\n\n" +
			"Anything after `--` is passed through to talosctl as-is, e.g.\n" +
			"  talstomize diff -f . -- --insecure\n\n" +
			"Exits 0 if every node matches, 1 if any node differs (or on error).",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch output {
			case "plain", "diff", "pretty":
			default:
				return fmt.Errorf("invalid --output %q: must be one of plain, diff, pretty", output)
			}

			passthrough, err := passthroughArgs(cmd, args)
			if err != nil {
				return err
			}

			path := file
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

			errOut := cmd.ErrOrStderr()

			report := driftReport{Nodes: make([]nodeDriftResult, 0, len(names))}

			for _, name := range names {
				node, _ := engine.Node(name)

				rendered, err := engine.RenderNode(name)
				if err != nil {
					return err
				}

				running, err := getRunningConfig(node.IP, passthrough, errOut)
				if err != nil {
					return fmt.Errorf("node %q: fetching running config: %w", name, err)
				}

				result, err := computeNodeDrift(engine, node, rendered, running, errOut, name, passthrough)
				if err != nil {
					return err
				}

				report.Nodes = append(report.Nodes, result)
			}

			report.Kubernetes, err = computeKubernetesDrift(engine, errOut, passthrough)
			if err != nil {
				return err
			}

			switch output {
			case "plain":
				renderPlain(report, cmd.OutOrStdout())
			case "diff":
				if err := renderDiff(report, cmd.OutOrStdout()); err != nil {
					return err
				}
			case "pretty":
				if err := runPretty(report); err != nil {
					return err
				}
			}

			if report.drifted() {
				os.Exit(1)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "path to a talstomize.yaml file, or a directory containing one (defaults to the current directory); all relative paths in it resolve against that file's directory")
	cmd.Flags().StringVar(&nodeFilter, "node", "", "only diff this node (by name)")
	cmd.Flags().StringVarP(&output, "output", "o", "plain", "output format: plain, diff, or pretty")

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

	stdout, err := runTalosctlCaptured(stderr, args)
	if err != nil {
		return nil, err
	}

	var resource struct {
		Spec string `yaml:"spec"`
	}

	if err := yaml.Unmarshal([]byte(stdout), &resource); err != nil {
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
