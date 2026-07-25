package terminaltext

import (
	"strings"
	"testing"
)

func TestProfileForTerminalSpecificHangulCompatibilityJamoWidth(t *testing.T) {
	tests := []struct {
		name        string
		goos        string
		termProgram string
		term        string
		want        int
	}{
		{"macOS Terminal", "darwin", "Apple_Terminal", "xterm-256color", 1},
		{"macOS iTerm", "darwin", "iTerm.app", "xterm-256color", 1},
		{"macOS Ghostty", "darwin", "ghostty", "xterm-ghostty", 2},
		{"Linux Ghostty", "linux", "ghostty", "xterm-ghostty", 2},
		{"Linux Unicode default", "linux", "", "xterm-256color", 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ProfileFor(test.goos, test.termProgram, test.term).HangulCompatibilityJamoWidth; got != test.want {
				t.Fatalf("jamo width = %d, want %d", got, test.want)
			}
		})
	}
}

func TestWidthWithProfileUsesGraphemeClusters(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		profile WidthProfile
		want    int
	}{
		{"ASCII", "A", WidthProfile{}, 1},
		{"Hangul syllable", "한", WidthProfile{}, 2},
		{"compatibility jamo narrow", "ㄱ", WidthProfile{HangulCompatibilityJamoWidth: 1}, 1},
		{"compatibility jamo wide", "ㄱ", WidthProfile{HangulCompatibilityJamoWidth: 2}, 2},
		{"combining mark", "e\u0301", WidthProfile{}, 1},
		{"ZWJ emoji", "👩‍💻", WidthProfile{}, 2},
		{"rainbow flag", "🏳️‍🌈", WidthProfile{}, 2},
		{"regional-indicator flag", "🇰🇷", WidthProfile{}, 2},
		{"keycap", "1️⃣", WidthProfile{}, 2},
		{"mixed", "A한👩‍💻🇰🇷", WidthProfile{}, 7},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := WidthWithProfile(test.value, test.profile); got != test.want {
				t.Fatalf("WidthWithProfile(%q) = %d, want %d", test.value, got, test.want)
			}
		})
	}
}

func TestSplitTokenPreservesGraphemeClustersAndSemanticBoundaries(t *testing.T) {
	profile := WidthProfile{HangulCompatibilityJamoWidth: 2}
	for _, test := range []struct {
		value string
		width int
	}{
		{"prefix-👩‍💻-suffix", 9},
		{"flag=🇰🇷/status=ready", 10},
		{"한글-ㄱ-🏳️‍🌈-metadata", 8},
	} {
		parts := SplitTokenWithProfile(test.value, test.width, profile)
		if got := strings.Join(parts, ""); got != test.value {
			t.Fatalf("SplitTokenWithProfile(%q) changed content: %q", test.value, got)
		}
		for _, part := range parts {
			if got := WidthWithProfile(part, profile); got > test.width {
				t.Errorf("part %q width %d exceeds %d", part, got, test.width)
			}
			if strings.HasSuffix(part, "\u200d") || strings.HasPrefix(part, "\u200d") {
				t.Errorf("part split a ZWJ cluster: %q", part)
			}
		}
	}
}

func TestSplitTokenKeepsOverwideSingleClusterWhole(t *testing.T) {
	parts := SplitTokenWithProfile("👩‍💻", 1, WidthProfile{})
	if len(parts) != 1 || parts[0] != "👩‍💻" {
		t.Fatalf("overwide grapheme was split: %#v", parts)
	}
}
