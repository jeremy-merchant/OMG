package view

import (
	"fmt"
	"strings"

	"github.com/jeremy-merchant/OMG/internal/app"
	"github.com/jeremy-merchant/OMG/internal/app/query"
)

// RenderPreflightTTY presents the canonical preflight projection for operators.
// It only formats facts supplied by the application and reuses the board's
// terminal-safe row renderers.
func RenderPreflightTTY(preflight app.PreflightView) string {
	return RenderPreflightTTYWithOptions(preflight, defaultTTYWidth, false)
}

// RenderPreflightTTYWithOptions renders the preflight projection with the same
// width and color semantics as the interactive board.
func RenderPreflightTTYWithOptions(preflight app.PreflightView, width int, color bool) string {
	theme := newViewTerminalTheme(color, width)
	var out strings.Builder
	state := presentStatus("verified")
	headline := "Ready to coordinate"
	if !preflight.Healthy {
		state = presentStatus("warning")
		headline = "Startup attention required"
	}
	out.WriteString(theme.bold("OMG") + theme.accent("  OPERATOR LEDGER") + theme.dim(" / PREFLIGHT") + "\n")
	writeTTYStatusLine(&out, theme, "", "", state, headline, "startup readiness snapshot")
	out.WriteString(theme.dim(strings.Repeat("━", theme.terminalWidth())) + "\n")

	writeTTYHeading(&out, theme, "State", "operator summary")
	writeTTYStatusLine(&out, theme, "  ", "", presentStatus(map[bool]string{true: "verified", false: "warning"}[preflight.Healthy]), "Healthy: "+fmt.Sprint(preflight.Healthy), "")
	migrationState := presentStatus("verified")
	if preflight.PendingMigrations != 0 {
		migrationState = presentStatus("warning")
	}
	writeTTYStatusLine(&out, theme, "  ", "", migrationState, "Pending migrations: "+fmt.Sprint(preflight.PendingMigrations), "")
	if automatic := preflight.AutomaticMigration; automatic != nil {
		status := "automatic migration recovery failed"
		state := presentStatus("warning")
		if automatic.Applied {
			status = fmt.Sprintf("backup-verified migration applied: v%d → v%d", automatic.FromVersion, automatic.ToVersion)
			state = presentStatus("verified")
		}
		writeTTYStatusLine(&out, theme, "  ", "", state, status, "plan="+automatic.PlanID)
	}
	writeTTYStatusLine(&out, theme, "  ", "", presentStatus("info"), "Active sessions: "+fmt.Sprint(preflight.ActiveSessions), "stale="+fmt.Sprint(preflight.StaleSessions))
	writeTTYStatusLine(&out, theme, "  ", "", presentStatus("info"), "Conflicts: "+fmt.Sprint(preflight.Conflicts), "integration_queue="+fmt.Sprint(preflight.IntegrationQueue))
	if preflight.Details == nil {
		return out.String()
	}
	details := preflight.Details
	writePreflightSection(&out, theme, "Identity", preflightIdentityRows(details.Identity))
	writePreflightSection(&out, theme, "Sessions + tasks", preflightSessionsTasksRows(details))
	writePreflightSection(&out, theme, "Inbox", inboxRows(query.BoardSnapshot{Inbox: details.Inbox}, text))
	writePreflightSection(&out, theme, "Dependencies", dependenciesRows(query.BoardSnapshot{Dependencies: details.Dependencies}, text))
	writePreflightSection(&out, theme, "Reservations", reservationRows(query.BoardSnapshot{Reservations: details.Reservations}, text))
	writePreflightSection(&out, theme, "Git", gitRows(query.BoardSnapshot{Git: details.Git, Warnings: details.Warnings}, text))
	return out.String()
}

func writePreflightSection(out *strings.Builder, theme terminalTheme, title string, rows []string) {
	writeTTYHeading(out, theme, title, fmt.Sprintf("%d record(s)", len(rows)))
	if len(rows) == 0 {
		writeTTYEmpty(out, theme, "No record in this preflight snapshot")
		return
	}
	for index, row := range rows {
		connector := "├─ "
		if index == len(rows)-1 {
			connector = "└─ "
		}
		writeTTYStatusLine(out, theme, "  ", connector, presentStatus("info"), row, "")
	}
}

func preflightIdentityRows(identityView *query.IdentityView) []string {
	if identityView == nil {
		return nil
	}
	return []string{"Current: " + identity(identityView, text)}
}

func preflightSessionsTasksRows(preflight *app.PreflightDetails) []string {
	rows := make([]string, 0, len(preflight.Sessions)+len(preflight.Tasks))
	for i := range preflight.Sessions {
		rows = append(rows, "Session: "+identity(&preflight.Sessions[i], text))
	}
	return append(rows, tasksRunsRows(query.BoardSnapshot{Tasks: preflight.Tasks}, text)...)
}
