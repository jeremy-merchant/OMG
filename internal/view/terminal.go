package view

import (
	"fmt"
	"strings"

	"github.com/jeremy-merchant/OMG/internal/app/query"
	"github.com/jeremy-merchant/OMG/internal/terminalstyle"
)

type terminalTheme struct {
	enabled bool
	width   int
	palette terminalstyle.Palette
}

func (theme terminalTheme) terminalWidth() int {
	if theme.width == 0 {
		return defaultTTYWidth
	}
	return normalizeTTYWidth(theme.width)
}

func (theme terminalTheme) paint(code, value string) string {
	if !theme.enabled || value == "" {
		return value
	}
	return code + value + terminalstyle.Reset
}

func (theme terminalTheme) bold(value string) string { return theme.paint(terminalstyle.Bold, value) }
func (theme terminalTheme) dim(value string) string  { return theme.paint(theme.palette.Muted, value) }
func (theme terminalTheme) accent(value string) string {
	return theme.paint(theme.palette.Accent, value)
}

func (theme terminalTheme) status(status statusPresentation) string {
	code := theme.palette.Accent
	switch status.Semantic {
	case stateSuccess:
		code = theme.palette.Success
	case stateWarning, statePending:
		code = theme.palette.Warning
	case stateBlocked:
		code = theme.palette.Blocked
	case stateDanger:
		code = theme.palette.Danger
	case stateMuted:
		code = theme.palette.Muted
	}
	return theme.paint(code, status.Glyph+" "+strings.ToUpper(status.Label))
}

func newViewTerminalTheme(color bool, width int) terminalTheme {
	return terminalTheme{enabled: color, width: normalizeTTYWidth(width), palette: terminalstyle.CurrentPalette()}
}

func renderTTY(board query.BoardSnapshot, color bool) string {
	return renderTTYWidth(board, color, defaultTTYWidth)
}

func renderTTYWidth(board query.BoardSnapshot, color bool, width int) string {
	theme := newViewTerminalTheme(color, width)
	health := boardHealth(board)
	var out strings.Builder

	product := theme.bold("OMG") + theme.accent("  OPERATOR LEDGER") + theme.dim(" / BOARD")
	out.WriteString(product + "\n")
	writeTTYWrapped(&out, theme, "", "", boardContext(board), theme.dim)
	writeTTYStatusLine(&out, theme, "", "", health.Status, health.Headline, health.Detail)
	out.WriteString(theme.dim(strings.Repeat("━", theme.terminalWidth())) + "\n")

	writeTTYNow(&out, theme, board)
	writeTTYSessions(&out, theme, board)
	writeTTYWork(&out, theme, board)
	writeTTYDependencies(&out, theme, board)
	writeTTYInbox(&out, theme, board)
	writeTTYHandoffs(&out, theme, board)
	writeTTYReservations(&out, theme, board)
	writeTTYGit(&out, theme, board)
	writeTTYActions(&out, theme, board)
	writeTTYSnapshot(&out, theme, board)
	return out.String()
}

func writeTTYHeading(out *strings.Builder, theme terminalTheme, title, detail string) {
	out.WriteString("\n")
	title = strings.ToUpper(title)
	combined := title
	if detail != "" {
		combined += "  " + detail
	}
	if ttyDisplayWidth(combined) <= theme.terminalWidth() {
		out.WriteString(theme.accent(title))
		if detail != "" {
			out.WriteString(theme.dim("  " + detail))
		}
		out.WriteByte('\n')
		return
	}
	out.WriteString(theme.accent(title) + "\n")
	if detail != "" {
		writeTTYWrapped(out, theme, "  ", "  ", detail, theme.dim)
	}
}

func writeTTYEmpty(out *strings.Builder, theme terminalTheme, message string) {
	writeTTYWrapped(out, theme, "  · ", "    ", message, theme.dim)
}

func writeTTYStatusLine(out *strings.Builder, theme terminalTheme, indent, connector string, status statusPresentation, primary, meta string) {
	prefix := indent + connector
	styledPrefix := indent + theme.dim(connector) + theme.status(status)
	prefixWidth := ttyDisplayWidth(prefix) + ttyDisplayWidth(theme.status(status))
	content := primary
	if meta != "" {
		if content != "" {
			content += " · "
		}
		content += meta
	}
	if content == "" {
		out.WriteString(styledPrefix + "\n")
		return
	}
	if ttyDisplayWidth(styledPrefix)+2+ttyDisplayWidth(content) <= theme.terminalWidth() {
		out.WriteString(styledPrefix + "  " + primary)
		if meta != "" {
			out.WriteString(theme.dim(" · " + meta))
		}
		out.WriteByte('\n')
		return
	}
	available := theme.terminalWidth() - prefixWidth - 2
	if available < 8 {
		out.WriteString(styledPrefix + "\n")
		continuation := strings.Repeat(" ", minTTYIndent(prefixWidth+2, theme.terminalWidth()))
		writeTTYWrapped(out, theme, continuation, continuation, content, nil)
		return
	}
	lines := wrapTTYText(content, available)
	if len(lines) == 0 {
		out.WriteString(styledPrefix + "\n")
		return
	}
	out.WriteString(styledPrefix + "  " + lines[0] + "\n")
	continuation := strings.Repeat(" ", prefixWidth+2)
	for _, line := range lines[1:] {
		out.WriteString(continuation + line + "\n")
	}
}

func writeTTYWrapped(out *strings.Builder, theme terminalTheme, firstPrefix, continuationPrefix, value string, style func(string) string) {
	available := theme.terminalWidth() - ttyDisplayWidth(firstPrefix)
	if continuationAvailable := theme.terminalWidth() - ttyDisplayWidth(continuationPrefix); continuationAvailable < available {
		available = continuationAvailable
	}
	if available < 1 {
		available = 1
	}
	lines := wrapTTYText(value, available)
	if len(lines) == 0 {
		return
	}
	for index, line := range lines {
		prefix := continuationPrefix
		if index == 0 {
			prefix = firstPrefix
		}
		rendered := prefix + line
		if style != nil {
			rendered = style(rendered)
		}
		out.WriteString(rendered + "\n")
	}
}

func minTTYIndent(indent, width int) int {
	maximum := width / 2
	if maximum < 2 {
		maximum = 2
	}
	if indent > maximum {
		return maximum
	}
	return indent
}

func writeTTYNow(out *strings.Builder, theme terminalTheme, board query.BoardSnapshot) {
	writeTTYHeading(out, theme, "Now", "who is working and what is blocked")
	activeSessions := 0
	for _, identity := range allIdentities(board) {
		if activeIdentity(identity) {
			activeSessions++
		}
	}
	activeTasks := 0
	for _, task := range board.Tasks {
		state := presentStatus(task.State).Semantic
		if state == stateWorking || state == statePending || state == stateBlocked {
			activeTasks++
		}
	}
	blocked := 0
	for _, dependency := range board.Dependencies {
		if !dependency.Satisfied {
			blocked++
		}
	}
	writeTTYStatusLine(out, theme, "  ", "", presentStatus("working"), fmt.Sprintf("%d active session(s) · %d open task(s)", activeSessions, activeTasks), "canonical snapshot")
	if blocked > 0 {
		writeTTYStatusLine(out, theme, "  ", "", presentStatus("blocked"), fmt.Sprintf("%d dependency blocker(s)", blocked), "resolve before claiming dependent work")
	} else {
		writeTTYStatusLine(out, theme, "  ", "", presentStatus("verified"), "No unsatisfied dependency", "work graph is clear")
	}
	if len(board.Warnings) > 0 {
		writeTTYStatusLine(out, theme, "  ", "", presentStatus("warning"), fmt.Sprintf("%d advisory warning(s)", len(board.Warnings)), "see Git + warnings")
	}
}

func writeTTYSessions(out *strings.Builder, theme terminalTheme, board query.BoardSnapshot) {
	identities := allIdentities(board)
	writeTTYHeading(out, theme, "Sessions", fmt.Sprintf("%d identity record(s)", len(identities)))
	if len(identities) == 0 {
		writeTTYEmpty(out, theme, "No session identity recorded")
		return
	}
	children := make(map[string][]query.IdentityView)
	roots := make([]query.IdentityView, 0, len(identities))
	known := make(map[string]struct{}, len(identities))
	for _, identity := range identities {
		known[identity.ID] = struct{}{}
	}
	for _, identity := range identities {
		if identity.ParentSessionID == "" {
			roots = append(roots, identity)
			continue
		}
		if _, exists := known[identity.ParentSessionID]; !exists {
			roots = append(roots, identity)
			continue
		}
		children[identity.ParentSessionID] = append(children[identity.ParentSessionID], identity)
	}
	if len(roots) == 0 {
		roots = append(roots, identities...)
	}
	visited := make(map[string]bool, len(identities))
	for index, root := range roots {
		writeTTYSessionNode(out, theme, root, children, visited, "  ", index == len(roots)-1)
	}
}

func writeTTYSessionNode(out *strings.Builder, theme terminalTheme, session query.IdentityView, children map[string][]query.IdentityView, visited map[string]bool, prefix string, last bool) {
	if visited[session.ID] {
		writeTTYStatusLine(out, theme, prefix, "└─ ", presentStatus("warning"), shortID(session.ID), "cycle suppressed")
		return
	}
	visited[session.ID] = true
	connector := "├─ "
	childPrefix := prefix + "│  "
	if last {
		connector = "└─ "
		childPrefix = prefix + "   "
	}
	state := presentStatus(string(session.Liveness))
	if session.Liveness == "" {
		state = presentStatus(session.NativeAccessState)
	}
	primary := shortID(session.ID)
	if session.Role != "" {
		primary += "  " + text(session.Role)
	}
	meta := strings.Join(nonEmpty(
		"kind="+text(session.Kind),
		"runtime="+text(session.Runtime),
		"liveness="+text(string(session.Liveness)),
		"access="+text(session.NativeAccessState),
		"current_task="+text(session.TaskID),
	), " · ")
	if compactTTY(theme) {
		meta = ""
	}
	writeTTYStatusLine(out, theme, prefix, connector, state, primary, meta)
	writeTTYWrapped(out, theme, childPrefix+"   ", childPrefix+"   ", identity(&session, text), theme.dim)
	for index, child := range children[session.ID] {
		writeTTYSessionNode(out, theme, child, children, visited, childPrefix, index == len(children[session.ID])-1)
	}
}

func writeTTYWork(out *strings.Builder, theme terminalTheme, board query.BoardSnapshot) {
	writeTTYHeading(out, theme, "Work graph", fmt.Sprintf("%d task(s) · %d run(s) · %d progress update(s)", len(board.Tasks), len(board.Runs), len(board.Progress)))
	if len(board.Tasks) == 0 && len(board.Runs) == 0 && len(board.Progress) == 0 {
		writeTTYEmpty(out, theme, "No task or run recorded")
		return
	}
	children := make(map[string][]query.TaskView)
	roots := make([]query.TaskView, 0, len(board.Tasks))
	known := make(map[string]struct{}, len(board.Tasks))
	for _, task := range board.Tasks {
		known[task.ID] = struct{}{}
	}
	for _, task := range board.Tasks {
		if task.ParentTaskID == "" {
			roots = append(roots, task)
			continue
		}
		if _, exists := known[task.ParentTaskID]; !exists {
			roots = append(roots, task)
			continue
		}
		children[task.ParentTaskID] = append(children[task.ParentTaskID], task)
	}
	if len(roots) == 0 {
		roots = append(roots, board.Tasks...)
	}
	runsByTask := make(map[string][]query.RunView)
	for _, run := range board.Runs {
		runsByTask[run.TaskID] = append(runsByTask[run.TaskID], run)
	}
	progressByTask := make(map[string][]query.ProgressView)
	for _, progress := range board.Progress {
		progressByTask[progress.TaskID] = append(progressByTask[progress.TaskID], progress)
	}
	visited := make(map[string]bool, len(board.Tasks))
	for index, task := range roots {
		writeTTYTaskNode(out, theme, task, children, runsByTask, progressByTask, visited, "  ", index == len(roots)-1)
	}
	for _, run := range board.Runs {
		if _, exists := known[run.TaskID]; exists {
			continue
		}
		writeTTYStatusLine(out, theme, "  ", "└─ ", presentStatus(run.State), "run "+shortID(run.ID), "task="+text(run.TaskID)+" · session="+text(run.SessionID)+" · state="+text(run.State))
	}
}

func writeTTYTaskNode(out *strings.Builder, theme terminalTheme, task query.TaskView, children map[string][]query.TaskView, runs map[string][]query.RunView, progress map[string][]query.ProgressView, visited map[string]bool, prefix string, last bool) {
	if visited[task.ID] {
		writeTTYStatusLine(out, theme, prefix, "└─ ", presentStatus("warning"), shortID(task.ID), "cycle suppressed")
		return
	}
	visited[task.ID] = true
	connector := "├─ "
	childPrefix := prefix + "│  "
	if last {
		connector = "└─ "
		childPrefix = prefix + "   "
	}
	primary := fmt.Sprintf("#%d %s", task.DisplayNumber, text(task.Title))
	meta := strings.Join(nonEmpty("task="+text(task.ID), "claimed_by_session="+text(task.ClaimedBySessionID), "parent_task="+text(task.ParentTaskID)), " · ")
	if compactTTY(theme) {
		meta = ""
	}
	writeTTYStatusLine(out, theme, prefix, connector, presentStatus(task.State), primary, meta)
	writeTTYWrapped(out, theme, childPrefix+"   ", childPrefix+"   ", ttyTaskMetadata(task), theme.dim)
	for _, run := range runs[task.ID] {
		writeTTYStatusLine(out, theme, childPrefix, "├─ ", presentStatus(run.State), "run "+shortID(run.ID), strings.Join(nonEmpty("task="+text(run.TaskID), "session="+text(run.SessionID), "state="+text(run.State)), " · "))
	}
	for _, item := range progress[task.ID] {
		state := presentStatus("working")
		if len(item.Doing) == 0 && len(item.Next) == 0 {
			state = presentStatus("verified")
		}
		writeTTYStatusLine(out, theme, childPrefix, "├─ ", state, "progress "+shortID(item.ID), strings.Join(nonEmpty("task="+text(item.TaskID), "run="+text(item.RunID), "phase="+text(item.Phase)), " · "))
		for _, value := range item.Done {
			writeTTYWrapped(out, theme, childPrefix+"│    ✔ done  ", childPrefix+"│            ", text(value), theme.dim)
		}
		for _, value := range item.Doing {
			writeTTYWrapped(out, theme, childPrefix+"│    ⟳ doing ", childPrefix+"│            ", text(value), nil)
		}
		for _, value := range item.Next {
			writeTTYWrapped(out, theme, childPrefix+"│    ○ next  ", childPrefix+"│            ", text(value), theme.dim)
		}
		writeTTYWrapped(out, theme, childPrefix+"│    ", childPrefix+"│    ", ttyProgressMetadata(item), theme.dim)
	}
	for index, child := range children[task.ID] {
		writeTTYTaskNode(out, theme, child, children, runs, progress, visited, childPrefix, index == len(children[task.ID])-1)
	}
}

func compactTTY(theme terminalTheme) bool {
	return theme.terminalWidth() < 68
}

func ttyTaskMetadata(task query.TaskView) string {
	return strings.Join(nonEmpty(
		"task="+text(task.ID),
		"created_by_session="+text(task.CreatedBySessionID),
		"claimed_by_session="+text(task.ClaimedBySessionID),
		"parent_task="+text(task.ParentTaskID),
		"created="+stamp(task.CreatedAt),
		"updated="+stamp(task.UpdatedAt),
	), " · ")
}

func ttyProgressMetadata(progress query.ProgressView) string {
	fields := nonEmpty(
		"progress_id="+text(progress.ID),
		"session="+text(progress.SessionID),
		"created="+stamp(progress.CreatedAt),
	)
	if len(progress.Done) == 0 {
		fields = append(fields, "done=none")
	}
	if len(progress.Doing) == 0 {
		fields = append(fields, "doing=none")
	}
	if len(progress.Next) == 0 {
		fields = append(fields, "next=none")
	}
	return strings.Join(fields, " · ")
}

func writeTTYDependencies(out *strings.Builder, theme terminalTheme, board query.BoardSnapshot) {
	writeTTYHeading(out, theme, "Dependencies", fmt.Sprintf("%d directed edge(s)", len(board.Dependencies)))
	if len(board.Dependencies) == 0 {
		writeTTYEmpty(out, theme, "No dependency edge recorded")
		return
	}
	for index, dependency := range board.Dependencies {
		connector := "├─ "
		if index == len(board.Dependencies)-1 {
			connector = "└─ "
		}
		primary := shortID(dependency.DependentTaskID) + "  ←  " + shortID(dependency.BlockerTaskID)
		meta := strings.TrimPrefix(dependenciesRows(query.BoardSnapshot{Dependencies: []query.DependencyView{dependency}}, text)[0], "Dependency "+text(dependency.ID)+" ")
		writeTTYStatusLine(out, theme, "  ", connector, dependencyStatus(dependency), primary, "dependency="+text(dependency.ID)+" · "+meta)
	}
}

func writeTTYInbox(out *strings.Builder, theme terminalTheme, board query.BoardSnapshot) {
	writeTTYHeading(out, theme, "Inbox", fmt.Sprintf("%d message(s)", len(board.Inbox)))
	if len(board.Inbox) == 0 {
		writeTTYEmpty(out, theme, "Inbox is clear")
		return
	}
	for index, message := range board.Inbox {
		connector := "├─ "
		if index == len(board.Inbox)-1 {
			connector = "└─ "
		}
		state := presentStatus(message.Acknowledgement)
		if strings.EqualFold(message.Acknowledgement, "pending") || strings.EqualFold(message.Acknowledgement, "unread") {
			state = presentStatus("pending")
		}
		primary := text(message.Subject)
		if primary == "" {
			primary = text(message.Type) + " message"
		}
		meta := strings.TrimPrefix(inboxRows(query.BoardSnapshot{Inbox: []query.InboxItemView{message}}, text)[0], "Inbox "+text(message.MessageID)+" ")
		writeTTYStatusLine(out, theme, "  ", connector, state, primary, "message="+text(message.MessageID)+" · "+meta)
	}
}

func writeTTYHandoffs(out *strings.Builder, theme terminalTheme, board query.BoardSnapshot) {
	writeTTYHeading(out, theme, "Handoffs", fmt.Sprintf("%d transfer(s)", len(board.Handoffs)))
	if len(board.Handoffs) == 0 {
		writeTTYEmpty(out, theme, "No handoff waiting")
		return
	}
	for index, handoff := range board.Handoffs {
		connector := "├─ "
		if index == len(board.Handoffs)-1 {
			connector = "└─ "
		}
		stateValue := handoff.Status
		if handoff.Decision != nil && handoff.Decision.Decision != "" {
			stateValue = handoff.Decision.Decision
		}
		primary := shortID(handoff.SourceSessionID) + "  →  " + shortID(firstNonEmpty(handoff.TargetSessionID, handoff.TargetTaskID))
		if handoff.Summary != "" {
			primary += "  " + text(handoff.Summary)
		}
		meta := strings.TrimPrefix(handoffRows(query.BoardSnapshot{Handoffs: []query.HandoffView{handoff}}, text)[0], "Handoff "+text(handoff.ID)+" ")
		writeTTYStatusLine(out, theme, "  ", connector, presentStatus(stateValue), primary, "handoff="+text(handoff.ID)+" · "+meta)
	}
}

func writeTTYReservations(out *strings.Builder, theme terminalTheme, board query.BoardSnapshot) {
	writeTTYHeading(out, theme, "Reservations", fmt.Sprintf("%d path claim(s)", len(board.Reservations)))
	if len(board.Reservations) == 0 {
		writeTTYEmpty(out, theme, "No active path reservation")
		return
	}
	for index, reservation := range board.Reservations {
		connector := "├─ "
		if index == len(board.Reservations)-1 {
			connector = "└─ "
		}
		primary := firstNonEmpty(reservation.Pattern, reservation.PatternFingerprint, reservation.PatternKind)
		meta := strings.TrimPrefix(reservationRows(query.BoardSnapshot{Reservations: []query.ReservationView{reservation}}, text)[0], "Reservation "+text(reservation.ID)+" ")
		writeTTYStatusLine(out, theme, "  ", connector, reservationStatus(reservation), text(primary), "reservation="+text(reservation.ID)+" · "+meta)
	}
}

func writeTTYGit(out *strings.Builder, theme terminalTheme, board query.BoardSnapshot) {
	assetCount := 0
	if board.Git != nil {
		assetCount = len(board.Git.Assets)
	}
	writeTTYHeading(out, theme, "Git + warnings", fmt.Sprintf("%d asset(s) · %d warning(s)", assetCount, len(board.Warnings)))
	if len(board.Warnings) == 0 && assetCount == 0 {
		writeTTYEmpty(out, theme, "No Git observation or warning recorded")
		return
	}
	for _, warning := range board.Warnings {
		writeTTYStatusLine(out, theme, "  ", "├─ ", presentStatus("warning"), text(warning), "advisory")
	}
	if board.Git != nil {
		for index, asset := range board.Git.Assets {
			connector := "├─ "
			if index == len(board.Git.Assets)-1 {
				connector = "└─ "
			}
			primary := strings.Join(nonEmpty(text(asset.Branch), shortID(asset.Head), text(asset.Type)), "  ")
			meta := strings.TrimPrefix(gitRows(query.BoardSnapshot{Git: &query.GitView{Assets: []query.GitAssetView{asset}}}, text)[0], "Asset ")
			writeTTYStatusLine(out, theme, "  ", connector, gitAssetStatus(asset), primary, meta)
		}
	}
}

func writeTTYActions(out *strings.Builder, theme terminalTheme, board query.BoardSnapshot) {
	writeTTYHeading(out, theme, "Command palette", fmt.Sprintf("%d safe action(s)", len(board.SuggestedActions)))
	if len(board.SuggestedActions) == 0 {
		writeTTYEmpty(out, theme, "No suggested action for this scope")
		return
	}
	writeTTYWrapped(out, theme, "  ", "  ", "Copy a command, replace '<PROJECT_PATH>' with the intended absolute checkout path, review its scope, then run it in your shell. Nothing is executed by this view.", theme.dim)
	for _, group := range groupHTMLActions(board.SuggestedActions) {
		if len(group.Actions) == 0 {
			continue
		}
		writeTTYWrapped(out, theme, "\n  ", "  ", strings.ToUpper(group.Label)+"  "+fmt.Sprintf("%d action(s)", len(group.Actions)), theme.accent)
		for _, action := range group.Actions {
			label := text(action.Code)
			if label == "" {
				label = "safe action"
			}
			writeTTYWrapped(out, theme, "    ", "    ", label+"  "+htmlActionDescription(action.Code), theme.dim)
			writeTTYWrapped(out, theme, "    ❯ ", "      ", text(action.Command), nil)
		}
	}
}

func writeTTYSnapshot(out *strings.Builder, theme terminalTheme, board query.BoardSnapshot) {
	writeTTYHeading(out, theme, "Snapshot", "canonical metadata")
	fields := []string{
		"project=" + primaryProject(board),
		"workspace=" + text(board.Scope.WorkspaceID),
		"mode=" + text(string(board.Scope.Mode)),
		"selector=" + text(board.Scope.Selector),
		"generated_at=" + stamp(board.GeneratedAt),
		"snapshot_cursor=" + text(board.SnapshotCursor),
		"schema_version=" + fmt.Sprint(board.SchemaVersion),
		"view_version=" + fmt.Sprint(board.ViewVersion),
		"redaction_policy=" + text(board.Redaction.PolicyName),
		"redaction_policy_version=" + fmt.Sprint(board.Redaction.PolicyVersion),
		"content_omitted=" + fmt.Sprint(board.Redaction.ContentOmitted),
		"content_redacted=" + fmt.Sprint(board.Redaction.ContentRedacted),
	}
	for _, field := range fields {
		if strings.HasSuffix(field, "=") {
			continue
		}
		writeTTYWrapped(out, theme, "  · ", "    ", field, theme.dim)
	}
}

func nonEmpty(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || strings.HasSuffix(value, "=") {
			continue
		}
		result = append(result, value)
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return "unassigned"
}
