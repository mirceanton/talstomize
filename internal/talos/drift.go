package talos

import (
	"regexp"
	"strings"
)

var detectedKubernetesVersionRE = regexp.MustCompile(`automatically detected the lowest Kubernetes version (\S+)`)

// ParseKubernetesVersion extracts the auto-detected current Kubernetes
// version from `talosctl upgrade-k8s --dry-run`'s output (the line
// "automatically detected the lowest Kubernetes version X"). Live-verified
// against a real cluster: that line is present on every successful
// dry-run regardless of whether an upgrade is actually needed - the rest
// of the output is an unconditional action trace (component "update"
// steps, manifest-annotation diffs) that appears even when the detected
// version already matches the target, so it's not usable as a drift
// signal on its own. Returns "" if the line isn't found (output format
// changed, or --from somehow wasn't auto-detected).
func ParseKubernetesVersion(dryRunOutput string) string {
	m := detectedKubernetesVersionRE.FindStringSubmatch(dryRunOutput)
	if m == nil {
		return ""
	}

	return m[1]
}

// ImageTag returns the tag component of an OCI image reference ref: the
// substring after the last colon, but only when that colon comes after
// the last slash - a colon before the last slash is a registry port (e.g.
// "registry:5000/image"), not a tag separator. Returns "" if ref has no
// parseable tag (digest reference, or ref itself is "").
func ImageTag(ref string) string {
	if ref == "" {
		return ""
	}

	// A digest suffix (@sha256:...) has its own colon that isn't a tag
	// separator - strip it before hunting for one.
	if at := strings.LastIndex(ref, "@"); at >= 0 {
		ref = ref[:at]
	}

	lastColon := strings.LastIndex(ref, ":")
	lastSlash := strings.LastIndex(ref, "/")

	if lastColon <= lastSlash {
		return ""
	}

	return ref[lastColon+1:]
}

// TalosVersionDrift reports whether a node's currently-running Talos OS
// version (running, e.g. "v1.13.7") differs from the version tag of its
// rendered target install image (targetImage, machine.install.image).
// ok=false means "nothing to compare, not a drift candidate" - targetImage
// is "" (nothing configured; left for patches to set) or has no
// parseable tag - not an error.
func TalosVersionDrift(running, targetImage string) (drifted, ok bool) {
	target := ImageTag(targetImage)
	if target == "" {
		return false, false
	}

	normalize := func(v string) string { return "v" + strings.TrimPrefix(v, "v") }

	return normalize(running) != normalize(target), true
}

// ExtensionDrift compares installed extension names against desired
// (schematic officialExtensions entries), matching on the suffix after
// the last "/" on both sides - so "siderolabs/zfs" and "zfs" compare
// equal regardless of which format either side reports. missing = desired
// but not installed; unexpected = installed but not desired. Both empty
// means no drift.
func ExtensionDrift(installed, desired []string) (missing, unexpected []string) {
	installedShort := make(map[string]bool, len(installed))
	for _, name := range installed {
		installedShort[extensionShortName(name)] = true
	}

	desiredShort := make(map[string]bool, len(desired))
	for _, name := range desired {
		desiredShort[extensionShortName(name)] = true
	}

	for _, name := range desired {
		if !installedShort[extensionShortName(name)] {
			missing = append(missing, name)
		}
	}

	for _, name := range installed {
		if !desiredShort[extensionShortName(name)] {
			unexpected = append(unexpected, name)
		}
	}

	return missing, unexpected
}

func extensionShortName(name string) string {
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		return name[idx+1:]
	}

	return name
}

// KernelArgDrift reports which of desired's extraKernelArgs entries are
// absent from cmdline (the live /proc/cmdline), matched as
// whitespace-delimited tokens. Only checks for missing entries - cmdline
// always contains Talos/kernel boilerplate no schematic declares, so
// "extra" isn't a meaningful signal here (unlike extensions, where the
// schematic is the sole source of truth for what's installed).
func KernelArgDrift(cmdline string, desired []string) (missing []string) {
	tokens := make(map[string]bool)
	for tok := range strings.FieldsSeq(cmdline) {
		tokens[tok] = true
	}

	for _, arg := range desired {
		if !tokens[arg] {
			missing = append(missing, arg)
		}
	}

	return missing
}
