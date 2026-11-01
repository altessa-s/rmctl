// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package vault presents a token store of the transport — the token file or
// the Keychain — as port.SecretVault, so the secrets use case moves tokens
// without knowing rmcloud-go or where they are kept.
package vault

import (
	"context"

	"github.com/altessa-s/rmcloud-go"

	"github.com/altessa-s/rmctl/internal/port"
)

// names are the secrets the transport keeps, in a fixed order.
func names() []rmcloud.SecretName {
	return []rmcloud.SecretName{rmcloud.SecretDeviceToken, rmcloud.SecretDeviceID, rmcloud.SecretUserToken}
}

// Vault is one token store of a profile.
type Vault struct {
	store    rmcloud.SecretStore
	backend  port.SecretsBackend
	location string
}

var _ port.SecretVault = (*Vault)(nil)

// New presents store, kept by backend at location, as a vault.
func New(store rmcloud.SecretStore, backend port.SecretsBackend, location string) *Vault {
	if store == nil {
		panic("vault: a store is required")
	}
	return &Vault{store: store, backend: backend, location: location}
}

// Backend implements port.SecretVault.
func (v *Vault) Backend() port.SecretsBackend { return v.backend }

// Location implements port.SecretVault.
func (v *Vault) Location() string { return v.location }

// Names implements port.SecretVault.
func (v *Vault) Names() []string {
	out := make([]string, 0, len(names()))
	for _, n := range names() {
		out = append(out, string(n))
	}
	return out
}

// Get implements port.SecretVault.
func (v *Vault) Get(ctx context.Context, name string) (string, bool, error) {
	return v.store.Secret(ctx, rmcloud.SecretName(name))
}

// Put implements port.SecretVault.
func (v *Vault) Put(ctx context.Context, name, value string) error {
	return v.store.Save(ctx, rmcloud.SecretName(name), value)
}

// Delete implements port.SecretVault.
func (v *Vault) Delete(ctx context.Context, name string) error {
	return v.store.Forget(ctx, rmcloud.SecretName(name))
}
