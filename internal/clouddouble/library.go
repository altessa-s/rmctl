// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package clouddouble

import (
	"encoding/json/v2"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmcloud-go"
)

// Kind is what a seeded item is.
type Kind int

// The kinds a seeded item can be.
const (
	Folder Kind = iota + 1
	PDF
	EPUB
	Notebook
	Template
)

// Item is one item of a seeded library, laid out the way the tablet writes
// it: <id>.metadata, <id>.content, the original, <id>.pagedata and one
// <id>/<page>.rm per page.
type Item struct {
	ID     string
	Name   string
	Parent string
	Kind   Kind
	// Body is the original's bytes for a PDF or an EPUB; empty uses a
	// placeholder.
	Body []byte
	// Pages is the number of .rm pages; on a PDF they make it annotated.
	// PageBodies, when set, are the pages' bytes, and their number wins.
	Pages int
	// PageBodies are real .rm pages: page p is <id>/page-<p>.rm.
	PageBodies [][]byte
	// Trashed puts the item in the trash the way the tablet does (parent
	// "trash"); Deleted sets the deleted flag instead.
	Trashed bool
	Deleted bool
	Pinned  bool
	Tags    []string
	// Modified is lastModified; the zero time is 2026-01-01.
	Modified time.Time
	// Minimal leaves out the optional fields (version, deleted, synced,
	// metadatamodified).
	Minimal bool
	// RecordType is the root entry's type; empty is 80000000, as the tablet
	// writes it.
	RecordType string
	// Raw replaces the generated .metadata bytes when set.
	Raw []byte
	// ByNodeHash gives the root record the node hash of the index rather
	// than the hash of the index file's bytes: the other way a record may
	// address its index.
	ByNodeHash bool
	// IndexV3 writes the item's index in schema 3, with no count line.
	IndexV3 bool
}

// tagStamp is the millisecond timestamp of every seeded tag.
const tagStamp = 1735689600000

// Seed adds items to the library in one root write, in order. Seeding the
// same ID twice gives a root that repeats it, as a broken account does.
func (c *Cloud) Seed(tb testing.TB, items ...Item) {
	tb.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	root, err := rmcloud.Parse(string(c.blobs[c.rootHash]))
	if err != nil {
		tb.Fatalf("clouddouble: the root does not parse: %v", err)
	}
	entries := root.Entries
	for _, it := range items {
		it.Pages = max(it.Pages, len(it.PageBodies))
		entries = append(entries, c.seedItem(tb, it))
	}
	c.rootHash = c.put([]byte(rmcloud.SerializeRoot(entries)))
	c.generation++
}

func (c *Cloud) seedItem(tb testing.TB, it Item) rmcloud.Entry {
	tb.Helper()
	var files []rmcloud.Entry
	add := func(name string, body []byte) {
		files = append(files, rmcloud.Entry{Hash: c.put(body), Type: "0", DocumentID: name, Size: int64(len(body))})
	}
	meta := it.Raw
	if meta == nil {
		meta = metadata(tb, it)
	}
	add(it.ID+".metadata", meta)
	add(it.ID+".content", content(tb, it))
	switch it.Kind {
	case PDF:
		add(it.ID+".pdf", orDefault(it.Body, "%PDF-1.4\n% seeded "+it.ID+"\n"))
	case EPUB:
		add(it.ID+".epub", orDefault(it.Body, "PK\x03\x04 seeded "+it.ID))
	default:
		// A folder, a notebook and a template have no original.
	}
	if it.Kind != Folder {
		for p := range it.Pages {
			body := []byte("reMarkable .lines file, version=6 " + strconv.Itoa(p))
			if p < len(it.PageBodies) {
				body = it.PageBodies[p]
			}
			add(fmt.Sprintf("%s/page-%d.rm", it.ID, p), body)
		}
		if it.Pages > 0 {
			add(it.ID+".pagedata", []byte(strings.Repeat("Blank\n", it.Pages)))
		}
	}
	index := []byte(rmcloud.SerializeDocument(files, it.ID))
	if it.IndexV3 {
		var b strings.Builder
		b.WriteString(string(rmcloud.SchemaV3) + "\n")
		for _, f := range files {
			b.WriteString(f.Line() + "\n")
		}
		index = []byte(b.String())
	}
	var size int64
	for _, f := range files {
		size += f.Size
	}
	hash := c.put(index)
	if it.ByNodeHash {
		node, err := rmcloud.NodeHash(files)
		if err != nil {
			tb.Fatalf("clouddouble: node hash of %s: %v", it.ID, err)
		}
		// The index is served under its node hash, as the service does.
		delete(c.blobs, hash)
		c.blobs[node] = index
		hash = node
	}
	recordType := it.RecordType
	if recordType == "" {
		recordType = "80000000"
	}
	return rmcloud.Entry{Hash: hash, Type: recordType, DocumentID: it.ID, Subfiles: len(files), Size: size}
}

func metadata(tb testing.TB, it Item) []byte {
	tb.Helper()
	modified := it.Modified
	if modified.IsZero() {
		modified = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	parent := it.Parent
	if it.Trashed {
		parent = "trash"
	}
	m := map[string]any{
		"visibleName":  it.Name,
		"parent":       parent,
		"pinned":       it.Pinned,
		"lastModified": strconv.FormatInt(modified.UnixMilli(), 10),
		"type":         "DocumentType",
	}
	switch it.Kind {
	case Folder:
		m["type"] = "CollectionType"
	case Template:
		m["type"] = "TemplateType"
	default:
		m["createdTime"] = strconv.FormatInt(modified.UnixMilli(), 10)
		m["lastOpenedPage"] = 0
	}
	if !it.Minimal {
		m["version"] = 1
		m["deleted"] = it.Deleted
		m["synced"] = true
		m["metadatamodified"] = false
	}
	// Deterministic: an item seeded again unchanged has the same bytes, as
	// the tablet writes them.
	out, err := json.Marshal(m, json.Deterministic(true))
	if err != nil {
		tb.Fatal(err)
	}
	return out
}

func content(tb testing.TB, it Item) []byte {
	tb.Helper()
	if it.Kind == Folder {
		return []byte("{}")
	}
	tags := make([]map[string]any, 0, len(it.Tags))
	for _, t := range it.Tags {
		tags = append(tags, map[string]any{"name": t, "timestamp": tagStamp})
	}
	fileType := map[Kind]string{PDF: "pdf", EPUB: "epub", Notebook: "notebook", Template: "notebook"}[it.Kind]
	pages := make([]string, 0, it.Pages)
	for p := range it.Pages {
		pages = append(pages, fmt.Sprintf("page-%d", p))
	}
	out, err := json.Marshal(map[string]any{
		"fileType":      fileType,
		"formatVersion": 1,
		"pageCount":     it.Pages,
		"pages":         pages,
		"tags":          tags,
	}, json.Deterministic(true))
	if err != nil {
		tb.Fatal(err)
	}
	return out
}

func orDefault(body []byte, placeholder string) []byte {
	if len(body) > 0 {
		return body
	}
	return []byte(placeholder)
}

// Records are the entries of the current root index.
func (c *Cloud) Records(tb testing.TB) []rmcloud.Entry {
	tb.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	root, err := rmcloud.Parse(string(c.blobs[c.rootHash]))
	if err != nil {
		tb.Fatalf("clouddouble: the root does not parse: %v", err)
	}
	return root.Entries
}

// Files are the entries of the index of the item id, or nil when the root
// does not hold it.
func (c *Cloud) Files(tb testing.TB, id string) []rmcloud.Entry {
	tb.Helper()
	for _, rec := range c.Records(tb) {
		if rec.DocumentID != id {
			continue
		}
		body, ok := c.Blob(rec.Hash)
		if !ok {
			tb.Fatalf("clouddouble: the index of %s is not stored", id)
		}
		index, err := rmcloud.Parse(string(body))
		if err != nil {
			tb.Fatalf("clouddouble: the index of %s does not parse: %v", id, err)
		}
		return index.Entries
	}
	return nil
}

// File returns the bytes of the file of item id whose record name is name.
func (c *Cloud) File(tb testing.TB, id, name string) ([]byte, bool) {
	tb.Helper()
	for _, f := range c.Files(tb, id) {
		if f.DocumentID == name {
			return c.Blob(f.Hash)
		}
	}
	return nil, false
}

// Metadata decodes the current .metadata of item id, or nil.
func (c *Cloud) Metadata(tb testing.TB, id string) map[string]any {
	tb.Helper()
	body, ok := c.File(tb, id, id+".metadata")
	if !ok {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		tb.Fatalf("clouddouble: the metadata of %s does not decode: %v", id, err)
	}
	return m
}

// Replace puts items in the place of the root entries with their
// identifiers, in one root write, as a change synced from the tablet does;
// an item the root does not hold is added.
func (c *Cloud) Replace(tb testing.TB, items ...Item) {
	tb.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	root, err := rmcloud.Parse(string(c.blobs[c.rootHash]))
	if err != nil {
		tb.Fatalf("clouddouble: the root does not parse: %v", err)
	}
	entries := root.Entries
	for _, it := range items {
		it.Pages = max(it.Pages, len(it.PageBodies))
		entry := c.seedItem(tb, it)
		found := false
		for i := range entries {
			if entries[i].DocumentID == it.ID {
				entries[i], found = entry, true
			}
		}
		if !found {
			entries = append(entries, entry)
		}
	}
	c.rootHash = c.put([]byte(rmcloud.SerializeRoot(entries)))
	c.generation++
}

// Remove takes the items ids out of the root, in one root write, as a
// deletion on the tablet does.
func (c *Cloud) Remove(tb testing.TB, ids ...string) {
	tb.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	root, err := rmcloud.Parse(string(c.blobs[c.rootHash]))
	if err != nil {
		tb.Fatalf("clouddouble: the root does not parse: %v", err)
	}
	entries := root.Entries[:0:0]
	for _, e := range root.Entries {
		if !slices.Contains(ids, e.DocumentID) {
			entries = append(entries, e)
		}
	}
	c.rootHash = c.put([]byte(rmcloud.SerializeRoot(entries)))
	c.generation++
}
