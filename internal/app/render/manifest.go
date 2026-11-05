// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"time"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// ManifestName is the file a folder drawing keeps in its directory: what
// each PDF was drawn from, and with which options.
const ManifestName = ".rmctl-render.json"

// manifestVersion is the layout of the manifest; another is no manifest.
const manifestVersion = 1

// maxManifest bounds the manifest read: far above any library.
const maxManifest = 64 << 20

// manifest is what a folder drawing recorded of each PDF it wrote, by its
// path relative to the directory, with slashes.
type manifest struct {
	Version   int                      `json:"v"`
	Documents map[string]manifestEntry `json:"documents"`
}

// manifestEntry is one PDF: the document it was drawn from, the
// document's modification time then, and the options it was drawn with.
type manifestEntry struct {
	ID       domain.ItemID `json:"id"`
	Modified time.Time     `json:"modified"`
	Options  string        `json:"options"`
}

// fingerprint names the options a folder's PDF is drawn with, which an
// incremental drawing must find the same to keep the PDF.
func fingerprint(opts port.RenderOptions) string {
	templates := "templates"
	if opts.NoTemplates {
		templates = "no-templates"
	}
	return fmt.Sprintf("%s/%s/%s", opts.Mode, opts.Paper, templates)
}

// entryOf is what the manifest records of a document drawn with opts.
func entryOf(it domain.Item, opts string) manifestEntry {
	return manifestEntry{ID: it.ID, Modified: it.Modified, Options: opts}
}

// matches reports whether e records a drawing of it with opts.
func (e manifestEntry) matches(it domain.Item, opts string) bool {
	return e.ID == it.ID && e.Modified.Equal(it.Modified) && e.Options == opts
}

// manifestKey is the manifest's name of the PDF at path under dir.
func manifestKey(dir, path string) string {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// readManifest is the manifest of dir: empty when there is none, or it
// cannot be read or decoded, so every document is drawn.
func readManifest(files port.LocalFiles, dir string) map[string]manifestEntry {
	path := filepath.Join(dir, ManifestName)
	if _, isDir, exists, err := files.Stat(path); err != nil || !exists || isDir {
		return map[string]manifestEntry{}
	}
	r, err := files.Open(path)
	if err != nil {
		return map[string]manifestEntry{}
	}
	defer func() { _ = r.Close() }()
	var buf bytes.Buffer
	if _, err = io.Copy(&buf, io.LimitReader(r, maxManifest)); err != nil {
		return map[string]manifestEntry{}
	}
	var m manifest
	if err = json.Unmarshal(buf.Bytes(), &m); err != nil || m.Version != manifestVersion || m.Documents == nil {
		return map[string]manifestEntry{}
	}
	return m.Documents
}

// writeManifest writes the manifest of dir whole, or leaves the one there.
// It is written even when ctx is done: it records PDFs already written.
func writeManifest(ctx context.Context, files port.LocalFiles, dir string, docs map[string]manifestEntry) error {
	path := filepath.Join(dir, ManifestName)
	err := files.Produce(context.WithoutCancel(ctx), path, func(w io.Writer) error {
		return json.MarshalWrite(w, manifest{Version: manifestVersion, Documents: docs}, json.Deterministic(true))
	})
	if err != nil {
		return fmt.Errorf("record the drawing in %s: %w", path, err)
	}
	return nil
}

// pruned is docs without the entries of plan that do not record its
// document as drawn now with opts, and whether it dropped any. Written
// before any PDF is, it keeps a drawing that stops halfway honest: a PDF
// redrawn under other options is never recorded under the old ones.
func pruned(docs map[string]manifestEntry, dir string, plan []Planned, opts string) (map[string]manifestEntry, bool) {
	out := maps.Clone(docs)
	dropped := false
	for _, p := range plan {
		key := manifestKey(dir, p.Path)
		if e, ok := out[key]; ok && !e.matches(p.Document, opts) {
			delete(out, key)
			dropped = true
		}
	}
	return out, dropped
}
