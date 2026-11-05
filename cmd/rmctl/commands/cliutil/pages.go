// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/altessa-s/rmctl/internal/domain"
)

// ErrNoPages is what a page command says when this build reads no pages.
var ErrNoPages = errors.New("this build reads no pages: rebuild with cgo and a C23 compiler")

// ErrNoArchives is what an archive command says when this build reads and
// writes no .rmdoc archives.
var ErrNoArchives = errors.New("this build reads and writes no .rmdoc archives: rebuild with cgo and a C23 compiler")

// ErrNoRender is what a render command says when this build draws nothing.
var ErrNoRender = errors.New("rendering is not available in this build: it is in the macOS, Linux and Windows builds of rmctl")

// PagesErr names err for the command at path: a build without the page
// format library, the container library or the renderer gets one sentence
// that says how to get one, anything else keeps its own words.
func PagesErr(path string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, domain.ErrPagesUnsupported) {
		return fmt.Errorf("%s: %w", path, ErrNoPages)
	}
	if errors.Is(err, domain.ErrArchivesUnsupported) {
		return fmt.Errorf("%s: %w", path, ErrNoArchives)
	}
	if errors.Is(err, domain.ErrRenderUnsupported) {
		return fmt.Errorf("%s: %w", path, ErrNoRender)
	}
	return fmt.Errorf("%s: %w", path, err)
}

// SplitPages reads the --page values: each one is a page number or a page
// identifier, and a comma separates several in one value.
func SplitPages(values []string) []string {
	var out []string
	for _, v := range values {
		for part := range strings.SplitSeq(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// LocalArchive reports an argument that names a local .rmdoc archive: it
// ends with .rmdoc and is a regular file here. Anything else is a path or an
// identifier in the cloud; "./" in front, or the identifier, settles a cloud
// document whose name ends with .rmdoc.
func LocalArchive(arg string) bool {
	if !strings.EqualFold(filepath.Ext(arg), ".rmdoc") {
		return false
	}
	fi, err := os.Stat(arg)
	return err == nil && fi.Mode().IsRegular()
}
