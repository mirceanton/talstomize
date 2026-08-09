package cli

import (
	"slices"
	"testing"
)

// realExtensionsYAML is a trimmed but otherwise verbatim capture of
// `talosctl get extensions -o yaml` against a real node - three real
// extensions plus the two Talos-internal synthetic entries
// (syntheticExtensionNames) that appear on every node regardless of what
// the schematic actually declares.
const realExtensionsYAML = `node: 10.0.0.15
metadata:
    namespace: runtime
    type: ExtensionStatuses.runtime.talos.dev
    id: "0"
    version: 1
spec:
    image: 0.sqsh
    metadata:
        name: amd-ucode
        version: "20260622"
---
node: 10.0.0.15
metadata:
    namespace: runtime
    type: ExtensionStatuses.runtime.talos.dev
    id: "1"
    version: 1
spec:
    image: 1.sqsh
    metadata:
        name: amdgpu
        version: 20260622-v1.13.7
---
node: 10.0.0.15
metadata:
    namespace: runtime
    type: ExtensionStatuses.runtime.talos.dev
    id: "2"
    version: 1
spec:
    image: 2.sqsh
    metadata:
        name: nvidia-container-toolkit-production
        version: 595.71.05-v1.19.1
---
node: 10.0.0.15
metadata:
    namespace: runtime
    type: ExtensionStatuses.runtime.talos.dev
    id: "9"
    version: 1
spec:
    image: 9.sqsh
    metadata:
        name: schematic
---
node: 10.0.0.15
metadata:
    namespace: runtime
    type: ExtensionStatuses.runtime.talos.dev
    id: "10"
    version: 1
spec:
    image: modules.dep.sqsh
    metadata:
        name: modules.dep
`

func TestParseInstalledExtensionsFiltersSyntheticNames(t *testing.T) {
	got, err := parseInstalledExtensions(realExtensionsYAML)
	if err != nil {
		t.Fatalf("parseInstalledExtensions: %v", err)
	}

	want := []string{"amd-ucode", "amdgpu", "nvidia-container-toolkit-production"}
	if !slices.Equal(got, want) {
		t.Errorf("parseInstalledExtensions(...) = %v, want %v (schematic/modules.dep should be filtered)", got, want)
	}
}

func TestParseInstalledExtensionsEmpty(t *testing.T) {
	got, err := parseInstalledExtensions("")
	if err != nil {
		t.Fatalf("parseInstalledExtensions: %v", err)
	}

	if len(got) != 0 {
		t.Errorf("parseInstalledExtensions(\"\") = %v, want empty", got)
	}
}

func TestParseInstalledExtensionsMalformed(t *testing.T) {
	if _, err := parseInstalledExtensions("not: [valid, yaml"); err == nil {
		t.Fatal("parseInstalledExtensions: expected an error for malformed YAML, got nil")
	}
}
