package talos

import (
	"errors"
	"testing"

	"gopkg.in/yaml.v3"

	tstomcfg "github.com/mirceanton/talstomize/internal/config"
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

func TestEffectiveInstallImagePrecedence(t *testing.T) {
	calls := 0

	e := &Engine{
		cfg: &tstomcfg.Config{
			Installer: tstomcfg.Installer{Image: "cluster-wide-image"},
		},
		talosVersion: "v1.13.8",
		resolveSchematic: func(_ []byte) (string, error) {
			calls++

			return "cluster-schematic-id", nil
		},
	}

	// node.Installer.Image wins outright, no schematic call.
	node := tstomcfg.Node{Installer: tstomcfg.NodeInstaller{Image: "node-image"}}

	got, err := e.effectiveInstallImage(node)
	if err != nil {
		t.Fatalf("effectiveInstallImage: %v", err)
	}

	if got != "node-image" {
		t.Errorf("effectiveInstallImage() = %q, want %q", got, "node-image")
	}

	// Nothing set on the node: falls back to the cluster-wide Installer.Image.
	got, err = e.effectiveInstallImage(tstomcfg.Node{})
	if err != nil {
		t.Fatalf("effectiveInstallImage: %v", err)
	}

	if got != "cluster-wide-image" {
		t.Errorf("effectiveInstallImage() = %q, want %q", got, "cluster-wide-image")
	}

	if calls != 0 {
		t.Errorf("resolveSchematic called %d times, want 0 (Installer.Image should win over Installer.Schematic)", calls)
	}
}

func TestEffectiveInstallImageNodeSchematicOverridesClusterInstallImage(t *testing.T) {
	e := &Engine{
		cfg: &tstomcfg.Config{
			Installer: tstomcfg.Installer{Image: "cluster-wide-image"},
		},
		talosVersion: "v1.13.8",
		resolveSchematic: func(raw []byte) (string, error) {
			return "node-schematic-id", nil
		},
	}

	node := tstomcfg.Node{
		Installer: tstomcfg.NodeInstaller{Schematic: yamlNode(t, "customization:\n  extraKernelArgs: [foo]\n")},
	}

	got, err := e.effectiveInstallImage(node)
	if err != nil {
		t.Fatalf("effectiveInstallImage: %v", err)
	}

	want := "factory.talos.dev/metal-installer/node-schematic-id:v1.13.8"
	if got != want {
		t.Errorf("effectiveInstallImage() = %q, want %q", got, want)
	}
}

func TestEffectiveInstallImageClusterSchematic(t *testing.T) {
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

	got, err := e.effectiveInstallImage(tstomcfg.Node{})
	if err != nil {
		t.Fatalf("effectiveInstallImage: %v", err)
	}

	want := "factory.talos.dev/metal-installer/cluster-schematic-id:v1.9.5"
	if got != want {
		t.Errorf("effectiveInstallImage() = %q, want %q", got, want)
	}
}

func TestEffectiveInstallImageNoneSet(t *testing.T) {
	e := &Engine{cfg: &tstomcfg.Config{}, talosVersion: "v1.13.8"}

	got, err := e.effectiveInstallImage(tstomcfg.Node{})
	if err != nil {
		t.Fatalf("effectiveInstallImage: %v", err)
	}

	if got != "" {
		t.Errorf("effectiveInstallImage() = %q, want empty (left for patches)", got)
	}
}

func TestEffectiveInstallImageSchematicError(t *testing.T) {
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

	if _, err := e.effectiveInstallImage(tstomcfg.Node{}); err == nil {
		t.Fatal("effectiveInstallImage: expected an error, got nil")
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
