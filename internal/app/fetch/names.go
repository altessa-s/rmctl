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

// SafeName turns a visible name into one path component: see
// [domain.SafeName].
func SafeName(name string) string { return domain.SafeName(name) }

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
