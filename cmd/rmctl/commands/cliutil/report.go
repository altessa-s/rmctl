// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"fmt"
	"io"

	"github.com/altessa-s/rmctl/internal/port"
)

// GroupRead is the help group of the reading commands. It lives here because
// commands of several packages belong to it.
const GroupRead = "read"

type reportView struct {
	Written int `json:"written"`
	Skipped int `json:"skipped"`
	Deleted int `json:"deleted"`
}

// PrintReport writes what a recursive transfer did: mget and mput both
// report this way.
func PrintReport(out io.Writer, asJSON bool, r port.MirrorReport) error {
	if asJSON {
		return WriteJSON(out, reportView(r))
	}
	_, err := fmt.Fprintf(out, "written %d, skipped %d, deleted %d\n", r.Written, r.Skipped, r.Deleted)
	return err
}
