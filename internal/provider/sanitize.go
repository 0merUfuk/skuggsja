package provider

import (
	pathpkg "path"
	"strings"
	"unicode"
)

// SafeLabel keeps a source-provided label useful while excluding control
// characters and unreasonably large values from generated output.
func SafeLabel(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	value = strings.TrimSpace(value)
	filtered := make([]rune, 0, min(len(value), maxRunes))
	for _, r := range value {
		if unicode.IsControl(r) {
			continue
		}
		filtered = append(filtered, r)
		if len(filtered) == maxRunes {
			break
		}
	}
	value = strings.TrimSpace(string(filtered))
	if looksAbsolutePath(value) {
		return ""
	}
	return value
}

// ProjectName reduces an absolute source path to its final directory name.
func ProjectName(path string) string {
	if path == "" {
		return ""
	}
	// Source paths may have been produced on a different OS than the one
	// running Skuggsja, so normalize both separator styles before taking a base.
	normalized := strings.ReplaceAll(path, `\`, "/")
	name := pathpkg.Base(pathpkg.Clean(normalized))
	if name == "." || name == "/" || strings.HasSuffix(name, ":") {
		return ""
	}
	return SafeLabel(name, 80)
}

func looksAbsolutePath(value string) bool {
	lower := strings.ToLower(value)
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, `\`) || strings.HasPrefix(lower, "file:") {
		return true
	}
	return len(value) >= 3 && unicode.IsLetter(rune(value[0])) && value[1] == ':' &&
		(value[2] == '/' || value[2] == '\\')
}
