// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
)

// PageSlot is one page of a document: its identifier, and the page of the
// original PDF it shows, counted from 0, or -1 for a page the tablet added
// with no page of the original under it.
type PageSlot struct {
	ID       string
	Original int
	// From is the identifier a kept page had before a relayout gave it a
	// new one; "" for a page that is new.
	From string
}

// String is the slot for a diagnostic: {id original}, or {id original
// from} for a kept page with a new identifier.
func (p PageSlot) String() string {
	if p.From == "" {
		return "{" + p.ID + " " + strconv.Itoa(p.Original) + "}"
	}
	return "{" + p.ID + " " + strconv.Itoa(p.Original) + " " + p.From + "}"
}

// noOriginal is the Original of a page with no page of the PDF under it.
const noOriginal = -1

// ContentPages reads a document's pages from its .content, in reading
// order: formatVersion 1's pages and redirectionPageMap (a missing map maps
// each page to the original page of its index), or cPages.pages ordered by
// their idx keys, deleted pages left out. A document with no page list has
// no pages.
func ContentPages(content []byte) ([]PageSlot, error) {
	var c struct {
		Pages    []string `json:"pages"`
		Redirect []int    `json:"redirectionPageMap"`
		CPages   *struct {
			Pages []cPageSlot `json:"pages"`
		} `json:"cPages"`
	}
	if err := json.Unmarshal(content, &c); err != nil {
		return nil, MalformedFile("content", err.Error())
	}
	if c.CPages != nil {
		live := slices.DeleteFunc(slices.Clone(c.CPages.Pages), func(p cPageSlot) bool { return p.ID == "" || p.deleted() })
		slices.SortStableFunc(live, func(a, b cPageSlot) int { return strings.Compare(a.Idx.Value, b.Idx.Value) })
		out := make([]PageSlot, len(live))
		for i, p := range live {
			out[i] = PageSlot{ID: p.ID, Original: noOriginal}
			if p.Redir != nil {
				out[i].Original = p.Redir.Value
			}
		}
		return out, nil
	}
	out := make([]PageSlot, 0, len(c.Pages))
	for i, id := range c.Pages {
		if id == "" {
			continue
		}
		original := i
		if c.Redirect != nil {
			original = noOriginal
			if i < len(c.Redirect) {
				original = c.Redirect[i]
			}
		}
		out = append(out, PageSlot{ID: id, Original: original})
	}
	return out, nil
}

// cPageSlot is what ContentPages reads of a cPages entry.
type cPageSlot struct {
	ID  string `json:"id"`
	Idx struct {
		Value string `json:"value"`
	} `json:"idx"`
	Redir *struct {
		Value int `json:"value"`
	} `json:"redir"`
	Deleted *struct {
		Value jsontext.Value `json:"value"`
	} `json:"deleted"`
}

func (p cPageSlot) deleted() bool {
	return cPage{ID: p.ID, Deleted: p.Deleted}.deleted()
}

// Relayout is how a document's pages follow a new original of Count pages.
// Every page that still has its page of the original keeps its place and
// its annotations under a new identifier — the tablet keeps page previews
// by identifier, and a page with its old one would show the old original's
// picture; a page whose original page is gone is dropped; and every page of
// the new original that no page shows yet gets a new page, appended.
type Relayout struct {
	// Count is the new original's page count.
	Count int
	// Kept are the pages that stay, in order: those whose original page is
	// below Count, and those the tablet added with none. Each has a new ID
	// and its old one in From.
	Kept []PageSlot
	// Appended are the new pages, in the original's order.
	Appended []PageSlot
	// Dropped are the pages whose original page is at or beyond Count.
	Dropped []PageSlot
}

// PlanRelayout lays pages out for a new original of count pages; newID
// names each kept page anew, in order, then each appended page. It is
// given the page it names: a kept page as it was, an appended one as its
// Original alone, so a caller can give the same page the same identifier
// on every plan.
func PlanRelayout(pages []PageSlot, count int, newID func(from PageSlot) string) Relayout {
	r := Relayout{Count: count}
	shown := make([]bool, count)
	for _, p := range pages {
		if p.Original >= count {
			r.Dropped = append(r.Dropped, p)
			continue
		}
		if p.Original >= 0 {
			shown[p.Original] = true
		}
		r.Kept = append(r.Kept, PageSlot{ID: newID(p), Original: p.Original, From: p.ID})
	}
	for i, ok := range shown {
		if !ok {
			r.Appended = append(r.Appended, PageSlot{ID: newID(PageSlot{Original: i}), Original: i})
		}
	}
	return r
}

// Pages are the document's pages after the relayout, in order.
func (r Relayout) Pages() []PageSlot {
	return append(slices.Clone(r.Kept), r.Appended...)
}

// Renamed maps each kept page's old identifier to its new one.
func (r Relayout) Renamed() map[string]string {
	m := make(map[string]string, len(r.Kept))
	for _, p := range r.Kept {
		m[p.From] = p.ID
	}
	return m
}

// newPageStamp is the CRDT timestamp of a page entry rmctl adds.
const newPageStamp = "1:1"

// RelaidContent is content after r, for a new original of size bytes: the
// page list, the redirection, the page counts and the size follow the new
// original, and every other field — the cover, the tags, the reading
// settings, what rmctl has no name for — stays as it was. current, when
// not empty, is the page the document opens on; a last-opened page that r
// dropped becomes the first page.
func RelaidContent(content []byte, r Relayout, size int64, current string) ([]byte, error) {
	var top map[string]jsontext.Value
	if err := json.Unmarshal(content, &top); err != nil || top == nil {
		return nil, MalformedFile("content", "not an object")
	}
	pages := r.Pages()
	ids := make([]string, len(pages))
	redirect := make([]int, len(pages))
	for i, p := range pages {
		ids[i], redirect[i] = p.ID, p.Original
	}
	set := func(key string, v any) error {
		data, err := json.Marshal(v)
		top[key] = data
		return err
	}
	errs := []error{
		set("pageCount", len(pages)),
		set("originalPageCount", r.Count),
		set("sizeInBytes", strconv.FormatInt(size, 10)),
	}
	if raw, ok := top["cPages"]; ok {
		cp, err := relaidCPages(raw, r, current)
		if err != nil {
			return nil, err
		}
		top["cPages"] = cp
		if _, ok := top["redirectionPageMap"]; ok {
			errs = append(errs, set("redirectionPageMap", redirect))
		}
	} else {
		errs = append(errs, set("pages", ids), set("redirectionPageMap", redirect))
	}
	if tags, ok := top["pageTags"]; ok {
		top["pageTags"] = withoutPages(tags, r.Dropped)
	}
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	out, err := json.Marshal(top, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	return renameStrings(out, r.Renamed())
}

// withoutPages is a pageTags list without the tags of the dropped pages; a
// value that is not a list of objects is kept as it is.
func withoutPages(raw jsontext.Value, dropped []PageSlot) jsontext.Value {
	var tags []map[string]jsontext.Value
	if json.Unmarshal(raw, &tags) != nil {
		return raw
	}
	gone := map[string]bool{}
	for _, p := range dropped {
		gone[p.ID] = true
	}
	tags = slices.DeleteFunc(tags, func(t map[string]jsontext.Value) bool {
		var id string
		return json.Unmarshal(t["pageId"], &id) == nil && gone[id]
	})
	out, err := json.Marshal(tags, json.Deterministic(true))
	if err != nil {
		return raw
	}
	return out
}

// renameStrings rewrites every string of a JSON document — value or object
// key — that is exactly an old page identifier of renamed into the new one,
// so every reference to a kept page follows it: the page lists, the last
// opened page, the page of each page tag, and whatever field rmctl does not
// know. Page identifiers are UUIDs, which no other string of a .content is.
func renameStrings(data []byte, renamed map[string]string) ([]byte, error) {
	if len(renamed) == 0 {
		return data, nil
	}
	dec := jsontext.NewDecoder(bytes.NewReader(data))
	var buf bytes.Buffer
	enc := jsontext.NewEncoder(&buf)
	for {
		tok, err := dec.ReadToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, MalformedFile("content", err.Error())
		}
		if tok.Kind() == '"' {
			if to, ok := renamed[tok.String()]; ok {
				tok = jsontext.String(to)
			}
		}
		if err := enc.WriteToken(tok); err != nil {
			return nil, err
		}
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// relaidCPages rewrites a cPages object for r: the dropped entries go, the
// kept and deleted ones stay as they are, and each appended page is a new
// entry ordered after every other.
func relaidCPages(raw jsontext.Value, r Relayout, current string) (jsontext.Value, error) {
	var cp map[string]jsontext.Value
	if err := json.Unmarshal(raw, &cp); err != nil || cp == nil {
		return nil, MalformedFile("content", "cPages is not an object")
	}
	var entries []map[string]jsontext.Value
	if err := json.Unmarshal(cp["pages"], &entries); err != nil {
		return nil, MalformedFile("content", "cPages.pages is not a list")
	}
	dropped := map[string]bool{}
	for _, p := range r.Dropped {
		dropped[p.ID] = true
	}
	last := ""
	out := make([]map[string]jsontext.Value, 0, len(entries)+len(r.Appended))
	for _, e := range entries {
		var slot cPageSlot
		if data, err := json.Marshal(e); err == nil {
			_ = json.Unmarshal(data, &slot) //nolint:errcheck // a slot that does not decode keeps its entry as it is
		}
		if dropped[slot.ID] {
			continue
		}
		last = max(last, slot.Idx.Value)
		out = append(out, e)
	}
	for i, p := range r.Appended {
		entry, err := json.Marshal(map[string]any{
			"id":    p.ID,
			"idx":   stamped[string]{Timestamp: newPageStamp, Value: last + IndexKey(i)},
			"redir": stamped[int]{Timestamp: newPageStamp, Value: p.Original},
		})
		if err != nil {
			return nil, err
		}
		var m map[string]jsontext.Value
		if err := json.Unmarshal(entry, &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	list, err := json.Marshal(out, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	cp["pages"] = list
	if orig, ok := cp["original"]; ok {
		cp["original"] = restamped(orig, r.Count)
	}
	open := current
	if open == "" {
		if lo, ok := cp["lastOpened"]; ok {
			var v stamped[string]
			if json.Unmarshal(lo, &v) == nil && dropped[v.Value] && len(r.Kept)+len(r.Appended) > 0 {
				open = r.Pages()[0].ID
			}
		}
	}
	if open != "" {
		cp["lastOpened"] = restamped(cp["lastOpened"], open)
	}
	return json.Marshal(cp, json.Deterministic(true))
}

// restamped is a {timestamp, value} pair with value, keeping its timestamp
// when it had one.
func restamped[T any](raw jsontext.Value, value T) jsontext.Value {
	v := stamped[T]{Timestamp: newPageStamp}
	var old stamped[jsontext.Value]
	if len(raw) > 0 && json.Unmarshal(raw, &old) == nil && old.Timestamp != "" {
		v.Timestamp = old.Timestamp
	}
	v.Value = value
	data, err := json.Marshal(v)
	if err != nil {
		panic("domain: a stamped value does not encode: " + err.Error())
	}
	return data
}

// RelaidPagedata is the .pagedata after r: each kept page keeps its line
// (its template), each appended page is Blank. old lists the templates of
// before, one line per page of before.
func RelaidPagedata(old []byte, before []PageSlot, r Relayout) []byte {
	lines := strings.Split(strings.TrimSuffix(string(old), "\n"), "\n")
	line := map[string]string{}
	for i, p := range before {
		if i < len(lines) && strings.TrimSpace(lines[i]) != "" {
			line[p.ID] = lines[i]
		}
	}
	var b strings.Builder
	for _, p := range r.Pages() {
		t, ok := line[p.From]
		if !ok {
			t = DefaultTemplate
		}
		b.WriteString(t)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// pageFileNames are the names of the files of page in document doc: its
// .rm, its old-style highlights (both annotations), its per-page metadata
// and its old text conversion.
func pageFileNames(doc ItemID, page string) (annotations, other []string) {
	d := string(doc)
	return []string{d + "/" + page + ".rm", d + ".highlights/" + page + ".json"},
		[]string{d + "/" + page + "-metadata.json", d + ".textconversion/" + page + ".json"}
}

// allPageFileNames are every name pageFileNames gives, annotations first.
func allPageFileNames(doc ItemID, page string) []string {
	notes, other := pageFileNames(doc, page)
	return slices.Concat(notes, other)
}

// PageFiles are the files of document doc that belong to page: its .rm,
// its old-style highlights, its per-page metadata and its old text
// conversion. Annotations are the .rm and the highlights.
func PageFiles(doc ItemID, page string, files []File) (all, annotations []File) {
	notes, other := pageFileNames(doc, page)
	for _, f := range files {
		switch {
		case slices.Contains(notes, f.Name):
			all = append(all, f)
			annotations = append(annotations, f)
		case slices.Contains(other, f.Name):
			all = append(all, f)
		}
	}
	return all, annotations
}

// RenamedPageFile is f filed under the new identifier of its page, when f
// is one of the files of a page renamed maps; the blob, its hash and its
// size stay as they are.
func RenamedPageFile(doc ItemID, f File, renamed map[string]string) (File, bool) {
	for old, page := range renamed {
		if i := slices.Index(allPageFileNames(doc, old), f.Name); i >= 0 {
			f.Name = allPageFileNames(doc, page)[i]
			return f, true
		}
	}
	return f, false
}

// IsThumbnail reports whether f is a cached page image of document doc: the
// tablet draws them again, and those of a replaced original show the old
// one.
func IsThumbnail(doc ItemID, f File) bool {
	return strings.HasPrefix(f.Name, string(doc)+".thumbnails/")
}

// ContentOriginalPages is how many pages the original of a .content had:
// cPages.original, else originalPageCount, else one past the highest
// original page a page shows.
func ContentOriginalPages(content []byte) int {
	var c struct {
		Original *int `json:"originalPageCount"`
		CPages   *struct {
			Original *struct {
				Value int `json:"value"`
			} `json:"original"`
		} `json:"cPages"`
	}
	if json.Unmarshal(content, &c) == nil {
		switch {
		case c.CPages != nil && c.CPages.Original != nil && c.CPages.Original.Value >= 0:
			return c.CPages.Original.Value
		case c.Original != nil && *c.Original >= 0:
			return *c.Original
		}
	}
	pages, _ := ContentPages(content) //nolint:errcheck // a .content that does not decode has no pages
	n := 0
	for _, p := range pages {
		n = max(n, p.Original+1)
	}
	return n
}
