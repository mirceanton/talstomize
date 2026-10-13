package talos

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"gopkg.in/yaml.v3"

	tstomcfg "github.com/mirceanton/talstomize/internal/config"
	"github.com/mirceanton/talstomize/internal/factory"
)

func yamlNode(t *testing.T, doc string) yaml.Node {
	t.Helper()

	var n yaml.Node
	if err := yaml.Unmarshal([]byte(doc), &n); err != nil {
		t.Fatalf("unmarshaling %q: %v", doc, err)
	}

	if len(n.Content) != 1 {
		t.Fatalf("unmarshaling %q: expected a single document node", doc)
	}

	return *n.Content[0]
}

func TestInstallImageNodeSchematicOverridesCluster(t *testing.T) {
	e := &Engine{
		cfg: &tstomcfg.Config{
			Installer: tstomcfg.Installer{Schematic: yamlNode(t, "customization:\n  extraKernelArgs: [cluster]\n")},
		},
		talosVersion: "v1.13.8",
		resolveSchematic: func(raw []byte) (string, error) {
			if strings.Contains(string(raw), "cluster") {
				return "cluster-schematic-id", nil
			}

			return "node-schematic-id", nil
		},
	}

	node := tstomcfg.Node{
		Installer: tstomcfg.Installer{Schematic: yamlNode(t, "customization:\n  extraKernelArgs: [node]\n")},
	}

	got, err := e.InstallImage(node)
	if err != nil {
		t.Fatalf("InstallImage: %v", err)
	}

	want := "factory.talos.dev/metal-installer/node-schematic-id:v1.13.8"
	if got != want {
		t.Errorf("InstallImage() = %q, want %q", got, want)
	}
}

func TestInstallImageClusterSchematic(t *testing.T) {
	e := &Engine{
		cfg: &tstomcfg.Config{
			Installer: tstomcfg.Installer{
				Schematic: yamlNode(t, "customization:\n  systemExtensions:\n    officialExtensions: [siderolabs/zfs]\n"),
			},
		},
		talosVersion: "v1.9.5",
		resolveSchematic: func(_ []byte) (string, error) {
			return "cluster-schematic-id", nil
		},
	}

	got, err := e.InstallImage(tstomcfg.Node{})
	if err != nil {
		t.Fatalf("InstallImage: %v", err)
	}

	want := "factory.talos.dev/metal-installer/cluster-schematic-id:v1.9.5"
	if got != want {
		t.Errorf("InstallImage() = %q, want %q", got, want)
	}
}

func TestInstallImageNoneSetUsesVanillaSchematic(t *testing.T) {
	e := &Engine{
		cfg:          &tstomcfg.Config{},
		talosVersion: "v1.13.8",
		resolveSchematic: func(_ []byte) (string, error) {
			t.Fatal("resolveSchematic called, want the vanilla schematic ID without a Factory round trip")

			return "", nil
		},
	}

	got, err := e.InstallImage(tstomcfg.Node{})
	if err != nil {
		t.Fatalf("InstallImage: %v", err)
	}

	want := "factory.talos.dev/metal-installer/" + factory.VanillaSchematicID + ":v1.13.8"
	if got != want {
		t.Errorf("InstallImage() = %q, want %q", got, want)
	}
}

func TestInstallImageSchematicError(t *testing.T) {
	e := &Engine{
		cfg: &tstomcfg.Config{
			Installer: tstomcfg.Installer{
				Schematic: yamlNode(t, "customization: {}\n"),
			},
		},
		talosVersion: "v1.13.8",
		resolveSchematic: func(_ []byte) (string, error) {
			return "", errors.New("factory unreachable")
		},
	}

	if _, err := e.InstallImage(tstomcfg.Node{}); err == nil {
		t.Fatal("InstallImage: expected an error, got nil")
	}
}

func TestEffectiveCustomizationNodeSchematic(t *testing.T) {
	e := &Engine{cfg: &tstomcfg.Config{}}

	node := tstomcfg.Node{
		Installer: tstomcfg.Installer{
			Schematic: yamlNode(t, "customization:\n  extraKernelArgs: [foo=1]\n  systemExtensions:\n    officialExtensions: [siderolabs/zfs]\n"),
		},
	}

	got, err := e.EffectiveCustomization(node)
	if err != nil {
		t.Fatalf("EffectiveCustomization: %v", err)
	}

	if want := []string{"foo=1"}; !slices.Equal(got.Customization.ExtraKernelArgs, want) {
		t.Errorf("ExtraKernelArgs = %v, want %v", got.Customization.ExtraKernelArgs, want)
	}

	if want := []string{"siderolabs/zfs"}; !slices.Equal(got.Customization.SystemExtensions.OfficialExtensions, want) {
		t.Errorf("OfficialExtensions = %v, want %v", got.Customization.SystemExtensions.OfficialExtensions, want)
	}
}

func TestEffectiveCustomizationClusterSchematic(t *testing.T) {
	e := &Engine{
		cfg: &tstomcfg.Config{
			Installer: tstomcfg.Installer{
				Schematic: yamlNode(t, "customization:\n  systemExtensions:\n    officialExtensions: [siderolabs/zfs]\n"),
			},
		},
	}

	got, err := e.EffectiveCustomization(tstomcfg.Node{})
	if err != nil {
		t.Fatalf("EffectiveCustomization: %v", err)
	}

	if want := []string{"siderolabs/zfs"}; !slices.Equal(got.Customization.SystemExtensions.OfficialExtensions, want) {
		t.Errorf("OfficialExtensions = %v, want %v", got.Customization.SystemExtensions.OfficialExtensions, want)
	}
}

func TestEffectiveCustomizationNodeSchematicOverridesCluster(t *testing.T) {
	e := &Engine{
		cfg: &tstomcfg.Config{
			Installer: tstomcfg.Installer{
				Schematic: yamlNode(t, "customization:\n  systemExtensions:\n    officialExtensions: [siderolabs/zfs]\n"),
			},
		},
	}

	node := tstomcfg.Node{
		Installer: tstomcfg.Installer{
			Schematic: yamlNode(t, "customization:\n  systemExtensions:\n    officialExtensions: [siderolabs/nvidia-container-toolkit-production]\n"),
		},
	}

	got, err := e.EffectiveCustomization(node)
	if err != nil {
		t.Fatalf("EffectiveCustomization: %v", err)
	}

	want := []string{"siderolabs/nvidia-container-toolkit-production"}
	if !slices.Equal(got.Customization.SystemExtensions.OfficialExtensions, want) {
		t.Errorf("OfficialExtensions = %v, want %v (node schematic should win)", got.Customization.SystemExtensions.OfficialExtensions, want)
	}
}

func TestEffectiveCustomizationNoneSet(t *testing.T) {
	e := &Engine{cfg: &tstomcfg.Config{}}

	got, err := e.EffectiveCustomization(tstomcfg.Node{})
	if err != nil {
		t.Fatalf("EffectiveCustomization: %v", err)
	}

	if len(got.Customization.ExtraKernelArgs) != 0 || len(got.Customization.SystemExtensions.OfficialExtensions) != 0 {
		t.Errorf("EffectiveCustomization() = %+v, want empty (vanilla schematic)", got)
	}
}

func TestResolveSchematicImageMemoizes(t *testing.T) {
	calls := 0

	e := &Engine{
		talosVersion: "v1.13.8",
		resolveSchematic: func(_ []byte) (string, error) {
			calls++

			return "schematic-id", nil
		},
	}

	schematic := yamlNode(t, "customization:\n  extraKernelArgs: [foo]\n")

	for range 3 {
		if _, err := e.resolveSchematicImage(schematic); err != nil {
			t.Fatalf("resolveSchematicImage: %v", err)
		}
	}

	if calls != 1 {
		t.Errorf("resolveSchematic called %d times, want 1 (should be memoized)", calls)
	}
}

// A node's own installer replaces the cluster-wide one wholesale when
// rendering, and both are tagged with the cluster-wide talosVersion.
func TestRenderNodeNodeInstallerOverride(t *testing.T) {
	dir := t.TempDir()

	bundle, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), tconfig.TalosVersionCurrent)
	if err != nil {
		t.Fatalf("generating secrets bundle: %v", err)
	}

	raw, err := yaml.Marshal(bundle)
	if err != nil {
		t.Fatalf("encoding secrets bundle: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "talos-secrets.yaml"), raw, 0o600); err != nil {
		t.Fatalf("writing secrets bundle: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "talstomize.yaml"), []byte(`
apiVersion: config.talstomize.dev/v1alpha1
kind: Talstomize
clusterName: test-cluster
controlPlaneEndpoint: https://10.5.0.2:6443
secrets: ./talos-secrets.yaml
talosVersion: v1.14.2
installer:
  schematic:
    customization:
      systemExtensions:
        officialExtensions: [siderolabs/zfs]
nodes:
  gpu:
    ip: 10.5.0.11
    kind: controlplane
    installer:
      schematic:
        customization:
          systemExtensions:
            officialExtensions: [siderolabs/nvidia-container-toolkit-production]
  plain:
    ip: 10.5.0.12
    kind: worker
`), 0o600); err != nil {
		t.Fatalf("writing talstomize.yaml: %v", err)
	}

	cfg, err := tstomcfg.Load(dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	e, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	e.resolveSchematic = func(raw []byte) (string, error) {
		if strings.Contains(string(raw), "nvidia") {
			return "gpu-schematic-id", nil
		}

		return "cluster-schematic-id", nil
	}

	for node, want := range map[string]string{
		"gpu":   "factory.talos.dev/metal-installer/gpu-schematic-id:v1.14.2",
		"plain": "factory.talos.dev/metal-installer/cluster-schematic-id:v1.14.2",
	} {
		rendered, err := e.RenderNode(node)
		if err != nil {
			t.Fatalf("RenderNode(%s): %v", node, err)
		}

		if got := InstallImage(rendered); got != want {
			t.Errorf("%s install image = %q, want %q", node, got, want)
		}
	}
}
