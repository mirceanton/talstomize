package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"

	tconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configdiff"
	"github.com/siderolabs/talos/pkg/machinery/textdiff"

	"github.com/mirceanton/talstomize/internal/config"
	"github.com/mirceanton/talstomize/internal/talos"
)

// talosVersionCheck is a node's actually-running Talos OS version compared
// against its rendered target install image. Checked is false when
// there's nothing to compare - machine.install.image is unset (left for
// patches to set) - which isn't drift, just "not a candidate".
type talosVersionCheck struct {
	Checked   bool
	Drifted   bool
	Running   string
	Want      string // target version tag, e.g. "v1.13.8"
	WantImage string // full install image the tag was extracted from
}

// extensionsCheck is a node's installed system extensions compared against
// its schematic's desired officialExtensions. Checked is false when the
// node has no schematic configured.
type extensionsCheck struct {
	Checked             bool
	Installed, Desired  []string
	Missing, Unexpected []string
}

func (c extensionsCheck) drifted() bool {
	return len(c.Missing) > 0 || len(c.Unexpected) > 0
}

// kernelArgsCheck is a node's live kernel cmdline compared against its
// schematic's desired extraKernelArgs. Checked is false when the node has
// no schematic configured. Only missing entries count as drift - cmdline
// always contains boilerplate no schematic declares (see
// talos.KernelArgDrift).
type kernelArgsCheck struct {
	Checked bool
	Desired []string
	Missing []string
}

func (c kernelArgsCheck) drifted() bool {
	return len(c.Missing) > 0
}

// nodeDriftResult is everything diff found (or didn't) for a single node.
type nodeDriftResult struct {
	Name, IP string

	// ConfigDiff is a unified diff of the node's running machine config
	// against its rendered target ("" if they match).
	ConfigDiff string

	TalosVersion talosVersionCheck
	Extensions   extensionsCheck
	KernelArgs   kernelArgsCheck
}

func (n nodeDriftResult) drifted() bool {
	return n.ConfigDiff != "" || n.TalosVersion.Drifted || n.Extensions.drifted() || n.KernelArgs.drifted()
}

// extraLines renders the non-config-diff checks as the indented
// "    <check>: ..." lines used by the plain and pretty renderers, one per
// check that found drift (nil if everything but the config matched).
func (n nodeDriftResult) extraLines() []string {
	var lines []string

	if n.TalosVersion.Drifted {
		lines = append(lines, fmt.Sprintf("    talos: running %s, want %s (%s)", n.TalosVersion.Running, n.TalosVersion.Want, n.TalosVersion.WantImage))
	}

	if n.Extensions.drifted() {
		var parts []string

		if len(n.Extensions.Missing) > 0 {
			parts = append(parts, fmt.Sprintf("missing %v", n.Extensions.Missing))
		}

		if len(n.Extensions.Unexpected) > 0 {
			parts = append(parts, fmt.Sprintf("unexpected %v", n.Extensions.Unexpected))
		}

		lines = append(lines, "    extensions: "+strings.Join(parts, ", "))
	}

	if n.KernelArgs.drifted() {
		lines = append(lines, fmt.Sprintf("    kernel args: missing %v", n.KernelArgs.Missing))
	}

	return lines
}

// kubernetesCheck is the cluster-wide comparison of the running Kubernetes
// version against the configured target. Checked is false only on error
// paths that already fail the whole command, so in practice it's always
// true once a driftReport is fully built.
type kubernetesCheck struct {
	Checked       bool
	Running, Want string
}

func (c kubernetesCheck) drifted() bool {
	return c.Checked && c.Running != c.Want
}

// driftReport is the full result of a `diff` run, computed once up front
// and handed to whichever renderer --output selected.
type driftReport struct {
	Nodes      []nodeDriftResult
	Kubernetes kubernetesCheck
}

func (r driftReport) drifted() bool {
	if r.Kubernetes.drifted() {
		return true
	}

	for _, n := range r.Nodes {
		if n.drifted() {
			return true
		}
	}

	return false
}

// computeNodeDrift compares node's live state - its running machine
// config, actual Talos OS version, and (if a schematic is configured) its
// installed system extensions and kernel args - against what rendered
// declares. This is what a plain machine-config diff can't catch: bumping
// installer.talosVersion and applying config only changes the *declared*
// install image, not what's actually booted - the two can already agree
// with each other while the real node is still on the old version.
func computeNodeDrift(engine *talos.Engine, node config.Node, rendered, running tconfig.Provider, errOut io.Writer, name string, passthrough []string) (nodeDriftResult, error) {
	result := nodeDriftResult{Name: name, IP: node.IP}

	configDiff, err := configdiff.DiffConfigs(running, rendered)
	if err != nil {
		return result, fmt.Errorf("node %q: diffing: %w", name, err)
	}

	result.ConfigDiff = configDiff

	if target := rendered.Machine().Install().Image(); target != "" {
		runningVersion, err := getRunningTalosVersion(errOut, node.IP, passthrough)
		if err != nil {
			return result, fmt.Errorf("node %q: fetching running Talos version: %w", name, err)
		}

		drifted, ok := talos.TalosVersionDrift(runningVersion, target)
		result.TalosVersion = talosVersionCheck{
			Checked: ok, Drifted: ok && drifted,
			Running: runningVersion, Want: talos.ImageTag(target), WantImage: target,
		}
	}

	customization, ok, err := engine.EffectiveCustomization(node)
	if err != nil {
		return result, fmt.Errorf("node %q: resolving schematic: %w", name, err)
	}

	if !ok {
		return result, nil
	}

	installed, err := getInstalledExtensions(errOut, node.IP, passthrough)
	if err != nil {
		return result, fmt.Errorf("node %q: fetching installed extensions: %w", name, err)
	}

	desiredExtensions := customization.Customization.SystemExtensions.OfficialExtensions
	missing, unexpected := talos.ExtensionDrift(installed, desiredExtensions)
	result.Extensions = extensionsCheck{
		Checked: true, Installed: installed, Desired: desiredExtensions,
		Missing: missing, Unexpected: unexpected,
	}

	cmdline, err := getKernelCmdline(errOut, node.IP, passthrough)
	if err != nil {
		return result, fmt.Errorf("node %q: fetching kernel cmdline: %w", name, err)
	}

	desiredArgs := customization.Customization.ExtraKernelArgs
	result.KernelArgs = kernelArgsCheck{
		Checked: true, Desired: desiredArgs,
		Missing: talos.KernelArgDrift(cmdline, desiredArgs),
	}

	return result, nil
}

// computeKubernetesDrift reports whether the cluster's running Kubernetes
// version differs from target (Engine.KubernetesVersion).
//
// `talosctl upgrade-k8s --dry-run`'s plan output turns out not to be a
// usable "is there drift" signal on its own - live-verified against a
// real cluster already sitting at the target version, it still
// unconditionally prints "updating <component> to version ..." action
// lines and a manifest-annotation diff (SSA ownership metadata churn
// unrelated to version drift). The one reliable signal in it is the
// auto-detected current version, parsed via talos.ParseKubernetesVersion.
func computeKubernetesDrift(engine *talos.Engine, errOut io.Writer, passthrough []string) (kubernetesCheck, error) {
	target := engine.KubernetesVersion()

	plan, err := getK8sUpgradePlan(errOut, target, passthrough)
	if err != nil {
		return kubernetesCheck{}, fmt.Errorf("fetching Kubernetes upgrade plan: %w", err)
	}

	running := talos.ParseKubernetesVersion(plan)
	if running == "" {
		return kubernetesCheck{}, fmt.Errorf("could not determine the cluster's current Kubernetes version from talosctl upgrade-k8s --dry-run output")
	}

	return kubernetesCheck{Checked: true, Running: running, Want: target}, nil
}

// repathDiff rewrites a unified diff's "--- a\n+++ b\n" header (the form
// configdiff.DiffConfigs always produces) to use aPath/bPath instead, so
// multiple nodes' config diffs can be told apart once concatenated into
// one multi-file patch. Returns diff unchanged if it doesn't have that
// exact header (e.g. "" - no drift).
func repathDiff(diff, aPath, bPath string) string {
	body, ok := strings.CutPrefix(diff, "--- a\n+++ b\n")
	if !ok {
		return diff
	}

	return fmt.Sprintf("--- %s\n+++ %s\n%s", aPath, bPath, body)
}

// sortedLines joins items, sorted, one per line, with a trailing newline -
// the shape textdiff.DiffWithCustomPaths wants for its two sides. Returns
// "" for an empty items so an empty-vs-empty diff comes out "" too.
func sortedLines(items []string) string {
	if len(items) == 0 {
		return ""
	}

	sorted := slices.Clone(items)
	slices.Sort(sorted)

	return strings.Join(sorted, "\n") + "\n"
}

// diff renders c as a unified diff of "running" vs "want" ("" if c isn't
// drifted).
func (c talosVersionCheck) diff(node string) (string, error) {
	if !c.Drifted {
		return "", nil
	}

	path := node + "/talos-version"

	return textdiff.DiffWithCustomPaths(c.Running+"\n", c.Want+"\n", "a/"+path, "b/"+path)
}

// diff renders c as a unified diff of installed vs desired extensions,
// sorted one-per-line, so missing entries show as additions and
// unexpected ones as removals ("" if c isn't drifted).
func (c extensionsCheck) diff(node string) (string, error) {
	if !c.drifted() {
		return "", nil
	}

	path := node + "/extensions"

	return textdiff.DiffWithCustomPaths(sortedLines(c.Installed), sortedLines(c.Desired), "a/"+path, "b/"+path)
}

// diff renders c as a unified diff of the desired kernel args currently
// present on the node vs the full desired list, so only the missing ones
// show up, as additions ("" if c isn't drifted).
func (c kernelArgsCheck) diff(node string) (string, error) {
	if !c.drifted() {
		return "", nil
	}

	missing := make(map[string]bool, len(c.Missing))
	for _, arg := range c.Missing {
		missing[arg] = true
	}

	present := make([]string, 0, len(c.Desired))

	for _, arg := range c.Desired {
		if !missing[arg] {
			present = append(present, arg)
		}
	}

	path := node + "/kernel-args"

	return textdiff.DiffWithCustomPaths(sortedLines(present), sortedLines(c.Desired), "a/"+path, "b/"+path)
}

// diff renders c as a unified diff of "running" vs "want" ("" if c isn't
// drifted).
func (c kubernetesCheck) diff() (string, error) {
	if !c.drifted() {
		return "", nil
	}

	return textdiff.DiffWithCustomPaths(c.Running+"\n", c.Want+"\n", "a/kubernetes-version", "b/kubernetes-version")
}
