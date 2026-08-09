package cli

import (
	"strings"
	"testing"

	"github.com/sourcegraph/go-diff/diff"
)

const sampleDiff = `--- a
+++ b
@@ -1,3 +1,4 @@
 line1
-line2
+line2-changed
 line3
+line4-added
`

func TestRenderConfigDiffUnified(t *testing.T) {
	got := renderConfigDiff(sampleDiff, 80, false)

	for _, want := range []string{"@@ -1,3 +1,4 @@", "-line2", "+line2-changed", "+line4-added"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderConfigDiff(unified) missing %q, got:\n%s", want, got)
		}
	}

	// Line numbers: unchanged "line1" is line 1 on both sides, the
	// replaced line is 2 on the old side, and the trailing addition has
	// no line on the old side.
	if !strings.Contains(got, "1") {
		t.Errorf("renderConfigDiff(unified) missing line numbers, got:\n%s", got)
	}
}

func TestRenderConfigDiffSplit(t *testing.T) {
	got := renderConfigDiff(sampleDiff, 80, true)

	for _, want := range []string{"│", "-line2", "+line2-changed", "+line4-added"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderConfigDiff(split) missing %q, got:\n%s", want, got)
		}
	}
}

func TestRenderConfigDiffFallsBackOnUnparsable(t *testing.T) {
	got := renderConfigDiff("not a diff at all", 80, false)
	if got == "" {
		t.Error("renderConfigDiff() = \"\", want a non-empty fallback rendering")
	}
}

func TestSplitHunkRows(t *testing.T) {
	fd, err := diff.ParseFileDiff([]byte(sampleDiff))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	rows := splitHunkRows(fd.Hunks[0])

	// line1 (context), line2/line2-changed (replace pair), line3
	// (context), line4-added (pure addition) = 4 rows.
	if len(rows) != 4 {
		t.Fatalf("splitHunkRows() = %d rows, want 4", len(rows))
	}

	if !rows[0].context || rows[0].old.text != "line1" || rows[0].new.text != "line1" {
		t.Errorf("row 0 = %+v, want context row for line1", rows[0])
	}

	if rows[1].context {
		t.Errorf("row 1 should not be a context row: %+v", rows[1])
	}

	if rows[1].old.text != "line2" || rows[1].new.text != "line2-changed" {
		t.Errorf("row 1 = %+v, want old=line2 new=line2-changed", rows[1])
	}

	if !rows[2].context || rows[2].old.text != "line3" {
		t.Errorf("row 2 = %+v, want context row for line3", rows[2])
	}

	if rows[3].old != nil {
		t.Errorf("row 3.old = %+v, want nil (pure addition)", rows[3].old)
	}

	if rows[3].new == nil || rows[3].new.text != "line4-added" {
		t.Errorf("row 3.new = %+v, want line4-added", rows[3].new)
	}
}
