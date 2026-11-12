// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// NoText stands for the words of a highlight over a page with no text
// layer, which the tablet could not copy out.
const NoText = "(no text)"

// NotesRemovedDir is the directory of a folder's notes that keeps, with
// their paths, the notes pruned for documents no longer below the folder.
// Its leading dot hides it from Obsidian and Logseq, as their own trash.
const NotesRemovedDir = ".rmctl-removed"

// HighlightText is a highlight's words, or NoText.
func HighlightText(h Highlight) string {
	if !h.HasText || strings.TrimSpace(h.Text) == "" {
		return NoText
	}
	return h.Text
}

// isListItem reports a paragraph that markdown writes as a list line, which
// must not be cut from its neighbors by a blank line.
func isListItem(s ParagraphStyle) bool {
	switch s {
	case StyleBullet, StyleBulletNested, StyleCheckbox, StyleCheckboxDone, StyleNumbered, StyleNumberedNested:
		return true
	default:
		return false
	}
}

// PageMarkdown is the typed text of a page as markdown, each paragraph in
// the markdown of its style, a heading after heading ("#", or deeper in a
// document that has headings of its own). A numbered list counts from 1
// again after anything that is not a numbered line. A page with no text
// is "".
func PageMarkdown(p Page, heading string) string {
	var b strings.Builder
	number, nested := 0, 0
	prevList, written := false, false
	for _, para := range p.Paragraphs {
		if strings.TrimSpace(para.Text) == "" {
			// An empty paragraph only parts what is around it.
			number, nested, prevList = 0, 0, false
			continue
		}
		list := isListItem(para.Style)
		if written && (!list || !prevList) {
			b.WriteByte('\n')
		}
		written = true
		// A nested line does not end the numbered list it hangs from.
		if para.Style != StyleNumbered && para.Style != StyleNumberedNested {
			number = 0
		}
		if para.Style != StyleNumberedNested {
			nested = 0
		}
		switch para.Style {
		case StyleNumbered:
			number++
		case StyleNumberedNested:
			nested++
		default:
		}
		writeMarkdownLine(&b, para, heading, number, nested)
		b.WriteByte('\n')
		prevList = list
	}
	return b.String()
}

// writeMarkdownLine writes one paragraph in the markdown of its style.
func writeMarkdownLine(b *strings.Builder, p Paragraph, heading string, number, nested int) {
	switch p.Style {
	case StyleHeading:
		b.WriteString(heading)
		b.WriteByte(' ')
	case StyleBold:
		b.WriteString("**")
		b.WriteString(strings.TrimSpace(p.Text))
		b.WriteString("**")
		return
	case StyleBullet:
		b.WriteString("- ")
	case StyleBulletNested:
		b.WriteString("  - ")
	case StyleCheckbox:
		b.WriteString("- [ ] ")
	case StyleCheckboxDone:
		b.WriteString("- [x] ")
	case StyleNumbered:
		b.WriteString(strconv.Itoa(number))
		b.WriteString(". ")
	case StyleNumberedNested:
		b.WriteString("   ")
		b.WriteString(strconv.Itoa(nested))
		b.WriteString(". ")
	default:
	}
	b.WriteString(p.Text)
}

// ErrBadPageLink means a page link pattern names a placeholder there is
// none of.
var ErrBadPageLink = errors.New("bad page link pattern")

// The placeholders of a page link pattern.
var pageLinkFields = []string{"{root}", "{path}", "{name}", "{id}", "{page}"} //nolint:gochecknoglobals // a fixed list

// PageLink is a pattern for the link from a highlight to its page in a PDF
// kept beside the notes, such as the one "rmctl render" draws of the same
// folder. Its placeholders are {root}, the way from the note's directory up
// to the top of the notes ("" or "../" repeated); {path}, the note's path
// below that top, with slashes and without its extension; {name}, the last
// part of {path}; {id}, the document's identifier; and {page}, the page,
// from 1. "{root}../pdf/{path}.pdf#page={page}" links the notes in
// vault/notes to the PDFs "rmctl render" draws of the same folder into
// vault/pdf. The zero PageLink links nothing.
type PageLink struct {
	pattern string
}

// ParsePageLink checks a pattern: every {...} in it must be a placeholder.
func ParsePageLink(pattern string) (PageLink, error) {
	rest := pattern
	for _, f := range pageLinkFields {
		rest = strings.ReplaceAll(rest, f, "")
	}
	if i := strings.IndexByte(rest, '{'); i >= 0 {
		end := strings.IndexByte(rest[i:], '}')
		bad := rest[i:]
		if end >= 0 {
			bad = rest[i : i+end+1]
		}
		return PageLink{}, fmt.Errorf("%w: %q: %s is no placeholder; use %s", ErrBadPageLink, pattern, bad, strings.Join(pageLinkFields, ", "))
	}
	return PageLink{pattern: pattern}, nil
}

// IsZero reports a PageLink that links nothing.
func (l PageLink) IsZero() bool { return l.pattern == "" }

// String is the pattern.
func (l PageLink) String() string { return l.pattern }

// Target is the link of page of the note at rel (its path below the top of
// the notes, with slashes, without its extension) of the document id, made
// safe for a markdown link between angle brackets.
func (l PageLink) Target(rel string, id ItemID, page int) string {
	root := strings.Repeat("../", strings.Count(rel, "/"))
	out := strings.NewReplacer(
		"{root}", root, "{path}", rel, "{name}", path.Base(rel), "{id}", string(id), "{page}", strconv.Itoa(page),
	).Replace(l.pattern)
	return strings.NewReplacer("<", "%3C", ">", "%3E", "\n", "%0A", "\r", "%0D").Replace(out)
}

// Note is a document's highlights and typed text as a markdown note for a
// notes app such as Obsidian or Logseq: YAML front matter, the title, the
// highlights by page as quotes, then the typed text by page. The template
// is stable: the same document writes the same bytes, so a note written
// again over an unchanged document is no change at all.
type Note struct {
	Item Item
	// Path is the document's path in the library; "" for a document read
	// from a local archive, whose note then names none.
	Path  string
	Pages []Page
	// Link links each highlight to its page; the zero PageLink links none.
	Link PageLink
	// Rel is the note's path below the top of the notes, with slashes and
	// without its extension: what Link's {path} stands for.
	Rel string
}

// NoteType is the kind of a document as a note names it: pdf, epub or
// notebook.
func NoteType(k Kind) string {
	switch k {
	case KindPDF, KindAnnotatedPDF:
		return "pdf"
	case KindEPUB:
		return "epub"
	default:
		return "notebook"
	}
}

// NoteTags are a document's tags as a note's front matter lists them: a
// tag in a notes app holds no space, so each run of white space becomes
// "-", and a "#" in front goes; empty and repeated tags go, and the rest
// are sorted, so the order the tablet keeps them in changes nothing.
func NoteTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.Join(strings.FieldsFunc(t, unicode.IsSpace), "-")
		if t = strings.TrimLeft(t, "#"); t != "" {
			out = append(out, t)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Highlights is how many highlights the pages hold.
func Highlights(pages []Page) int {
	n := 0
	for _, p := range pages {
		n += len(p.Highlights)
	}
	return n
}

// ReadingOrder is a page's highlights from the top of the page down, and
// from the left on one line; one with no rectangle keeps its place after
// those that have one.
func ReadingOrder(hs []Highlight) []Highlight {
	top := func(h Highlight) (float64, float64) {
		if len(h.Rects) == 0 {
			return math.Inf(1), math.Inf(1)
		}
		y, x := math.Inf(1), math.Inf(1)
		for _, r := range h.Rects {
			y, x = min(y, r.Y), min(x, r.X)
		}
		return y, x
	}
	out := slices.Clone(hs)
	slices.SortStableFunc(out, func(a, b Highlight) int {
		ay, ax := top(a)
		by, bx := top(b)
		return cmp.Or(cmp.Compare(ay, by), cmp.Compare(ax, bx))
	})
	return out
}

// yamlString is s as a YAML double-quoted scalar: a Go quoted string,
// whose escapes YAML reads the same.
func yamlString(s string) string { return strconv.Quote(s) }

// Title is the note's title: the document's name, without the .pdf or
// .epub it ends with.
func (n Note) Title() string { return TrimOriginalExt(n.Item.Name) }

// Markdown is the note.
func (n Note) Markdown() string {
	var b strings.Builder
	n.frontMatter(&b)
	fmt.Fprintf(&b, "\n# %s\n", oneLine(n.Title()))
	n.highlights(&b)
	n.text(&b)
	return b.String()
}

// frontMatter writes the YAML front matter, its keys always in this order
// and those that do not apply left out.
func (n Note) frontMatter(b *strings.Builder) {
	b.WriteString("---\n")
	fmt.Fprintf(b, "title: %s\n", yamlString(n.Title()))
	fmt.Fprintf(b, "remarkable-id: %s\n", yamlString(string(n.Item.ID)))
	if n.Path != "" {
		fmt.Fprintf(b, "remarkable-path: %s\n", yamlString(n.Path))
	}
	fmt.Fprintf(b, "type: %s\n", NoteType(n.Item.Kind))
	if !n.Item.Modified.IsZero() {
		fmt.Fprintf(b, "modified: %s\n", n.Item.Modified.UTC().Truncate(time.Second).Format(time.RFC3339))
	}
	fmt.Fprintf(b, "pages: %d\n", len(n.Pages))
	fmt.Fprintf(b, "highlights: %d\n", Highlights(n.Pages))
	if tags := NoteTags(n.Item.Tags); len(tags) > 0 {
		b.WriteString("tags:\n")
		for _, t := range tags {
			fmt.Fprintf(b, "  - %s\n", yamlString(t))
		}
	}
	b.WriteString("---\n")
}

// highlights writes "## Highlights", a "### Page N" for each page that has
// any, and each highlight as a quote ending with its page and color.
func (n Note) highlights(b *strings.Builder) {
	if Highlights(n.Pages) == 0 {
		return
	}
	b.WriteString("\n## Highlights\n")
	for _, p := range n.Pages {
		if len(p.Highlights) == 0 {
			continue
		}
		fmt.Fprintf(b, "\n### Page %d\n", p.Number)
		for _, h := range ReadingOrder(p.Highlights) {
			b.WriteByte('\n')
			for line := range strings.SplitSeq(strings.TrimRight(HighlightText(h), "\r\n"), "\n") {
				if line = strings.TrimRight(line, " \t\r"); line == "" {
					b.WriteString(">\n")
					continue
				}
				fmt.Fprintf(b, "> %s\n", line)
			}
			b.WriteString(">\n> — ")
			ref := "p. " + strconv.Itoa(p.Number)
			if n.Link.IsZero() {
				b.WriteString(ref)
			} else {
				fmt.Fprintf(b, "[%s](<%s>)", ref, n.Link.Target(n.Rel, n.Item.ID, p.Number))
			}
			if c := noteColor(h.Color); c != "" {
				b.WriteString(" #highlight/")
				b.WriteString(c)
			}
			b.WriteByte('\n')
		}
	}
}

// text writes "## Typed text" and a "### Page N" for each page that has
// any, its own headings under them.
func (n Note) text(b *strings.Builder) {
	started := false
	for _, p := range n.Pages {
		md := PageMarkdown(p, "####")
		if md == "" {
			continue
		}
		if !started {
			b.WriteString("\n## Typed text\n")
			started = true
		}
		fmt.Fprintf(b, "\n### Page %d\n\n%s", p.Number, md)
	}
}

// noteColor is a color's name as a tag takes it, or "" for none.
func noteColor(c string) string {
	return strings.Join(strings.FieldsFunc(c, func(r rune) bool { return unicode.IsSpace(r) || r == '#' }), "-")
}

// oneLine is s with its line breaks as spaces, for a heading.
func oneLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' }), " ")
}
