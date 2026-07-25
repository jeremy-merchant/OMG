// Package terminaltext provides deterministic grapheme-aware terminal cell measurement.
package terminaltext

import (
	"os"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// WidthProfile captures terminal-specific width behavior that differs from the
// Unicode default. A zero HangulCompatibilityJamoWidth delegates to Unicode.
type WidthProfile struct {
	HangulCompatibilityJamoWidth int
}

// CurrentWidthProfile resolves the running terminal's known width behavior.
func CurrentWidthProfile() WidthProfile {
	return ProfileFor(runtime.GOOS, os.Getenv("TERM_PROGRAM"), os.Getenv("TERM"))
}

// ProfileFor resolves the Hangul compatibility-jamo width from platform and
// terminal identity. Ghostty renders these code points wide even on macOS;
// other macOS terminals commonly render them narrow.
func ProfileFor(goos, termProgram, term string) WidthProfile {
	identity := strings.ToLower(strings.TrimSpace(termProgram + " " + term))
	if strings.Contains(identity, "ghostty") {
		return WidthProfile{HangulCompatibilityJamoWidth: 2}
	}
	if strings.EqualFold(strings.TrimSpace(goos), "darwin") {
		return WidthProfile{HangulCompatibilityJamoWidth: 1}
	}
	return WidthProfile{}
}

// Width returns the number of terminal cells occupied by value.
func Width(value string) int {
	return WidthWithProfile(value, CurrentWidthProfile())
}

// WidthWithProfile returns the terminal-cell width under an explicit profile.
func WidthWithProfile(value string, profile WidthProfile) int {
	width := 0
	state := -1
	for len(value) > 0 {
		cluster, rest, boundaries, nextState := uniseg.StepString(value, state)
		clusterWidth := resolvedClusterWidth(cluster, boundaries>>uniseg.ShiftWidth, profile)
		width += clusterWidth
		value = rest
		state = nextState
	}
	return width
}

// SplitToken splits a token on grapheme-cluster boundaries while preferring
// readable punctuation boundaries near the available width.
func SplitToken(value string, width int) []string {
	return SplitTokenWithProfile(value, width, CurrentWidthProfile())
}

// SplitTokenWithProfile is SplitToken under an explicit width profile.
func SplitTokenWithProfile(value string, width int, profile WidthProfile) []string {
	if value == "" {
		return nil
	}
	if width < 1 {
		width = 1
	}
	clusters := graphemeClusters(value, profile)
	if clusterSliceWidth(clusters) <= width {
		return []string{value}
	}
	parts := make([]string, 0, 2)
	for start := 0; start < len(clusters); {
		end := start
		used := 0
		for end < len(clusters) {
			clusterWidth := clusters[end].width
			if used > 0 && used+clusterWidth > width {
				break
			}
			used += clusterWidth
			end++
		}
		if end == start {
			end++
		}
		cut := preferredBreak(clusters, start, end, width)
		if cut <= start {
			cut = end
		}
		var part strings.Builder
		for _, cluster := range clusters[start:cut] {
			part.WriteString(cluster.text)
		}
		parts = append(parts, part.String())
		start = cut
	}
	return parts
}

type graphemeCluster struct {
	text  string
	width int
}

func graphemeClusters(value string, profile WidthProfile) []graphemeCluster {
	clusters := make([]graphemeCluster, 0, utf8.RuneCountInString(value))
	state := -1
	for len(value) > 0 {
		cluster, rest, boundaries, nextState := uniseg.StepString(value, state)
		clusterWidth := resolvedClusterWidth(cluster, boundaries>>uniseg.ShiftWidth, profile)
		clusters = append(clusters, graphemeCluster{text: cluster, width: clusterWidth})
		value = rest
		state = nextState
	}
	return clusters
}

func clusterSliceWidth(clusters []graphemeCluster) int {
	width := 0
	for _, cluster := range clusters {
		width += cluster.width
	}
	return width
}

func preferredBreak(clusters []graphemeCluster, start, end, width int) int {
	equals := -1
	boundary := -1
	used := 0
	for index := start; index < end; index++ {
		used += clusters[index].width
		switch clusters[index].text {
		case "=":
			if used*2 >= width {
				equals = index + 1
			}
		case "/", ":", "_", "-", ".", ",", ";", "?", "&", "#", "@":
			if used*3 >= width*2 {
				boundary = index + 1
			}
		}
	}
	if equals > start {
		return equals
	}
	if boundary > start {
		return boundary
	}
	return end
}

func resolvedClusterWidth(cluster string, unicodeWidth int, profile WidthProfile) int {
	if isHangulCompatibilityJamoCluster(cluster) && (profile.HangulCompatibilityJamoWidth == 1 || profile.HangulCompatibilityJamoWidth == 2) {
		return profile.HangulCompatibilityJamoWidth
	}
	if strings.ContainsRune(cluster, '⃣') {
		return 2
	}
	return unicodeWidth
}

func isHangulCompatibilityJamoCluster(cluster string) bool {
	r, size := utf8.DecodeRuneInString(cluster)
	if r < 0x3131 || r > 0x318e {
		return false
	}
	return size == len(cluster)
}
