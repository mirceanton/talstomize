package cli

import (
	"strings"
	"testing"
)

func TestRenderPlain(t *testing.T) {
	report := driftReport{
		Nodes: []nodeDriftResult{
			{Name: "cp1", IP: "10.0.0.1"},
			{
				Name: "cp2", IP: "10.0.0.2",
				ConfigDiff:   "--- a\n+++ b\n@@ -1 +1 @@\n-old\n+new\n",
				TalosVersion: talosVersionCheck{Checked: true, Drifted: true, Running: "v1.13.7", Want: "v1.13.8", WantImage: "ghcr.io/siderolabs/installer:v1.13.8"},
			},
		},
		Kubernetes: kubernetesCheck{Checked: true, Running: "v1.30.0", Want: "v1.31.0"},
	}

	var buf strings.Builder
	renderPlain(report, &buf)

	want := "==> cp1 (10.0.0.1): no differences\n" +
		"==> cp2 (10.0.0.2):\n" +
		"--- a\n+++ b\n@@ -1 +1 @@\n-old\n+new\n\n" +
		"    talos: running v1.13.7, want v1.13.8 (ghcr.io/siderolabs/installer:v1.13.8)\n" +
		"==> kubernetes: running v1.30.0, want v1.31.0\n"

	if buf.String() != want {
		t.Errorf("renderPlain() =\n%q\nwant\n%q", buf.String(), want)
	}
}

func TestRenderPlainNoDrift(t *testing.T) {
	report := driftReport{
		Nodes:      []nodeDriftResult{{Name: "cp1", IP: "10.0.0.1"}},
		Kubernetes: kubernetesCheck{Checked: true, Running: "v1.31.0", Want: "v1.31.0"},
	}

	var buf strings.Builder
	renderPlain(report, &buf)

	want := "==> cp1 (10.0.0.1): no differences\n"
	if buf.String() != want {
		t.Errorf("renderPlain() = %q, want %q", buf.String(), want)
	}

	if report.drifted() {
		t.Error("report.drifted() = true, want false")
	}
}

func TestRenderDiff(t *testing.T) {
	report := driftReport{
		Nodes: []nodeDriftResult{
			{
				Name:       "cp1",
				IP:         "10.0.0.1",
				ConfigDiff: "--- a\n+++ b\n@@ -1 +1 @@\n-old\n+new\n",
			},
			{Name: "cp2", IP: "10.0.0.2"},
		},
		Kubernetes: kubernetesCheck{Checked: true, Running: "v1.30.0", Want: "v1.31.0"},
	}

	var buf strings.Builder
	if err := renderDiff(report, &buf); err != nil {
		t.Fatalf("renderDiff() error = %v", err)
	}

	got := buf.String()

	// The config diff must be repathed per-node so multiple nodes don't
	// collide once concatenated into one patch stream.
	if !strings.Contains(got, "--- a/cp1.yaml\n+++ b/cp1.yaml\n") {
		t.Errorf("renderDiff() missing repathed config diff header:\n%s", got)
	}

	// cp2 has no drift at all, so it must contribute nothing.
	if strings.Contains(got, "cp2") {
		t.Errorf("renderDiff() unexpectedly mentions non-drifted node cp2:\n%s", got)
	}

	if !strings.Contains(got, "--- a/kubernetes-version\n+++ b/kubernetes-version\n") {
		t.Errorf("renderDiff() missing kubernetes version diff header:\n%s", got)
	}

	// Every non-empty block must be a well-formed unified diff hunk.
	for block := range strings.SplitSeq(strings.TrimSpace(got), "--- ") {
		if block == "" {
			continue
		}

		if !strings.Contains(block, "\n+++ ") || !strings.Contains(block, "@@") {
			t.Errorf("renderDiff() produced a malformed block: %q", block)
		}
	}
}

func TestRenderDiffNoDrift(t *testing.T) {
	report := driftReport{
		Nodes:      []nodeDriftResult{{Name: "cp1", IP: "10.0.0.1"}},
		Kubernetes: kubernetesCheck{Checked: true, Running: "v1.31.0", Want: "v1.31.0"},
	}

	var buf strings.Builder
	if err := renderDiff(report, &buf); err != nil {
		t.Fatalf("renderDiff() error = %v", err)
	}

	if buf.String() != "" {
		t.Errorf("renderDiff() = %q, want empty", buf.String())
	}
}
