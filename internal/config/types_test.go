package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, contents string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	return path
}

func TestLoadValid(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, dir, "talstomize.yaml", `
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

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.ClusterName != "test-cluster" {
		t.Errorf("ClusterName = %q, want %q", cfg.ClusterName, "test-cluster")
	}

	if len(cfg.Nodes) != 2 {
		t.Errorf("len(Nodes) = %d, want 2", len(cfg.Nodes))
	}

	wantSecrets := filepath.Join(dir, "talos-secrets.yaml")
	if cfg.SecretsPath() != wantSecrets {
		t.Errorf("SecretsPath() = %q, want %q", cfg.SecretsPath(), wantSecrets)
	}
}

func TestLoadDirectFile(t *testing.T) {
	dir := t.TempDir()

	path := writeFile(t, dir, "custom.yaml", `
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

	if _, err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestLoadValidationErrors(t *testing.T) {
	for name, contents := range map[string]string{
		"missing everything": `
apiVersion: config.talstomize.dev/v1alpha1
kind: Talstomize
`,
		"wrong apiVersion/kind": `
apiVersion: v1
kind: ConfigMap
clusterName: test-cluster
controlPlaneEndpoint: https://10.5.0.2:6443
secrets: ./talos-secrets.yaml
nodes:
  nodea:
    ip: 10.5.0.11
    kind: controlplane
`,
		"bad node kind": `
apiVersion: config.talstomize.dev/v1alpha1
kind: Talstomize
clusterName: test-cluster
controlPlaneEndpoint: https://10.5.0.2:6443
secrets: ./talos-secrets.yaml
nodes:
  nodea:
    ip: 10.5.0.11
    kind: init
`,
		"missing node ip": `
apiVersion: config.talstomize.dev/v1alpha1
kind: Talstomize
clusterName: test-cluster
controlPlaneEndpoint: https://10.5.0.2:6443
secrets: ./talos-secrets.yaml
nodes:
  nodea:
    kind: worker
`,
		"cluster-wide installer.image and installer.schematic both set": `
apiVersion: config.talstomize.dev/v1alpha1
kind: Talstomize
clusterName: test-cluster
controlPlaneEndpoint: https://10.5.0.2:6443
secrets: ./talos-secrets.yaml
installer:
  image: ghcr.io/siderolabs/installer:v1.13.8
  schematic:
    customization: {}
nodes:
  nodea:
    ip: 10.5.0.11
    kind: controlplane
`,
		"node installer.image and installer.schematic both set": `
apiVersion: config.talstomize.dev/v1alpha1
kind: Talstomize
clusterName: test-cluster
controlPlaneEndpoint: https://10.5.0.2:6443
secrets: ./talos-secrets.yaml
nodes:
  nodea:
    ip: 10.5.0.11
    kind: controlplane
    installer:
      image: ghcr.io/siderolabs/installer:v1.13.8
      schematic:
        customization: {}
`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "talstomize.yaml", contents)

			if _, err := Load(dir); err == nil {
				t.Fatal("Load: expected an error, got nil")
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml")); err == nil {
		t.Fatal("Load: expected an error, got nil")
	}
}

func TestLoadEnvsubst(t *testing.T) {
	t.Setenv("TALSTOMIZE_TEST_CLUSTER_NAME", "envsubst-cluster")

	dir := t.TempDir()

	writeFile(t, dir, "talstomize.yaml", `
apiVersion: config.talstomize.dev/v1alpha1
kind: Talstomize
clusterName: ${TALSTOMIZE_TEST_CLUSTER_NAME}
controlPlaneEndpoint: https://10.5.0.2:6443
secrets: ./talos-secrets.yaml
nodes:
  nodea:
    ip: 10.5.0.11
    kind: controlplane
`)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.ClusterName != "envsubst-cluster" {
		t.Errorf("ClusterName = %q, want %q", cfg.ClusterName, "envsubst-cluster")
	}
}

func TestLoadEnvsubstUndefined(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, dir, "talstomize.yaml", `
apiVersion: config.talstomize.dev/v1alpha1
kind: Talstomize
clusterName: ${TALSTOMIZE_TEST_UNDEFINED_CLUSTER_NAME}
controlPlaneEndpoint: https://10.5.0.2:6443
secrets: ./talos-secrets.yaml
nodes:
  nodea:
    ip: 10.5.0.11
    kind: controlplane
`)

	if _, err := Load(dir); err == nil {
		t.Fatal("Load: expected an error for an undefined variable, got nil")
	}
}
