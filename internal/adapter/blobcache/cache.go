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
	"slices"
	"sync"
	"time"

	"github.com/altessa-s/rmctl/internal/port"
)

// dirMode is the mode of the cache directory.
const dirMode fs.FileMode = 0o700

// fileMode is the mode of a cached file: it may hold the user's documents.
const fileMode fs.FileMode = 0o600

// hashLen is the length of a sha256 hash in hex.
const hashLen = 64

// Cache keeps files by their sha256 hash in one flat directory. A file is
// checked against its hash when it is read, so a damaged file is a miss.
// The directory is held under maxBytes by removing the files used least
// recently. The cache never fails a command: every error is a miss, noted
// at debug level.
type Cache struct {
	dir      string
	maxBytes int64
	log      *slog.Logger
	clock    port.Clock
	mu       sync.Mutex
}

var _ port.BlobCache = (*Cache)(nil)

// New returns the cache kept in dir, bounded to maxBytes; clock stamps each
// use for the eviction order. A nil logger discards.
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

// Get returns the file stored under hash.
func (c *Cache) Get(hash string) ([]byte, bool) {
	if !validHash(hash) {
		return nil, false
	}
	// The root scopes every access to the cache directory: a name that is
	// not a plain file name inside it cannot be read, whatever validHash says.
	root, err := os.OpenRoot(c.dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			c.log.Debug("blob cache open failed", "err", err)
		}
		return nil, false
	}
	defer func() { _ = root.Close() }()
	data, err := root.ReadFile(hash)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			c.log.Debug("blob cache read failed", "err", err)
		}
		return nil, false
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != hash {
		c.log.Debug("blob cache entry does not match its hash; dropping it")
		_ = root.Remove(hash)
		return nil, false
	}
	now := c.clock.Now()
	_ = root.Chtimes(hash, now, now)
	return data, true
}

// Put stores data under hash. Data that does not hash to hash is not stored.
func (c *Cache) Put(hash string, data []byte) {
	if !validHash(hash) {
		return
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != hash {
		return
	}
	if int64(len(data)) > c.maxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.write(hash, data); err != nil {
		c.log.Debug("blob cache write failed", "err", err)
		return
	}
	c.evict()
}

func (c *Cache) write(hash string, data []byte) error {
	if err := os.MkdirAll(c.dir, dirMode); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(c.dir, "."+hash+".*.part")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	_, werr := tmp.Write(data)
	if err := errors.Join(tmp.Chmod(fileMode), werr, tmp.Close()); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(c.dir, hash))
}

// evict removes the least recently used files until the directory fits.
func (c *Cache) evict() {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		c.log.Debug("blob cache scan failed", "err", err)
		return
	}
	type cached struct {
		name string
		size int64
		used time.Time
	}
	var all []cached
	var total int64
	for _, e := range entries {
		if !validHash(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		all = append(all, cached{name: e.Name(), size: info.Size(), used: info.ModTime()})
		total += info.Size()
	}
	if total <= c.maxBytes {
		return
	}
	slices.SortFunc(all, func(a, b cached) int { return cmp.Compare(a.used.UnixNano(), b.used.UnixNano()) })
	for _, f := range all {
		if total <= c.maxBytes {
			return
		}
		if err := os.Remove(filepath.Join(c.dir, f.name)); err != nil {
			c.log.Debug("blob cache eviction failed", "err", err)
			continue
		}
		total -= f.size
	}
}

// validHash reports 64 lowercase hex digits: the only names this cache
// ever turns into a path.
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
