// Package terminalstyle centralizes OMG's semantic ANSI palettes.
package terminalstyle

import (
	"os"
	"strconv"
	"strings"
)

// Scheme identifies the background family the foreground palette targets.
type Scheme string

const (
	SchemeDark  Scheme = "dark"
	SchemeLight Scheme = "light"

	Reset = "\x1b[0m"
	Bold  = "\x1b[1m"
)

// Palette contains semantic ANSI Select Graphic Rendition sequences.
type Palette struct {
	Scheme  Scheme
	Accent  string
	Success string
	Warning string
	Blocked string
	Danger  string
	Muted   string
}

// CurrentPalette resolves the process environment without querying or writing
// terminal control protocols. OMG_COLOR_SCHEME is authoritative when valid;
// COLORFGBG is only a conservative hint.
func CurrentPalette() Palette {
	return PaletteFor(ResolveScheme(os.Getenv("OMG_COLOR_SCHEME"), os.Getenv("COLORFGBG")))
}

// ResolveScheme returns light or dark from an explicit preference and a
// conservative COLORFGBG fallback. Unknown inputs preserve the historical dark
// terminal palette.
func ResolveScheme(explicit, colorFGBG string) Scheme {
	switch strings.ToLower(strings.TrimSpace(explicit)) {
	case "light":
		return SchemeLight
	case "dark":
		return SchemeDark
	}
	parts := strings.FieldsFunc(strings.TrimSpace(colorFGBG), func(character rune) bool {
		return character == ';' || character == ':' || character == ','
	})
	if len(parts) == 0 {
		return SchemeDark
	}
	background, err := strconv.Atoi(strings.TrimSpace(parts[len(parts)-1]))
	if err != nil {
		return SchemeDark
	}
	// 7 and 15 are the conventional white/light ANSI backgrounds reported by
	// COLORFGBG. Other indexed colors vary too much by terminal theme to infer
	// safely, so they retain the established dark palette unless explicitly set.
	if background == 7 || background == 15 {
		return SchemeLight
	}
	return SchemeDark
}

// PaletteFor returns semantic foreground colors chosen for the target
// background family. The light palette uses normal ANSI colors and gray
// metadata; the dark palette preserves OMG's existing bright Titanium roles.
func PaletteFor(scheme Scheme) Palette {
	if scheme == SchemeLight {
		return Palette{
			Scheme:  SchemeLight,
			Accent:  "\x1b[36m",
			Success: "\x1b[32m",
			Warning: "\x1b[33m",
			Blocked: "\x1b[35m",
			Danger:  "\x1b[31m",
			Muted:   "\x1b[90m",
		}
	}
	return Palette{
		Scheme:  SchemeDark,
		Accent:  "\x1b[96m",
		Success: "\x1b[92m",
		Warning: "\x1b[93m",
		Blocked: "\x1b[95m",
		Danger:  "\x1b[91m",
		Muted:   "\x1b[2m",
	}
}
