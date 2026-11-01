// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"maps"
	"strconv"
	"time"
)

// The .metadata types the library knows.
const (
	TypeCollection = "CollectionType"
	TypeDocument   = "DocumentType"
	TypeTemplate   = "TemplateType"
)

// Metadata is a .metadata file. Edits work on the raw object, so a field
// this tool does not know survives a rewrite unchanged.
type Metadata struct {
	raw map[string]jsontext.Value
}

// ParseMetadata decodes a .metadata file. visibleName and type are required.
func ParseMetadata(data []byte) (Metadata, error) {
	var raw map[string]jsontext.Value
	if err := json.Unmarshal(data, &raw); err != nil {
		return Metadata{}, MalformedFile("metadata", err.Error())
	}
	if raw == nil {
		return Metadata{}, MalformedFile("metadata", "not an object")
	}
	m := Metadata{raw: raw}
	if _, ok := m.str("visibleName"); !ok {
		return Metadata{}, MalformedFile("metadata", "visibleName is missing")
	}
	if _, ok := m.str("type"); !ok {
		return Metadata{}, MalformedFile("metadata", "type is missing")
	}
	return m, nil
}

// NewFolderMetadata is the .metadata of a new folder.
func NewFolderMetadata(name string, parent ItemID, now time.Time) Metadata {
	m := Metadata{raw: map[string]jsontext.Value{}}
	m.set("deleted", false)
	m.set("lastModified", stamp(now))
	m.set("metadatamodified", false)
	m.set("modified", false)
	m.set("parent", string(parent))
	m.set("pinned", false)
	m.set("synced", true)
	m.set("type", TypeCollection)
	m.set("version", 1)
	m.set("visibleName", name)
	return m
}

// NewDocumentMetadata is the .metadata of a new uploaded document.
func NewDocumentMetadata(name string, parent ItemID, now time.Time) Metadata {
	m := NewFolderMetadata(name, parent, now)
	m.set("type", TypeDocument)
	m.set("createdTime", stamp(now))
	m.set("lastOpened", "")
	m.set("lastOpenedPage", -1)
	m.set("new", true)
	m.set("source", "")
	return m
}

// Bytes encodes the metadata with sorted keys: the bytes are content
// addressed, so the same metadata must always encode the same way.
func (m Metadata) Bytes() []byte {
	out, err := json.Marshal(m.raw, json.Deterministic(true))
	if err != nil {
		// Every value was decoded or encoded by this package.
		panic("domain: metadata does not encode: " + err.Error())
	}
	return out
}

// VisibleName is the name the tablet shows.
func (m Metadata) VisibleName() string { s, _ := m.str("visibleName"); return s }

// Type is CollectionType, DocumentType or TemplateType.
func (m Metadata) Type() string { s, _ := m.str("type"); return s }

// ParentID is the parent folder, or RootID for the top level and the trash.
func (m Metadata) ParentID() ItemID {
	p, _ := m.str("parent")
	if ItemID(p) == TrashID {
		return RootID
	}
	return ItemID(p)
}

// Trashed reports parent "trash" or deleted true.
func (m Metadata) Trashed() bool {
	p, _ := m.str("parent")
	return ItemID(p) == TrashID || m.boolean("deleted")
}

// Pinned is the favorite flag.
func (m Metadata) Pinned() bool { return m.boolean("pinned") }

// LastModified is lastModified, or the zero time when it is absent or not a
// millisecond stamp.
func (m Metadata) LastModified() time.Time { return m.time("lastModified") }

// Created is createdTime, or the zero time.
func (m Metadata) Created() time.Time { return m.time("createdTime") }

// Renamed is the metadata with a new visible name.
func (m Metadata) Renamed(name string, now time.Time) Metadata {
	out := m.clone()
	out.set("visibleName", name)
	return out.edited(now)
}

// Moved is the metadata under a new parent.
func (m Metadata) Moved(parent ItemID, now time.Time) Metadata {
	out := m.clone()
	out.set("parent", string(parent))
	return out.edited(now)
}

// MovedToTrash is the metadata in the trash. deleted is left as it is: the
// tablet itself only sets the parent.
func (m Metadata) MovedToTrash(now time.Time) Metadata {
	out := m.clone()
	out.set("parent", string(TrashID))
	return out.edited(now)
}

// Restored is the metadata out of the trash, under parent.
func (m Metadata) Restored(parent ItemID, now time.Time) Metadata {
	out := m.clone()
	out.set("parent", string(parent))
	out.set("deleted", false)
	return out.edited(now)
}

// ImportedUnder is the metadata of a document an import files under parent.
// It is not an edit — version and lastModified stay as the archive holds
// them — but a document taken out of an archive is meant to be seen, so a
// deleted flag is cleared.
func (m Metadata) ImportedUnder(parent ItemID) Metadata {
	out := m.clone()
	out.set("parent", string(parent))
	if out.boolean("deleted") {
		out.set("deleted", false)
	}
	return out
}

// WithPinned is the metadata with the favorite flag set to on. It is the
// same metadata, unedited, when the flag already says so.
func (m Metadata) WithPinned(on bool, now time.Time) Metadata {
	if _, ok := m.raw["pinned"]; ok && m.Pinned() == on {
		return m
	}
	out := m.clone()
	out.set("pinned", on)
	return out.edited(now)
}

// Rewritten is the metadata of a document whose content was replaced with
// one of pages pages: version up by one when it is there, lastModified now,
// and an opened page past the new end brought back to the last page. The
// name, the parent and every unknown field stay; metadatamodified is not set.
func (m Metadata) Rewritten(pages int, now time.Time) Metadata {
	out := m.clone()
	if v, ok := out.integer("version"); ok {
		out.set("version", v+1)
	}
	if p, ok := out.integer("lastOpenedPage"); ok && p > int64(pages)-1 {
		out.set("lastOpenedPage", int64(pages)-1)
	}
	out.set("lastModified", stamp(now))
	return out
}

// Edited is the metadata marked changed, the way the tablet marks an edit of
// a document's pages or files: see edited.
func (m Metadata) Edited(now time.Time) Metadata { return m.clone().edited(now) }

// edited marks a change the way the tablet does: version up by one and
// metadatamodified on, each only when the field is already there (both are
// optional, and an edit never adds them), and lastModified now.
func (m Metadata) edited(now time.Time) Metadata {
	if v, ok := m.integer("version"); ok {
		m.set("version", v+1)
	}
	if _, ok := m.raw["metadatamodified"]; ok {
		m.set("metadatamodified", true)
	}
	m.set("lastModified", stamp(now))
	return m
}

func (m Metadata) clone() Metadata { return Metadata{raw: maps.Clone(m.raw)} }

func (m Metadata) set(key string, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic("domain: metadata value does not encode: " + err.Error())
	}
	m.raw[key] = encoded
}

func (m Metadata) str(key string) (string, bool) {
	v, ok := m.raw[key]
	if !ok || v.Kind() != '"' {
		return "", false
	}
	var s string
	if json.Unmarshal(v, &s) != nil {
		return "", false
	}
	return s, true
}

func (m Metadata) boolean(key string) bool {
	v, ok := m.raw[key]
	return ok && v.Kind() == 't'
}

func (m Metadata) integer(key string) (int64, bool) {
	v, ok := m.raw[key]
	if !ok || v.Kind() != '0' {
		return 0, false
	}
	n, err := strconv.ParseInt(string(v), 10, 64)
	return n, err == nil
}

func (m Metadata) time(key string) time.Time {
	s, ok := m.str(key)
	if !ok {
		return time.Time{}
	}
	ms, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

// stamp is a millisecond epoch as a decimal string, the way .metadata keeps
// its times.
func stamp(t time.Time) string { return strconv.FormatInt(t.UnixMilli(), 10) }
