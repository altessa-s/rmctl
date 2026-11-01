// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The markdown a notebook can take, line by line. What has no paragraph
// style becomes plain text: inline emphasis, links and code lose their
// marks, and a fenced block keeps its lines as plain paragraphs.
var (
	headingLine  = regexp.MustCompile(`^(#{1,6})[ \t]+(.*?)[ \t#]*$`)
	bulletLine   = regexp.MustCompile(`^[ \t]*[-*+][ \t]+(.*)$`)
	numberedLine = regexp.MustCompile(`^[ \t]*\d{1,9}[.)][ \t]+(.*)$`)
	breakLine    = regexp.MustCompile(`^[ \t]*(?:(?:-[ \t]*){3,}|(?:\*[ \t]*){3,}|(?:_[ \t]*){3,})$`)
	tableLine    = regexp.MustCompile(`^[ \t]*\|.*\|[ \t]*$`)
	// tableDelimiter is the row under a table's header: cells of dashes,
	// colons for alignment, outer pipes optional, one pipe at least.
	tableDelimiter = regexp.MustCompile(`^[ \t]*\|?[ \t]*:?-+:?[ \t]*(\|[ \t]*:?-+:?[ \t]*)+\|?[ \t]*$|^[ \t]*\|[ \t]*:?-+:?[ \t]*\|?[ \t]*$`)
	imageInline    = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	linkInline     = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	wholeBold      = regexp.MustCompile(`^(\*\*|__)(.+)(\*\*|__)$`)
	// Emphasis opens on a mark followed by a non-space and closes on one
	// preceded by a non-space; see underscores for the other mark.
	strongStar = regexp.MustCompile(`\*\*(\S(?:.*?\S)??)\*\*`)
	emStar     = regexp.MustCompile(`\*(\S(?:[^*]*?\S)??)\*`)
	quoteLine  = regexp.MustCompile(`^[ \t]*>[ \t]?(.*)$`)
)

// thematicBreak is how a horizontal rule is typed into a notebook.
const thematicBreak = "———"

// ParseMarkdown turns markdown into notebook paragraphs: a level-one
// heading is a heading, deeper headings and wholly bold lines are bold,
// bullets and numbered items are list paragraphs (a task item stays a
// bullet, its box as text), a rule is a dash line, and consecutive lines
// of text join into one paragraph. Tables and pictures have no place in a
// notebook and are refused, naming their line.
func ParseMarkdown(text string) ([]Paragraph, error) {
	var out []Paragraph
	var pending []string
	flush := func() {
		if len(pending) > 0 {
			out = append(out, styled(strings.Join(pending, " ")))
			pending = nil
		}
	}
	fenced := false
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for n, line := range lines {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			flush()
			fenced = !fenced
			continue
		}
		if fenced {
			out = append(out, Paragraph{Text: line, Style: StylePlain})
			continue
		}
		next := ""
		if n+1 < len(lines) {
			next = lines[n+1]
		}
		if err := refused(n+1, line, next); err != nil {
			return nil, err
		}
		if trimmed == "" {
			flush()
			continue
		}
		// Only a line that starts with a mark can be anything but text: the
		// first byte picks the one or two patterns worth trying.
		p, ok := block(line, trimmed)
		if !ok {
			pending = append(pending, trimmed)
			continue
		}
		flush()
		out = append(out, p)
	}
	flush()
	return out, nil
}

// block reads a line that is a block of its own: a rule, a heading, a list
// item or a quote. ok false is a line of running text.
func block(line, trimmed string) (Paragraph, bool) {
	switch trimmed[0] {
	case '-', '*', '_', '+':
		if trimmed[0] != '+' && breakLine.MatchString(line) {
			return Paragraph{Text: thematicBreak, Style: StylePlain}, true
		}
		if trimmed[0] != '_' {
			if m := bulletLine.FindStringSubmatch(line); m != nil {
				return Paragraph{Text: inline(m[1]), Style: StyleBullet}, true
			}
		}
	case '#':
		if m := headingLine.FindStringSubmatch(trimmed); m != nil {
			style := StyleBold
			if len(m[1]) == 1 {
				style = StyleHeading
			}
			return Paragraph{Text: inline(m[2]), Style: style}, true
		}
	case '>':
		if m := quoteLine.FindStringSubmatch(line); m != nil {
			return styled(m[1]), true
		}
	default:
		if trimmed[0] >= '0' && trimmed[0] <= '9' {
			if m := numberedLine.FindStringSubmatch(line); m != nil {
				return Paragraph{Text: inline(m[1]), Style: StyleNumbered}, true
			}
		}
	}
	return Paragraph{}, false
}

// refused names what markdown cannot carry into a notebook.
// A table is a line between pipes, a delimiter row, or the header row a
// delimiter row follows: GFM tables need no outer pipes, and without them
// only the delimiter tells a table from prose with a pipe in it.
func refused(n int, line, next string) error {
	if !strings.Contains(line, "|") && !strings.Contains(line, "![") {
		return nil
	}
	switch {
	case tableLine.MatchString(line), tableDelimiter.MatchString(line),
		strings.Contains(next, "|") && tableDelimiter.MatchString(next):
		return fmt.Errorf("%w: line %d: a table cannot go into a notebook", ErrUnsupportedFile, n)
	case imageInline.MatchString(line):
		return fmt.Errorf("%w: line %d: a picture cannot go into a notebook", ErrUnsupportedFile, n)
	default:
		return nil
	}
}

// styled is a paragraph of text: bold when the whole of it is.
func styled(text string) Paragraph {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "**") || strings.HasPrefix(text, "__") {
		if m := wholeBold.FindStringSubmatch(text); m != nil && m[1] == m[3] && !strings.Contains(m[2], m[1]) {
			return Paragraph{Text: inline(m[2]), Style: StyleBold}
		}
	}
	return Paragraph{Text: inline(text), Style: StylePlain}
}

// inline drops the marks of emphasis, code and links, keeping their text.
func inline(text string) string {
	if strings.Contains(text, "](") {
		text = linkInline.ReplaceAllString(text, "$1")
	}
	if strings.Contains(text, "`") {
		text = strings.ReplaceAll(text, "`", "")
	}
	if strings.Contains(text, "*") {
		text = strongStar.ReplaceAllString(text, "$1")
		text = emStar.ReplaceAllString(text, "$1")
	}
	if strings.Contains(text, "_") {
		text = underscores(underscores(text, "__"), "_")
	}
	return strings.TrimSpace(text)
}

// underscores drops the marks of emphasis made with mark, "__" or "_". A
// mark opens with a non-space after it and no letter, digit or underscore
// before it, and closes with a non-space before it and none of those after
// it, so snake_case and a lone underscore stay as they are. The text of a
// single mark holds no underscore. One pass, left to right.
func underscores(text, mark string) string {
	var b strings.Builder
	i := 0
	for i < len(text) {
		j := strings.Index(text[i:], mark)
		if j < 0 {
			break
		}
		j += i
		start := j + len(mark)
		if !opens(text, j, start) {
			b.WriteString(text[i:start])
			i = start
			continue
		}
		k, ok := closer(text, start, mark)
		if !ok {
			// Nothing up to k closes this mark; a later one may open there.
			b.WriteString(text[i:k])
			i = k
			continue
		}
		b.WriteString(text[i:j])
		b.WriteString(text[start:k])
		i = k + len(mark)
	}
	b.WriteString(text[i:])
	return b.String()
}

// opens reports a mark at text[j:start] that may open emphasis.
func opens(text string, j, start int) bool {
	if start >= len(text) || text[start] == '_' || isSpace(text[start]) {
		return false
	}
	before, _ := utf8.DecodeLastRuneInString(text[:j])
	return j == 0 || !wordRune(before)
}

// closer finds the first mark after from that closes emphasis. When none
// does it answers where to go on: the underscore a single mark's text may
// not hold, or a later opener, which a closer pairs with first, or the end.
func closer(text string, from int, mark string) (int, bool) {
	for k := from + 1; k+len(mark) <= len(text); k++ {
		if text[k] != '_' {
			continue
		}
		if strings.HasPrefix(text[k:], mark) && closes(text, k, mark) {
			return k, true
		}
		if len(mark) == 1 || (strings.HasPrefix(text[k:], mark) && opens(text, k, k+len(mark))) {
			return k, false
		}
	}
	return len(text), false
}

// closes reports a mark at k that may close emphasis.
func closes(text string, k int, mark string) bool {
	end := k + len(mark)
	if isSpace(text[k-1]) || text[k-1] == '_' || (end < len(text) && text[end] == '_') {
		return false
	}
	after, _ := utf8.DecodeRuneInString(text[end:])
	return end == len(text) || !wordRune(after)
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' }

func wordRune(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// ParsePlainText turns text into notebook paragraphs, one per line; a blank
// line stays an empty paragraph.
func ParsePlainText(text string) []Paragraph {
	text = strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	lines := strings.Split(text, "\n")
	out := make([]Paragraph, len(lines))
	for i, line := range lines {
		out[i] = Paragraph{Text: strings.TrimRight(line, "\r"), Style: StylePlain}
	}
	return out
}
