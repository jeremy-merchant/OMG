package view

import (
	"fmt"
	"strings"

	"example.invalid/coordledger/internal/app"
	"example.invalid/coordledger/internal/app/query"
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
	theme := terminalTheme{enabled: color, width: normalizeTTYWidth(width)}
	var out strings.Builder
	state := presentStatus("verified")
	headline := "Ready to coordinate"
	if !preflight.Initialized {
		state = presentStatus("error")
		headline = "Ledger is not initialized"
	} else if preflight.PendingMigrations != 0 {
		state = presentStatus("warning")
		headline = "Schema migration required"
	}
	out.WriteString(theme.bold("OMG") + theme.accent("  OPERATOR LEDGER") + theme.dim(" / PREFLIGHT") + "\n")
	writeTTYStatusLine(&out, theme, "", "", state, headline, "startup readiness snapshot")
	out.WriteString(theme.dim(strings.Repeat("━", theme.terminalWidth())) + "\n")

	writeTTYHeading(&out, theme, "State", "startup gates")
	writeTTYStatusLine(&out, theme, "  ", "", presentStatus(map[bool]string{true: "verified", false: "error"}[preflight.Initialized]), "Initialized: "+fmt.Sprint(preflight.Initialized), "schema_state="+preflightSchemaState(preflight))
	migrationState := presentStatus("verified")
	if preflight.PendingMigrations != 0 {
		migrationState = presentStatus("warning")
	}
	writeTTYStatusLine(&out, theme, "  ", "", migrationState, "Pending migrations: "+fmt.Sprint(preflight.PendingMigrations), "schema_state="+preflightSchemaState(preflight))
	writePreflightSection(&out, theme, "Identity", preflightIdentityRows(preflight.Identity))
	writePreflightSection(&out, theme, "Sessions + tasks", preflightSessionsTasksRows(preflight))
	writePreflightSection(&out, theme, "Inbox", inboxRows(query.BoardSnapshot{Inbox: preflight.Inbox}, text))
	writePreflightSection(&out, theme, "Dependencies", dependenciesRows(query.BoardSnapshot{Dependencies: preflight.Dependencies}, text))
	writePreflightSection(&out, theme, "Reservations", reservationRows(query.BoardSnapshot{Reservations: preflight.Reservations}, text))
	writePreflightSection(&out, theme, "Git", gitRows(query.BoardSnapshot{Git: preflight.Git, Warnings: preflight.Warnings}, text))
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

func preflightSchemaState(preflight app.PreflightView) string {
	if !preflight.Initialized {
		return "uninitialized"
	}
	if preflight.PendingMigrations != 0 {
		return "migration required"
	}
	return "current"
}

func preflightIdentityRows(identityView *query.IdentityView) []string {
	if identityView == nil {
		return nil
	}
	return []string{"Current: " + identity(identityView, text)}
}

func preflightSessionsTasksRows(preflight app.PreflightView) []string {
	rows := make([]string, 0, len(preflight.Sessions)+len(preflight.Tasks))
	for i := range preflight.Sessions {
		rows = append(rows, "Session: "+identity(&preflight.Sessions[i], text))
	}
	return append(rows, tasksRunsRows(query.BoardSnapshot{Tasks: preflight.Tasks}, text)...)
}
