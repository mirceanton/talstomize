package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// runTalosctlStreaming runs talosctl with args, streaming stdout/stderr
// live to out/errOut and connecting os.Stdin - for long-running or
// interactive subcommands (apply-config, upgrade, upgrade-k8s) where the
// caller wants to watch progress as it happens rather than parse a result.
func runTalosctlStreaming(out, errOut io.Writer, args []string) error {
	run := exec.Command("talosctl", args...)
	run.Stdout = out
	run.Stderr = errOut
	run.Stdin = os.Stdin

	if err := run.Run(); err != nil {
		return fmt.Errorf("talosctl %s: %w", subcommandLabel(args), err)
	}

	return nil
}

// runTalosctlCaptured runs talosctl with args, streaming only stderr live
// and returning stdout as a string for the caller to parse - for
// subcommands whose output needs to be read as a unit (get machineconfig,
// get version, get extensions, get cmdline, upgrade-k8s --dry-run).
func runTalosctlCaptured(errOut io.Writer, args []string) (string, error) {
	var stdout bytes.Buffer

	run := exec.Command("talosctl", args...)
	run.Stdout = &stdout
	run.Stderr = errOut

	if err := run.Run(); err != nil {
		return "", fmt.Errorf("talosctl %s: %w", subcommandLabel(args), err)
	}

	return stdout.String(), nil
}

// subcommandLabel returns a short, human-readable label for an error
// message - the leading non-flag tokens of args, e.g. "apply-config" or
// "get machineconfig", not the full argument list (which would include
// node IPs and file paths).
func subcommandLabel(args []string) string {
	var words []string

	for _, a := range args {
		if strings.HasPrefix(a, "-") || len(words) >= 2 {
			break
		}

		words = append(words, a)
	}

	return strings.Join(words, " ")
}
