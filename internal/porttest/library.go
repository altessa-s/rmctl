// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest

import (
	"encoding/json/v2"
	"strconv"
	"time"

	"github.com/altessa-s/rmctl/internal/domain"
)

// Epoch is the time the builders stamp their metadata with.
var Epoch = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) //nolint:gochecknoglobals // a fixed test instant

// Doc describes one item a test library holds.
type Doc struct {
	ID     domain.ItemID
	Name   string
	Parent domain.ItemID
	// Folder makes a folder; otherwise Ext ("pdf", "epub" or "" for a
	// notebook) names the original.
	Folder bool
	Ext    string
	// Body is the original's bytes; nil is a short placeholder.
	Body []byte
	// Trashed puts the item in the trash; Deleted sets deleted instead.
	Trashed bool
	Deleted bool
	Pinned  bool
	Tags    []string
	// RM adds a page of strokes, which makes a PDF an annotated one.
	RM bool
	// Template marks the metadata TemplateType.
	Template bool
	// NoMetadata leaves the .metadata out of the index.
	NoMetadata bool
	// BadMetadata stores a .metadata that does not decode.
	BadMetadata bool
	// Extra adds files to the index.
	Extra []domain.File
	// Pages are the bodies of the document's .rm pages, in order: page i is
	// <id>/p<i+1>.rm, and the .content lists them. A notebook gets a
	// formatVersion 2 .content, a PDF a formatVersion 1 one. PageBytes
	// builds a body the Pages double reads.
	Pages [][]byte
	// Modified is the lastModified of the metadata; zero is Epoch.
	Modified time.Time
}

// PageID is the identifier the builders give the i-th page (from 0).
func PageID(i int) string { return "p" + strconv.Itoa(i+1) }

// Add stores the documents and makes them the root, in order.
func (c *Cloud) Add(docs ...Doc) []domain.Record {
	records := c.Records()
	for _, d := range docs {
		records = append(records, c.Record(d))
	}
	c.SetRoot(records)
	return records
}

// Record stores one document's files and index and returns its record,
// without touching the root.
func (c *Cloud) Record(d Doc) domain.Record {
	id := string(d.ID)
	var files []domain.File
	if !d.NoMetadata {
		files = append(files, c.Put(id+".metadata", metadataOf(d)))
	}
	if d.Folder {
		files = append(files, c.Put(id+".content", domain.FolderContent()))
	} else {
		files = append(files, c.Put(id+".content", contentOf(d)))
		if d.Ext != "" {
			body := d.Body
			if body == nil {
				body = []byte("%" + d.Ext + " " + id)
			}
			files = append(files, c.Put(id+"."+d.Ext, body))
		}
		if d.RM {
			files = append(files, c.Put(id+"/page-1.rm", []byte("reMarkable lines "+id)))
		}
		for i, body := range d.Pages {
			files = append(files, c.Put(domain.PageFileName(d.ID, PageID(i)), body))
		}
	}
	files = append(files, d.Extra...)
	return c.storeIndex(d.ID, files)
}

func metadataOf(d Doc) []byte {
	if d.BadMetadata {
		return []byte("{not json")
	}
	var meta domain.Metadata
	modified := Epoch
	if !d.Modified.IsZero() {
		modified = d.Modified
	}
	if d.Folder {
		meta = domain.NewFolderMetadata(d.Name, d.Parent, modified)
	} else {
		meta = domain.NewDocumentMetadata(d.Name, d.Parent, modified)
	}
	if d.Trashed {
		meta = meta.MovedToTrash(Epoch)
	}
	if d.Pinned {
		meta = meta.WithPinned(true, Epoch)
	}
	if !d.Deleted && !d.Template {
		return meta.Bytes()
	}
	var raw map[string]any
	if err := json.Unmarshal(meta.Bytes(), &raw); err != nil {
		panic(err)
	}
	if d.Deleted {
		raw["deleted"] = true
	}
	if d.Template {
		raw["type"] = domain.TypeTemplate
	}
	out, err := json.Marshal(raw, json.Deterministic(true))
	if err != nil {
		panic(err)
	}
	return out
}

func contentOf(d Doc) []byte {
	tags := make([]domain.Tag, len(d.Tags))
	for i, t := range d.Tags {
		tags[i] = domain.Tag{Name: t, Timestamp: Epoch.UnixMilli()}
	}
	if d.Ext == "epub" {
		return domain.NewEPUBContent(int64(len(d.Body)), tags)
	}
	if len(d.Pages) > 0 {
		ids := make([]string, len(d.Pages))
		for i := range ids {
			ids[i] = PageID(i)
		}
		if d.Ext == "" {
			return domain.NewNotebookContent(ids, "00000000-0000-4000-8000-000000000000", tags)
		}
		return domain.NewPDFContent(ids, int64(len(d.Body)), tags)
	}
	return domain.NewPDFContent([]string{"page-1"}, int64(len(d.Body)), tags)
}
