// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/altessa-s/rmctl/internal/app/outdir"
	"github.com/altessa-s/rmctl/internal/port"
)

// ManifestName is the file a folder drawing keeps in its directory: what
// each PDF was drawn from, and with which options.
const ManifestName = ".rmctl-render.json"

// fingerprint names the options a folder's PDF is drawn with, which an
// incremental drawing must find the same to keep the PDF. The default
// grain adds nothing, so the records written before there was a choice
// still hold. What becomes of a page that does not parse is left out: a PDF
// holding such a page is never recorded (see Service.record).
func fingerprint(opts port.RenderOptions) string {
	templates := "templates"
	if opts.NoTemplates {
		templates = "no-templates"
	}
	f := fmt.Sprintf("%s/%s/%s", opts.Mode, opts.Paper, templates)
	if opts.Grain != "" && opts.Grain != port.PDFGrainMask {
		f += "/grain-" + string(opts.Grain)
	}
	return f
}

// writeManifest writes the manifest of dir whole, or leaves the one there.
// It is written even when ctx is done: it records PDFs already written.
func writeManifest(ctx context.Context, files port.LocalFiles, dir string, docs map[string]outdir.Entry) error {
	if err := outdir.Write(ctx, files, dir, ManifestName, docs); err != nil {
		return fmt.Errorf("record the drawing in %s: %w", filepath.Join(dir, ManifestName), err)
	}
	return nil
}
