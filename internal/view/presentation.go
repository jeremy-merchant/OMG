package view

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/jeremy-merchant/OMG/internal/app/query"
)

type semanticState string

const (
	stateSuccess semanticState = "success"
	stateWorking semanticState = "working"
	statePending semanticState = "pending"
	stateWarning semanticState = "warning"
	stateBlocked semanticState = "blocked"
	stateDanger  semanticState = "danger"
	stateInfo    semanticState = "info"
	stateMuted   semanticState = "muted"
)

type statusPresentation struct {
	Semantic semanticState
	Glyph    string
	Label    string
}

func presentStatus(value string) statusPresentation {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "verified", "verified_done", "accepted", "acknowledged", "satisfied", "released", "completed", "complete", "done", "succeeded", "success", "work_complete", "available", "certain", "high":
		return statusPresentation{stateSuccess, "✔", labelStatus(normalized, "verified")}
	case "running", "working", "in_progress", "active", "claimed", "alive", "started", "executing":
		return statusPresentation{stateWorking, "⟳", labelStatus(normalized, "working")}
	case "pending", "waiting", "submitted", "open", "unread", "no_signal", "unknown", "queued":
		return statusPresentation{statePending, "○", labelStatus(normalized, "pending")}
	case "stale", "dirty", "advisory", "warning", "degraded":
		return statusPresentation{stateWarning, "⚠", labelStatus(normalized, "warning")}
	case "blocked", "conflict", "unsatisfied", "shadowed", "reserved", "unavailable":
		return statusPresentation{stateBlocked, "⦸", labelStatus(normalized, "blocked")}
	case "failed", "failure", "error", "rejected", "interrupted", "aborted", "orphaned", "parent_lost":
		return statusPresentation{stateDanger, "✘", labelStatus(normalized, "error")}
	case "info", "notice":
		return statusPresentation{stateInfo, "ⓘ", labelStatus(normalized, "info")}
	case "", "none", "inactive", "disabled":
		return statusPresentation{stateMuted, "·", labelStatus(normalized, "inactive")}
	default:
		return statusPresentation{stateInfo, "ⓘ", normalized}
	}
}

func labelStatus(value, fallback string) string {
	if value == "" || value == "unknown" {
		return fallback
	}
	return strings.ReplaceAll(value, "_", " ")
}

func dependencyStatus(dependency query.DependencyView) statusPresentation {
	if dependency.Satisfied {
		return statusPresentation{stateSuccess, "✔", "satisfied"}
	}
	return statusPresentation{stateBlocked, "⦸", "blocked"}
}

func reservationStatus(reservation query.ReservationView) statusPresentation {
	if len(reservation.ConflictIDs) > 0 {
		return statusPresentation{stateBlocked, "⦸", "conflict"}
	}
	return presentStatus(reservation.Lifecycle)
}

func gitAssetStatus(asset query.GitAssetView) statusPresentation {
	if asset.TrackedDirty > 0 || asset.UntrackedDirty > 0 {
		return statusPresentation{stateWarning, "⚠", "dirty"}
	}
	if asset.BehindDefault > 0 || asset.BehindUpstream > 0 {
		return statusPresentation{statePending, "○", "behind"}
	}
	return statusPresentation{stateSuccess, "✔", "clean"}
}

type boardHealthView struct {
	Status   statusPresentation
	Headline string
	Detail   string
}

func boardHealth(board query.BoardSnapshot) boardHealthView {
	blocked := 0
	for _, dependency := range board.Dependencies {
		if !dependency.Satisfied {
			blocked++
		}
	}
	conflicts := 0
	for _, reservation := range board.Reservations {
		if len(reservation.ConflictIDs) > 0 {
			conflicts++
		}
	}
	failed := 0
	for _, run := range board.Runs {
		if presentStatus(run.State).Semantic == stateDanger {
			failed++
		}
	}
	if failed > 0 {
		return boardHealthView{
			Status:   statusPresentation{stateDanger, "✘", "action required"},
			Headline: fmt.Sprintf("%d failed run(s)", failed),
			Detail:   fmt.Sprintf("%d blocker(s) · %d reservation conflict(s) · %d warning(s)", blocked, conflicts, len(board.Warnings)),
		}
	}
	if blocked > 0 || conflicts > 0 {
		return boardHealthView{
			Status:   statusPresentation{stateBlocked, "⦸", "blocked"},
			Headline: fmt.Sprintf("%d blocker(s) · %d reservation conflict(s)", blocked, conflicts),
			Detail:   fmt.Sprintf("%d warning(s) in the current snapshot", len(board.Warnings)),
		}
	}
	if len(board.Warnings) > 0 {
		return boardHealthView{
			Status:   statusPresentation{stateWarning, "⚠", "attention"},
			Headline: fmt.Sprintf("%d advisory warning(s)", len(board.Warnings)),
			Detail:   "Coordination can continue; inspect warnings before acting.",
		}
	}
	return boardHealthView{
		Status:   statusPresentation{stateSuccess, "✔", "clear"},
		Headline: "No active blockers in this snapshot",
		Detail:   "Canonical coordination facts are ready for inspection.",
	}
}

func terminalColorEnabled(output io.Writer) bool {
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

func shortID(value string) string {
	value = text(value)
	const (
		max         = 22
		prefixRunes = 11
		suffixRunes = 8
	)
	runeCount := utf8.RuneCountInString(value)
	if runeCount <= max {
		return value
	}
	prefixEnd, suffixStart := len(value), len(value)
	runeIndex := 0
	for byteIndex := range value {
		if runeIndex == prefixRunes {
			prefixEnd = byteIndex
		}
		if runeIndex == runeCount-suffixRunes {
			suffixStart = byteIndex
			break
		}
		runeIndex++
	}
	return value[:prefixEnd] + "…" + value[suffixStart:]
}

func activeIdentity(identity query.IdentityView) bool {
	if identity.Liveness != "" {
		return presentStatus(string(identity.Liveness)).Semantic == stateWorking
	}
	return presentStatus(identity.NativeAccessState).Semantic == stateSuccess
}

func primaryProject(board query.BoardSnapshot) string {
	if board.Scope.ProjectID != "" {
		return text(board.Scope.ProjectID)
	}
	return text(board.ProjectID)
}

func boardContext(board query.BoardSnapshot) string {
	parts := []string{primaryProject(board), string(board.Mode)}
	if board.Scope.WorkspaceID != "" {
		parts = append(parts, text(board.Scope.WorkspaceID))
	}
	if board.Scope.Selector != "" {
		parts = append(parts, shortID(board.Scope.Selector))
	}
	return strings.Join(parts, " · ")
}

func allIdentities(board query.BoardSnapshot) []query.IdentityView {
	seen := make(map[string]struct{}, len(board.Sessions)+1)
	identities := make([]query.IdentityView, 0, len(board.Sessions)+1)
	if board.Identity != nil {
		identities = append(identities, *board.Identity)
		seen[board.Identity.ID] = struct{}{}
	}
	for _, identity := range board.Sessions {
		if _, exists := seen[identity.ID]; exists {
			continue
		}
		identities = append(identities, identity)
		seen[identity.ID] = struct{}{}
	}
	return identities
}
