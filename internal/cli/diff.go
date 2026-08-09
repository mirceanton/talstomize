package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

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
		file       string
		nodeFilter string
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
			"Anything after `--` is passed through to talosctl as-is, e.g.\n" +
			"  talstomize diff -f . -- --insecure\n\n" +
			"Exits 0 if every node matches, 1 if any node differs (or on error).",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
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

			out := cmd.OutOrStdout()
			errOut := cmd.ErrOrStderr()
			anyDiff := false

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

				configDiff, err := configdiff.DiffConfigs(running, rendered)
				if err != nil {
					return fmt.Errorf("node %q: diffing: %w", name, err)
				}

				extraLines, err := diffLiveState(engine, node, rendered, errOut, name, passthrough)
				if err != nil {
					return err
				}

				if configDiff == "" && len(extraLines) == 0 {
					fmt.Fprintf(out, "==> %s (%s): no differences\n", name, node.IP)

					continue
				}

				anyDiff = true

				fmt.Fprintf(out, "==> %s (%s):\n", name, node.IP)

				if configDiff != "" {
					fmt.Fprintln(out, configDiff)
				}

				for _, line := range extraLines {
					fmt.Fprintln(out, line)
				}
			}

			k8sDrift, err := diffKubernetesVersion(engine, errOut, passthrough)
			if err != nil {
				return err
			}

			if k8sDrift != "" {
				anyDiff = true

				fmt.Fprintf(out, "==> kubernetes: %s\n", k8sDrift)
			}

			if anyDiff {
				os.Exit(1)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "path to a talstomize.yaml file, or a directory containing one (defaults to the current directory); all relative paths in it resolve against that file's directory")
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

// diffLiveState compares node's actual live state against what rendered
// declares - its running Talos OS version, and, if a schematic is
// configured (Engine.EffectiveCustomization), its installed system
// extensions and kernel args - returning one line per check that found
// drift (nil if everything matches). This is what a plain machine-config
// diff can't catch: bumping installer.talosVersion and applying config
// only changes the *declared* install image, not what's actually booted -
// the two can already agree with each other while the real node is still
// on the old version.
func diffLiveState(engine *talos.Engine, node config.Node, rendered tconfig.Provider, errOut io.Writer, name string, passthrough []string) ([]string, error) {
	var lines []string

	if target := rendered.Machine().Install().Image(); target != "" {
		running, err := getRunningTalosVersion(errOut, node.IP, passthrough)
		if err != nil {
			return nil, fmt.Errorf("node %q: fetching running Talos version: %w", name, err)
		}

		if drifted, ok := talos.TalosVersionDrift(running, target); ok && drifted {
			lines = append(lines, fmt.Sprintf("    talos: running %s, want %s (%s)", running, talos.ImageTag(target), target))
		}
	}

	customization, ok, err := engine.EffectiveCustomization(node)
	if err != nil {
		return nil, fmt.Errorf("node %q: resolving schematic: %w", name, err)
	}

	if !ok {
		return lines, nil
	}

	installed, err := getInstalledExtensions(errOut, node.IP, passthrough)
	if err != nil {
		return nil, fmt.Errorf("node %q: fetching installed extensions: %w", name, err)
	}

	desiredExtensions := customization.Customization.SystemExtensions.OfficialExtensions
	if missing, unexpected := talos.ExtensionDrift(installed, desiredExtensions); len(missing) > 0 || len(unexpected) > 0 {
		var parts []string

		if len(missing) > 0 {
			parts = append(parts, fmt.Sprintf("missing %v", missing))
		}

		if len(unexpected) > 0 {
			parts = append(parts, fmt.Sprintf("unexpected %v", unexpected))
		}

		lines = append(lines, "    extensions: "+strings.Join(parts, ", "))
	}

	cmdline, err := getKernelCmdline(errOut, node.IP, passthrough)
	if err != nil {
		return nil, fmt.Errorf("node %q: fetching kernel cmdline: %w", name, err)
	}

	desiredArgs := customization.Customization.ExtraKernelArgs
	if missing := talos.KernelArgDrift(cmdline, desiredArgs); len(missing) > 0 {
		lines = append(lines, fmt.Sprintf("    kernel args: missing %v", missing))
	}

	return lines, nil
}

// diffKubernetesVersion reports whether the cluster's running Kubernetes
// version differs from target (Engine.KubernetesVersion), returning a
// one-line "running X, want Y" summary if so, "" if the cluster is
// already at target.
//
// `talosctl upgrade-k8s --dry-run`'s plan output turns out not to be a
// usable "is there drift" signal on its own - live-verified against a
// real cluster already sitting at the target version, it still
// unconditionally prints "updating <component> to version ..." action
// lines and a manifest-annotation diff (SSA ownership metadata churn
// unrelated to version drift). The one reliable signal in it is the
// auto-detected current version, parsed via talos.ParseKubernetesVersion.
func diffKubernetesVersion(engine *talos.Engine, errOut io.Writer, passthrough []string) (string, error) {
	target := engine.KubernetesVersion()

	plan, err := getK8sUpgradePlan(errOut, target, passthrough)
	if err != nil {
		return "", fmt.Errorf("fetching Kubernetes upgrade plan: %w", err)
	}

	running := talos.ParseKubernetesVersion(plan)
	if running == "" {
		return "", fmt.Errorf("could not determine the cluster's current Kubernetes version from talosctl upgrade-k8s --dry-run output")
	}

	if running == target {
		return "", nil
	}

	return fmt.Sprintf("running %s, want %s", running, target), nil
}
