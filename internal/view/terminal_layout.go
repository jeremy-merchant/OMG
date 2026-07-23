package view

import (
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"
)

const (
	minimumTTYWidth = 36
	defaultTTYWidth = 96
	maximumTTYWidth = 160
)

func ttyOutputWidth(output io.Writer) int {
	file, isFile := output.(*os.File)
	if !isFile {
		return defaultTTYWidth
	}
	info, err := file.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return defaultTTYWidth
	}
	if configured, ok := configuredTTYWidth(); ok {
		return configured
	}
	if width, ok := platformTTYWidth(file); ok {
		return normalizeTTYWidth(width)
	}
	return defaultTTYWidth
}

func configuredTTYWidth() (int, bool) {
	value := strings.TrimSpace(os.Getenv("COLUMNS"))
	if value == "" {
		return 0, false
	}
	width, err := strconv.Atoi(value)
	if err != nil || width <= 0 {
		return 0, false
	}
	return normalizeTTYWidth(width), true
}

func normalizeTTYWidth(width int) int {
	if width < minimumTTYWidth {
		return minimumTTYWidth
	}
	if width > maximumTTYWidth {
		return maximumTTYWidth
	}
	return width
}

func ttyDisplayWidth(value string) int {
	value = stripTTYANSI(value)
	width := 0
	for _, character := range value {
		width += ttyRuneWidth(character)
	}
	return width
}

func ttyRuneWidth(character rune) int {
	if character == 0 || unicode.IsControl(character) || unicode.Is(unicode.Mn, character) || unicode.Is(unicode.Me, character) {
		return 0
	}
	if character >= 0x1100 && (character <= 0x115f ||
		character == 0x2329 || character == 0x232a ||
		(character >= 0x2e80 && character <= 0xa4cf && character != 0x303f) ||
		(character >= 0xac00 && character <= 0xd7a3) ||
		(character >= 0xf900 && character <= 0xfaff) ||
		(character >= 0xfe10 && character <= 0xfe19) ||
		(character >= 0xfe30 && character <= 0xfe6f) ||
		(character >= 0xff00 && character <= 0xff60) ||
		(character >= 0xffe0 && character <= 0xffe6) ||
		(character >= 0x1f300 && character <= 0x1faff) ||
		(character >= 0x20000 && character <= 0x3fffd)) {
		return 2
	}
	return 1
}

func wrapTTYText(value string, width int) []string {
	value = strings.TrimSpace(strings.Map(func(character rune) rune {
		if character == '\n' || character == '\r' || character == '\t' {
			return ' '
		}
		return character
	}, stripTTYANSI(value)))
	if value == "" {
		return nil
	}
	if width < 1 {
		width = 1
	}
	words := strings.Fields(value)
	lines := make([]string, 0, len(words))
	current := ""
	for _, word := range words {
		for _, part := range splitTTYToken(word, width) {
			if current == "" {
				current = part
				continue
			}
			if ttyDisplayWidth(current)+1+ttyDisplayWidth(part) <= width {
				current += " " + part
				continue
			}
			lines = append(lines, current)
			current = part
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func splitTTYToken(value string, width int) []string {
	if ttyDisplayWidth(value) <= width {
		return []string{value}
	}
	parts := make([]string, 0, 2)
	var current strings.Builder
	currentWidth := 0
	for _, character := range value {
		cellWidth := ttyRuneWidth(character)
		if currentWidth > 0 && currentWidth+cellWidth > width {
			parts = append(parts, current.String())
			current.Reset()
			currentWidth = 0
		}
		current.WriteRune(character)
		currentWidth += cellWidth
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func stripTTYANSI(value string) string {
	var output strings.Builder
	for index := 0; index < len(value); {
		if value[index] == 0x1b && index+1 < len(value) && value[index+1] == '[' {
			index += 2
			for index < len(value) {
				character := value[index]
				index++
				if character >= 0x40 && character <= 0x7e {
					break
				}
			}
			continue
		}
		output.WriteByte(value[index])
		index++
	}
	return output.String()
}

func ttyPadRight(value string, width int) string {
	padding := width - ttyDisplayWidth(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}
