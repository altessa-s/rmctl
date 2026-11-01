// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fetch

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/altessa-s/rmctl/internal/domain"
)

// unnamed stands in for a name that sanitizes to nothing usable.
const unnamed = "unnamed"

// firstPrintable is the first character that is not a control character,
// and del the one control character above it.
const (
	firstPrintable = 0x20
	del            = 0x7f
)

// SafeName turns a visible name into one path component: '/', '\', ':',
// control characters and DEL become '-', and an empty name, "." or ".."
// becomes "unnamed". The cloud lets a name hold anything; a local path must
// not be steered out of the directory it is written into.
func SafeName(name string) string {
	out := strings.Map(func(r rune) rune {
		switch {
		case r == '/', r == '\\', r == ':', r < firstPrintable, r == del:
			return '-'
		default:
			return r
		}
	}, name)
	switch strings.TrimSpace(out) {
	case "", ".", "..":
		return unnamed
	}
	return out
}

// FileName is the local file name of a document's original: its safe name,
// with the extension added unless the name already ends with it.
func FileName(name, ext string) string {
	base := SafeName(name)
	if ext == "" || strings.HasSuffix(strings.ToLower(base), "."+ext) {
		return base
	}
	return base + "." + ext
}

// idSuffixLen is how many identifier characters tell two equal names apart.
const idSuffixLen = 8

// Disambiguated tells apart two items that would take one local name: the
// first eight letters, digits or dashes of the identifier go in brackets
// before the extension, or at the end of a folder's name.
func Disambiguated(name string, id domain.ItemID, ext string) string {
	var tag strings.Builder
	for _, r := range string(id) {
		if tag.Len() == idSuffixLen {
			break
		}
		if r == '-' || r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			tag.WriteRune(r)
		}
	}
	if ext != "" && strings.HasSuffix(strings.ToLower(name), "."+ext) {
		stem := name[:len(name)-len(ext)-1]
		return fmt.Sprintf("%s (%s).%s", stem, tag.String(), ext)
	}
	return fmt.Sprintf("%s (%s)", name, tag.String())
}
