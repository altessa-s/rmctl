// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/domain"
)

func TestErrRenderLite(t *testing.T) {
	t.Parallel()
	wrapped := fmt.Errorf("render: %w", domain.ErrRenderLite)
	if !errors.Is(wrapped, domain.ErrRenderLite) || !errors.Is(wrapped, domain.ErrRenderUnsupported) {
		t.Fatal("rmctl-lite's error is not a build that draws nothing")
	}
	if errors.Is(domain.ErrRenderUnsupported, domain.ErrRenderLite) || errors.Is(domain.ErrRenderLite, domain.ErrNoSuchPage) {
		t.Fatal("rmctl-lite's error matches too much")
	}
	if !strings.Contains(domain.ErrRenderLite.Error(), "rmctl-lite") {
		t.Fatal(domain.ErrRenderLite)
	}
}
