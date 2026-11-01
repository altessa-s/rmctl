// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
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

// Report is what a recursive transfer did, as a record: mget and mput both
// answer this way.
func Report(r port.MirrorReport) output.Result {
	return output.Result{JSON: reportView(r), Doc: output.Doc{output.Record{Fields: []output.Field{
		{Key: "written", Value: output.Count(r.Written)},
		{Key: "skipped", Value: output.Count(r.Skipped)},
		{Key: "deleted", Value: output.Count(r.Deleted)},
	}}}}
}
