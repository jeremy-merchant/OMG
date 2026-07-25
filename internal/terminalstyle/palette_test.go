package terminalstyle

import "testing"

func TestResolveSchemeUsesExplicitPreferenceBeforeHints(t *testing.T) {
	for _, test := range []struct {
		explicit string
		hint     string
		want     Scheme
	}{
		{"light", "15;0", SchemeLight},
		{"LIGHT", "15;0", SchemeLight},
		{"dark", "0;15", SchemeDark},
		{" dark ", "0;15", SchemeDark},
	} {
		if got := ResolveScheme(test.explicit, test.hint); got != test.want {
			t.Errorf("ResolveScheme(%q, %q) = %q, want %q", test.explicit, test.hint, got, test.want)
		}
	}
}

func TestResolveSchemeUsesConservativeColorFGBG(t *testing.T) {
	for _, test := range []struct {
		hint string
		want Scheme
	}{
		{"0;15", SchemeLight},
		{"0:7", SchemeLight},
		{"15;0", SchemeDark},
		{"0;8", SchemeDark},
		{"", SchemeDark},
		{"invalid", SchemeDark},
		{"0;12", SchemeDark},
	} {
		if got := ResolveScheme("", test.hint); got != test.want {
			t.Errorf("ResolveScheme(\"\", %q) = %q, want %q", test.hint, got, test.want)
		}
	}
}

func TestPaletteForKeepsSemanticRolesDistinct(t *testing.T) {
	dark := PaletteFor(SchemeDark)
	light := PaletteFor(SchemeLight)
	if dark.Scheme != SchemeDark || light.Scheme != SchemeLight {
		t.Fatalf("palette schemes = %q / %q", dark.Scheme, light.Scheme)
	}
	for name, value := range map[string]string{
		"dark accent": dark.Accent, "dark success": dark.Success, "dark warning": dark.Warning,
		"dark blocked": dark.Blocked, "dark danger": dark.Danger, "dark muted": dark.Muted,
		"light accent": light.Accent, "light success": light.Success, "light warning": light.Warning,
		"light blocked": light.Blocked, "light danger": light.Danger, "light muted": light.Muted,
	} {
		if value == "" {
			t.Errorf("%s is empty", name)
		}
	}
	if dark.Accent == light.Accent || dark.Success == light.Success || dark.Muted == light.Muted {
		t.Fatal("light and dark palettes do not meaningfully differ")
	}
	seen := map[string]bool{}
	for _, value := range []string{dark.Accent, dark.Success, dark.Warning, dark.Blocked, dark.Danger} {
		if seen[value] {
			t.Fatalf("dark semantic role reused ANSI sequence %q", value)
		}
		seen[value] = true
	}
	seen = map[string]bool{}
	for _, value := range []string{light.Accent, light.Success, light.Warning, light.Blocked, light.Danger} {
		if seen[value] {
			t.Fatalf("light semantic role reused ANSI sequence %q", value)
		}
		seen[value] = true
	}
}

func TestCurrentPaletteReadsEnvironmentWithoutTerminalQueries(t *testing.T) {
	t.Setenv("OMG_COLOR_SCHEME", "light")
	t.Setenv("COLORFGBG", "15;0")
	if got := CurrentPalette(); got.Scheme != SchemeLight || got.Accent != "\x1b[36m" {
		t.Fatalf("CurrentPalette explicit light = %+v", got)
	}
	t.Setenv("OMG_COLOR_SCHEME", "")
	t.Setenv("COLORFGBG", "0;15")
	if got := CurrentPalette(); got.Scheme != SchemeLight {
		t.Fatalf("CurrentPalette COLORFGBG light = %+v", got)
	}
}
