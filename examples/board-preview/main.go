// Command board-preview renders one deterministic hostile-input operator board
// across every supported presentation for offline QA.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/app/query"
	"github.com/jeremy-merchant/oh-my-group/internal/view"
)

func main() {
	formatValue := flag.String("format", "html", "output format: html, tty, markdown, or json")
	output := flag.String("output", "", "output path; use - for stdout")
	flag.Parse()
	if flag.NArg() != 0 {
		fatal("board-preview accepts only --format <format> and --output <path|->")
	}

	format, extension, err := parsePreviewFormat(*formatValue)
	if err != nil {
		fatal(err.Error())
	}
	if *output == "" {
		*output = "board-preview." + extension
	}
	model, err := previewViewModel()
	if err != nil {
		fatal(err.Error())
	}
	if err := writePreview(*output, format, model, os.Stdout); err != nil {
		fatal(err.Error())
	}
	if *output != "-" {
		fmt.Println(*output)
	}
}

func parsePreviewFormat(value string) (view.Format, string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "html":
		return view.FormatHTML, "html", nil
	case "tty":
		return view.FormatTTY, "txt", nil
	case "markdown", "md":
		return view.FormatMarkdown, "md", nil
	case "json":
		return view.FormatJSON, "json", nil
	default:
		return "", "", fmt.Errorf("board-preview: unsupported format %q", value)
	}
}

func previewViewModel() (query.ViewModel, error) {
	snapshot := previewSnapshot()
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return query.ViewModel{}, fmt.Errorf("board-preview: encode snapshot: %w", err)
	}
	model, err := query.NewViewModel("board", snapshot.SnapshotCursor, payload)
	if err != nil {
		return query.ViewModel{}, fmt.Errorf("board-preview: create view model: %w", err)
	}
	return model, nil
}

func previewSnapshot() query.BoardSnapshot {
	at := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	return query.BoardSnapshot{
		SchemaVersion:  1,
		ViewVersion:    1,
		GeneratedAt:    at,
		Scope:          query.BoardScope{ProjectID: "preview-project", Mode: query.BoardAll},
		Mode:           query.BoardAll,
		ProjectID:      "preview-project",
		SnapshotCursor: "preview-cursor",
		Redaction: query.RedactionView{
			PolicyName:    query.BoardRedactionPolicyName,
			PolicyVersion: query.BoardRedactionPolicyVersion,
		},
		Identity: &query.IdentityView{
			ID:                   "agt-preview",
			Kind:                 "agent_delegated",
			Runtime:              "generic",
			Role:                 "reviewer",
			InstructionSource:    "delegation_token",
			ProvenanceConfidence: "verified",
			RootSessionID:        "agt-root",
			RootHumanID:          "human-owner",
			TaskID:               "AK-000001",
			PreviousTaskID:       "AK-000000",
			WorktreeBound:        true,
			NativeAccessState:    "available",
			StartedAt:            at,
		},
		Tasks: []query.TaskView{{
			ID:                 "task-preview",
			DisplayNumber:      1,
			Title:              `Review </td><script>globalThis.pwned=1</script> & 협업`,
			State:              "WAITING",
			CreatedBySessionID: "agt-root",
			ClaimedBySessionID: "agt-preview",
			CreatedAt:          at,
			UpdatedAt:          at,
		}},
		Runs: []query.RunView{{ID: "run-preview", TaskID: "task-preview", SessionID: "agt-preview", State: "WAITING", StartedAt: at}},
		Progress: []query.ProgressView{{
			ID: "progress-preview", TaskID: "task-preview", RunID: "run-preview", SessionID: "agt-preview", Phase: "review",
			Done: []string{"inspected <unsafe> input 👩‍💻"}, Doing: []string{"keyboard QA ㄱ"}, Next: []string{"verify 🇰🇷 flag width and no network requests"}, CreatedAt: at,
		}},
		Dependencies: []query.DependencyView{{ID: "dependency-preview", DependentTaskID: "task-preview", BlockerTaskID: "task-blocker", Type: "hard", UnblockOn: "verified_done", Satisfied: false}},
		Inbox:        []query.InboxItemView{{MessageID: "message-preview", Type: "NOTICE", Subject: `Untrusted "message" <img src=x onerror=alert(1)>`, SenderSessionID: "agt-root", RelatedTaskID: "task-preview", Acknowledgement: "pending", CreatedAt: at}},
		Handoffs:     []query.HandoffView{{ID: "handoff-preview", TaskID: "task-preview", RunID: "run-preview", SourceSessionID: "agt-root", TargetSessionID: "agt-preview", Summary: "Review & verify — do not execute text", FinalOutputPolicy: "hash_only", FinalOutputHash: "sha256:preview", ChangedFileCount: 2, VerificationItemCount: 3, Status: "submitted", CreatedAt: at}},
		Reservations: []query.ReservationView{{ID: "reservation-preview", SessionID: "agt-preview", TaskID: "task-preview", RunID: "run-preview", PatternKind: "glob", PatternFingerprint: "sha256:pattern", CaseSensitivity: "sensitive", Mode: "exclusive", Intent: "review generated output", Lifecycle: "active", ExpiresAt: at.Add(30 * time.Minute), ConflictIDs: []string{"reservation-conflict"}}},
		Git:          &query.GitView{ObservationID: "git-preview", ObservedAt: at, Repository: "local", Confidence: "certain", DefaultBranch: "main", Assets: []query.GitAssetView{{Fingerprint: "sha256:asset", Type: "local_branch", Branch: `<img src=x onerror=alert(2)>`, Head: "0123456789abcdef", AheadDefault: 1, TrackedDirty: 1, Classification: []string{"DIRTY_UNOWNED"}, Confidence: "certain"}}},
		Warnings:     []string{`[click](javascript:alert(1)) <script>alert(3)</script>`},
		SuggestedActions: []query.SuggestedActionView{
			{Code: "inspect-task", Command: "omg board task --task task-preview"},
			{Code: "reservation_history", Command: `omg reserve history --payload '{"reservation_id":"reservation-preview"}'`},
			{Code: "git_cleanup_plan", Command: "omg git cleanup-plan"},
		},
	}
}

func writePreview(outputPath string, format view.Format, model query.ViewModel, stdout io.Writer) error {
	if outputPath == "-" {
		if stdout == nil {
			return errors.New("board-preview: stdout is unavailable")
		}
		return view.Render(format, model, stdout)
	}
	file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("board-preview: create output: %w", err)
	}
	if err := view.Render(format, model, file); err != nil {
		_ = file.Close()
		return fmt.Errorf("board-preview: render output: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("board-preview: close output: %w", err)
	}
	return nil
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
