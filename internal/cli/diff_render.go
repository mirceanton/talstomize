package cli

import (
	"fmt"
	"io"
)

// renderPlain writes report as plain, human-readable text - one "==> name
// (ip): ..." block per node, plus a trailing cluster-wide Kubernetes line
// if it drifted. This is the original, non-interactive diff output.
func renderPlain(report driftReport, out io.Writer) {
	for _, n := range report.Nodes {
		lines := n.extraLines()

		if n.ConfigDiff == "" && len(lines) == 0 {
			fmt.Fprintf(out, "==> %s (%s): no differences\n", n.Name, n.IP)

			continue
		}

		fmt.Fprintf(out, "==> %s (%s):\n", n.Name, n.IP)

		if n.ConfigDiff != "" {
			fmt.Fprintln(out, n.ConfigDiff)
		}

		for _, line := range lines {
			fmt.Fprintln(out, line)
		}
	}

	if report.Kubernetes.drifted() {
		fmt.Fprintf(out, "==> kubernetes: running %s, want %s\n", report.Kubernetes.Running, report.Kubernetes.Want)
	}
}

// renderDiff writes report as a single multi-file unified diff: each
// node's config diff (repathed to a/<node>.yaml, b/<node>.yaml so nodes
// don't collide once concatenated), plus a synthetic diff block per
// drifted non-config check (talos version, extensions, kernel args,
// cluster-wide Kubernetes version). Every block is a real "--- / +++ /
// @@" unified diff, so the whole stream is a valid patch file - openable
// as a .diff in VS Code (or any diff/patch viewer) with correct
// highlighting, even though it spans multiple synthetic "files" rather
// than one real changeset.
func renderDiff(report driftReport, out io.Writer) error {
	for _, n := range report.Nodes {
		if n.ConfigDiff != "" {
			fmt.Fprint(out, repathDiff(n.ConfigDiff, "a/"+n.Name+".yaml", "b/"+n.Name+".yaml"))
		}

		for _, diff := range []func() (string, error){
			func() (string, error) { return n.TalosVersion.diff(n.Name) },
			func() (string, error) { return n.Extensions.diff(n.Name) },
			func() (string, error) { return n.KernelArgs.diff(n.Name) },
		} {
			d, err := diff()
			if err != nil {
				return fmt.Errorf("node %q: rendering diff: %w", n.Name, err)
			}

			fmt.Fprint(out, d)
		}
	}

	d, err := report.Kubernetes.diff()
	if err != nil {
		return fmt.Errorf("rendering kubernetes diff: %w", err)
	}

	fmt.Fprint(out, d)

	return nil
}
