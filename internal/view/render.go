// Package view renders canonical, already-authorized query snapshots.
package view

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"example.invalid/coordledger/internal/app/query"
)

// Format identifies a supported board presentation.
type Format string

const (
	FormatTTY      Format = "tty"
	FormatJSON     Format = "json"
	FormatMarkdown Format = "markdown"
	FormatHTML     Format = "html"
)

// Render validates a board ViewModel once and writes the requested pure presentation.
func Render(format Format, model query.ViewModel, output io.Writer) error {
	if !supportedFormat(format) {
		return fmt.Errorf("view: unsupported format %q", format)
	}
	board, err := decodeBoard(model)
	if err != nil {
		return err
	}
	if format == FormatJSON {
		encoded, err := model.MarshalJSON()
		if err != nil {
			return fmt.Errorf("view: marshal model: %w", err)
		}
		_, err = output.Write(append(encoded, '\n'))
		return err
	}

	var rendered string
	switch format {
	case FormatTTY:
		rendered = renderTTY(board, terminalColorEnabled(output))
	case FormatMarkdown:
		rendered = renderMarkdown(board)
	case FormatHTML:
		rendered = renderHTML(board)
	}
	_, err = io.WriteString(output, rendered)
	return err
}

func supportedFormat(format Format) bool {
	switch format {
	case FormatTTY, FormatJSON, FormatMarkdown, FormatHTML:
		return true
	default:
		return false
	}
}

func decodeBoard(model query.ViewModel) (query.BoardSnapshot, error) {
	if model.Kind() != "board" {
		return query.BoardSnapshot{}, fmt.Errorf("view: unsupported model kind %q", model.Kind())
	}
	var board query.BoardSnapshot
	if err := json.Unmarshal(model.Data(), &board); err != nil {
		return query.BoardSnapshot{}, fmt.Errorf("view: invalid board model: %w", err)
	}
	if board.SchemaVersion != query.BoardSchemaVersion {
		return query.BoardSnapshot{}, fmt.Errorf("view: unsupported board schema %d", board.SchemaVersion)
	}
	return board, nil
}

func renderMarkdown(board query.BoardSnapshot) string {
	var out strings.Builder
	out.WriteString("# Board\n\n")
	out.WriteString("- View version: " + fmt.Sprint(board.ViewVersion) + "\n")
	out.WriteString("- Schema version: " + fmt.Sprint(board.SchemaVersion) + "\n")
	out.WriteString("- Generated at: " + stamp(board.GeneratedAt) + "\n")
	out.WriteString("- Project: " + markdown(board.Scope.ProjectID) + "\n")
	if board.Scope.WorkspaceID != "" {
		out.WriteString("- Workspace: " + markdown(board.Scope.WorkspaceID) + "\n")
	}
	out.WriteString("- Mode: " + markdown(string(board.Scope.Mode)) + "\n")
	if board.Scope.Selector != "" {
		out.WriteString("- Selector: " + markdown(board.Scope.Selector) + "\n")
	}
	out.WriteString("- Snapshot: " + markdown(board.SnapshotCursor) + "\n")
	out.WriteString("- Redaction: " + markdown(board.Redaction.PolicyName) + " v" + fmt.Sprint(board.Redaction.PolicyVersion) + " content_omitted=" + fmt.Sprint(board.Redaction.ContentOmitted) + " content_redacted=" + fmt.Sprint(board.Redaction.ContentRedacted) + "\n")
	writeMarkdownSection(&out, "Identity", markdownIdentity(board))
	writeMarkdownSection(&out, "Current tasks/runs", markdownTasksRuns(board))
	writeMarkdownSection(&out, "Progress", markdownProgress(board))
	writeMarkdownSection(&out, "Dependencies", markdownDependencies(board))
	writeMarkdownSection(&out, "Inbox", markdownInbox(board))
	writeMarkdownSection(&out, "Handoffs", markdownHandoffs(board))
	writeMarkdownSection(&out, "Reservations", markdownReservations(board))
	writeMarkdownSection(&out, "Git warnings/assets", markdownGit(board))
	writeMarkdownSection(&out, "Suggested safe actions", markdownActions(board))
	return out.String()
}

func writeMarkdownSection(out *strings.Builder, title string, rows []string) {
	out.WriteString("\n## " + title + "\n\n")
	if len(rows) == 0 {
		out.WriteString("None\n")
		return
	}
	for _, row := range rows {
		out.WriteString("- " + row + "\n")
	}
}

func ttyIdentity(board query.BoardSnapshot) []string      { return identityRows(board, text) }
func markdownIdentity(board query.BoardSnapshot) []string { return identityRows(board, markdown) }
func identityRows(board query.BoardSnapshot, escape func(string) string) []string {
	if board.Identity == nil && len(board.Sessions) == 0 {
		return nil
	}
	rows := make([]string, 0, 1+len(board.Sessions))
	if board.Identity != nil {
		rows = append(rows, "Current: "+identity(board.Identity, escape))
	}
	for i := range board.Sessions {
		rows = append(rows, "Session: "+identity(&board.Sessions[i], escape))
	}
	return rows
}
func identity(v *query.IdentityView, escape func(string) string) string {
	row := "id=" + escape(v.ID) + " kind=" + escape(v.Kind) + " role=" + escape(v.Role) + " runtime=" + escape(v.Runtime) + " instruction_source=" + escape(v.InstructionSource) + " provenance_confidence=" + escape(v.ProvenanceConfidence) + " access=" + escape(v.NativeAccessState) + " liveness=" + escape(string(v.Liveness)) + " worktree_bound=" + fmt.Sprint(v.WorktreeBound) + " started=" + stamp(v.StartedAt)
	for _, field := range []struct {
		name  string
		value string
	}{
		{"human_id", v.HumanID},
		{"parent_session", v.ParentSessionID},
		{"root_session", v.RootSessionID},
		{"root_human_id", v.RootHumanID},
		{"continuation_of", v.ContinuationOfID},
		{"current_task", v.TaskID},
		{"previous_task", v.PreviousTaskID},
		{"branch", v.Branch},
		{"worktree_fingerprint", v.WorktreeFingerprint},
	} {
		if field.value != "" {
			row += " " + field.name + "=" + escape(field.value)
		}
	}
	if v.HeartbeatAt != nil {
		row += " heartbeat=" + stamp(*v.HeartbeatAt)
	}
	if v.EndedAt != nil {
		row += " ended=" + stamp(*v.EndedAt)
	}
	if v.InterruptedAt != nil {
		row += " interrupted=" + stamp(*v.InterruptedAt)
	}
	return row
}

func ttyTasksRuns(b query.BoardSnapshot) []string      { return tasksRunsRows(b, text) }
func markdownTasksRuns(b query.BoardSnapshot) []string { return tasksRunsRows(b, markdown) }
func tasksRunsRows(b query.BoardSnapshot, escape func(string) string) []string {
	rows := make([]string, 0, len(b.Tasks)+len(b.Runs))
	for _, v := range b.Tasks {
		rows = append(rows, "Task "+escape(v.ID)+" #"+fmt.Sprint(v.DisplayNumber)+" state="+escape(v.State)+" title="+escape(v.Title)+" created_by_session="+escape(v.CreatedBySessionID)+" claimed_by_session="+escape(v.ClaimedBySessionID)+" parent_task="+escape(v.ParentTaskID))
	}
	for _, v := range b.Runs {
		rows = append(rows, "Run "+escape(v.ID)+" task="+escape(v.TaskID)+" session="+escape(v.SessionID)+" state="+escape(v.State))
	}
	return rows
}
func ttyProgress(b query.BoardSnapshot) []string      { return progressRows(b, text) }
func markdownProgress(b query.BoardSnapshot) []string { return progressRows(b, markdown) }
func progressRows(b query.BoardSnapshot, escape func(string) string) []string {
	rows := make([]string, 0, len(b.Progress))
	for _, v := range b.Progress {
		rows = append(rows, "Progress "+escape(v.ID)+" task="+escape(v.TaskID)+" run="+escape(v.RunID)+" phase="+escape(v.Phase)+" done="+join(v.Done, escape)+" doing="+join(v.Doing, escape)+" next="+join(v.Next, escape))
	}
	return rows
}
func ttyDependencies(b query.BoardSnapshot) []string      { return dependenciesRows(b, text) }
func markdownDependencies(b query.BoardSnapshot) []string { return dependenciesRows(b, markdown) }
func dependenciesRows(b query.BoardSnapshot, escape func(string) string) []string {
	rows := make([]string, 0, len(b.Dependencies))
	for _, v := range b.Dependencies {
		rows = append(rows, "Dependency "+escape(v.ID)+" dependent="+escape(v.DependentTaskID)+" blocker="+escape(v.BlockerTaskID)+" type="+escape(v.Type)+" unblock_on="+escape(v.UnblockOn)+" satisfied="+fmt.Sprint(v.Satisfied))
	}
	return rows
}
func ttyInbox(b query.BoardSnapshot) []string      { return inboxRows(b, text) }
func markdownInbox(b query.BoardSnapshot) []string { return inboxRows(b, markdown) }
func inboxRows(b query.BoardSnapshot, escape func(string) string) []string {
	rows := make([]string, 0, len(b.Inbox))
	for _, v := range b.Inbox {
		rows = append(rows, "Inbox "+escape(v.MessageID)+" type="+escape(v.Type)+" subject="+escape(v.Subject)+" sender="+escape(v.SenderSessionID)+" acknowledgement="+escape(v.Acknowledgement))
	}
	return rows
}
func ttyHandoffs(b query.BoardSnapshot) []string      { return handoffRows(b, text) }
func markdownHandoffs(b query.BoardSnapshot) []string { return handoffRows(b, markdown) }
func handoffRows(b query.BoardSnapshot, escape func(string) string) []string {
	rows := make([]string, 0, len(b.Handoffs))
	for _, v := range b.Handoffs {
		row := "Handoff " + escape(v.ID) + " task=" + escape(v.TaskID) + " run=" + escape(v.RunID) + " run_state=" + escape(v.RunState) + " source=" + escape(v.SourceSessionID) + " target_session=" + escape(v.TargetSessionID) + " target_task=" + escape(v.TargetTaskID) + " summary=" + escape(v.Summary) + " policy=" + escape(v.FinalOutputPolicy) + " final_output_hash=" + escape(v.FinalOutputHash) + " status=" + escape(v.Status)
		if v.Decision != nil {
			row += " decision=" + escape(v.Decision.Decision)
		}
		row += " changed_files=" + fmt.Sprint(v.ChangedFileCount) + " verification_items=" + fmt.Sprint(v.VerificationItemCount)
		rows = append(rows, row)
	}
	return rows
}
func ttyReservations(b query.BoardSnapshot) []string      { return reservationRows(b, text) }
func markdownReservations(b query.BoardSnapshot) []string { return reservationRows(b, markdown) }
func reservationRows(b query.BoardSnapshot, escape func(string) string) []string {
	rows := make([]string, 0, len(b.Reservations))
	for _, v := range b.Reservations {
		rows = append(rows, "Reservation "+escape(v.ID)+" session="+escape(v.SessionID)+" task="+escape(v.TaskID)+" run="+escape(v.RunID)+" kind="+escape(v.PatternKind)+" pattern="+escape(v.Pattern)+" fingerprint="+escape(v.PatternFingerprint)+" case="+escape(v.CaseSensitivity)+" mode="+escape(v.Mode)+" intent="+escape(v.Intent)+" lifecycle="+escape(v.Lifecycle)+" expires="+stamp(v.ExpiresAt)+" conflicts="+join(v.ConflictIDs, escape))
	}
	return rows
}
func ttyGit(b query.BoardSnapshot) []string      { return gitRows(b, text) }
func markdownGit(b query.BoardSnapshot) []string { return gitRows(b, markdown) }
func gitRows(b query.BoardSnapshot, escape func(string) string) []string {
	rows := make([]string, 0, len(b.Warnings)+1)
	for _, warning := range b.Warnings {
		rows = append(rows, "Warning: "+escape(warning))
	}
	if b.Git != nil {
		for _, asset := range b.Git.Assets {
			rows = append(rows, "Asset fingerprint="+escape(asset.Fingerprint)+" type="+escape(asset.Type)+" branch="+escape(asset.Branch)+" head="+escape(asset.Head)+" upstream="+escape(asset.Upstream)+" ahead_default="+fmt.Sprint(asset.AheadDefault)+" behind_default="+fmt.Sprint(asset.BehindDefault)+" ahead_upstream="+fmt.Sprint(asset.AheadUpstream)+" behind_upstream="+fmt.Sprint(asset.BehindUpstream)+" tracked_dirty="+fmt.Sprint(asset.TrackedDirty)+" untracked_dirty="+fmt.Sprint(asset.UntrackedDirty)+" classification="+join(asset.Classification, escape)+" confidence="+escape(asset.Confidence)+" owner_state="+escape(asset.OwnerState)+" owner_session="+escape(asset.OwnerSessionID)+" owner_task="+escape(asset.OwnerTaskID))
		}
	}
	return rows
}
func ttyActions(b query.BoardSnapshot) []string      { return actionRows(b, text) }
func markdownActions(b query.BoardSnapshot) []string { return actionRows(b, markdown) }
func actionRows(b query.BoardSnapshot, escape func(string) string) []string {
	rows := make([]string, 0, len(b.SuggestedActions))
	for _, v := range b.SuggestedActions {
		rows = append(rows, escape(v.Code)+": "+escape(v.Command))
	}
	return rows
}

func text(value string) string {
	return strings.Map(func(r rune) rune {
		if r <= 0x1f || (r >= 0x7f && r <= 0x9f) {
			return ' '
		}
		return r
	}, value)
}
func markdown(value string) string {
	return strings.NewReplacer(
		"\\", "\\\\",
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"`", "\\`",
		"|", "\\|",
		"*", "\\*",
		"_", "\\_",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"#", "\\#",
	).Replace(text(value))
}
func join(values []string, escape func(string) string) string {
	if len(values) == 0 {
		return "None"
	}
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = escape(value)
	}
	return strings.Join(result, ", ")
}
func stamp(value time.Time) string {
	if value.IsZero() {
		return "unknown"
	}
	return value.UTC().Format(time.RFC3339)
}
