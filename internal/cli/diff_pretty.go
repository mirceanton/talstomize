package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

var (
	okColor     = lipgloss.Color("2")  // green
	driftColor  = lipgloss.Color("1")  // red
	dimColor    = lipgloss.Color("8")  // grey
	addColor    = lipgloss.Color("2")  // green
	removeColor = lipgloss.Color("1")  // red
	hunkColor   = lipgloss.Color("6")  // cyan
	accentColor = lipgloss.Color("13") // magenta

	activeTabStyle = lipgloss.NewStyle().
			Bold(true).
			Padding(0, 1).
			Foreground(lipgloss.Color("0")).
			Background(accentColor)
	inactiveTabStyle = lipgloss.NewStyle().
				Padding(0, 1).
				Foreground(dimColor)
	okStyle       = lipgloss.NewStyle().Foreground(okColor).Bold(true)
	driftStyle    = lipgloss.NewStyle().Foreground(driftColor).Bold(true)
	headerStyle   = lipgloss.NewStyle().Bold(true)
	addLineStyle  = lipgloss.NewStyle().Foreground(addColor)
	delLineStyle  = lipgloss.NewStyle().Foreground(removeColor)
	hunkLineStyle = lipgloss.NewStyle().Foreground(hunkColor)
	footerStyle   = lipgloss.NewStyle().Foreground(dimColor)
)

// prettyModel is the bubbletea model for `diff --output pretty`: a leading
// "dashboard" tab summarizing every node's status at a glance (including
// cluster-wide Kubernetes version drift, which doesn't get a tab of its
// own - it's a single line, not worth a whole pane), plus one tab per
// node.
type prettyModel struct {
	report driftReport
	tabs   []string

	active    int
	splitDiff bool
	width     int
	viewport  viewport.Model
	ready     bool
}

func newPrettyModel(report driftReport) prettyModel {
	tabs := make([]string, 0, len(report.Nodes)+1)
	tabs = append(tabs, "dashboard")

	for _, n := range report.Nodes {
		tabs = append(tabs, n.Name)
	}

	return prettyModel{report: report, tabs: tabs}
}

func (m prettyModel) Init() tea.Cmd {
	return nil
}

func (m prettyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "right", "l", "tab":
			m.setActive((m.active + 1) % len(m.tabs))
		case "left", "h", "shift+tab":
			m.setActive((m.active - 1 + len(m.tabs)) % len(m.tabs))
		case "n":
			m.setActive(m.nextDrifted())
		case "s":
			m.splitDiff = !m.splitDiff
			m.refreshContent()
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		vpHeight := msg.Height - lipgloss.Height(m.renderTabs()) - lipgloss.Height(m.renderFooter())

		if !m.ready {
			m.viewport = viewport.New(msg.Width, vpHeight)
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = vpHeight
		}

		m.refreshContent()
	}

	var cmd tea.Cmd

	m.viewport, cmd = m.viewport.Update(msg)

	return m, cmd
}

func (m *prettyModel) setActive(i int) {
	m.active = i
	m.refreshContent()
	m.viewport.GotoTop()
}

// refreshContent re-renders the viewport's content for the current tab,
// split mode, and width. A no-op before the first WindowSizeMsg, when the
// viewport doesn't exist yet.
func (m *prettyModel) refreshContent() {
	if m.ready {
		m.viewport.SetContent(m.content())
	}
}

// tabDrifted reports whether tab i (dashboard or a node) found any drift.
// The dashboard tab mirrors the report's overall status, which includes
// cluster-wide Kubernetes drift even though that has no tab of its own.
func (m prettyModel) tabDrifted(i int) bool {
	if i == 0 {
		return m.report.drifted()
	}

	return m.report.Nodes[i-1].drifted()
}

// nextDrifted returns the index of the next drifted node tab after the
// active one, cycling and skipping the dashboard tab. Returns the active
// tab unchanged if nothing is drifted (e.g. only cluster-wide Kubernetes
// drifted - that's shown on the dashboard itself, nowhere to jump to).
func (m prettyModel) nextDrifted() int {
	for step := 1; step <= len(m.tabs); step++ {
		i := (m.active + step) % len(m.tabs)
		if i != 0 && m.tabDrifted(i) {
			return i
		}
	}

	return m.active
}

func (m prettyModel) View() string {
	if !m.ready {
		return "loading...\n"
	}

	return m.renderTabs() + "\n" + m.viewport.View() + "\n" + m.renderFooter()
}

func (m prettyModel) renderTabs() string {
	rendered := make([]string, len(m.tabs))

	for i, name := range m.tabs {
		dot := okStyle.Render("●")
		if m.tabDrifted(i) {
			dot = driftStyle.Render("●")
		}

		style := inactiveTabStyle
		if i == m.active {
			style = activeTabStyle
		}

		rendered[i] = style.Render(dot + " " + name)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}

func (m prettyModel) renderFooter() string {
	if m.active == 0 {
		return footerStyle.Render("←/→ switch tabs · n next issue · ↑/↓ scroll · q quit")
	}

	mode := "unified"
	if m.splitDiff {
		mode = "split"
	}

	return footerStyle.Render(fmt.Sprintf("←/→ switch tabs · n next issue · s toggle diff view (%s) · ↑/↓ scroll · q quit", mode))
}

func (m prettyModel) content() string {
	if m.active == 0 {
		return renderDashboardContent(m.report)
	}

	return renderNodeContent(m.report.Nodes[m.active-1], m.width, m.splitDiff)
}

func renderDashboardContent(report driftReport) string {
	var b strings.Builder

	b.WriteString(headerStyle.Render("cluster overview"))
	b.WriteString("\n\n")

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(dimColor)).
		Headers("NODE", "CONFIG", "TALOS", "EXTENSIONS", "KERNEL ARGS")

	drifted := 0

	for _, n := range report.Nodes {
		if n.drifted() {
			drifted++
		}

		t.Row(
			n.Name,
			statusCell(n.ConfigDiff == ""),
			checkCell(n.TalosVersion.Checked, !n.TalosVersion.Drifted),
			checkCell(n.Extensions.Checked, !n.Extensions.drifted()),
			checkCell(n.KernelArgs.Checked, !n.KernelArgs.drifted()),
		)
	}

	b.WriteString(t.String())
	b.WriteString("\n\n")

	if report.Kubernetes.Checked {
		writeCheckStatus(&b, "kubernetes version", !report.Kubernetes.drifted())

		if report.Kubernetes.drifted() {
			fmt.Fprintf(&b, "    running %s, want %s\n", report.Kubernetes.Running, report.Kubernetes.Want)
		}

		b.WriteString("\n")
	}

	if drifted == 0 {
		b.WriteString(okStyle.Render(fmt.Sprintf("all %d nodes match\n", len(report.Nodes))))
	} else {
		b.WriteString(driftStyle.Render(fmt.Sprintf("%d of %d nodes drifted\n", drifted, len(report.Nodes))))
		b.WriteString(footerStyle.Render("press n to jump to the next issue\n"))
	}

	return b.String()
}

// statusCell renders a single ✓/✗ table cell.
func statusCell(ok bool) string {
	if ok {
		return okStyle.Render("✓")
	}

	return driftStyle.Render("✗")
}

// checkCell renders a table cell for a check that might not have run at
// all (checked=false, e.g. no schematic configured) - shown as a dim "–"
// rather than a false "✓".
func checkCell(checked, ok bool) string {
	if !checked {
		return footerStyle.Render("–")
	}

	return statusCell(ok)
}

func renderNodeContent(n nodeDriftResult, width int, split bool) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s\n\n", headerStyle.Render(fmt.Sprintf("%s (%s)", n.Name, n.IP)))

	writeCheckStatus(&b, "config", n.ConfigDiff == "")

	if n.TalosVersion.Checked {
		writeCheckStatus(&b, "talos version", !n.TalosVersion.Drifted)
	}

	if n.Extensions.Checked {
		writeCheckStatus(&b, "extensions", !n.Extensions.drifted())
	}

	if n.KernelArgs.Checked {
		writeCheckStatus(&b, "kernel args", !n.KernelArgs.drifted())
	}

	b.WriteString("\n")

	for _, line := range n.extraLines() {
		b.WriteString(strings.TrimPrefix(line, "    "))
		b.WriteString("\n")
	}

	if n.ConfigDiff != "" {
		b.WriteString("\n")
		b.WriteString(renderConfigDiff(n.ConfigDiff, width, split))
	}

	return b.String()
}

func writeCheckStatus(b *strings.Builder, name string, ok bool) {
	if ok {
		fmt.Fprintf(b, "  %s %s\n", okStyle.Render("✓"), name)
	} else {
		fmt.Fprintf(b, "  %s %s\n", driftStyle.Render("✗"), name)
	}
}

// styleDiff colors a unified diff's added/removed/hunk lines for
// terminal display.
func styleDiff(diff string) string {
	lines := strings.Split(strings.TrimRight(diff, "\n"), "\n")

	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			lines[i] = headerStyle.Render(line)
		case strings.HasPrefix(line, "+"):
			lines[i] = addLineStyle.Render(line)
		case strings.HasPrefix(line, "-"):
			lines[i] = delLineStyle.Render(line)
		case strings.HasPrefix(line, "@@"):
			lines[i] = hunkLineStyle.Render(line)
		}
	}

	return strings.Join(lines, "\n")
}

// runPretty launches the interactive bubbletea diff viewer for report. It
// always talks to the real terminal (os.Stdin/os.Stdout) rather than the
// command's configured streams, since it's inherently interactive.
func runPretty(report driftReport) error {
	_, err := tea.NewProgram(newPrettyModel(report), tea.WithAltScreen()).Run()

	return err
}
