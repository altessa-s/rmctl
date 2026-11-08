// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// Viewer implements port.Viewing: a document drawn as [Service] draws it,
// into the previews, then shown with the opener. A drawing is kept under a
// key made of what it depends on: the document's version (its record hash
// in the library, which changes whenever any of its files does; a local
// archive's path, size and modification time), every option, and the
// version of rmctl, so a newer renderer draws it again.
type Viewer struct {
	svc      *Service
	previews port.Previews
	opener   port.Opener
	version  string
}

var _ port.Viewing = (*Viewer)(nil)

// NewViewer returns the open use case over svc. version is rmctl's, part
// of every key. A nil dependency panics.
func NewViewer(svc *Service, previews port.Previews, opener port.Opener, version string) *Viewer {
	switch {
	case svc == nil:
		panic("render: Service is required")
	case previews == nil:
		panic("render: Previews are required")
	case opener == nil:
		panic("render: Opener is required")
	}
	return &Viewer{svc: svc, previews: previews, opener: opener, version: version}
}

// Open implements port.Viewing. The document is downloaded only when its
// drawing is not kept.
func (v *Viewer) Open(ctx context.Context, query string, opts port.RenderOptions) (port.OpenReport, error) {
	opts, err := v.svc.ready(opts)
	if err != nil {
		return port.OpenReport{}, err
	}
	it, err := v.svc.document(ctx, query)
	if err != nil {
		return port.OpenReport{}, err
	}
	for _, f := range it.Files {
		if cerr := v.svc.writer.CheckName(f.Name); cerr != nil {
			return port.OpenReport{}, fmt.Errorf("%s: %w", it.Name, cerr)
		}
	}
	key := previewKey(opts, v.version, "library", string(it.ID), it.Hash)
	return v.show(ctx, key, it, opts, func() ([]byte, error) { return v.svc.download(ctx, it) })
}

// OpenFile implements port.Viewing.
func (v *Viewer) OpenFile(ctx context.Context, local string, opts port.RenderOptions) (port.OpenReport, error) {
	opts, err := v.svc.ready(opts)
	if err != nil {
		return port.OpenReport{}, err
	}
	it, err := v.svc.archived(local, opts.Document)
	if err != nil {
		return port.OpenReport{}, fmt.Errorf("%s: %w", local, err)
	}
	size, _, _, err := v.svc.files.Stat(local)
	if err != nil {
		return port.OpenReport{}, err
	}
	mtime, _, err := v.svc.files.ModTime(local)
	if err != nil {
		return port.OpenReport{}, err
	}
	abs, err := filepath.Abs(local)
	if err != nil {
		return port.OpenReport{}, err
	}
	key := previewKey(opts, v.version, "archive", abs, strconv.FormatInt(size, 10), mtime.UTC().Format(time.RFC3339Nano), string(it.ID))
	return v.show(ctx, key, it, opts, func() ([]byte, error) { return v.svc.readAll(local) })
}

// show draws it from the archive load reads into the previews, unless they
// keep its drawing under key, and opens the file.
func (v *Viewer) show(ctx context.Context, key string, it domain.Item, opts port.RenderOptions, load func() ([]byte, error)) (port.OpenReport, error) {
	var report port.RenderReport
	path, drawn, err := v.previews.Preview(ctx, key, FileName(it.Name, opts), func(w io.Writer) error {
		archive, lerr := load()
		if lerr != nil {
			return lerr
		}
		var rerr error
		if report, rerr = v.svc.renderer.Render(ctx, archive, it.ID, opts, w); rerr != nil {
			return fmt.Errorf("%s: %w", it.Name, rerr)
		}
		return nil
	})
	if err != nil {
		return port.OpenReport{}, err
	}
	if !drawn {
		report = port.RenderReport{}
	}
	report.Path = path
	out := port.OpenReport{RenderReport: report, Drawn: drawn}
	if err = v.opener.Open(ctx, path); err != nil {
		return out, err
	}
	return out, nil
}

// previewKey is the sha256, in hex, of the normalized options and of
// parts, which name the document and its version.
func previewKey(opts port.RenderOptions, parts ...string) string {
	all := append(slices.Clone(parts),
		string(opts.Format), string(opts.Mode), string(opts.Grain), string(opts.Paper), string(opts.Unreadable),
		strconv.Itoa(opts.Page), strconv.FormatFloat(opts.Scale, 'g', -1, 64), strconv.Itoa(opts.Quality),
		strconv.FormatBool(opts.NoTemplates), opts.Document)
	sum := sha256.Sum256([]byte(strings.Join(all, "\x00")))
	return hex.EncodeToString(sum[:])
}
