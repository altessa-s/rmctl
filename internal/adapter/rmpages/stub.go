// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build !cgo

package rmpages

import (
	"log/slog"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// Pages answers every call with domain.ErrPagesUnsupported: this build has
// no C compiler behind it, and the page format library is C.
type Pages struct{}

var (
	_ port.PageReader = Pages{}
	_ port.PageEditor = Pages{}
	_ port.PageWriter = Pages{}
)

// New returns the page adapter of a build without cgo.
func New(*slog.Logger) Pages { return Pages{} }

// ReadPage implements port.PageReader.
func (Pages) ReadPage([]byte) (domain.Page, error) { return domain.Page{}, domain.ErrPagesUnsupported }

// Highlights implements port.PageEditor.
func (Pages) Highlights([]byte) ([]string, error) { return nil, domain.ErrPagesUnsupported }

// Erase implements port.PageEditor.
func (Pages) Erase([]byte, []string) ([]byte, error) { return nil, domain.ErrPagesUnsupported }

// WritePage implements port.PageWriter.
func (Pages) WritePage(domain.PageDraft) ([]byte, error) { return nil, domain.ErrPagesUnsupported }
