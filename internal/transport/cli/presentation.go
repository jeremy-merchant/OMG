package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/jeremy-merchant/OMG/internal/agentinstall"
	"github.com/jeremy-merchant/OMG/internal/app"
	"github.com/jeremy-merchant/OMG/internal/domain"
	"github.com/jeremy-merchant/OMG/internal/terminalstyle"
)

type terminalTheme struct {
	color   bool
	palette terminalstyle.Palette
}

type terminalErrorContext struct {
	Hint string
	Next string
}

type presentationFact struct {
	Label string
	Value string
}

func newTerminalTheme(color bool) terminalTheme {
	return terminalTheme{color: color, palette: terminalstyle.CurrentPalette()}
}

func (theme terminalTheme) paint(code, value string) string {
	if !theme.color || value == "" {
		return value
	}
	return code + value + terminalstyle.Reset
}

func (theme terminalTheme) bold(value string) string { return theme.paint(terminalstyle.Bold, value) }
func (theme terminalTheme) dim(value string) string  { return theme.paint(theme.palette.Muted, value) }
func (theme terminalTheme) info(value string) string { return theme.paint(theme.palette.Accent, value) }
func (theme terminalTheme) success(value string) string {
	return theme.paint(theme.palette.Success, value)
}
func (theme terminalTheme) warn(value string) string {
	return theme.paint(theme.palette.Warning, value)
}
func (theme terminalTheme) blocked(value string) string {
	return theme.paint(theme.palette.Blocked, value)
}
func (theme terminalTheme) danger(value string) string {
	return theme.paint(theme.palette.Danger, value)
}

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

func renderSuccess(output io.Writer, data any) {
	theme := newTerminalTheme(cliTerminalColorEnabled(output))
	width := cliTerminalWidth(output)
	var rendered strings.Builder
	rendered.WriteString(theme.success("✔") + " " + theme.bold("OMG") + theme.success("  VERIFIED") + "\n")
	facts := presentationFacts(data)
	if len(facts) == 0 {
		rendered.WriteString("  " + theme.dim("Command completed without a result payload.") + "\n")
		_, _ = io.WriteString(output, rendered.String())
		return
	}
	writePresentationFacts(&rendered, theme, width, facts)
	_, _ = io.WriteString(output, rendered.String())
}

func renderError(output io.Writer, err domain.DomainError, exit int) {
	renderErrorWithContext(output, err, exit, terminalErrorContext{})
}

func renderErrorWithContext(output io.Writer, err domain.DomainError, exit int, context terminalErrorContext) {
	theme := newTerminalTheme(cliTerminalColorEnabled(output))
	width := cliTerminalWidth(output)
	var rendered strings.Builder
	rendered.WriteString(theme.danger("✘") + " " + theme.bold("OMG") + theme.danger("  ERROR") + "\n")

	next := context.Next
	if next == "" {
		next = nextCommand(err)
	}
	facts := []presentationFact{
		{Label: "code", Value: string(err.Code)},
		{Label: "cause", Value: neutralizeTerminalControls(err.Message)},
		{Label: "retryable", Value: retryLabel(err.Retryable)},
	}
	if context.Hint != "" {
		facts = append(facts, presentationFact{Label: "hint", Value: neutralizeTerminalControls(context.Hint)})
	}
	facts = append(facts,
		presentationFact{Label: "next", Value: next},
		presentationFact{Label: "exit", Value: fmt.Sprint(exit)},
	)
	writePresentationFacts(&rendered, theme, width, facts)
	_, _ = io.WriteString(output, rendered.String())
}

func renderRuntimeResult(output io.Writer, result app.CLIRuntimeResult) {
	theme := newTerminalTheme(cliTerminalColorEnabled(output))
	width := cliTerminalWidth(output)
	var rendered strings.Builder
	glyph := theme.success("✔")
	heading := theme.success("  RUN COMPLETE")
	if result.ExitCode != 0 || strings.Contains(strings.ToLower(result.Status), "fail") {
		glyph = theme.warn("⚠")
		heading = theme.warn("  RUN EXITED")
	}
	rendered.WriteString("\n" + glyph + " " + theme.bold("OMG") + heading + "\n")
	writePresentationFacts(&rendered, theme, width, []presentationFact{
		{Label: "status", Value: result.Status},
		{Label: "runtime", Value: result.Runtime},
		{Label: "executable", Value: result.Executable},
		{Label: "resolution", Value: result.Resolution},
		{Label: "exit", Value: fmt.Sprint(result.ExitCode)},
	})
	_, _ = io.WriteString(output, rendered.String())
}

func renderAgentReport(output io.Writer, report agentinstall.Report) {
	theme := newTerminalTheme(cliTerminalColorEnabled(output))
	width := cliTerminalWidth(output)
	var rendered strings.Builder
	glyph, heading := theme.success("✔"), theme.success("  AGENT HARNESS")
	if report.Summary.Unsafe > 0 || report.Summary.Drifted > 0 {
		glyph, heading = theme.warn("⚠"), theme.warn("  AGENT HARNESS NEEDS ATTENTION")
	} else if report.Summary.Missing > 0 && report.Status != "uninstalled" {
		glyph, heading = theme.info("○"), theme.info("  AGENT HARNESS")
	}
	rendered.WriteString(glyph + " " + theme.bold("OMG") + heading + "\n")
	writePresentationFacts(&rendered, theme, width, []presentationFact{
		{Label: "status", Value: report.Status},
		{Label: "detected", Value: fmt.Sprint(report.Summary.Detected)},
		{Label: "installed", Value: fmt.Sprint(report.Summary.Installed)},
		{Label: "attention", Value: fmt.Sprint(report.Summary.Drifted + report.Summary.Unsafe)},
	})
	if len(report.Surfaces) > 0 {
		rendered.WriteString("\n  " + theme.bold("Discovery surfaces") + "\n")
	}
	for index, surface := range report.Surfaces {
		branch := "├─"
		if index == len(report.Surfaces)-1 {
			branch = "└─"
		}
		stateGlyph, stateText := agentSurfaceStyle(theme, surface.State)
		statusLine := fmt.Sprintf("  %s %s %s %s", branch, stateGlyph, theme.bold(surface.Provider), theme.dim(surface.Kind))
		rendered.WriteString(statusLine + "\n")

		metadata := neutralizeTerminalControls(surface.Path)
		if surface.Detected {
			metadata += " · detected"
		}
		writeAgentMetadata(&rendered, theme, width, metadata)
		if surface.Action != "" && surface.Action != "none" {
			writeAgentMetadata(&rendered, theme, width, "action · "+stateText+" · "+neutralizeTerminalControls(surface.Action))
		}
	}
	_, _ = io.WriteString(output, rendered.String())
}

func writeAgentMetadata(output *strings.Builder, theme terminalTheme, width int, value string) {
	const prefix = "     "
	available := width - terminalDisplayWidth(prefix)
	if available < 1 {
		available = 1
	}
	lines := wrapTerminalText(value, available)
	if len(lines) == 0 {
		lines = []string{"—"}
	}
	for _, line := range lines {
		output.WriteString(prefix + theme.dim(line) + "\n")
	}
}

func agentSurfaceStyle(theme terminalTheme, state agentinstall.State) (string, string) {
	switch state {
	case agentinstall.StateInstalled:
		return theme.success("✔"), theme.success(string(state))
	case agentinstall.StateMissing:
		return theme.dim("○"), theme.dim(string(state))
	case agentinstall.StateDrifted:
		return theme.warn("⚠"), theme.warn(string(state))
	case agentinstall.StateUnsafe:
		return theme.danger("✘"), theme.danger(string(state))
	default:
		return theme.dim("○"), theme.dim(string(state))
	}
}

func writePresentationFacts(output *strings.Builder, theme terminalTheme, width int, facts []presentationFact) {
	labelWidth := 0
	for _, fact := range facts {
		if measured := terminalDisplayWidth(fact.Label); measured > labelWidth {
			labelWidth = measured
		}
	}
	if labelWidth > 14 {
		labelWidth = 14
	}
	wide := width >= 54
	for _, fact := range facts {
		value := neutralizeTerminalControls(fact.Value)
		if value == "" {
			value = "—"
		}
		if wide {
			prefix := "  " + theme.dim(padTerminalRight(fact.Label, labelWidth)) + "  "
			available := width - labelWidth - 4
			lines := wrapTerminalText(value, available)
			if len(lines) == 0 {
				lines = []string{"—"}
			}
			output.WriteString(prefix + styleFactValue(theme, fact.Label, lines[0]) + "\n")
			continuation := strings.Repeat(" ", labelWidth+4)
			for _, line := range lines[1:] {
				output.WriteString(continuation + line + "\n")
			}
			continue
		}
		output.WriteString("  " + theme.dim(fact.Label) + "\n")
		for _, line := range wrapTerminalText(value, width-4) {
			output.WriteString("    " + styleFactValue(theme, fact.Label, line) + "\n")
		}
	}
}

func styleFactValue(theme terminalTheme, label, value string) string {
	switch label {
	case "status":
		return theme.success(value)
	case "hint":
		return theme.warn(value)
	case "next":
		return theme.info(value)
	case "code", "exit":
		return theme.bold(value)
	default:
		return value
	}
}

func presentationFacts(data any) []presentationFact {
	value := reflect.ValueOf(data)
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	if !value.IsValid() {
		return nil
	}
	return factsFromValue(value, "")
}

func factsFromValue(value reflect.Value, label string) []presentationFact {
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return []presentationFact{{Label: label, Value: "—"}}
		}
		value = value.Elem()
	}
	if !value.IsValid() {
		return nil
	}

	switch value.Kind() {
	case reflect.Struct:
		facts := make([]presentationFact, 0, value.NumField())
		typeInfo := value.Type()
		for index := 0; index < value.NumField(); index++ {
			fieldInfo := typeInfo.Field(index)
			if fieldInfo.PkgPath != "" {
				continue
			}
			name := jsonFieldName(fieldInfo)
			if name == "-" {
				continue
			}
			field := value.Field(index)
			if hasJSONOmitEmpty(fieldInfo) && isZeroPresentationValue(field) {
				continue
			}
			facts = append(facts, factsFromValue(field, humanizeFactLabel(name))...)
		}
		return facts
	case reflect.Map:
		keys := value.MapKeys()
		sort.Slice(keys, func(first, second int) bool {
			return fmt.Sprint(keys[first].Interface()) < fmt.Sprint(keys[second].Interface())
		})
		facts := make([]presentationFact, 0, len(keys))
		for _, key := range keys {
			keyLabel := humanizeFactLabel(fmt.Sprint(key.Interface()))
			facts = append(facts, factsFromValue(value.MapIndex(key), keyLabel)...)
		}
		return facts
	case reflect.Slice, reflect.Array:
		if value.Len() == 0 {
			return []presentationFact{{Label: label, Value: "none"}}
		}
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return []presentationFact{{Label: label, Value: fmt.Sprint(value.Interface())}}
		}
		items := make([]string, 0, value.Len())
		for index := 0; index < value.Len(); index++ {
			items = append(items, compactPresentationValue(value.Index(index)))
		}
		return []presentationFact{{Label: label, Value: strings.Join(items, " · ")}}
	default:
		return []presentationFact{{Label: label, Value: scalarPresentationValue(value)}}
	}
}

func compactPresentationValue(value reflect.Value) string {
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return "—"
		}
		value = value.Elem()
	}
	if !value.IsValid() {
		return "—"
	}
	switch value.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return scalarPresentationValue(value)
	}
	encoded, err := json.Marshal(value.Interface())
	if err != nil {
		return neutralizeTerminalControls(fmt.Sprint(value.Interface()))
	}
	return neutralizeTerminalControls(string(encoded))
}

func scalarPresentationValue(value reflect.Value) string {
	if value.IsValid() && value.CanInterface() {
		return neutralizeTerminalControls(fmt.Sprint(value.Interface()))
	}
	return "—"
}

func jsonFieldName(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if tag == "" {
		return field.Name
	}
	name := strings.Split(tag, ",")[0]
	if name == "" {
		return field.Name
	}
	return name
}

func hasJSONOmitEmpty(field reflect.StructField) bool {
	for _, option := range strings.Split(field.Tag.Get("json"), ",")[1:] {
		if option == "omitempty" {
			return true
		}
	}
	return false
}

func isZeroPresentationValue(value reflect.Value) bool {
	for value.IsValid() && value.Kind() == reflect.Interface {
		if value.IsNil() {
			return true
		}
		value = value.Elem()
	}
	return !value.IsValid() || value.IsZero()
}

func humanizeFactLabel(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "_", " "))
	if value == "" {
		return "result"
	}
	return value
}

func retryLabel(retryable bool) string {
	if retryable {
		return "available"
	}
	return "unavailable"
}

func nextCommand(err domain.DomainError) string {
	switch err.Code {
	case domain.CodeInvalidArgument:
		return "omg --help"
	case domain.CodeNotFound:
		return "omg board all"
	case domain.CodeConflict:
		return "omg board all"
	case domain.CodeUninitialized:
		return "omg init"
	case domain.CodeUnavailable:
		return "omg doctor"
	case domain.CodeCommandNotWired:
		return "omg --help"
	default:
		if err.Retryable {
			return "retry the same command"
		}
		return "omg doctor"
	}
}
