package talos_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"gopkg.in/yaml.v3"

	"github.com/mirceanton/talstomize/internal/config"
	"github.com/mirceanton/talstomize/internal/talos"
)

func writeFile(t *testing.T, path, contents string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// writeSecretsBundle generates a self-contained secrets bundle (no network,
// no talosctl required) and writes it in the same format `talosctl gen
// secrets` produces.
func writeSecretsBundle(t *testing.T, dir string) {
	t.Helper()

	bundle, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), tconfig.TalosVersionCurrent)
	if err != nil {
		t.Fatalf("generating secrets bundle: %v", err)
	}

	f, err := os.Create(filepath.Join(dir, "talos-secrets.yaml"))
	if err != nil {
		t.Fatalf("creating secrets file: %v", err)
	}
	defer f.Close() //nolint:errcheck

	if err := yaml.NewEncoder(f).Encode(bundle); err != nil {
		t.Fatalf("encoding secrets bundle: %v", err)
	}
}

func TestEngineRenderNode(t *testing.T) {
	dir := t.TempDir()

	writeSecretsBundle(t, dir)

	writeFile(t, filepath.Join(dir, "patches", "nodea-disk.yaml"), "machine:\n  install:\n    disk: /dev/sda\n")
	writeFile(t, filepath.Join(dir, "patches", "controlplane-common.yaml"), "cluster:\n  allowSchedulingOnControlPlanes: true\n")

	writeFile(t, filepath.Join(dir, "talstomize.yaml"), `
apiVersion: config.talstomize.dev/v1alpha1
kind: Talstomize
clusterName: test-cluster
controlPlaneEndpoint: https://10.5.0.2:6443
secrets: ./talos-secrets.yaml
nodes:
  nodea:
    ip: 10.5.0.11
    kind: controlplane
    patches:
      - ./patches/nodea-disk.yaml
  nodeb:
    ip: 10.5.0.12
    kind: worker
    patches:
      - machine:
          kubelet:
            extraMounts:
              - destination: /var/lib/longhorn
                type: bind
                source: /var/lib/longhorn
                options: ["bind", "rshared", "rw"]
controlplanePatches:
  - ./patches/controlplane-common.yaml
workerPatches: []
`)

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	engine, err := talos.NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	if got, want := engine.NodeNames(""), []string{"nodea", "nodeb"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("NodeNames(\"\") = %v, want %v", got, want)
	}

	nodea, err := engine.RenderNode("nodea")
	if err != nil {
		t.Fatalf("RenderNode(nodea): %v", err)
	}

	nodeaYAML := mustString(t, nodea)

	for _, want := range []string{"hostname: nodea", "disk: /dev/sda", "allowSchedulingOnControlPlanes: true"} {
		if !hasSetLine(nodeaYAML, want) {
			t.Errorf("nodea config missing %q as a set (uncommented) line", want)
		}
	}

	nodeb, err := engine.RenderNode("nodeb")
	if err != nil {
		t.Fatalf("RenderNode(nodeb): %v", err)
	}

	nodebYAML := mustString(t, nodeb)

	for _, want := range []string{"hostname: nodeb", "destination: /var/lib/longhorn"} {
		if !hasSetLine(nodebYAML, want) {
			t.Errorf("nodeb config missing %q as a set (uncommented) line", want)
		}
	}

	// The controlplane-only and nodea-only patches must not leak onto nodeb.
	for _, unwanted := range []string{"disk: /dev/sda", "allowSchedulingOnControlPlanes: true"} {
		if hasSetLine(nodebYAML, unwanted) {
			t.Errorf("nodeb config unexpectedly has %q as a set (uncommented) line", unwanted)
		}
	}
}

// hasSetLine reports whether yaml contains needle on a line that isn't a
// comment. Talos machine configs document every field with a commented-out
// example value (e.g. "# disk: /dev/sda"), so a plain substring match can't
// tell an actually-applied patch from its own documentation.
func hasSetLine(yaml, needle string) bool {
	for line := range strings.SplitSeq(yaml, "\n") {
		if strings.Contains(line, needle) && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			return true
		}
	}

	return false
}

// TestEngineRenderNodeSopsSecrets exercises the full pipeline with a
// sops-encrypted secrets bundle: generate a plaintext bundle, encrypt it
// with a disposable test-only age key (testdata/age-key.txt, used for
// nothing else), point talstomize.yaml at the encrypted file, and confirm
// NewEngine/RenderNode transparently decrypt it. Requires sops on PATH;
// skipped otherwise (see .mise.toml, where it's pinned for CI).
func TestEngineRenderNodeSopsSecrets(t *testing.T) {
	if _, err := exec.LookPath("sops"); err != nil {
		t.Skip("sops not found on PATH")
	}

	const recipient = "age1a6eapksmqtlc2a88a30s8j8e5ts5hn7c8hqpj6rcrzzx2xwryc8qj8hrp5"

	keyPath, err := filepath.Abs("testdata/age-key.txt")
	if err != nil {
		t.Fatalf("resolving key path: %v", err)
	}

	t.Setenv("SOPS_AGE_KEY_FILE", keyPath)

	plainDir := t.TempDir()
	writeSecretsBundle(t, plainDir)

	encrypted, err := exec.Command("sops", "--encrypt", "--age", recipient, filepath.Join(plainDir, "talos-secrets.yaml")).Output()
	if err != nil {
		t.Fatalf("sops --encrypt: %v", err)
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "talos-secrets.yaml"), string(encrypted))

	writeFile(t, filepath.Join(dir, "talstomize.yaml"), `
apiVersion: config.talstomize.dev/v1alpha1
kind: Talstomize
clusterName: test-cluster
controlPlaneEndpoint: https://10.5.0.2:6443
secrets: ./talos-secrets.yaml
nodes:
  nodea:
    ip: 10.5.0.11
    kind: controlplane
controlplanePatches: []
workerPatches: []
`)

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	engine, err := talos.NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	if _, err := engine.RenderNode("nodea"); err != nil {
		t.Fatalf("RenderNode(nodea): %v", err)
	}
}

func TestEngineRenderNodeEnvsubst(t *testing.T) {
	t.Setenv("TALSTOMIZE_TEST_REGISTRY_USER", "envsubst-user")

	dir := t.TempDir()

	writeSecretsBundle(t, dir)

	writeFile(t, filepath.Join(dir, "patches", "registry.yaml"),
		"machine:\n  registries:\n    config:\n      example.com:\n        auth:\n          username: ${TALSTOMIZE_TEST_REGISTRY_USER}\n")

	writeFile(t, filepath.Join(dir, "talstomize.yaml"), `
apiVersion: config.talstomize.dev/v1alpha1
kind: Talstomize
clusterName: test-cluster
controlPlaneEndpoint: https://10.5.0.2:6443
secrets: ./talos-secrets.yaml
nodes:
  nodea:
    ip: 10.5.0.11
    kind: controlplane
    patches:
      - ./patches/registry.yaml
controlplanePatches: []
workerPatches: []
`)

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	engine, err := talos.NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	nodea, err := engine.RenderNode("nodea")
	if err != nil {
		t.Fatalf("RenderNode(nodea): %v", err)
	}

	if !hasSetLine(mustString(t, nodea), "username: envsubst-user") {
		t.Errorf("nodea config missing expanded registry username")
	}
}

func TestEngineRenderNodeEnvsubstUndefined(t *testing.T) {
	dir := t.TempDir()

	writeSecretsBundle(t, dir)

	writeFile(t, filepath.Join(dir, "patches", "registry.yaml"),
		"machine:\n  registries:\n    config:\n      example.com:\n        auth:\n          username: ${TALSTOMIZE_TEST_REGISTRY_USER_UNDEFINED}\n")

	writeFile(t, filepath.Join(dir, "talstomize.yaml"), `
apiVersion: config.talstomize.dev/v1alpha1
kind: Talstomize
clusterName: test-cluster
controlPlaneEndpoint: https://10.5.0.2:6443
secrets: ./talos-secrets.yaml
nodes:
  nodea:
    ip: 10.5.0.11
    kind: controlplane
    patches:
      - ./patches/registry.yaml
controlplanePatches: []
workerPatches: []
`)

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	engine, err := talos.NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	if _, err := engine.RenderNode("nodea"); err == nil {
		t.Fatal("RenderNode(nodea): expected an error for an undefined variable, got nil")
	}
}

func TestEngineRenderNodeUnknown(t *testing.T) {
	dir := t.TempDir()

	writeSecretsBundle(t, dir)
	writeFile(t, filepath.Join(dir, "talstomize.yaml"), `
apiVersion: config.talstomize.dev/v1alpha1
kind: Talstomize
clusterName: test-cluster
controlPlaneEndpoint: https://10.5.0.2:6443
secrets: ./talos-secrets.yaml
nodes:
  nodea:
    ip: 10.5.0.11
    kind: controlplane
`)

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	engine, err := talos.NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	if _, err := engine.RenderNode("does-not-exist"); err == nil {
		t.Fatal("RenderNode: expected an error for an unknown node, got nil")
	}
}

func mustString(t *testing.T, provider tconfig.Provider) string {
	t.Helper()

	b, err := provider.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}

	return string(b)
}
