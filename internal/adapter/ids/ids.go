// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package ids

import (
	"strings"
	"uuid"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// Random makes random version 4 UUIDs, lowercase, as the tablet writes them.
type Random struct{}

var _ port.IDs = Random{}

// New returns the generator.
func New() Random { return Random{} }

// NewID returns a fresh identifier.
func (Random) NewID() domain.ItemID {
	return domain.ItemID(strings.ToLower(uuid.NewV4().String()))
}
