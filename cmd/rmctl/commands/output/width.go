// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package output

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
)

// ellipsis ends a truncated value.
const ellipsis = "…"

// Width is the number of terminal columns s takes: a wide rune (CJK, most
// emoji) takes two, a combining mark none.
func Width(s string) int { return runewidth.StringWidth(s) }

// Truncate shortens s to at most w columns, ending it with "…" when
// anything was cut. It never splits a wide rune or a rune from its
// combining marks.
func Truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if Width(s) <= w {
		return s
	}
	return runewidth.Truncate(s, w, ellipsis)
}

// pad fills s with spaces to w columns, on the left when right is set.
func pad(s string, sw, w int, right bool) string {
	if sw >= w {
		return s
	}
	fill := strings.Repeat(" ", w-sw)
	if right {
		return fill + s
	}
	return s + fill
}

// The units of [HumanSize].
const (
	unit     = 1024
	maxPower = 4
)

// HumanSize is a byte count for a person, in binary units: 512 B, 1.5 KiB,
// 3.2 MiB.
func HumanSize(n int64) string {
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < maxPower; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTP"[exp])
}

// oneLine keeps a value of a table on its line: TABs and line breaks become
// spaces.
var oneLine = strings.NewReplacer("\t", " ", "\r\n", " ", "\n", " ", "\r", " ") //nolint:gochecknoglobals // immutable
