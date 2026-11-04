// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package blobcache

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/altessa-s/rmctl/internal/port"
)

// dirMode and fileMode keep the cache to its owner.
const (
	dirMode  fs.FileMode = 0o700
	fileMode fs.FileMode = 0o600
)

// hashLen is the length of a sha256 hash in hex.
const hashLen = 64

// The spaces of the cache: each is a directory under the cache's root.
const (
	// SpaceBlobs holds downloaded files, named and checked by their sha256.
	SpaceBlobs = "blobs"
	// SpaceIndexes holds document and root indexes, named by the hash
	// that addresses them; the reader checks them.
	SpaceIndexes = "indexes"
	// SpaceState holds a few small named files, such as the profile's last
	// known root. They are never evicted.
	SpaceState = "state"
)

// Spaces are every space, in the order a report lists them.
func Spaces() []string { return []string{SpaceBlobs, SpaceIndexes, SpaceState} }

// evictable are the spaces the byte budget covers.
func evictable(space string) bool { return space == SpaceBlobs || space == SpaceIndexes }

// nameKey is a name of the state space, and a profile's directory: no
// separator, no dot, nothing a path could climb out with.
var nameKey = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`) //nolint:gochecknoglobals // a compiled pattern

// Cache is one profile's cache: a directory with a subdirectory per space,
// the blobs and indexes bounded together by one byte budget,
// evicting the least recently used. Writes go through a staging file and a
// rename, so another rmctl process reads an entry whole or not at all; no
// cache error ever fails a command.
type Cache struct {
	dir      string
	maxBytes int64
	log      *slog.Logger
	clock    port.Clock
	mu       sync.Mutex
	// total is what the evictable spaces take, as far as this process
	// knows: counted once, then added to by each write, and counted again
	// whenever it passes the budget. known says it was counted.
	total int64
	known bool
}

var _ port.BlobCache = (*Cache)(nil)

// New returns a cache under dir holding at most maxBytes of blobs and
// indexes. dir and clock are required.
func New(dir string, maxBytes int64, clock port.Clock, log *slog.Logger) *Cache {
	if dir == "" {
		panic("blobcache: a directory is required")
	}
	if clock == nil {
		panic("blobcache: a clock is required")
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Cache{dir: dir, maxBytes: maxBytes, log: log, clock: clock}
}

// Dir is the cache's root directory.
func (c *Cache) Dir() string { return c.dir }

// Budget is the byte budget of the blobs and indexes.
func (c *Cache) Budget() int64 { return c.maxBytes }

// Get implements port.BlobCache: the blob of hash, checked against it.
func (c *Cache) Get(hash string) ([]byte, bool) {
	if !validHash(hash) {
		return nil, false
	}
	data, ok := c.Entry(SpaceBlobs, hash)
	if !ok {
		return nil, false
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != hash {
		c.log.Debug("blob cache entry does not match its hash; dropping it")
		c.Drop(SpaceBlobs, hash)
		return nil, false
	}
	return data, true
}

// Put implements port.BlobCache: data under hash, if it is the hash of data.
func (c *Cache) Put(hash string, data []byte) {
	if !validHash(hash) {
		return
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != hash {
		return
	}
	c.PutEntry(SpaceBlobs, hash, data)
}

// validKey reports whether key may name a file of space: a hash in the
// evictable spaces, a plain name in the state space.
func validKey(space, key string) bool {
	switch {
	case evictable(space):
		return validHash(key)
	case space == SpaceState:
		return nameKey.MatchString(key)
	default:
		return false
	}
}

// Entry is the entry key of space, as it was stored; a missing, unreadable
// or misnamed entry is a miss. A hit counts as a use for eviction. The
// caller checks the bytes.
func (c *Cache) Entry(space, key string) ([]byte, bool) {
	if !validKey(space, key) {
		return nil, false
	}
	root, err := os.OpenRoot(filepath.Join(c.dir, space))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			c.log.Debug("cache open failed", "space", space, "err", err)
		}
		return nil, false
	}
	defer func() { _ = root.Close() }()
	data, err := root.ReadFile(key)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			c.log.Debug("cache read failed", "space", space, "err", err)
		}
		return nil, false
	}
	now := c.clock.Now()
	_ = root.Chtimes(key, now, now)
	return data, true
}

// PutEntry stores data as the entry key of space, replacing any, then
// evicts down to the budget.
func (c *Cache) PutEntry(space, key string, data []byte) {
	if !validKey(space, key) || (evictable(space) && int64(len(data)) > c.maxBytes) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.write(filepath.Join(c.dir, space), key, data); err != nil {
		c.log.Debug("cache write failed", "space", space, "err", err)
		return
	}
	if evictable(space) {
		c.account(int64(len(data)))
	}
}

// account adds n bytes just written to the total, and evicts when the
// total passes the budget. The total may count a replaced entry twice, or
// miss what another process wrote; the count it evicts by is always the
// one on disk. The caller holds mu.
func (c *Cache) account(n int64) {
	if c.known {
		c.total += n
		if c.total <= c.maxBytes {
			return
		}
	}
	c.evict()
}

// Drop removes the entry key of space, if there is one.
func (c *Cache) Drop(space, key string) {
	if !validKey(space, key) {
		return
	}
	if err := os.Remove(filepath.Join(c.dir, space, key)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		c.log.Debug("cache drop failed", "space", space, "err", err)
	}
}

func (c *Cache) write(dir, key string, data []byte) error {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+key+".*.part")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	_, werr := tmp.Write(data)
	if err := errors.Join(tmp.Chmod(fileMode), werr, tmp.Close()); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(dir, key))
}

// cached is one entry on disk.
type cached struct {
	path string
	size int64
	used time.Time
}

// scan lists the entries of space that are the cache's own: regular files
// with a valid name.
func (c *Cache) scan(space string) []cached {
	dir := filepath.Join(c.dir, space)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			c.log.Debug("cache scan failed", "space", space, "err", err)
		}
		return nil
	}
	var out []cached
	for _, e := range entries {
		if !validKey(space, e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		out = append(out, cached{path: filepath.Join(dir, e.Name()), size: info.Size(), used: info.ModTime()})
	}
	return out
}

func (c *Cache) evict() {
	var all []cached
	var total int64
	for _, space := range Spaces() {
		if !evictable(space) {
			continue
		}
		for _, e := range c.scan(space) {
			all = append(all, e)
			total += e.size
		}
	}
	c.total, c.known = total, true
	if total <= c.maxBytes {
		return
	}
	slices.SortFunc(all, func(a, b cached) int { return cmp.Compare(a.used.UnixNano(), b.used.UnixNano()) })
	for _, f := range all {
		if total <= c.maxBytes {
			break
		}
		if err := os.Remove(f.path); err != nil {
			c.log.Debug("cache eviction failed", "err", err)
			continue
		}
		total -= f.size
	}
	c.total = total
}

// Usage is what the evictable spaces take in bytes, and the number of
// entries of every space.
func (c *Cache) Usage() (bytes int64, entries map[string]int) {
	entries = map[string]int{}
	for _, space := range Spaces() {
		found := c.scan(space)
		entries[space] = len(found)
		if evictable(space) {
			for _, e := range found {
				bytes += e.size
			}
		}
	}
	return bytes, entries
}

// Clear removes every entry of every space, then the directories left
// empty. Files the cache did not write are left where they are.
func (c *Cache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var errs []error
	for _, space := range Spaces() {
		for _, e := range c.scan(space) {
			if err := os.Remove(e.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
			}
		}
		_ = os.Remove(filepath.Join(c.dir, space))
	}
	c.total, c.known = 0, false
	_ = os.Remove(c.dir)
	return errors.Join(errs...)
}

func validHash(hash string) bool {
	if len(hash) != hashLen {
		return false
	}
	for i := range len(hash) {
		ch := hash[i]
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}
