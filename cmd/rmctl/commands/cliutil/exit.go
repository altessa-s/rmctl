// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/internal/port"
)

// The exit codes of rmctl.
const (
	CodeOK            = 0
	CodeError         = 1
	CodeUsage         = 2
	CodeNotRegistered = 3
	CodeConflict      = 4
	CodeInterrupted   = 130
)

// ExitError carries the exit code an error ends the process with.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit status %d", e.Code)
	}
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error { return e.Err }

// Usage marks err as a usage error: exit code 2.
func Usage(err error) error {
	if err == nil {
		return nil
	}
	return &ExitError{Code: CodeUsage, Err: err}
}

// Usagef is [Usage] of a formatted message.
func Usagef(format string, args ...any) error {
	return Usage(fmt.Errorf(format, args...))
}

// ErrInterrupted is the error of a command stopped by Ctrl-C.
var ErrInterrupted = errors.New("interrupted")

// ExitCode is the process exit code for err.
func ExitCode(err error) int {
	if err == nil {
		return CodeOK
	}
	if exit, ok := errors.AsType[*ExitError](err); ok {
		return exit.Code
	}
	switch {
	case errors.Is(err, ErrInterrupted):
		return CodeInterrupted
	case errors.Is(err, port.ErrNotRegistered):
		return CodeNotRegistered
	case errors.Is(err, port.ErrRootConflict):
		return CodeConflict
	case isCobraUsage(err):
		return CodeUsage
	default:
		return CodeError
	}
}

// isCobraUsage recognizes the errors cobra returns before a command runs:
// an unknown command or flag, a flag without its value, a required flag
// missing.
func isCobraUsage(err error) bool {
	msg := err.Error()
	for _, prefix := range []string{
		"unknown command", "unknown flag", "unknown shorthand flag", "flag needs an argument", "invalid argument", "required flag(s) ",
	} {
		if strings.HasPrefix(msg, prefix) {
			return true
		}
	}
	return false
}

// Args wraps a cobra argument validator so that its refusal is a usage error.
func Args(v cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		return Usage(v(cmd, args))
	}
}

// FlagError is a cobra flag error function that marks its error as usage.
func FlagError(_ *cobra.Command, err error) error {
	return Usage(err)
}

// Interrupted reports err as an interruption when ctx was canceled by it.
func Interrupted(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(context.Cause(ctx), ErrInterrupted) {
		return ErrInterrupted
	}
	return err
}
