package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// getRunningTalosVersion fetches the Talos OS version currently *running*
// on the node at ip - its live `version` runtime resource (spec.version,
// e.g. "v1.13.7") - not the version implied by its last-applied (and
// possibly stale) machine config.
func getRunningTalosVersion(errOut io.Writer, ip string, passthrough []string) (string, error) {
	args := append([]string{"get", "version", "--nodes", ip, "-o", "yaml"}, passthrough...)

	stdout, err := runTalosctlCaptured(errOut, args)
	if err != nil {
		return "", err
	}

	var resource struct {
		Spec struct {
			Version string `yaml:"version"`
		} `yaml:"spec"`
	}

	if err := yaml.Unmarshal([]byte(stdout), &resource); err != nil {
		return "", fmt.Errorf("parsing version resource: %w", err)
	}

	return resource.Spec.Version, nil
}

// syntheticExtensionNames are entries `talosctl get extensions` always
// reports alongside real, user-declared ones, regardless of what's in a
// schematic's officialExtensions list - "schematic" is Talos's own
// bookkeeping record of the schematic itself, "modules.dep" an
// auto-generated kernel-module dependency layer. Live-verified against a
// real node: both appear even though neither is (or could be) a
// legitimate officialExtensions entry. Excluded so ExtensionDrift's
// "unexpected" check doesn't flag them on every node.
var syntheticExtensionNames = map[string]bool{
	"schematic":   true,
	"modules.dep": true,
}

// getInstalledExtensions fetches the names of every real system extension
// currently installed on the node at ip, from its live `extensions`
// runtime resources - one COSI document per installed extension, unlike
// the single-resource cases above.
func getInstalledExtensions(errOut io.Writer, ip string, passthrough []string) ([]string, error) {
	args := append([]string{"get", "extensions", "--nodes", ip, "-o", "yaml"}, passthrough...)

	stdout, err := runTalosctlCaptured(errOut, args)
	if err != nil {
		return nil, err
	}

	return parseInstalledExtensions(stdout)
}

// parseInstalledExtensions decodes `talosctl get extensions -o yaml`'s
// multi-document YAML stream (one COSI resource per installed extension)
// into a list of names, filtering out Talos-internal synthetic entries
// (see syntheticExtensionNames) so ExtensionDrift's "unexpected" check
// doesn't flag them on every node.
func parseInstalledExtensions(yamlStream string) ([]string, error) {
	dec := yaml.NewDecoder(strings.NewReader(yamlStream))

	var names []string

	for {
		var resource struct {
			Spec struct {
				Metadata struct {
					Name string `yaml:"name"`
				} `yaml:"metadata"`
			} `yaml:"spec"`
		}

		err := dec.Decode(&resource)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("parsing extension resource: %w", err)
		}

		if name := resource.Spec.Metadata.Name; name != "" && !syntheticExtensionNames[name] {
			names = append(names, name)
		}
	}

	return names, nil
}

// getKernelCmdline fetches the live kernel command line (contents of
// /proc/cmdline as actually booted) from the node at ip's `cmdline`
// runtime resource.
func getKernelCmdline(errOut io.Writer, ip string, passthrough []string) (string, error) {
	args := append([]string{"get", "cmdline", "--nodes", ip, "-o", "yaml"}, passthrough...)

	stdout, err := runTalosctlCaptured(errOut, args)
	if err != nil {
		return "", err
	}

	var resource struct {
		Spec struct {
			Cmdline string `yaml:"cmdline"`
		} `yaml:"spec"`
	}

	if err := yaml.Unmarshal([]byte(stdout), &resource); err != nil {
		return "", fmt.Errorf("parsing cmdline resource: %w", err)
	}

	return resource.Spec.Cmdline, nil
}

// getK8sUpgradePlan runs `talosctl upgrade-k8s --to target --dry-run` and
// returns its raw stdout - used only to detect/report whether the
// cluster's current Kubernetes version differs from target, never to
// execute anything.
func getK8sUpgradePlan(errOut io.Writer, target string, passthrough []string) (string, error) {
	args := append([]string{"upgrade-k8s", "--to", target, "--dry-run"}, passthrough...)

	return runTalosctlCaptured(errOut, args)
}
