// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package indexcache

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"log/slog"
	"strconv"
	"sync"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// listingKey is the name, among the state, of the listing of the library.
const listingKey = "listing"

// listing is the library as a command last loaded it, on disk: the root
// it was built from and, per item, what completion needs. Hash comes first,
// so a writer tells the listing of a root from the first bytes alone.
type listing struct {
	Version    int           `json:"v"`
	Hash       string        `json:"hash"`
	Generation int64         `json:"generation"`
	Items      []listingItem `json:"items"`
}

type listingItem struct {
	ID      string `json:"i"`
	Name    string `json:"n"`
	Kind    string `json:"k"`
	Parent  string `json:"p,omitempty"`
	Trashed bool   `json:"t,omitempty"`
}

// listingHead is the start of the listing of the root hash names.
func listingHead(hash string) []byte {
	return []byte(`{"v":` + strconv.Itoa(entryVersion) + `,"hash":` + strconv.Quote(hash) + `,`)
}

// Listings implements port.ListingCache over the profile's state: one entry,
// replaced whenever a command loads a root other than the one it lists.
// It is a few dozen bytes an item, and never evicted.
type Listings struct {
	store Store
	log   *slog.Logger

	mu   sync.Mutex
	last string
}

var _ port.ListingCache = (*Listings)(nil)

// NewListings keeps the listing in store.
func NewListings(store Store, log *slog.Logger) *Listings {
	if store == nil {
		panic("indexcache: a store is required")
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Listings{store: store, log: log}
}

// PutListing implements port.ListingCache. The listing of a root already
// kept is not written again: this process remembers the last one, and the
// first call reads the head of the entry.
func (l *Listings) PutListing(hash string, tree *domain.Tree) {
	if tree == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if hash == l.last && hash != "" {
		return
	}
	if l.last == "" && hash != "" {
		if data, ok := l.store.Entry(spaceState, listingKey); ok && bytes.HasPrefix(data, listingHead(hash)) {
			l.last = hash
			return
		}
	}
	v := listing{Version: entryVersion, Hash: hash, Generation: tree.Generation, Items: make([]listingItem, 0, tree.Len())}
	for _, it := range tree.Items() {
		v.Items = append(v.Items, listingItem{
			ID: string(it.ID), Name: it.Name, Kind: string(it.Kind), Parent: string(it.Parent), Trashed: it.TrashedHere(),
		})
	}
	data, err := json.Marshal(v)
	if err != nil {
		l.log.Debug("listing not encoded", "err", err)
		return
	}
	l.store.PutEntry(spaceState, listingKey, data)
	l.last = hash
}

// Peeker reads an entry without counting it as a use or dropping it when
// it is damaged; blobcache's Cache implements it.
type Peeker interface {
	Peek(space, key string) ([]byte, bool)
}

// Completion implements port.Completion: the listing [Listings] kept,
// read once and never written. A missing, damaged or older listing is
// port.ErrNoListing.
type Completion struct {
	store Peeker
}

var _ port.Completion = Completion{}

// NewCompletion reads the listing from store; a nil store holds none, as
// under --no-cache.
func NewCompletion(store Peeker) Completion { return Completion{store: store} }

// Listing implements port.Completion.
func (c Completion) Listing(context.Context) (*domain.Tree, error) {
	if c.store == nil {
		return nil, port.ErrNoListing
	}
	data, ok := c.store.Peek(spaceState, listingKey)
	if !ok {
		return nil, port.ErrNoListing
	}
	var v listing
	if err := json.Unmarshal(data, &v); err != nil || v.Version != entryVersion {
		return nil, port.ErrNoListing
	}
	items := make([]domain.Item, 0, len(v.Items))
	for _, it := range v.Items {
		items = append(items, domain.NewListedItem(domain.ItemID(it.ID), it.Name, domain.Kind(it.Kind), domain.ItemID(it.Parent), it.Trashed))
	}
	return domain.NewTree(v.Generation, items), nil
}
