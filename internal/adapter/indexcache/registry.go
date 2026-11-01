// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package indexcache

import (
	"context"

	"github.com/altessa-s/rmctl/internal/port"
)

// Registry clears the profile's cache whenever its account changes hands:
// after a registration and after the token is forgotten, so whoever
// registers next under the profile never sees what the last account read.
type Registry struct {
	port.Registry
	store ProfileStore
}

var _ port.Registry = (*Registry)(nil)

// NewRegistry decorates reg with the clearing of store.
func NewRegistry(reg port.Registry, store ProfileStore) *Registry {
	if reg == nil || store == nil {
		panic("indexcache: a registry and a store are required")
	}
	return &Registry{Registry: reg, store: store}
}

// Register implements port.Registry.
func (r *Registry) Register(ctx context.Context, code string) error {
	if err := r.Registry.Register(ctx, code); err != nil {
		return err
	}
	return r.store.Clear()
}

// Forget implements port.Registry.
func (r *Registry) Forget(ctx context.Context) error {
	if err := r.Registry.Forget(ctx); err != nil {
		return err
	}
	return r.store.Clear()
}
