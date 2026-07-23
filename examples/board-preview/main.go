// Command board-preview writes a deterministic, hostile-input HTML board for offline QA.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"example.invalid/coordledger/internal/app/query"
	"example.invalid/coordledger/internal/view"
)

func main() {
	output := flag.String("output", "board-preview.html", "HTML output path")
	flag.Parse()
	if flag.NArg() != 0 {
		fatal("board-preview accepts only --output <path>")
	}

	at := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	snapshot := query.BoardSnapshot{
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
			Done: []string{"inspected <unsafe> input"}, Doing: []string{"keyboard QA"}, Next: []string{"verify no network requests"}, CreatedAt: at,
		}},
		Dependencies:     []query.DependencyView{{ID: "dependency-preview", DependentTaskID: "task-preview", BlockerTaskID: "task-blocker", Type: "hard", UnblockOn: "verified_done", Satisfied: false}},
		Inbox:            []query.InboxItemView{{MessageID: "message-preview", Type: "NOTICE", Subject: `Untrusted "message" <img src=x onerror=alert(1)>`, SenderSessionID: "agt-root", RelatedTaskID: "task-preview", Acknowledgement: "pending", CreatedAt: at}},
		Handoffs:         []query.HandoffView{{ID: "handoff-preview", TaskID: "task-preview", RunID: "run-preview", SourceSessionID: "agt-root", TargetSessionID: "agt-preview", Summary: "Review & verify — do not execute text", FinalOutputPolicy: "hash_only", FinalOutputHash: "sha256:preview", ChangedFileCount: 2, VerificationItemCount: 3, Status: "submitted", CreatedAt: at}},
		Reservations:     []query.ReservationView{{ID: "reservation-preview", SessionID: "agt-preview", TaskID: "task-preview", RunID: "run-preview", PatternKind: "glob", PatternFingerprint: "sha256:pattern", CaseSensitivity: "sensitive", Mode: "exclusive", Intent: "review generated output", Lifecycle: "active", ExpiresAt: at.Add(30 * time.Minute), ConflictIDs: []string{"reservation-conflict"}}},
		Git:              &query.GitView{ObservationID: "git-preview", ObservedAt: at, Repository: "local", Confidence: "certain", DefaultBranch: "main", Assets: []query.GitAssetView{{Fingerprint: "sha256:asset", Type: "local_branch", Branch: `<img src=x onerror=alert(2)>`, Head: "0123456789abcdef", AheadDefault: 1, TrackedDirty: 1, Classification: []string{"DIRTY_UNOWNED"}, Confidence: "certain"}}},
		Warnings:         []string{`[click](javascript:alert(1)) <script>alert(3)</script>`},
		SuggestedActions: []query.SuggestedActionView{{Code: "show-task", Command: "omg task show --task AK-000001"}},
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		fatal(err.Error())
	}
	model, err := query.NewViewModel("board", snapshot.SnapshotCursor, payload)
	if err != nil {
		fatal(err.Error())
	}
	file, err := os.OpenFile(*output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		fatal(err.Error())
	}
	if err := view.Render(view.FormatHTML, model, file); err != nil {
		_ = file.Close()
		fatal(err.Error())
	}
	if err := file.Close(); err != nil {
		fatal(err.Error())
	}
	fmt.Println(*output)
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
