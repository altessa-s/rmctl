// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
)

// resolve runs a command with args in streams and returns the output it
// would use.
func resolve(t *testing.T, streams Streams, args ...string) (Output, error) {
	t.Helper()
	env := NewEnv(streams, Fixed(Services{}))
	var got Output
	var err error
	root := &cobra.Command{Use: "rmctl", SilenceErrors: true, SilenceUsage: true}
	AddPersistentFlags(root, env)
	child := &cobra.Command{Use: "x", RunE: func(cmd *cobra.Command, _ []string) error {
		got, err = env.Output(cmd)
		return nil
	}}
	root.AddCommand(child)
	root.SetOut(&bytes.Buffer{})
	root.SetArgs(append([]string{"x"}, args...))
	if runErr := root.Execute(); runErr != nil {
		return Output{}, runErr
	}
	return got, err
}

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestOutputPrecedence(t *testing.T) {
	t.Parallel()
	tty := Streams{OutTerminal: true, OutColor: true, Width: 80, Getenv: envOf(nil)}
	pipe := Streams{Getenv: envOf(nil)}
	withEnv := func(s Streams, m map[string]string) Streams { s.Getenv = envOf(m); return s }
	tests := []struct {
		name    string
		streams Streams
		args    []string
		format  string
		color   bool
		width   int
	}{
		{"a terminal is a colored table", tty, nil, "table", true, 80},
		{"a pipe is plain", pipe, nil, "plain", false, 0},
		{"--format beats the terminal", tty, []string{"--format", "plain"}, "plain", false, 80},
		{"--json is json", tty, []string{"--json"}, "json", false, 80},
		{"--json agrees with --format json", pipe, []string{"--json", "--format", "json"}, "json", false, 0},
		{"the environment beats the terminal", withEnv(tty, map[string]string{FormatEnv: "plain"}), nil, "plain", false, 80},
		{"--format beats the environment", withEnv(pipe, map[string]string{FormatEnv: "json"}), []string{"--format", "table"}, "table", false, 0},
		{"--json beats the environment", withEnv(pipe, map[string]string{FormatEnv: "table"}), []string{"--json"}, "json", false, 0},
		{"a table in a pipe has no color", pipe, []string{"--format", "table"}, "table", false, 0},
		{"--color always colors a pipe", pipe, []string{"--format", "table", "--color", "always"}, "table", true, 0},
		{"--color never", tty, []string{"--color", "never"}, "table", false, 80},
		{"NO_COLOR", withEnv(tty, map[string]string{NoColorEnv: "1"}), nil, "table", false, 80},
		{"TERM=dumb", withEnv(tty, map[string]string{TermEnv: "dumb"}), nil, "table", false, 80},
		{"a console without color", Streams{OutTerminal: true, Getenv: envOf(nil)}, nil, "table", false, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolve(t, tc.streams, tc.args...)
			if err != nil || got.Format != tc.format || got.Style.Color != tc.color || got.Style.Width != tc.width {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}

func TestOutputRefusals(t *testing.T) {
	t.Parallel()
	pipe := Streams{Getenv: envOf(nil)}
	for name, tc := range map[string]struct {
		streams Streams
		args    []string
	}{
		"an unknown --format":     {pipe, []string{"--format", "yaml"}},
		"--json against --format": {pipe, []string{"--json", "--format", "table"}},
		"an unknown RMCTL_FORMAT": {Streams{Getenv: envOf(map[string]string{FormatEnv: "xml"})}, nil},
		"an unknown --color":      {pipe, []string{"--color", "sometimes"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := resolve(t, tc.streams, tc.args...); ExitCode(err) != CodeUsage {
				t.Fatalf("%v", err)
			}
		})
	}
}

func TestEmitAndStream(t *testing.T) {
	t.Parallel()
	env := NewEnv(Streams{Getenv: envOf(nil)}, Fixed(Services{}))
	var out bytes.Buffer
	root := &cobra.Command{Use: "rmctl"}
	AddPersistentFlags(root, env)
	root.SetOut(&out)
	if err := env.Emit(root, output.Result{JSON: 1, Doc: output.Doc{output.Body{Text: "one"}}}); err != nil || out.String() != "one\n" {
		t.Fatalf("%q %v", out.String(), err)
	}
	out.Reset()
	// A stream with no rows and no final value prints nothing, not even a
	// header.
	s := env.Stream(root, []output.Column{{Title: "a"}})
	if err := s.End(nil); err != nil || out.Len() != 0 {
		t.Fatalf("%q %v", out.String(), err)
	}
	if err := root.ParseFlags([]string{"--json"}); err != nil {
		t.Fatal(err)
	}
	s = env.Stream(root, []output.Column{{Title: "a"}})
	if err := s.End([]int{}); err != nil || out.String() != "[]\n" {
		t.Fatalf("%q %v", out.String(), err)
	}
	if !env.JSON(root) {
		t.Fatal("--json is not json")
	}
}
