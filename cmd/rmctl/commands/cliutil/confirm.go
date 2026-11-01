// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"bufio"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// ErrDeclined means the person answered no to a confirmation.
var ErrDeclined = errors.New("not confirmed; nothing was changed")

// maxAnswer bounds the confirmation line read from standard input.
const maxAnswer = 256

// Confirm guards a command that changes the live library. --yes passes. On
// a terminal the question is asked and only y or yes passes. Anywhere else
// the command is refused as a usage error before it changes anything.
func Confirm(cmd *cobra.Command, env *Env, question string) error {
	if Yes(cmd) {
		return nil
	}
	if !env.Streams.IsTerminal {
		return Usagef("%s: this changes the library; pass --yes to confirm", cmd.CommandPath())
	}
	_, _ = fmt.Fprintf(env.Streams.Err, "%s [y/N] ", question) //nolint:errcheck // a prompt
	sc := bufio.NewScanner(env.Streams.In)
	sc.Buffer(make([]byte, 0, maxAnswer), maxAnswer)
	if !sc.Scan() {
		return ErrDeclined
	}
	switch strings.ToLower(strings.TrimSpace(sc.Text())) {
	case "y", "yes":
		return nil
	default:
		return ErrDeclined
	}
}
