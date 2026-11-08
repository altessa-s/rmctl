// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package opener_test

import (
	"testing"

	"github.com/altessa-s/rmctl/internal/adapter/opener"
)

func TestNew(t *testing.T) {
	t.Parallel()
	if opener.New() == nil {
		t.Fatal("no opener")
	}
}
