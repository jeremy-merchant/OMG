package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"example.invalid/coordledger/internal/domain"
)

const (
	cliANSIReset  = "\x1b[0m"
	cliANSIBold   = "\x1b[1m"
	cliANSIDim    = "\x1b[2m"
	cliANSICyan   = "\x1b[96m"
	cliANSIGreen  = "\x1b[92m"
	cliANSIYellow = "\x1b[93m"
	cliANSIRed    = "\x1b[91m"
)

type cliTheme struct {
	enabled bool
}

func (theme cliTheme) paint(code, value string) string {
	if !theme.enabled || value == "" {
		return value
	}
	return code + value + cliANSIReset
}

func (theme cliTheme) bold(value string) string    { return theme.paint(cliANSIBold, value) }
func (theme cliTheme) dim(value string) string     { return theme.paint(cliANSIDim, value) }
func (theme cliTheme) accent(value string) string  { return theme.paint(cliANSICyan, value) }
func (theme cliTheme) success(value string) string { return theme.paint(cliANSIGreen, value) }
func (theme cliTheme) warning(value string) string { return theme.paint(cliANSIYellow, value) }
func (theme cliTheme) danger(value string) string  { return theme.paint(cliANSIRed, value) }

func cliTerminalColorEnabled(output io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	file, ok := output.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func renderUsage(version string, color bool) string {
	theme := cliTheme{enabled: color}
	var out strings.Builder
	out.WriteString(theme.bold("OMG"))
	out.WriteString(theme.accent("  OPERATOR LEDGER"))
	out.WriteString(theme.dim("  " + version))
	out.WriteByte('\n')
	out.WriteString(theme.dim("Local coordination control plane for coding agents."))
	out.WriteByte('\n')
	out.WriteString(theme.dim(strings.Repeat("━", 78)))
	out.WriteString("\n\n")
	out.WriteString(theme.accent("Usage:"))
	out.WriteString("\n  " + theme.bold("omg <command> [subcommand] [options]") + "\n")

	groups := []struct {
		Title    string
		Commands [][2]string
	}{
		{
			Title: "START + VERIFY",
			Commands: [][2]string{
				{"init", "Initialize the local coordination ledger"},
				{"preflight", "Resolve identity, scope, inbox, blockers, and reservations"},
				{"doctor", "Inspect configuration, store, platform, and recovery health"},
				{"migration", "Plan or apply schema migration through guarded commands"},
				{"backup", "Create or validate local recovery artifacts"},
				{"release", "Inspect source-bound release readiness"},
			},
		},
		{
			Title: "COORDINATE WORK",
			Commands: [][2]string{
				{"human / session", "Register operators and agent sessions"},
				{"delegate / checkpoint", "Transfer authority and persist continuation points"},
				{"task / progress", "Claim work and report done, doing, and next"},
				{"dependency", "Connect blockers and unblock conditions"},
				{"message / receipt", "Exchange and acknowledge coordination messages"},
				{"handoff / reserve", "Transfer verified work and protect path intent"},
			},
		},
		{
			Title: "INSPECT + INTEGRATE",
			Commands: [][2]string{
				{"board", "Render the operator board as tty, json, markdown, or html"},
				{"git", "Observe branches, worktrees, ownership, and dirty state"},
				{"watch", "Follow coordination changes without polling by hand"},
				{"export / import", "Move canonical records through explicit transports"},
				{"integration / run", "Connect supported runtimes and guarded child commands"},
				{"shell-init / completion / mcp", "Install local interaction surfaces"},
			},
		},
	}
	for _, group := range groups {
		out.WriteString("\n" + theme.accent(group.Title) + "\n")
		for _, command := range group.Commands {
			out.WriteString("  " + theme.accent("❯") + " " + theme.bold(fmt.Sprintf("%-34s", command[0])) + theme.dim(command[1]) + "\n")
		}
	}

	out.WriteString("\n" + theme.accent("GLOBAL OPTIONS") + "\n")
	for _, option := range [][2]string{
		{"--project <path>", "Select a project root"},
		{"--workspace <path>", "Select a workspace"},
		{"--store <path>", "Use an explicit SQLite store"},
		{"--json", "Emit a stable JSON envelope"},
		{"--payload <json>", "Supply a strict command payload"},
		{"--payload-file <path>", "Read a strict command payload from a file"},
		{"--payload-stdin", "Read a strict command payload from stdin"},
	} {
		out.WriteString("  " + theme.bold(fmt.Sprintf("%-25s", option[0])) + theme.dim(option[1]) + "\n")
	}
	out.WriteString("\n" + theme.dim("Reference  docs/COMMAND_REFERENCE.md") + "\n")
	return out.String()
}

func renderSuccess(output io.Writer, data any) {
	theme := cliTheme{enabled: cliTerminalColorEnabled(output)}
	message := neutralizeTerminalControls(fmt.Sprint(data))
	_, _ = fmt.Fprintln(output, theme.success("✔")+" "+theme.bold("OMG")+theme.success("  VERIFIED"))
	_, _ = fmt.Fprintln(output, "  "+message)
}

func renderError(output io.Writer, err domain.DomainError, exit int) {
	theme := cliTheme{enabled: cliTerminalColorEnabled(output)}
	retry := "unavailable"
	if err.Retryable {
		retry = "available"
	}
	_, _ = fmt.Fprintln(output, theme.danger("✘")+" "+theme.bold("OMG")+theme.danger("  ERROR"))
	_, _ = fmt.Fprintln(output, "  "+theme.dim("code ")+"  "+neutralizeTerminalControls(string(err.Code)))
	_, _ = fmt.Fprintln(output, "  "+theme.dim("cause")+"  "+neutralizeTerminalControls(err.Message))
	_, _ = fmt.Fprintln(output, "  "+theme.dim("retryable")+"  "+retry)
	_, _ = fmt.Fprintln(output, "  "+theme.dim("next ")+"  "+theme.accent(nextCommand(err)))
	_, _ = fmt.Fprintln(output, "  "+theme.dim(fmt.Sprintf("exit=%d", exit)))
}

func nextCommand(err domain.DomainError) string {
	switch err.Code {
	case domain.CodeInvalidArgument:
		return "omg --help"
	case domain.CodeUninitialized:
		return "omg init --project <path>"
	case domain.CodeNotFound, domain.CodeConflict:
		return "omg board all"
	case domain.CodeCommandNotWired, domain.CodeUnavailable, domain.CodeInternal:
		return "omg doctor"
	default:
		if err.Retryable {
			return "omg preflight"
		}
		return "omg --help"
	}
}
