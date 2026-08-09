package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/sourcegraph/go-diff/diff"
)

// renderConfigDiff renders a node's unified config diff for the pretty
// viewer: line-numbered unified format, or a GitHub-style side-by-side
// split when split is true. width is the available terminal width, used
// to size split's two columns. Falls back to a plain, un-numbered
// rendering (styleDiff) if diffText doesn't parse as a unified diff -
// which shouldn't happen for anything talstomize itself generates, but a
// malformed diff shouldn't crash the viewer.
func renderConfigDiff(diffText string, width int, split bool) string {
	fd, err := diff.ParseFileDiff([]byte(diffText))
	if err != nil {
		return styleDiff(diffText)
	}

	if split {
		return renderSplitDiff(fd, width)
	}

	return renderUnifiedDiff(fd)
}

// gutterWidth is how many columns each line-number gutter reserves.
const gutterWidth = 5

func renderUnifiedDiff(fd *diff.FileDiff) string {
	var b strings.Builder

	for _, hunk := range fd.Hunks {
		fmt.Fprintln(&b, hunkLineStyle.Render(hunkHeader(hunk)))

		origLine, newLine := int(hunk.OrigStartLine), int(hunk.NewStartLine)

		for _, line := range hunkLines(hunk) {
			switch line.marker {
			case '-':
				fmt.Fprintln(&b, gutter(origLine, 0)+delLineStyle.Render("-"+line.text))
				origLine++
			case '+':
				fmt.Fprintln(&b, gutter(0, newLine)+addLineStyle.Render("+"+line.text))
				newLine++
			default:
				fmt.Fprintln(&b, gutter(origLine, newLine)+" "+line.text)
				origLine++
				newLine++
			}
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func renderSplitDiff(fd *diff.FileDiff, width int) string {
	colWidth := max((width-2*gutterWidth-3)/2, 8)

	var b strings.Builder

	for _, hunk := range fd.Hunks {
		fmt.Fprintln(&b, hunkLineStyle.Render(hunkHeader(hunk)))

		for _, row := range splitHunkRows(hunk) {
			fmt.Fprintln(&b, splitCell(row.old, colWidth, false, row.context)+" │ "+splitCell(row.new, colWidth, true, row.context))
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func hunkHeader(hunk *diff.Hunk) string {
	return fmt.Sprintf("@@ -%d,%d +%d,%d @@", hunk.OrigStartLine, hunk.OrigLines, hunk.NewStartLine, hunk.NewLines)
}

// gutter renders a two-column line-number prefix; a 0 means "no line on
// this side" (a pure addition or deletion), rendered blank.
func gutter(orig, new int) string {
	o, n := "", ""
	if orig > 0 {
		o = strconv.Itoa(orig)
	}

	if new > 0 {
		n = strconv.Itoa(new)
	}

	return footerStyle.Render(fmt.Sprintf("%*s %*s ", gutterWidth, o, gutterWidth, n))
}

// hunkLine is one line of a hunk body, tagged with its unified-diff
// marker (' ', '+', or '-').
type hunkLine struct {
	marker byte
	text   string
}

// hunkLines splits a hunk's raw body into marker+text pairs, skipping the
// "\ No newline at end of file" marker line (not a real content line).
func hunkLines(hunk *diff.Hunk) []hunkLine {
	var lines []hunkLine

	for raw := range strings.SplitSeq(strings.TrimSuffix(string(hunk.Body), "\n"), "\n") {
		if raw == "" || raw[0] == '\\' {
			continue
		}

		lines = append(lines, hunkLine{marker: raw[0], text: raw[1:]})
	}

	return lines
}

// splitRow is one row of a side-by-side diff: old and/or new may be nil
// (a blank cell) when a line only exists on one side. context is true for
// an unchanged line (present unmodified on both sides) - needed because
// old and new can both be non-nil for a genuine replace pair too, and
// those two cases render differently (plain vs +/- colored).
type splitRow struct {
	old, new *numberedLine
	context  bool
}

type numberedLine struct {
	num  int
	text string
}

// splitHunkRows lays a hunk out for side-by-side display. Context lines
// become a full row on both sides. Consecutive runs of deletions and
// additions (a "replace" block) are paired up left-to-right, index by
// index - the same convention GitHub's and most other split diff viewers
// use, since unified diff format doesn't itself pair removed/added lines.
func splitHunkRows(hunk *diff.Hunk) []splitRow {
	origLine, newLine := int(hunk.OrigStartLine), int(hunk.NewStartLine)

	var rows []splitRow

	var dels, adds []numberedLine

	flush := func() {
		n := max(len(dels), len(adds))

		for i := range n {
			var row splitRow

			if i < len(dels) {
				row.old = &dels[i]
			}

			if i < len(adds) {
				row.new = &adds[i]
			}

			rows = append(rows, row)
		}

		dels, adds = nil, nil
	}

	for _, line := range hunkLines(hunk) {
		switch line.marker {
		case '-':
			dels = append(dels, numberedLine{num: origLine, text: line.text})
			origLine++
		case '+':
			adds = append(adds, numberedLine{num: newLine, text: line.text})
			newLine++
		default:
			flush()

			rows = append(rows, splitRow{
				old:     &numberedLine{num: origLine, text: line.text},
				new:     &numberedLine{num: newLine, text: line.text},
				context: true,
			})
			origLine++
			newLine++
		}
	}

	flush()

	return rows
}

// splitCell renders one side of a splitRow: a right-aligned line-number
// gutter plus the (truncated, marker-prefixed) line text, or blank space
// if line is nil. context lines (unchanged, present on both sides) render
// plain regardless of isNew - only a genuine addition or deletion gets
// +/- coloring.
func splitCell(line *numberedLine, width int, isNew, context bool) string {
	style := lipgloss.NewStyle()

	marker := " "

	if line != nil && !context {
		switch {
		case isNew:
			style, marker = addLineStyle, "+"
		default:
			style, marker = delLineStyle, "-"
		}
	}

	num, text := "", ""
	if line != nil {
		num = strconv.Itoa(line.num)
		text = line.text
	}

	gutterCell := footerStyle.Width(gutterWidth).Align(lipgloss.Right).Render(num)
	content := style.Width(width).Render(marker + ansi.Truncate(text, width-1, "…"))

	return gutterCell + " " + content
}
