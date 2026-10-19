package talos_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirceanton/talstomize/internal/config"
	"github.com/mirceanton/talstomize/internal/talos"
)

func TestValidate(t *testing.T) {
	for _, tc := range []struct {
		name string
		// fields are extra top-level talstomize.yaml fields.
		fields string
		// want is a substring of the expected error, "" if the config is valid.
		want string
	}{
		{
			name: "patches matching the config style",
			fields: `talosVersion: v1.14.2
patches:
  - apiVersion: v1alpha1
    kind: UnattendedInstallConfig
    provisioning:
      diskSelector:
        match: disk.dev_path == "/dev/sda"
`,
		},
		{
			name: "classic patches on a classic config",
			fields: `talosVersion: v1.13.10
patches:
  - machine:
      install:
        disk: /dev/sda
    cluster:
      allowSchedulingOnControlPlanes: true
`,
		},
		{
			// The #21 case: a classic-field patch on the multi-document config style.
			name: "classic patches on a multi-document config",
			fields: `talosVersion: v1.14.2
patches:
  - machine:
      install:
        disk: /dev/sda
    cluster:
      allowSchedulingOnControlPlanes: true
`,
			want: "allowSchedulingOnControlPlanes is already set",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()

			writeSecretsBundle(t, dir)

			writeFile(t, filepath.Join(dir, "talstomize.yaml"), `
apiVersion: config.talstomize.dev/v1alpha1
kind: Talstomize
clusterName: test-cluster
controlPlaneEndpoint: https://10.5.0.2:6443
secrets: ./talos-secrets.yaml
`+tc.fields+`nodes:
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

			_, err = talos.Validate(mustRenderNode(t, engine, "nodea"), engine.Platform(cfg.Nodes["nodea"]))

			switch {
			case tc.want == "" && err != nil:
				t.Errorf("Validate: unexpected error: %v", err)
			case tc.want != "" && err == nil:
				t.Errorf("Validate: expected an error naming %q, got nil", tc.want)
			case tc.want != "" && !strings.Contains(err.Error(), tc.want):
				t.Errorf("Validate error = %q, want it to name %q", err, tc.want)
			}
		})
	}
}
