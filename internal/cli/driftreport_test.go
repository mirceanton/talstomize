package cli

import "testing"

func TestNodeDriftResultDrifted(t *testing.T) {
	for name, tc := range map[string]struct {
		result nodeDriftResult
		want   bool
	}{
		"nothing drifted":     {nodeDriftResult{}, false},
		"config diff":         {nodeDriftResult{ConfigDiff: "--- a\n+++ b\n"}, true},
		"talos version":       {nodeDriftResult{TalosVersion: talosVersionCheck{Checked: true, Drifted: true}}, true},
		"extensions missing":  {nodeDriftResult{Extensions: extensionsCheck{Checked: true, Missing: []string{"zfs"}}}, true},
		"kernel args missing": {nodeDriftResult{KernelArgs: kernelArgsCheck{Checked: true, Missing: []string{"foo=bar"}}}, true},
		"checked, no drift":   {nodeDriftResult{TalosVersion: talosVersionCheck{Checked: true}, Extensions: extensionsCheck{Checked: true}, KernelArgs: kernelArgsCheck{Checked: true}}, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tc.result.drifted(); got != tc.want {
				t.Errorf("drifted() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNodeDriftResultExtraLines(t *testing.T) {
	result := nodeDriftResult{
		TalosVersion: talosVersionCheck{Checked: true, Drifted: true, Running: "v1.13.7", Want: "v1.13.8", WantImage: "ghcr.io/siderolabs/installer:v1.13.8"},
		Extensions:   extensionsCheck{Checked: true, Missing: []string{"zfs"}, Unexpected: []string{"iscsi-tools"}},
		KernelArgs:   kernelArgsCheck{Checked: true, Missing: []string{"foo=bar"}},
	}

	want := []string{
		"    talos: running v1.13.7, want v1.13.8 (ghcr.io/siderolabs/installer:v1.13.8)",
		"    extensions: missing [zfs], unexpected [iscsi-tools]",
		"    kernel args: missing [foo=bar]",
	}

	got := result.extraLines()
	if len(got) != len(want) {
		t.Fatalf("extraLines() = %q, want %q", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("extraLines()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestKubernetesCheckDrifted(t *testing.T) {
	for name, tc := range map[string]struct {
		check kubernetesCheck
		want  bool
	}{
		"not checked": {kubernetesCheck{}, false},
		"match":       {kubernetesCheck{Checked: true, Running: "v1.31.0", Want: "v1.31.0"}, false},
		"mismatch":    {kubernetesCheck{Checked: true, Running: "v1.30.0", Want: "v1.31.0"}, true},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tc.check.drifted(); got != tc.want {
				t.Errorf("drifted() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRepathDiff(t *testing.T) {
	for name, tc := range map[string]struct {
		diff, aPath, bPath, want string
	}{
		"standard header": {
			diff:  "--- a\n+++ b\n@@ -1 +1 @@\n-old\n+new\n",
			aPath: "a/node1.yaml", bPath: "b/node1.yaml",
			want: "--- a/node1.yaml\n+++ b/node1.yaml\n@@ -1 +1 @@\n-old\n+new\n",
		},
		"empty diff untouched": {
			diff:  "",
			aPath: "a/node1.yaml", bPath: "b/node1.yaml",
			want: "",
		},
		"unrecognized header untouched": {
			diff:  "not a diff",
			aPath: "a/node1.yaml", bPath: "b/node1.yaml",
			want: "not a diff",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := repathDiff(tc.diff, tc.aPath, tc.bPath); got != tc.want {
				t.Errorf("repathDiff() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSortedLines(t *testing.T) {
	for name, tc := range map[string]struct {
		items []string
		want  string
	}{
		"empty": {nil, ""},
		"one":   {[]string{"zfs"}, "zfs\n"},
		"sorts": {[]string{"zfs", "iscsi-tools"}, "iscsi-tools\nzfs\n"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := sortedLines(tc.items); got != tc.want {
				t.Errorf("sortedLines(%v) = %q, want %q", tc.items, got, tc.want)
			}
		})
	}
}

func TestTalosVersionCheckDiff(t *testing.T) {
	notDrifted := talosVersionCheck{Checked: true}

	got, err := notDrifted.diff("node1")
	if err != nil {
		t.Fatalf("diff() error = %v", err)
	}

	if got != "" {
		t.Errorf("diff() for non-drifted check = %q, want \"\"", got)
	}

	drifted := talosVersionCheck{Checked: true, Drifted: true, Running: "v1.13.7", Want: "v1.13.8"}

	got, err = drifted.diff("node1")
	if err != nil {
		t.Fatalf("diff() error = %v", err)
	}

	want := "--- a/node1/talos-version\n+++ b/node1/talos-version\n@@ -1,1 +1,1 @@\n-v1.13.7\n+v1.13.8\n"
	if got != want {
		t.Errorf("diff() = %q, want %q", got, want)
	}
}

func TestExtensionsCheckDiff(t *testing.T) {
	check := extensionsCheck{
		Checked:    true,
		Installed:  []string{"iscsi-tools"},
		Desired:    []string{"zfs"},
		Missing:    []string{"zfs"},
		Unexpected: []string{"iscsi-tools"},
	}

	got, err := check.diff("node1")
	if err != nil {
		t.Fatalf("diff() error = %v", err)
	}

	want := "--- a/node1/extensions\n+++ b/node1/extensions\n@@ -1,1 +1,1 @@\n-iscsi-tools\n+zfs\n"
	if got != want {
		t.Errorf("diff() = %q, want %q", got, want)
	}
}

func TestKernelArgsCheckDiff(t *testing.T) {
	check := kernelArgsCheck{
		Checked: true,
		Desired: []string{"console=ttyS0", "foo=bar"},
		Missing: []string{"foo=bar"},
	}

	got, err := check.diff("node1")
	if err != nil {
		t.Fatalf("diff() error = %v", err)
	}

	want := "--- a/node1/kernel-args\n+++ b/node1/kernel-args\n@@ -1,1 +1,2 @@\n console=ttyS0\n+foo=bar\n"
	if got != want {
		t.Errorf("diff() = %q, want %q", got, want)
	}
}
