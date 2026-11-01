// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
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
// Annotations hang on page identifiers, so every page that still has its
// page of the original keeps its identifier and its place; a page whose
// original page is gone is dropped; and every page of the new original that
// no page shows yet gets a new page, appended.
type Relayout struct {
	// Count is the new original's page count.
	Count int
	// Kept are the pages that stay, in order: those whose original page is
	// below Count, and those the tablet added with none.
	Kept []PageSlot
	// Appended are the new pages, in the original's order.
	Appended []PageSlot
	// Dropped are the pages whose original page is at or beyond Count.
	Dropped []PageSlot
}

// PlanRelayout lays pages out for a new original of count pages; newID
// names each appended page.
func PlanRelayout(pages []PageSlot, count int, newID func() string) Relayout {
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
		r.Kept = append(r.Kept, p)
	}
	for i, ok := range shown {
		if !ok {
			r.Appended = append(r.Appended, PageSlot{ID: newID(), Original: i})
		}
	}
	return r
}

// Pages are the document's pages after the relayout, in order.
func (r Relayout) Pages() []PageSlot {
	return append(slices.Clone(r.Kept), r.Appended...)
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
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(top, json.Deterministic(true))
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
		t, ok := line[p.ID]
		if !ok {
			t = DefaultTemplate
		}
		b.WriteString(t)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// PageFiles are the files of document doc that belong to page: its .rm,
// its per-page metadata and its old-style highlights. Annotations are the
// .rm and the highlights.
func PageFiles(doc ItemID, page string, files []File) (all, annotations []File) {
	for _, f := range files {
		switch f.Name {
		case string(doc) + "/" + page + ".rm", string(doc) + ".highlights/" + page + ".json":
			all = append(all, f)
			annotations = append(annotations, f)
		case string(doc) + "/" + page + "-metadata.json":
			all = append(all, f)
		}
	}
	return all, annotations
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
