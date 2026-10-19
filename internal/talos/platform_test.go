package talos_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirceanton/talstomize/internal/config"
	"github.com/mirceanton/talstomize/internal/factory"
	"github.com/mirceanton/talstomize/internal/talos"
)

func loadEngine(t *testing.T, fields string) (*config.Config, *talos.Engine) {
	t.Helper()

	dir := t.TempDir()

	writeSecretsBundle(t, dir)

	writeFile(t, filepath.Join(dir, "talstomize.yaml"), `
apiVersion: config.talstomize.dev/v1alpha1
kind: Talstomize
clusterName: test-cluster
controlPlaneEndpoint: https://10.5.0.2:6443
secrets: ./talos-secrets.yaml
`+fields)

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	engine, err := talos.NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	return cfg, engine
}

// A cloud node boots from its platform's disk image instead of being
// installed, so it gets that platform's installer image and is validated in
// cloud mode, without an install disk.
func TestRenderNodePlatform(t *testing.T) {
	vanilla := func(platform, version string) string {
		return factory.InstallerImage(platform, factory.VanillaSchematicID, version)
	}

	for _, tc := range []struct {
		name   string
		fields string
		// want is the install image the rendered config declares, "" for none.
		want string
	}{
		{
			name: "cloud node gets no install config of its own",
			fields: `talosVersion: v1.14.2
platform: hcloud
nodes:
  nodea:
    ip: 10.5.0.11
    kind: controlplane
`,
			want: "",
		},
		{
			name: "cloud node with an install config patch gets its platform's installer",
			fields: `talosVersion: v1.14.2
platform: hcloud
nodes:
  nodea:
    ip: 10.5.0.11
    kind: controlplane
    patches:
      - apiVersion: v1alpha1
        kind: UnattendedInstallConfig
        provisioning:
          diskSelector:
            match: disk.dev_path == "/dev/sda"
`,
			want: vanilla("hcloud", "v1.14.2"),
		},
		{
			name: "classic cloud node keeps machine.install, with its platform's installer",
			fields: `talosVersion: v1.13.8
platform: aws
nodes:
  nodea:
    ip: 10.5.0.11
    kind: controlplane
`,
			want: vanilla("aws", "v1.13.8"),
		},
		{
			name: "node platform overrides the cluster-wide one",
			fields: `talosVersion: v1.14.2
platform: hcloud
nodes:
  nodea:
    ip: 10.5.0.11
    kind: controlplane
    platform: metal
    patches:
      - apiVersion: v1alpha1
        kind: UnattendedInstallConfig
        provisioning:
          diskSelector:
            match: disk.dev_path == "/dev/sda"
`,
			want: vanilla("metal", "v1.14.2"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, engine := loadEngine(t, tc.fields)

			rendered := mustRenderNode(t, engine, "nodea")

			if got := talos.InstallImage(rendered); got != tc.want {
				t.Errorf("install image = %q, want %q", got, tc.want)
			}

			if _, err := talos.Validate(rendered, engine.Platform(cfg.Nodes["nodea"])); err != nil {
				t.Errorf("Validate: %v", err)
			}
		})
	}
}

func TestNewEnginePlatformTooOld(t *testing.T) {
	dir := t.TempDir()

	writeSecretsBundle(t, dir)

	writeFile(t, filepath.Join(dir, "talstomize.yaml"), `
apiVersion: config.talstomize.dev/v1alpha1
kind: Talstomize
clusterName: test-cluster
controlPlaneEndpoint: https://10.5.0.2:6443
secrets: ./talos-secrets.yaml
talosVersion: v1.13.8
nodes:
  nodea:
    ip: 10.5.0.11
    kind: controlplane
    platform: alibabacloud
`)

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	_, err = talos.NewEngine(cfg)
	if err == nil || !strings.Contains(err.Error(), `platform "alibabacloud" requires Talos`) {
		t.Errorf("NewEngine error = %v, want one naming the platform's minimum Talos version", err)
	}
}
