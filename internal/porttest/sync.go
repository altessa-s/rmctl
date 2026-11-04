// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"strconv"
	"sync"

	"github.com/altessa-s/rmctl/internal/port"
)

// ErrHashMismatch is what SyncCache.Fill returns for bytes that are not
// those of the hash.
var ErrHashMismatch = errors.New("porttest: the bytes do not match the hash")

// SyncCache is a port.SyncCache in memory, and the port.BlobCache of the
// same blobs, so a library reading through it fills what sync finds.
// Indexes holds the sizes IndexSize reports, by hash.
type SyncCache struct {
	mu      sync.Mutex
	blobs   map[string][]byte
	Indexes map[string]int64
	mark    *port.SyncMark
	// Max is the budget it reports, 0 for no limit, and Source what set it.
	Max    int64
	Source string
	// FailFill makes Fill fail.
	FailFill error
	// UnknownRemovals makes Removals report that the generation cannot be
	// told, as a damaged one on disk.
	UnknownRemovals bool
	// BeforeMark, when set, runs as MarkSync starts, before the mark is
	// kept: a test removes entries there, as another process would after
	// the sync checked them.
	BeforeMark func()
	fills      int
	removals   int
}

// NewSyncCache returns an empty cache with budget max.
func NewSyncCache(maxBytes int64) *SyncCache {
	return &SyncCache{blobs: map[string][]byte{}, Indexes: map[string]int64{}, Max: maxBytes}
}

var (
	_ port.SyncCache = (*SyncCache)(nil)
	_ port.BlobCache = (*SyncCache)(nil)
)

// Get implements port.BlobCache.
func (c *SyncCache) Get(hash string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.blobs[hash]
	return slices.Clone(b), ok
}

// Put implements port.BlobCache.
func (c *SyncCache) Put(hash string, data []byte) {
	if Hash(data) != hash {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.blobs[hash] = slices.Clone(data)
}

// Holds implements port.SyncCache.
func (c *SyncCache) Holds(hash string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.blobs[hash]
	return ok
}

// IndexSize implements port.SyncCache.
func (c *SyncCache) IndexSize(hash string) (int64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, ok := c.Indexes[hash]
	return n, ok
}

// Fill implements port.SyncCache.
func (c *SyncCache) Fill(hash string, write func(w io.Writer) error) error {
	if c.FailFill != nil {
		return c.FailFill
	}
	var buf bytes.Buffer
	if err := write(&buf); err != nil {
		return err
	}
	if Hash(buf.Bytes()) != hash {
		return ErrHashMismatch
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.blobs[hash] = buf.Bytes()
	c.fills++
	return nil
}

// Fills is how many blobs Fill kept.
func (c *SyncCache) Fills() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fills
}

// Drop forgets the blob of hash, as an eviction would, and with it the
// last complete sync.
func (c *SyncCache) Drop(hash string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.blobs, hash)
	c.removed()
}

// KeepIndex keeps an index of size under hash, as reading it through the
// cache would, unless one is kept there already.
func (c *SyncCache) KeepIndex(hash string, size int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.Indexes[hash]; !ok {
		c.Indexes[hash] = size
	}
}

// DropIndex forgets the index of hash, as an eviction would, and with it
// the last complete sync.
func (c *SyncCache) DropIndex(hash string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.Indexes, hash)
	c.removed()
}

// Usage implements port.SyncCache: the bytes of the blobs and the sizes of
// the indexes.
func (c *SyncCache) Usage() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	var n int64
	for _, b := range c.blobs {
		n += int64(len(b))
	}
	for _, size := range c.Indexes {
		n += size
	}
	return n
}

// Budget implements port.SyncCache.
func (c *SyncCache) Budget() int64 { return c.Max }

// BudgetSource implements port.SyncCache.
func (c *SyncCache) BudgetSource() string { return c.Source }

// LastSync implements port.SyncCache.
func (c *SyncCache) LastSync() (port.SyncMark, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mark == nil {
		return port.SyncMark{}, false
	}
	return *c.mark, true
}

// MarkSync implements port.SyncCache.
func (c *SyncCache) MarkSync(m port.SyncMark) {
	if c.BeforeMark != nil {
		c.BeforeMark()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mark = &m
}

// DropSync implements port.SyncCache.
func (c *SyncCache) DropSync() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mark = nil
}

// Removals implements port.SyncCache: how many entries Drop and DropIndex
// removed.
func (c *SyncCache) Removals() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.UnknownRemovals {
		return "", false
	}
	return strconv.Itoa(c.removals), true
}

// removed records a removal, as blobcache does: the generation changes and
// the last complete sync is forgotten. The caller holds mu.
func (c *SyncCache) removed() {
	c.removals++
	c.mark = nil
}
