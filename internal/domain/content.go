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

// FolderContent is the .content of a folder: exactly an empty object. Each
// call returns fresh bytes, so no caller can change another's.
func FolderContent() []byte { return []byte("{}") }

// Tag is one document tag with its millisecond timestamp.
type Tag struct {
	Name      string `json:"name"`
	Timestamp int64  `json:"timestamp"`
}

// content is the .content of an uploaded PDF or EPUB. formatVersion 1 with
// pages is the layout; mixing in cPages gives a document the tablet lists
// and cannot open.
type content struct {
	CoverPageNumber    int            `json:"coverPageNumber"`
	DocumentMetadata   map[string]any `json:"documentMetadata"`
	DummyDocument      bool           `json:"dummyDocument"`
	ExtraMetadata      map[string]any `json:"extraMetadata"`
	FileType           string         `json:"fileType"`
	FontName           string         `json:"fontName"`
	FormatVersion      int            `json:"formatVersion"`
	LineHeight         int            `json:"lineHeight"`
	Orientation        string         `json:"orientation"`
	OriginalPageCount  int            `json:"originalPageCount"`
	PageCount          int            `json:"pageCount"`
	PageTags           []any          `json:"pageTags"`
	Pages              []string       `json:"pages"`
	RedirectionPageMap []int          `json:"redirectionPageMap"`
	SizeInBytes        string         `json:"sizeInBytes"`
	Tags               []Tag          `json:"tags"`
	TextAlignment      string         `json:"textAlignment"`
	TextScale          int            `json:"textScale"`
	ZoomMode           string         `json:"zoomMode"`
}

// The covers a document's .content can name.
const (
	// CoverLastOpened shows the page last read, the tablet's default.
	CoverLastOpened = -1
	// CoverFirstPage shows the first page.
	CoverFirstPage = 0
)

// NewPDFContent is the .content of a new PDF of len(pages) pages; pages are
// fresh lowercase UUIDs, one per page.
func NewPDFContent(pages []string, size int64, tags []Tag) []byte {
	return NewPDFContentWithCover(pages, size, tags, CoverLastOpened)
}

// NewPDFContentWithCover is NewPDFContent with the cover page named:
// CoverFirstPage or CoverLastOpened.
func NewPDFContentWithCover(pages []string, size int64, tags []Tag, cover int) []byte {
	redirect := make([]int, len(pages))
	for i := range redirect {
		redirect[i] = i
	}
	return newContent("pdf", pages, len(pages), redirect, size, tags, cover)
}

// NewEPUBContent is the .content of a new EPUB: the tablet paginates it, so
// it starts with no pages.
func NewEPUBContent(size int64, tags []Tag) []byte {
	return NewEPUBContentWithCover(size, tags, CoverLastOpened)
}

// NewEPUBContentWithCover is NewEPUBContent with the cover page named.
func NewEPUBContentWithCover(size int64, tags []Tag, cover int) []byte {
	return newContent("epub", []string{}, -1, []int{}, size, tags, cover)
}

func newContent(fileType string, pages []string, original int, redirect []int, size int64, tags []Tag, cover int) []byte {
	if tags == nil {
		tags = []Tag{}
	}
	c := content{
		CoverPageNumber:    cover,
		DocumentMetadata:   map[string]any{},
		ExtraMetadata:      map[string]any{},
		FileType:           fileType,
		FormatVersion:      1,
		LineHeight:         -1,
		Orientation:        "portrait",
		OriginalPageCount:  original,
		PageCount:          len(pages),
		PageTags:           []any{},
		Pages:              pages,
		RedirectionPageMap: redirect,
		SizeInBytes:        strconv.FormatInt(size, 10),
		Tags:               tags,
		TextAlignment:      "justify",
		TextScale:          1,
		ZoomMode:           "bestFit",
	}
	out, err := json.Marshal(c, json.Deterministic(true))
	if err != nil {
		panic("domain: content does not encode: " + err.Error())
	}
	return out
}

// ContentTags reads the tag names of a .content: empty names and repeats
// are dropped. A .content that does not decode has no tags.
func ContentTags(data []byte) []string {
	var c struct {
		Tags []struct {
			Name string `json:"name"`
		} `json:"tags"`
	}
	if json.Unmarshal(data, &c, json.RejectUnknownMembers(false)) != nil {
		return nil
	}
	var names []string
	seen := map[string]bool{}
	for _, t := range c.Tags {
		if t.Name == "" || seen[t.Name] {
			continue
		}
		seen[t.Name] = true
		names = append(names, t.Name)
	}
	return names
}

// ContentTagList reads the tags of a .content with their timestamps: empty
// names and repeats are dropped, the first of a name kept. A .content that
// does not decode has none.
func ContentTagList(data []byte) []Tag {
	var c struct {
		Tags []Tag `json:"tags"`
	}
	if json.Unmarshal(data, &c) != nil {
		return nil
	}
	var tags []Tag
	seen := map[string]bool{}
	for _, t := range c.Tags {
		if t.Name == "" || seen[t.Name] {
			continue
		}
		seen[t.Name] = true
		tags = append(tags, t)
	}
	return tags
}

// MergeTags keeps every tag of kept, with its timestamp, and adds the names
// it lacks, stamped stamp.
func MergeTags(kept []Tag, names []string, stamp int64) []Tag {
	out := make([]Tag, 0, len(kept)+len(names))
	seen := map[string]bool{}
	for _, t := range kept {
		if t.Name != "" && !seen[t.Name] {
			seen[t.Name] = true
			out = append(out, t)
		}
	}
	for _, n := range names {
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, Tag{Name: n, Timestamp: stamp})
		}
	}
	return out
}

// Pagedata is the .pagedata of a document of n pages: one "Blank" line per
// page.
func Pagedata(n int) []byte {
	return []byte(strings.Repeat("Blank\n", n))
}

// contentLayout reports the .content fields that describe the original's
// pages: a replaced original brings its own, and an old value would describe
// pages that are gone — an old formatVersion 2 cPages beside a new
// formatVersion 1 pages makes a document the tablet lists and cannot open.
func contentLayout(key string) bool {
	switch key {
	case "cPages", "pages", "redirectionPageMap", "pageCount", "originalPageCount", "sizeInBytes",
		"formatVersion", "fileType", "pageTags", "dummyDocument", "tags", "coverPageNumber":
		return true
	default:
		return false
	}
}

// contentReading reports the reading settings of a document: kept across a
// replacement of the same kind of file, since they are the reader's choice.
func contentReading(key string) bool {
	switch key {
	case "fontName", "lineHeight", "margins", "textAlignment", "textScale", "zoomMode", "orientation",
		"customZoomCenterX", "customZoomCenterY", "customZoomOrientation", "customZoomPageHeight",
		"customZoomPageWidth", "customZoomScale":
		return true
	default:
		return false
	}
}

// ReplacedContent is the .content of a document whose original was replaced:
// fresh, the new original's layout, tags and cover, with what the old one
// said that the new one has no word for. The old reading settings are kept
// when the kind of file is the same; every unknown field is kept. An old
// .content that does not decode is fresh.
func ReplacedContent(old, fresh []byte) []byte {
	var prev, next map[string]jsontext.Value
	if json.Unmarshal(old, &prev) != nil || json.Unmarshal(fresh, &next) != nil || prev == nil || next == nil {
		return fresh
	}
	sameKind := string(prev["fileType"]) == string(next["fileType"])
	for key, value := range prev {
		_, known := next[key]
		switch {
		case contentLayout(key):
		case contentReading(key):
			if sameKind {
				next[key] = value
			}
		case !known:
			next[key] = value
		}
	}
	out, err := json.Marshal(next, json.Deterministic(true))
	if err != nil {
		return fresh
	}
	return out
}

// ContentWithTags is content with its tags set to tags, and every other
// field as it was, unknown ones included. A .content that does not decode
// as an object is an error: there is nothing to keep it beside.
func ContentWithTags(content []byte, tags []Tag) ([]byte, error) {
	var top map[string]jsontext.Value
	if err := json.Unmarshal(content, &top); err != nil || top == nil {
		return nil, MalformedFile("content", "not an object")
	}
	if tags == nil {
		tags = []Tag{}
	}
	data, err := json.Marshal(tags)
	if err != nil {
		return nil, err
	}
	top["tags"] = data
	return json.Marshal(top, json.Deterministic(true))
}

// WithoutTags is tags without the names drop, in order.
func WithoutTags(tags []Tag, drop []string) []Tag {
	out := make([]Tag, 0, len(tags))
	for _, t := range tags {
		if !slices.Contains(drop, t.Name) {
			out = append(out, t)
		}
	}
	return out
}

// TagNames are the names of tags, in order.
func TagNames(tags []Tag) []string {
	out := make([]string, len(tags))
	for i, t := range tags {
		out[i] = t.Name
	}
	return out
}
