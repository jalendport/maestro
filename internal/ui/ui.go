// Package ui centralizes styled terminal output. Human output goes to stdout;
// progress, warnings, and errors go to stderr so piped stdout stays clean.
package ui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	Header  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#C4B5FD"})
	Section = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#374151", Dark: "#E5E7EB"})
	Name    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#2563EB", Dark: "#93C5FD"})
	Dim     = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#9CA3AF", Dark: "#6B7280"})

	warnStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FBBF24"})
	errStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#FCA5A5"})
	labelStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"})
)

// Quiet suppresses human event lines on stdout; set by --quiet.
var Quiet bool

// Warnf prints a styled warning to stderr.
func Warnf(format string, a ...any) {
	fmt.Fprintln(os.Stderr, warnStyle.Render("warning:")+" "+fmt.Sprintf(format, a...))
}

// Errorf prints a styled error to stderr.
func Errorf(format string, a ...any) {
	fmt.Fprintln(os.Stderr, errStyle.Render("error:")+" "+fmt.Sprintf(format, a...))
}

// Eventf prints one progress event line to stderr: a fixed-width label and a
// message.
func Eventf(label, format string, a ...any) {
	fmt.Fprintf(os.Stderr, "%s %s\n", labelStyle.Render(fmt.Sprintf("%-8s", label)), fmt.Sprintf(format, a...))
}

// Outf prints a human line to stdout unless Quiet is set.
func Outf(format string, a ...any) {
	if Quiet {
		return
	}
	fmt.Fprintf(os.Stdout, format+"\n", a...)
}

// FirstLine returns the first line, with an optional display limit (0 is unlimited).
func FirstLine(s string, limit int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if limit > 0 && len(s) > limit {
		s = s[:limit] + "…"
	}
	return s
}

// CutTitle limits a title by bytes and adds an ellipsis when truncated.
func CutTitle(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// CutText limits text to n bytes.
func CutText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Duration formats elapsed time for Run, Dispatch, and Council output.
func Duration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	if d >= time.Hour {
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}
