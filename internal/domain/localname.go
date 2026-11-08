// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain

import "strings"

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

// DerivedFileName is the local name of a file made from a document, such
// as its drawing or its note: its safe name, a .pdf or .epub it ends with
// replaced by "." and ext.
func DerivedFileName(name, ext string) string {
	return TrimOriginalExt(SafeName(name)) + "." + ext
}

// TrimOriginalExt is name without the .pdf or .epub it ends with, in any
// case, unless that is all of it.
func TrimOriginalExt(name string) string {
	for _, orig := range []string{".pdf", ".epub"} {
		if cut := len(name) - len(orig); cut > 0 && strings.EqualFold(name[cut:], orig) {
			return name[:cut]
		}
	}
	return name
}
