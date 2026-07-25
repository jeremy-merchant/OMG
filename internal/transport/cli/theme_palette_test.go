package cli

import (
	"strings"
	"testing"
)

func TestTerminalThemeUsesExplicitLightAndDarkPalettes(t *testing.T) {
	tests := []struct {
		scheme string
		want   []string
		avoid  []string
	}{
		{
			scheme: "light",
			want:   []string{"\x1b[36m", "\x1b[32m", "\x1b[33m", "\x1b[35m", "\x1b[31m", "\x1b[90m"},
			avoid:  []string{"\x1b[96m", "\x1b[92m", "\x1b[93m", "\x1b[95m", "\x1b[91m"},
		},
		{
			scheme: "dark",
			want:   []string{"\x1b[96m", "\x1b[92m", "\x1b[93m", "\x1b[95m", "\x1b[91m", "\x1b[2m"},
			avoid:  []string{"\x1b[36m", "\x1b[32m", "\x1b[33m", "\x1b[35m", "\x1b[31m", "\x1b[90m"},
		},
	}
	for _, test := range tests {
		t.Run(test.scheme, func(t *testing.T) {
			t.Setenv("OMG_COLOR_SCHEME", test.scheme)
			t.Setenv("COLORFGBG", "")
			theme := newTerminalTheme(true)
			rendered := strings.Join([]string{
				theme.info("info"), theme.success("success"), theme.warn("warning"),
				theme.blocked("blocked"), theme.danger("danger"), theme.dim("metadata"),
			}, " ")
			for _, want := range test.want {
				if !strings.Contains(rendered, want) {
					t.Errorf("%s theme output missing %q: %q", test.scheme, want, rendered)
				}
			}
			for _, forbidden := range test.avoid {
				if strings.Contains(rendered, forbidden) {
					t.Errorf("%s theme output contains opposite palette %q: %q", test.scheme, forbidden, rendered)
				}
			}
		})
	}
}

func TestDisabledTerminalThemeSuppressesPaletteRegardlessOfScheme(t *testing.T) {
	t.Setenv("OMG_COLOR_SCHEME", "light")
	theme := newTerminalTheme(false)
	rendered := theme.info("info") + theme.success("success") + theme.warn("warning") + theme.blocked("blocked") + theme.danger("danger") + theme.dim("metadata")
	if strings.Contains(rendered, "\x1b[") {
		t.Fatalf("disabled terminal theme emitted ANSI: %q", rendered)
	}
}
