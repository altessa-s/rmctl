// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package indexcache

import (
	"encoding/json/v2"
	"log/slog"
	"time"
)

// pointer is a profile's last known root and when the cloud confirmed it.
type pointer struct {
	Version    int       `json:"v"`
	Hash       string    `json:"hash"`
	Generation int64     `json:"generation"`
	Confirmed  time.Time `json:"confirmed"`
}

// readPointer is the profile's root pointer; a missing or unreadable one is
// unknown, and an unreadable one is dropped.
func readPointer(store Store, log *slog.Logger) (pointer, bool) {
	data, ok := store.Entry(spaceState, rootKey)
	if !ok {
		return pointer{}, false
	}
	var p pointer
	if err := json.Unmarshal(data, &p); err != nil || p.Version != entryVersion || p.Confirmed.IsZero() {
		log.Debug("root pointer dropped")
		store.Drop(spaceState, rootKey)
		return pointer{}, false
	}
	return p, true
}

func writePointer(store Store, p pointer, log *slog.Logger) {
	p.Version = entryVersion
	data, err := json.Marshal(p)
	if err != nil {
		log.Debug("root pointer not encoded", "err", err)
		return
	}
	store.PutEntry(spaceState, rootKey, data)
}
