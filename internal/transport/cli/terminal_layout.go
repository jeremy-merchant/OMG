package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/jeremy-merchant/oh-my-group/internal/terminaltext"
)

const (
	minimumTerminalWidth = 36
	defaultTerminalWidth = 96
	maximumTerminalWidth = 160

	minimumTerminalHeight = 12
	compactHelpHeight     = 36
	maximumTerminalHeight = 200
)

type helpTarget struct {
	Command    string
	Subcommand string
}

type helpRow struct {
	Marker      string
	Label       string
	Description string
	Mutation    bool
}

func cliTerminalWidth(output io.Writer) int {
	if configured, ok := configuredTerminalWidth(); ok {
		return configured
	}
	if file, ok := output.(*os.File); ok {
		if width, found := platformTerminalWidth(file); found {
			return normalizeTerminalWidth(width)
		}
	}
	return defaultTerminalWidth
}

func cliTerminalHeight(output io.Writer) int {
	file, ok := output.(*os.File)
	if !ok {
		return 0
	}
	info, err := file.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return 0
	}
	if configured, ok := configuredTerminalHeight(); ok {
		return configured
	}
	if height, found := platformTerminalHeight(file); found {
		return normalizeTerminalHeight(height)
	}
	return 0
}

func configuredTerminalHeight() (int, bool) {
	value := strings.TrimSpace(os.Getenv("LINES"))
	if value == "" {
		return 0, false
	}
	height, err := strconv.Atoi(value)
	if err != nil || height <= 0 {
		return 0, false
	}
	return normalizeTerminalHeight(height), true
}

func normalizeTerminalHeight(height int) int {
	if height < minimumTerminalHeight {
		return minimumTerminalHeight
	}
	if height > maximumTerminalHeight {
		return maximumTerminalHeight
	}
	return height
}

func configuredTerminalWidth() (int, bool) {
	value := strings.TrimSpace(os.Getenv("COLUMNS"))
	if value == "" {
		return 0, false
	}
	width, err := strconv.Atoi(value)
	if err != nil || width <= 0 {
		return 0, false
	}
	return normalizeTerminalWidth(width), true
}

func normalizeTerminalWidth(width int) int {
	if width < minimumTerminalWidth {
		return minimumTerminalWidth
	}
	if width > maximumTerminalWidth {
		return maximumTerminalWidth
	}
	return width
}

func parseHelpTarget(args []string) (helpTarget, bool) {
	if len(args) == 0 {
		return helpTarget{}, false
	}
	if args[0] == "help" {
		target := helpTarget{}
		if len(args) > 1 && !strings.HasPrefix(args[1], "-") {
			target.Command = args[1]
		}
		if len(args) > 2 && !strings.HasPrefix(args[2], "-") {
			target.Subcommand = args[2]
		}
		return target, true
	}

	command := ""
	subcommand := ""
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if argument == "--" {
			break
		}
		if strings.HasPrefix(argument, "--") && strings.Contains(argument, "=") {
			continue
		}
		if optionTakesValue(argument) {
			if index+1 < len(args) {
				index++
			}
			continue
		}
		if argument == "--help" || argument == "-h" || argument == "help" {
			return helpTarget{Command: command, Subcommand: subcommand}, true
		}
		if strings.HasPrefix(argument, "-") {
			continue
		}
		if command == "" {
			command = argument
			continue
		}
		if subcommand == "" && commandTakesSubcommand(command) {
			subcommand = argument
		}
	}
	return helpTarget{}, false
}

func renderUsage(version string, color bool) string {
	output, _ := renderHelp(version, color, defaultTerminalWidth, helpTarget{})
	return output
}

func renderHelp(version string, color bool, width int, target helpTarget) (string, bool) {
	return renderHelpWithHeight(version, color, width, 0, target)
}

func renderHelpWithHeight(version string, color bool, width, height int, target helpTarget) (string, bool) {
	width = normalizeTerminalWidth(width)
	height = normalizeHelpHeight(height)
	if target.Command == "" {
		return renderGlobalHelp(version, color, width, height), true
	}
	command, ok := helpCommandByName(target.Command)
	if !ok {
		return "", false
	}
	if target.Subcommand != "" {
		subcommand, found := helpSubcommandByName(command, target.Subcommand)
		if !found {
			return "", false
		}
		return renderSubcommandHelp(version, color, width, command, subcommand), true
	}
	return renderCommandHelp(version, color, width, command), true
}

func renderGlobalHelp(version string, color bool, width, height int) string {
	theme := newTerminalTheme(color)
	var output strings.Builder
	writeProductHeader(&output, theme, width, version, fmt.Sprintf("Local coordination control plane · %d commands · human and machine output", len(helpCommands)))

	writeHelpHeading(&output, theme, "Usage:")
	writeWrappedLine(&output, "  ", theme.bold("omg <command> [subcommand] [options]"), width)

	if height > 0 && height < compactHelpHeight {
		writeCompactGlobalHelp(&output, theme, width)
		return output.String()
	}

	writeHelpHeading(&output, theme, "WORKFLOWS")
	workflowRows := make([]helpRow, 0, len(helpWorkflows))
	for _, workflow := range helpWorkflows {
		workflowRows = append(workflowRows, helpRow{
			Marker:      workflow.Marker,
			Label:       workflow.Name,
			Description: workflow.Command + " · " + workflow.Summary,
		})
	}
	writeHelpRows(&output, theme, width, workflowRows)

	groups := []string{"START + VERIFY", "COORDINATE WORK", "INSPECT + INTEGRATE"}
	for _, group := range groups {
		commands := make([]helpCommand, 0)
		for _, command := range helpCommands {
			if command.Group == group {
				commands = append(commands, command)
			}
		}
		writeHelpHeading(&output, theme, fmt.Sprintf("%s · %d", group, len(commands)))
		if width < 68 {
			writeCompactCommandGrid(&output, theme, width, commands)
			continue
		}
		rows := make([]helpRow, 0, len(commands))
		for _, command := range commands {
			rows = append(rows, helpRow{Marker: "❯", Label: command.Name, Description: globalCommandSummary(command.Name)})
		}
		writeHelpRows(&output, theme, width, rows)
	}

	writeHelpHeading(&output, theme, "GLOBAL OPTIONS")
	writeHelpRows(&output, theme, width, []helpRow{
		{Label: "--project <path>", Description: "Select a project root."},
		{Label: "--workspace <path>", Description: "Select a local multi-project workspace."},
		{Label: "--store <path>", Description: "Use an exact local SQLite store."},
		{Label: "--json", Description: "Emit the stable machine envelope."},
		{Label: "--payload <json>", Description: "Supply one strict inline payload when allowed."},
		{Label: "--payload-file <path>", Description: "Read one strict owner-only payload file."},
		{Label: "--payload-stdin", Description: "Read one strict payload from standard input."},
	})

	output.WriteString("\n")
	writeWrappedLine(&output, "", theme.dim("Tip: use `omg <command> --help` to inspect one command family."), width)
	writeWrappedLine(&output, "", theme.dim("Reference: docs/COMMAND_REFERENCE.md"), width)
	return output.String()
}

func normalizeHelpHeight(height int) int {
	if height <= 0 {
		return 0
	}
	return normalizeTerminalHeight(height)
}

func writeCompactGlobalHelp(output *strings.Builder, theme terminalTheme, width int) {
	writeHelpHeading(output, theme, "WORKFLOWS")
	rows := make([]helpRow, 0, len(helpWorkflows))
	for _, workflow := range helpWorkflows {
		rows = append(rows, helpRow{Marker: workflow.Marker, Label: workflow.Name, Description: workflow.Command})
	}
	writeHelpRows(output, theme, width, rows)

	writeHelpHeading(output, theme, "COMMAND FAMILIES")
	for _, group := range []string{"START + VERIFY", "COORDINATE WORK", "INSPECT + INTEGRATE"} {
		names := make([]string, 0)
		for _, command := range helpCommands {
			if command.Group == group {
				names = append(names, command.Name)
			}
		}
		writeWrappedLine(output, "  ", theme.bold(group)+": "+strings.Join(names, " · "), width)
	}

	writeHelpHeading(output, theme, "COMMON OPTIONS")
	writeWrappedLine(output, "  ", "--project <path> · --json · --help", width)
	output.WriteString("\n")
	writeWrappedLine(output, "", theme.dim("Short terminal view · use `omg <command> --help` for the full command contract."), width)
	writeWrappedLine(output, "", theme.dim("Reference: docs/COMMAND_REFERENCE.md"), width)
}

func renderCommandHelp(version string, color bool, width int, command helpCommand) string {
	theme := newTerminalTheme(color)
	var output strings.Builder
	writeProductHeader(&output, theme, width, version, command.Summary)
	writeContextPath(&output, theme, width, command.Name, "")

	writeHelpHeading(&output, theme, "Usage:")
	for _, usage := range command.Usage {
		writeWrappedLine(&output, "  ", theme.bold(usage), width)
	}

	if len(command.Subcommands) > 0 {
		writeHelpHeading(&output, theme, "SUBCOMMANDS")
		rows := make([]helpRow, 0, len(command.Subcommands))
		for _, subcommand := range command.Subcommands {
			marker := "◇"
			if subcommand.Mutation {
				marker = "◆"
			}
			rows = append(rows, helpRow{Marker: marker, Label: subcommand.Name, Description: subcommand.Summary, Mutation: subcommand.Mutation})
		}
		writeHelpRows(&output, theme, width, rows)
		writeWrappedLine(&output, "  ", theme.dim("◆ writes canonical state · ◇ reads or renders state"), width)
	}

	writeCommandOptions(&output, theme, width, command, nil)
	writeExamples(&output, theme, width, command.Examples)
	writeRelatedCommands(&output, theme, width, command)

	output.WriteString("\n")
	writeWrappedLine(&output, "", theme.dim("Reference: docs/COMMAND_REFERENCE.md"), width)
	return output.String()
}

func renderSubcommandHelp(version string, color bool, width int, command helpCommand, subcommand helpSubcommand) string {
	theme := newTerminalTheme(color)
	var output strings.Builder
	writeProductHeader(&output, theme, width, version, subcommand.Summary)
	writeContextPath(&output, theme, width, command.Name, subcommand.Name)

	writeHelpHeading(&output, theme, "Usage:")
	usage := subcommandUsage(command, subcommand)
	writeWrappedLine(&output, "  ", theme.bold(usage), width)

	writeHelpHeading(&output, theme, "MODE")
	mode := "◇ read-only query"
	if subcommand.Mutation {
		mode = "◆ writes canonical state and requires an idempotency key"
	}
	writeWrappedLine(&output, "  ", mode, width)
	if subcommand.PayloadNote != "" {
		writeWrappedLine(&output, "  ", theme.warn(subcommand.PayloadNote), width)
	}

	writeCommandOptions(&output, theme, width, command, &subcommand)
	examples := []string{}
	if subcommand.Example != "" {
		examples = []string{subcommand.Example}
	}
	writeExamples(&output, theme, width, examples)
	writeRelatedCommands(&output, theme, width, command)

	output.WriteString("\n")
	writeWrappedLine(&output, "", theme.dim("Parent help: omg "+command.Name+" --help"), width)
	return output.String()
}

func subcommandUsage(command helpCommand, subcommand helpSubcommand) string {
	base := "omg " + command.Name + " " + subcommand.Name
	if usesTypedPayload(command.Name) {
		return base + " [selection] [payload options] [--json]"
	}
	switch command.Name {
	case "board":
		return base + " [selection] [selector] [--format FORMAT | --json]"
	case "export":
		return base + " [selection] --output NEW_FILE"
	case "integration":
		return base + " --project PATH [--json]"
	case "migration":
		return base + " [selection] [options] [--json]"
	case "backup":
		return base + " [selection] [options] [--json]"
	case "shell-init", "completion":
		return base
	case "watch":
		return base + " [selection] [--json]"
	case "release":
		return base + " [--json]"
	case "mcp":
		return base + " --stdio"
	default:
		return base + " [options]"
	}
}

func writeCommandOptions(output *strings.Builder, theme terminalTheme, width int, command helpCommand, subcommand *helpSubcommand) {
	rows := make([]helpRow, 0)
	if commandUsesSelection(command.Name) {
		rows = append(rows,
			helpRow{Label: "--project <path>", Description: "Select a project root."},
			helpRow{Label: "--workspace <path>", Description: "Select a workspace."},
			helpRow{Label: "--store <path>", Description: "Select an exact SQLite store."},
		)
	}
	if usesTypedPayload(command.Name) {
		rows = append(rows,
			helpRow{Label: "--payload <json>", Description: "Supply one strict inline JSON payload."},
			helpRow{Label: "--payload-file <path>", Description: "Read one strict owner-only payload file."},
			helpRow{Label: "--payload-stdin", Description: "Read one strict payload from standard input."},
		)
		if subcommand == nil || subcommand.Mutation {
			description := "Required for mutations; safe replay returns the original outcome."
			if subcommand != nil {
				description = "Required for this mutation; safe replay returns the original outcome."
			}
			rows = append(rows, helpRow{Label: "--idempotency-key <key>", Description: description})
		}
	}
	for _, option := range command.Options {
		rows = append(rows, helpRow{Label: option[0], Description: option[1]})
	}
	if commandSupportsJSON(command.Name) {
		rows = append(rows, helpRow{Label: "--json", Description: "Emit the stable machine envelope when supported."})
	}
	if len(rows) == 0 {
		return
	}
	writeHelpHeading(output, theme, "OPTIONS")
	writeHelpRows(output, theme, width, rows)
}

func writeExamples(output *strings.Builder, theme terminalTheme, width int, examples []string) {
	if len(examples) == 0 {
		return
	}
	writeHelpHeading(output, theme, "EXAMPLES")
	for _, example := range examples {
		firstPrefix := "  " + theme.info("❯") + " "
		continuation := "    "
		available := width - terminalDisplayWidth(firstPrefix)
		if available < 8 {
			available = 8
		}
		lines := wrapTerminalText(example, available)
		if len(lines) == 0 {
			continue
		}
		output.WriteString(firstPrefix + theme.bold(lines[0]) + "\n")
		for _, line := range lines[1:] {
			output.WriteString(continuation + theme.bold(line) + "\n")
		}
	}
}

func writeCompactCommandGrid(output *strings.Builder, theme terminalTheme, width int, commands []helpCommand) {
	names := make([]string, 0, len(commands))
	for _, command := range commands {
		names = append(names, command.Name)
	}
	for _, line := range wrapTerminalText(strings.Join(names, " · "), width-2) {
		output.WriteString("  " + theme.bold(line) + "\n")
	}
	writeWrappedLine(output, "  ", theme.dim("Open one family with: omg <command> --help"), width)
}

func writeRelatedCommands(output *strings.Builder, theme terminalTheme, width int, command helpCommand) {
	names := relatedCommandNames(command.Name)
	if len(names) == 0 {
		return
	}
	writeHelpHeading(output, theme, "RELATED PATHS")
	rows := make([]helpRow, 0, len(names))
	for _, name := range names {
		rows = append(rows, helpRow{
			Marker:      "→",
			Label:       "omg " + name + " --help",
			Description: globalCommandSummary(name),
		})
	}
	writeHelpRows(output, theme, width, rows)
}

func writeProductHeader(output *strings.Builder, theme terminalTheme, width int, version, subtitle string) {
	heading := theme.bold("OMG") + theme.info("  OPERATOR LEDGER")
	if version != "" {
		heading += theme.dim("  " + version)
	}
	if terminalDisplayWidth(heading) <= width {
		output.WriteString(heading + "\n")
	} else {
		writeWrappedLine(output, "", heading, width)
	}
	for _, line := range wrapTerminalText(subtitle, width) {
		output.WriteString(theme.dim(line) + "\n")
	}
	output.WriteString(theme.dim(strings.Repeat("━", width)) + "\n")
}

func writeContextPath(output *strings.Builder, theme terminalTheme, width int, command, subcommand string) {
	path := "OMG / " + strings.ToUpper(command)
	if subcommand != "" {
		path += " / " + strings.ToUpper(subcommand)
	}
	writeWrappedLine(output, "", theme.info(path), width)
}

func writeHelpHeading(output *strings.Builder, theme terminalTheme, heading string) {
	output.WriteString("\n" + theme.info(heading) + "\n")
}

func writeHelpRows(output *strings.Builder, theme terminalTheme, width int, rows []helpRow) {
	labelWidth := 0
	for _, row := range rows {
		label := row.Label
		if row.Marker != "" {
			label = row.Marker + " " + label
		}
		if measured := terminalDisplayWidth(label); measured > labelWidth {
			labelWidth = measured
		}
	}
	if labelWidth > 32 {
		labelWidth = 32
	}
	wide := width >= 68 && width-labelWidth-6 >= 24
	for _, row := range rows {
		marker := row.Marker
		label := row.Label
		styledMarker := marker
		if marker != "" {
			if row.Mutation {
				styledMarker = theme.warn(marker)
			} else {
				styledMarker = theme.info(marker)
			}
		}
		if wide {
			plainLabel := label
			if marker != "" {
				plainLabel = marker + " " + label
			}
			padded := padTerminalRight(plainLabel, labelWidth)
			styledLabel := theme.bold(padded)
			if marker != "" {
				styledLabel = styledMarker + " " + theme.bold(padTerminalRight(label, labelWidth-terminalDisplayWidth(marker)-1))
			}
			descriptionWidth := width - labelWidth - 6
			lines := wrapTerminalText(row.Description, descriptionWidth)
			if len(lines) == 0 {
				lines = []string{""}
			}
			output.WriteString("  " + styledLabel + "  " + theme.dim(lines[0]) + "\n")
			continuation := strings.Repeat(" ", labelWidth+4)
			for _, line := range lines[1:] {
				output.WriteString(continuation + theme.dim(line) + "\n")
			}
			continue
		}

		prefix := "  "
		if marker != "" {
			output.WriteString(prefix + styledMarker + " " + theme.bold(label) + "\n")
		} else {
			output.WriteString(prefix + theme.bold(label) + "\n")
		}
		for _, line := range wrapTerminalText(row.Description, width-4) {
			output.WriteString("    " + theme.dim(line) + "\n")
		}
	}
}

func writeWrappedLine(output *strings.Builder, prefix, value string, width int) {
	plainPrefixWidth := terminalDisplayWidth(prefix)
	available := width - plainPrefixWidth
	if available < 8 {
		available = 8
	}
	plain := stripTerminalANSI(value)
	lines := wrapTerminalText(plain, available)
	if len(lines) == 0 {
		output.WriteString(prefix + "\n")
		return
	}
	if plain == value {
		for _, line := range lines {
			output.WriteString(prefix + line + "\n")
		}
		return
	}
	if len(lines) == 1 {
		output.WriteString(prefix + value + "\n")
		return
	}
	// Styled text is reconstructed as plain wrapped text to avoid broken ANSI
	// spans. Multi-line styled prose remains readable; semantic color stays on
	// surrounding headings and markers.
	for _, line := range lines {
		output.WriteString(prefix + line + "\n")
	}
}

func wrapTerminalText(value string, width int) []string {
	value = strings.TrimSpace(strings.Map(func(character rune) rune {
		if character == '\n' || character == '\r' || character == '\t' {
			return ' '
		}
		return character
	}, value))
	if value == "" {
		return nil
	}
	if width < 1 {
		width = 1
	}
	words := strings.Fields(value)
	lines := make([]string, 0, len(words))
	current := ""
	for _, word := range words {
		parts := splitTerminalToken(word, width)
		for _, part := range parts {
			if current == "" {
				current = part
				continue
			}
			if terminalDisplayWidth(current)+1+terminalDisplayWidth(part) <= width {
				current += " " + part
				continue
			}
			lines = append(lines, current)
			current = part
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func splitTerminalToken(value string, width int) []string {
	return terminaltext.SplitToken(value, width)
}

func padTerminalRight(value string, width int) string {
	padding := width - terminalDisplayWidth(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}

func terminalDisplayWidth(value string) int {
	return terminaltext.Width(stripTerminalANSI(value))
}

func stripTerminalANSI(value string) string {
	var output strings.Builder
	for index := 0; index < len(value); {
		if value[index] == 0x1b && index+1 < len(value) && value[index+1] == '[' {
			index += 2
			for index < len(value) {
				character := value[index]
				index++
				if character >= 0x40 && character <= 0x7e {
					break
				}
			}
			continue
		}
		output.WriteByte(value[index])
		index++
	}
	return output.String()
}

func commandUsesSelection(name string) bool {
	switch name {
	case "version", "release", "run", "example", "shell-init", "completion", "mcp":
		return false
	default:
		return true
	}
}

func usesTypedPayload(name string) bool {
	return applicationCommandName(name)
}

func unknownHelpMessage(target helpTarget) (string, string, string) {
	if target.Command == "" {
		return "help topic is invalid", "", "omg --help"
	}
	command, found := helpCommandByName(target.Command)
	if !found {
		suggestion := closestCommand(target.Command)
		if suggestion == "" {
			return fmt.Sprintf("unknown help topic %q", target.Command), "", "omg --help"
		}
		return fmt.Sprintf("unknown help topic %q", target.Command), fmt.Sprintf("Did you mean %q?", suggestion), "omg " + suggestion + " --help"
	}
	suggestion := closestSubcommand(command, target.Subcommand)
	if suggestion == "" {
		return fmt.Sprintf("unknown %s subcommand %q", command.Name, target.Subcommand), "", "omg " + command.Name + " --help"
	}
	return fmt.Sprintf("unknown %s subcommand %q", command.Name, target.Subcommand), fmt.Sprintf("Did you mean %q?", suggestion), "omg " + command.Name + " " + suggestion + " --help"
}
