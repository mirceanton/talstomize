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

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()

	t.Cleanup(func() { os.Unsetenv("TALSTOMIZE_TEST_DOTENV_CLUSTER_NAME") })
	writeFile(t, dir, ".env", "TALSTOMIZE_TEST_DOTENV_CLUSTER_NAME=dotenv-cluster\n")

	writeFile(t, dir, "talstomize.yaml", `
apiVersion: config.talstomize.dev/v1alpha1
kind: Talstomize
clusterName: ${TALSTOMIZE_TEST_DOTENV_CLUSTER_NAME}
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

	if cfg.ClusterName != "dotenv-cluster" {
		t.Errorf("ClusterName = %q, want %q", cfg.ClusterName, "dotenv-cluster")
	}
}

func TestLoadDotEnvDoesNotOverrideExisting(t *testing.T) {
	t.Setenv("TALSTOMIZE_TEST_DOTENV_OVERRIDE", "from-process-env")

	dir := t.TempDir()

	writeFile(t, dir, ".env", "TALSTOMIZE_TEST_DOTENV_OVERRIDE=from-dotenv-file\n")

	writeFile(t, dir, "talstomize.yaml", `
apiVersion: config.talstomize.dev/v1alpha1
kind: Talstomize
clusterName: ${TALSTOMIZE_TEST_DOTENV_OVERRIDE}
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

	if cfg.ClusterName != "from-process-env" {
		t.Errorf("ClusterName = %q, want %q (an already-set env var must win over .env)", cfg.ClusterName, "from-process-env")
	}
}

func TestLoadDotEnvMalformed(t *testing.T) {
	dir := t.TempDir()

	// godotenv rejects lines with no "=" and no recognizable export/comment
	// syntax.
	writeFile(t, dir, ".env", "this is not valid dotenv syntax\n")

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
`)

	if _, err := Load(dir); err == nil {
		t.Fatal("Load: expected an error for a malformed .env file, got nil")
	}
}

// Unknown fields - typos, or ones removed from the talstomize.yaml format
// (e.g. installer.image, installer.talosVersion) - must fail loudly instead
// of being silently ignored.
func TestLoadRejectsUnknownFields(t *testing.T) {
	for name, extra := range map[string]string{
		"top-level typo":                 "kubernetesVerison: v1.37.1\n",
		"removed installer.image":        "installer:\n  image: ghcr.io/siderolabs/installer:v1.14.2\n",
		"removed installer.talosVersion": "installer:\n  talosVersion: v1.14.2\n",
		"removed contractVersion":        "contractVersion: v1.13.10\n",
	} {
		t.Run(name, func(t *testing.T) {
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
`+extra)

			if _, err := Load(dir); err == nil {
				t.Fatal("Load: expected an error for an unknown field, got nil")
			}
		})
	}
}

func TestLoadTalosVersion(t *testing.T) {
	for version, wantErr := range map[string]bool{
		"v1.14.2":      false,
		"1.13.8":       false,
		"latest":       true,
		"one.fourteen": true,
	} {
		t.Run(version, func(t *testing.T) {
			dir := t.TempDir()

			writeFile(t, dir, "talstomize.yaml", `
apiVersion: config.talstomize.dev/v1alpha1
kind: Talstomize
clusterName: test-cluster
controlPlaneEndpoint: https://10.5.0.2:6443
secrets: ./talos-secrets.yaml
talosVersion: `+version+`
nodes:
  nodea:
    ip: 10.5.0.11
    kind: controlplane
`)

			_, err := Load(dir)
			if gotErr := err != nil; gotErr != wantErr {
				t.Fatalf("Load: err = %v, want error = %v", err, wantErr)
			}
		})
	}
}

func TestLoadPlatform(t *testing.T) {
	for name, tc := range map[string]struct {
		// cluster and node are the cluster-wide and node platform fields, "" for unset.
		cluster, node string
		// want is the node's effective platform, "" if Load must fail.
		want string
	}{
		"unset defaults to metal": {want: "metal"},
		"cluster-wide":            {cluster: "hcloud", want: "hcloud"},
		"node override":           {cluster: "hcloud", node: "aws", want: "aws"},
		"unknown cluster-wide":    {cluster: "hetzner"},
		"unknown node":            {node: "hetzner"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()

			var cluster, node string
			if tc.cluster != "" {
				cluster = "platform: " + tc.cluster + "\n"
			}

			if tc.node != "" {
				node = "    platform: " + tc.node + "\n"
			}

			writeFile(t, dir, "talstomize.yaml", `
apiVersion: config.talstomize.dev/v1alpha1
kind: Talstomize
clusterName: test-cluster
controlPlaneEndpoint: https://10.5.0.2:6443
secrets: ./talos-secrets.yaml
`+cluster+`nodes:
  nodea:
    ip: 10.5.0.11
    kind: controlplane
`+node)

			cfg, err := Load(dir)
			if tc.want == "" {
				if err == nil {
					t.Fatal("Load: expected an error for an unknown platform, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("Load: %v", err)
			}

			if got := cfg.NodePlatform(cfg.Nodes["nodea"]); got != tc.want {
				t.Errorf("NodePlatform() = %q, want %q", got, tc.want)
			}
		})
	}
}
