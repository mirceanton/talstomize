package talos_test

import (
	"slices"
	"testing"

	"github.com/mirceanton/talstomize/internal/talos"
)

func TestImageTag(t *testing.T) {
	for name, tc := range map[string]struct {
		ref  string
		want string
	}{
		"bare tag":                 {"ghcr.io/siderolabs/installer:v1.13.8", "v1.13.8"},
		"schematic image with tag": {"factory.talos.dev/metal-installer/abc123:v1.13.8", "v1.13.8"},
		"registry port, no tag":    {"registry:5000/image", ""},
		"registry port, with tag":  {"registry:5000/image:v1.0.0", "v1.0.0"},
		"digest reference":         {"ghcr.io/siderolabs/installer@sha256:abcdef", ""},
		"empty":                    {"", ""},
		"no colon at all":          {"ghcr.io/siderolabs/installer", ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := talos.ImageTag(tc.ref); got != tc.want {
				t.Errorf("ImageTag(%q) = %q, want %q", tc.ref, got, tc.want)
			}
		})
	}
}

func TestTalosVersionDrift(t *testing.T) {
	for name, tc := range map[string]struct {
		running, targetImage string
		wantDrifted, wantOK  bool
	}{
		"match, both with v prefix": {
			running: "v1.13.8", targetImage: "ghcr.io/siderolabs/installer:v1.13.8",
			wantDrifted: false, wantOK: true,
		},
		"match, running without v prefix": {
			running: "1.13.8", targetImage: "ghcr.io/siderolabs/installer:v1.13.8",
			wantDrifted: false, wantOK: true,
		},
		"mismatch": {
			running: "v1.13.7", targetImage: "ghcr.io/siderolabs/installer:v1.13.8",
			wantDrifted: true, wantOK: true,
		},
		"empty target image": {
			running: "v1.13.7", targetImage: "",
			wantDrifted: false, wantOK: false,
		},
		"untagged target image": {
			running: "v1.13.7", targetImage: "ghcr.io/siderolabs/installer@sha256:abcdef",
			wantDrifted: false, wantOK: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			drifted, ok := talos.TalosVersionDrift(tc.running, tc.targetImage)
			if drifted != tc.wantDrifted || ok != tc.wantOK {
				t.Errorf("TalosVersionDrift(%q, %q) = (%v, %v), want (%v, %v)",
					tc.running, tc.targetImage, drifted, ok, tc.wantDrifted, tc.wantOK)
			}
		})
	}
}

func TestExtensionDrift(t *testing.T) {
	for name, tc := range map[string]struct {
		installed, desired     []string
		wantMissing, wantExtra []string
	}{
		"exact match": {
			installed: []string{"siderolabs/zfs", "siderolabs/nvidia-container-toolkit-production"},
			desired:   []string{"siderolabs/zfs", "siderolabs/nvidia-container-toolkit-production"},
		},
		"short vs qualified names still match": {
			installed: []string{"zfs", "nvidia-container-toolkit-production"},
			desired:   []string{"siderolabs/zfs", "siderolabs/nvidia-container-toolkit-production"},
		},
		"missing one": {
			installed:   []string{"siderolabs/zfs"},
			desired:     []string{"siderolabs/zfs", "siderolabs/qemu-guest-agent"},
			wantMissing: []string{"siderolabs/qemu-guest-agent"},
		},
		"unexpected one": {
			installed: []string{"siderolabs/zfs", "siderolabs/qemu-guest-agent"},
			desired:   []string{"siderolabs/zfs"},
			wantExtra: []string{"siderolabs/qemu-guest-agent"},
		},
		"both directions": {
			installed:   []string{"siderolabs/zfs"},
			desired:     []string{"siderolabs/nvidia-container-toolkit-production"},
			wantMissing: []string{"siderolabs/nvidia-container-toolkit-production"},
			wantExtra:   []string{"siderolabs/zfs"},
		},
		"nothing desired, nothing installed": {},
	} {
		t.Run(name, func(t *testing.T) {
			missing, extra := talos.ExtensionDrift(tc.installed, tc.desired)
			if !slices.Equal(missing, tc.wantMissing) {
				t.Errorf("ExtensionDrift(...) missing = %v, want %v", missing, tc.wantMissing)
			}
			if !slices.Equal(extra, tc.wantExtra) {
				t.Errorf("ExtensionDrift(...) unexpected = %v, want %v", extra, tc.wantExtra)
			}
		})
	}
}

func TestParseKubernetesVersion(t *testing.T) {
	for name, tc := range map[string]struct {
		output string
		want   string
	}{
		// Captured verbatim from a real `talosctl upgrade-k8s --dry-run` run.
		"real dry-run output, no-op case": {
			output: `automatically detected the lowest Kubernetes version 1.36.3
discovered controlplane nodes ["10.0.0.15"]
discovered worker nodes []
> "10.0.0.15": Talos version 1.13.7 is compatible with Kubernetes version 1.36.3
updating "kube-apiserver" to version "1.36.3"
`,
			want: "1.36.3",
		},
		"real dry-run output, unsupported path error still has the line": {
			output: `automatically detected the lowest Kubernetes version 1.36.3
unsupported upgrade path 1.36->1.35 (from "1.36.3" to "1.35.0")
`,
			want: "1.36.3",
		},
		"line not present (format changed, or unparseable)": {
			output: "some unrelated output\n",
			want:   "",
		},
		"empty": {
			output: "",
			want:   "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := talos.ParseKubernetesVersion(tc.output); got != tc.want {
				t.Errorf("ParseKubernetesVersion(...) = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestKernelArgDrift(t *testing.T) {
	const cmdline = "talos.platform=metal console=ttyS0 cpufreq.default_governor=performance init_on_alloc=1"

	for name, tc := range map[string]struct {
		desired []string
		want    []string
	}{
		"all present": {
			desired: []string{"cpufreq.default_governor=performance", "console=ttyS0"},
		},
		"one missing": {
			desired: []string{"cpufreq.default_governor=performance", "amdgpu.gttsize=40960"},
			want:    []string{"amdgpu.gttsize=40960"},
		},
		"all missing": {
			desired: []string{"amdgpu.gttsize=40960", "some.other.arg=1"},
			want:    []string{"amdgpu.gttsize=40960", "some.other.arg=1"},
		},
		"empty desired list": {},
		"substring should not falsely match": {
			// "governor" alone should not be considered present just
			// because it appears inside "cpufreq.default_governor=performance".
			desired: []string{"governor"},
			want:    []string{"governor"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := talos.KernelArgDrift(cmdline, tc.desired)
			if !slices.Equal(got, tc.want) {
				t.Errorf("KernelArgDrift(cmdline, %v) = %v, want %v", tc.desired, got, tc.want)
			}
		})
	}
}
