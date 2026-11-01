// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package pdf

import (
	"context"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"github.com/altessa-s/go-atlas-core/io/files"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// Inspector counts the pages of a PDF with pdfcpu.
type Inspector struct{}

var _ port.PDFInspector = Inspector{}

// New returns the inspector.
func New() Inspector { return Inspector{} }

// Pages counts the pages of the PDF at path. A PDF that needs a password to
// open, one that does not parse, and one with no pages are each
// domain.ErrUnsupportedFile: the tablet could not show them either.
func (Inspector) Pages(path string) (int, error) {
	f, err := files.OpenRegular(path)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", domain.ErrUnsupportedFile, err)
	}
	defer func() { _ = f.Close() }()
	// The stateless configuration never touches the file system: pdfcpu's
	// default one creates a configuration directory under the user's home.
	n, err := api.PageCount(context.Background(), f, model.NewStatelessConfiguration())
	if err != nil {
		return 0, fmt.Errorf("%w: the PDF cannot be opened (it may be locked): %v", domain.ErrUnsupportedFile, err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("%w: the PDF has no pages", domain.ErrUnsupportedFile)
	}
	return n, nil
}
