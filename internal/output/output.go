// Package output handles all human/JSON rendering, TTY detection, color,
// progress gating, secret redaction, and error reporting. Human output and
// JSON go to stdout; logs, prompts, and progress go to stderr.
package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
)

// Printer carries presentation flags resolved from global CLI flags.
type Printer struct {
	JSON    bool
	Quiet   bool
	Verbose bool
	NoColor bool

	Out io.Writer // stdout (data)
	Err io.Writer // stderr (logs/prompts/progress)
}

// New builds a Printer wired to os.Stdout/os.Stderr.
func New(jsonOut, quiet, verbose, noColor bool) *Printer {
	if os.Getenv("NO_COLOR") != "" {
		noColor = true
	}
	return &Printer{JSON: jsonOut, Quiet: quiet, Verbose: verbose, NoColor: noColor, Out: os.Stdout, Err: os.Stderr}
}

// IsTTY reports whether stderr is an interactive terminal (used to gate progress/color).
func (p *Printer) IsTTY() bool { return isTerminal(p.Err) }

// progressEnabled reports whether progress bars should render.
func (p *Printer) ProgressEnabled() bool {
	return !p.Quiet && !p.JSON && p.IsTTY()
}

// Printf writes human text to stdout. No-op in JSON mode (keeps --json clean).
func (p *Printer) Printf(format string, a ...any) {
	if p.JSON {
		return
	}
	fmt.Fprintf(p.Out, format, a...)
}

// Info writes a non-essential status line to stderr (suppressed when quiet/json).
func (p *Printer) Info(format string, a ...any) {
	if p.Quiet || p.JSON {
		return
	}
	fmt.Fprintf(p.Err, format+"\n", a...)
}

// Success prints a green check line to stderr.
func (p *Printer) Success(format string, a ...any) {
	if p.Quiet || p.JSON {
		return
	}
	fmt.Fprintf(p.Err, "%s %s\n", p.color(colorGreen, "✓"), fmt.Sprintf(format, a...))
}

// Debug logs to stderr only in verbose mode, with secret redaction applied.
func (p *Printer) Debug(format string, a ...any) {
	if !p.Verbose {
		return
	}
	fmt.Fprintln(p.Err, p.color(colorDim, "debug: "+Redact(fmt.Sprintf(format, a...))))
}

// JSONOut marshals v to stdout (indented). Used by --json read commands.
func (p *Printer) JSONOut(v any) error {
	enc := json.NewEncoder(p.Out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// Table returns a tabwriter writing to stdout. Caller writes tab-separated rows and Flush()es.
func (p *Printer) Table() *tabwriter.Writer {
	return tabwriter.NewWriter(p.Out, 0, 2, 2, ' ', 0)
}

// EmitError renders an error to stderr and returns the process exit code.
func (p *Printer) EmitError(err error) int {
	if err == nil {
		return ExitOK
	}
	var ce *CLIError
	if !errors.As(err, &ce) {
		ce = &CLIError{Code: ExitError, CodeName: "error", Message: err.Error()}
	}
	if p.JSON {
		_ = json.NewEncoder(p.Err).Encode(map[string]any{
			"error": map[string]string{"code": ce.CodeName, "message": ce.Message, "hint": ce.Hint},
		})
		return ce.Code
	}
	fmt.Fprintf(p.Err, "%s %s\n", p.color(colorRed, "error:"), ce.Message)
	if ce.Hint != "" {
		fmt.Fprintf(p.Err, "  %s\n", p.color(colorDim, ce.Hint))
	}
	return ce.Code
}

// asCLIError is a small wrapper around errors.As to avoid importing errors elsewhere.
func asCLIError(err error, target **CLIError) bool { return errors.As(err, target) }

// --- color ---

const (
	colorReset = "\033[0m"
	colorRed   = "\033[31m"
	colorGreen = "\033[32m"
	colorDim   = "\033[2m"
	colorBold  = "\033[1m"
)

func (p *Printer) color(code, s string) string {
	if p.NoColor || !p.IsTTY() {
		return s
	}
	return code + s + colorReset
}

// Bold returns s wrapped in bold when color is enabled.
func (p *Printer) Bold(s string) string { return p.color(colorBold, s) }

// HumanSize formats bytes base-1000 (MB/GB) to match the web UI.
func HumanSize(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "kMGTPE"[exp])
}

// HumanCount formats counts compactly (12.4k).
func HumanCount(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000), ".0") + "k"
	default:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1_000_000), ".0") + "M"
	}
}
