package view

import (
	"strings"
	"testing"

	"github.com/jeremy-merchant/oh-my-group/internal/app/query"
)

func TestTTYBoardUsesExplicitLightAndDarkSemanticPalettes(t *testing.T) {
	board := query.BoardSnapshot{
		SchemaVersion: 1,
		ViewVersion:   1,
		Mode:          query.BoardAll,
		ProjectID:     "palette-project",
		Scope:         query.BoardScope{ProjectID: "palette-project", Mode: query.BoardAll},
		Tasks:         []query.TaskView{{ID: "task-1", DisplayNumber: 1, Title: "Blocked work", State: "blocked"}},
		Warnings:      []string{"Review warning"},
	}
	for _, test := range []struct {
		scheme string
		want   []string
		avoid  []string
	}{
		{"light", []string{"\x1b[36m", "\x1b[33m", "\x1b[35m", "\x1b[90m"}, []string{"\x1b[96m", "\x1b[93m", "\x1b[95m"}},
		{"dark", []string{"\x1b[96m", "\x1b[93m", "\x1b[95m", "\x1b[2m"}, []string{"\x1b[36m", "\x1b[33m", "\x1b[35m", "\x1b[90m"}},
	} {
		t.Run(test.scheme, func(t *testing.T) {
			t.Setenv("OMG_COLOR_SCHEME", test.scheme)
			t.Setenv("COLORFGBG", "")
			got := renderTTYWidth(board, true, 80)
			for _, want := range test.want {
				if !strings.Contains(got, want) {
					t.Errorf("%s TTY board missing %q", test.scheme, want)
				}
			}
			for _, forbidden := range test.avoid {
				if strings.Contains(got, forbidden) {
					t.Errorf("%s TTY board contains opposite palette %q", test.scheme, forbidden)
				}
			}
		})
	}
}

func TestTTYBoardWithoutColorIgnoresPalettePreference(t *testing.T) {
	t.Setenv("OMG_COLOR_SCHEME", "light")
	board := query.BoardSnapshot{SchemaVersion: 1, ViewVersion: 1, Mode: query.BoardAll, ProjectID: "plain", Scope: query.BoardScope{ProjectID: "plain", Mode: query.BoardAll}}
	if got := renderTTYWidth(board, false, 80); strings.Contains(got, "\x1b[") {
		t.Fatalf("plain TTY board emitted ANSI: %q", got)
	}
}
