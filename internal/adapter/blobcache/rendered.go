// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package blobcache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/altessa-s/rmctl/internal/port"
)

var _ port.Previews = (*Cache)(nil)

// ErrPreviewName means a drawing's name could not name a file safely.
var ErrPreviewName = errors.New("blobcache: a drawing is named by a plain file name")

// Preview implements port.Previews in the drawings: the directory key holds
// the drawing, the one file name. A drawing kept counts as a use. One not
// kept is drawn into a staging directory, renamed into place once whole,
// and counted against the budget, which evicts the least recently used
// entries but never the drawing just made, even when it alone takes more:
// it is about to be shown. Unlike the reads and writes of a command's
// cache, a drawing reports what fails: it is what the command asked for.
func (c *Cache) Preview(ctx context.Context, key, name string, draw func(w io.Writer) error) (string, bool, error) {
	if !validHash(key) {
		return "", false, fmt.Errorf("blobcache: %q is not a sha256 hash", key)
	}
	if name == "" || name != filepath.Base(name) || strings.HasPrefix(name, ".") || strings.ContainsAny(name, `/\`) {
		return "", false, fmt.Errorf("%w: %q", ErrPreviewName, name)
	}
	dir := filepath.Join(c.dir, SpaceRendered)
	entry := filepath.Join(dir, key)
	path := filepath.Join(entry, name)
	if kept(path) {
		now := c.clock.Now()
		_ = os.Chtimes(entry, now, now)
		return path, false, nil
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return "", false, err
	}
	stage, err := os.MkdirTemp(dir, "."+key+".*.part")
	if err != nil {
		return "", false, err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	f, err := os.OpenFile(filepath.Join(stage, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return "", false, err
	}
	counter := &counting{w: f}
	if err = errors.Join(draw(counter), f.Close()); err != nil {
		return "", false, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// A drawing kept under another name, or left half there, goes first.
	_ = os.RemoveAll(entry)
	if err = os.Rename(stage, entry); err != nil {
		if kept(path) {
			// Another process kept the same drawing in between.
			return path, true, nil
		}
		return "", false, err
	}
	now := c.clock.Now()
	_ = os.Chtimes(entry, now, now)
	c.account(counter.n, entry)
	return path, true, nil
}

// kept reports whether path is a regular file.
func kept(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}
