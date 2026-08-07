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
patches:
  - machine:
      sysctls:
        net.core.somaxconn: "1024"
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

	for _, want := range []string{"hostname: nodea", "disk: /dev/sda", "allowSchedulingOnControlPlanes: true", "net.core.somaxconn:"} {
		if !hasSetLine(nodeaYAML, want) {
			t.Errorf("nodea config missing %q as a set (uncommented) line", want)
		}
	}

	nodeb, err := engine.RenderNode("nodeb")
	if err != nil {
		t.Fatalf("RenderNode(nodeb): %v", err)
	}

	nodebYAML := mustString(t, nodeb)

	for _, want := range []string{"hostname: nodeb", "destination: /var/lib/longhorn", "net.core.somaxconn:"} {
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

func TestEngineRenderNodeAdditionalSubjectAltNames(t *testing.T) {
	dir := t.TempDir()

	writeSecretsBundle(t, dir)

	writeFile(t, filepath.Join(dir, "talstomize.yaml"), `
apiVersion: config.talstomize.dev/v1alpha1
kind: Talstomize
clusterName: test-cluster
controlPlaneEndpoint: https://10.5.0.2:6443
secrets: ./talos-secrets.yaml
additionalSubjectAltNames:
  - cluster.example.com
  - 10.5.0.99
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

	nodea, err := engine.RenderNode("nodea")
	if err != nil {
		t.Fatalf("RenderNode(nodea): %v", err)
	}

	nodeaYAML := mustString(t, nodea)

	// Both the machine cert and the kube-apiserver cert should carry the
	// additional SANs - talosctl's --additional-sans covers both.
	for _, want := range []string{"cluster.example.com", "10.5.0.99"} {
		if strings.Count(nodeaYAML, want) < 2 {
			t.Errorf("nodea config should contain %q at least twice (machine.certSANs and cluster.apiServer.certSANs), got:\n%s", want, nodeaYAML)
		}
	}
}

func TestEngineRenderNodeInstallImage(t *testing.T) {
	dir := t.TempDir()

	writeSecretsBundle(t, dir)

	writeFile(t, filepath.Join(dir, "talstomize.yaml"), `
apiVersion: config.talstomize.dev/v1alpha1
kind: Talstomize
clusterName: test-cluster
controlPlaneEndpoint: https://10.5.0.2:6443
secrets: ./talos-secrets.yaml
installer:
  image: ghcr.io/siderolabs/installer:v1.9.5
nodes:
  nodea:
    ip: 10.5.0.11
    kind: controlplane
    installer:
      image: factory.talos.dev/metal-installer/abc123:v1.9.5
  nodeb:
    ip: 10.5.0.12
    kind: worker
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

	nodeaYAML := mustString(t, mustRenderNode(t, engine, "nodea"))

	if !hasSetLine(nodeaYAML, "image: factory.talos.dev/metal-installer/abc123:v1.9.5") {
		t.Errorf("nodea config should use its own installer.image override, got:\n%s", nodeaYAML)
	}

	if hasSetLine(nodeaYAML, "image: ghcr.io/siderolabs/installer:v1.9.5") {
		t.Errorf("nodea config should not fall back to the cluster-wide installer.image once overridden, got:\n%s", nodeaYAML)
	}

	nodebYAML := mustString(t, mustRenderNode(t, engine, "nodeb"))

	if !hasSetLine(nodebYAML, "image: ghcr.io/siderolabs/installer:v1.9.5") {
		t.Errorf("nodeb config should fall back to the cluster-wide installer.image, got:\n%s", nodebYAML)
	}
}

func mustRenderNode(t *testing.T, engine *talos.Engine, name string) tconfig.Provider {
	t.Helper()

	rendered, err := engine.RenderNode(name)
	if err != nil {
		t.Fatalf("RenderNode(%s): %v", name, err)
	}

	return rendered
}

func TestEngineRenderNodeDNSDomain(t *testing.T) {
	dir := t.TempDir()

	writeSecretsBundle(t, dir)

	writeFile(t, filepath.Join(dir, "talstomize.yaml"), `
apiVersion: config.talstomize.dev/v1alpha1
kind: Talstomize
clusterName: test-cluster
controlPlaneEndpoint: https://10.5.0.2:6443
secrets: ./talos-secrets.yaml
dnsDomain: cluster.internal
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

	nodea, err := engine.RenderNode("nodea")
	if err != nil {
		t.Fatalf("RenderNode(nodea): %v", err)
	}

	nodeaYAML := mustString(t, nodea)

	if !hasSetLine(nodeaYAML, "dnsDomain: cluster.internal") {
		t.Errorf("nodea config missing dnsDomain override, got:\n%s", nodeaYAML)
	}

	if strings.Contains(nodeaYAML, "cluster.local") {
		t.Errorf("nodea config should not fall back to the default cluster.local once dnsDomain is set, got:\n%s", nodeaYAML)
	}
}

func TestEngineRenderNodeDNSDomainDefault(t *testing.T) {
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

	if !hasSetLine(mustString(t, nodea), "dnsDomain: cluster.local") {
		t.Errorf("nodea config should keep the default cluster.local dnsDomain when unset")
	}
}

func TestEngineTalosconfig(t *testing.T) {
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
  nodeb:
    ip: 10.5.0.12
    kind: worker
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

	talosconfig, err := engine.Talosconfig()
	if err != nil {
		t.Fatalf("Talosconfig: %v", err)
	}

	if talosconfig.Context != "test-cluster" {
		t.Errorf("Talosconfig().Context = %q, want %q", talosconfig.Context, "test-cluster")
	}

	ctx, ok := talosconfig.Contexts["test-cluster"]
	if !ok {
		t.Fatalf("Talosconfig().Contexts missing %q, got %v", "test-cluster", talosconfig.Contexts)
	}

	// Only the controlplane node's IP is a valid API endpoint, not the worker's.
	if got, want := strings.Join(ctx.Endpoints, ","), "10.5.0.11"; got != want {
		t.Errorf("Talosconfig endpoints = %q, want %q", got, want)
	}

	if _, err := talosconfig.Bytes(); err != nil {
		t.Errorf("Talosconfig().Bytes(): %v", err)
	}
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

// TestEngineRenderNodeURLPatch checks that a patch entry parsed as a URL is
// actually routed to network fetching rather than treated as a literal
// file path. It doesn't re-verify the fetch mechanics themselves (success,
// 404 handling, https enforcement) - those are covered directly in
// internal/source's own tests against a real httptest TLS server, which
// this package-external test can't reach (its unexported http client isn't
// visible here). ".invalid" is reserved by RFC 2606 to never resolve, so
// this fails fast and deterministically with no real network dependency.
func TestEngineRenderNodeURLPatch(t *testing.T) {
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
    patches:
      - https://talstomize.invalid/patch.yaml
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

	_, err = engine.RenderNode("nodea")
	if err == nil {
		t.Fatal("RenderNode: expected an error fetching an unreachable URL patch, got nil")
	}

	if !strings.Contains(err.Error(), "fetching") {
		t.Errorf("RenderNode error = %q, want it to mention fetching (i.e. the patch was treated as a URL, not a file path)", err)
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
