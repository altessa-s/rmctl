// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
)

// The environment variables that shape the output.
const (
	// FormatEnv names the output format when --format and --json are not
	// given.
	FormatEnv = "RMCTL_FORMAT"
	// NoColorEnv, set to anything but "", turns automatic color off
	// (https://no-color.org).
	NoColorEnv = "NO_COLOR"
	// TermEnv set to "dumb" turns automatic color off.
	TermEnv = "TERM"
)

// The values of --color.
const (
	ColorAuto   = "auto"
	ColorAlways = "always"
	ColorNever  = "never"
)

// Output is how a command writes its answer: the renderer chosen and what
// it knows of the terminal.
type Output struct {
	// Format is the renderer's registered name.
	Format   string
	Renderer output.Renderer
	Style    output.Style
}

// getenv is the streams' environment, or the process's.
func (s Streams) getenv(key string) string {
	if s.Getenv != nil {
		return s.Getenv(key)
	}
	return os.Getenv(key)
}

// Output resolves the format and the color of cmd's answer: --format, else
// --json, else $RMCTL_FORMAT, else table on a terminal and plain anywhere
// else. --color auto colors a table on a terminal that can show color,
// unless $NO_COLOR is set or $TERM is dumb.
func (e *Env) Output(cmd *cobra.Command) (Output, error) {
	name, err := e.formatName(cmd)
	if err != nil {
		return Output{}, err
	}
	r, ok := output.Lookup(name)
	if !ok {
		return Output{}, Usagef("unknown output format %q: use %s", name, strings.Join(output.Names(), ", "))
	}
	color, err := e.color(cmd, name)
	if err != nil {
		return Output{}, err
	}
	style := output.Style{Color: color}
	if e.Streams.OutTerminal {
		style.Width = e.Streams.Width
	}
	return Output{Format: name, Renderer: r, Style: style}, nil
}

func (e *Env) formatName(cmd *cobra.Command) (string, error) {
	asJSON, _ := cmd.Flags().GetBool(FlagJSON) //nolint:errcheck // the flag is persistent on the root
	if f := globalFlag(cmd, FlagFormat); f != nil && f.Changed {
		name := f.Value.String()
		if asJSON && name != output.FormatJSON {
			return "", Usagef("--json and --format %s disagree", name)
		}
		return name, nil
	}
	if asJSON {
		return output.FormatJSON, nil
	}
	if name := e.Streams.getenv(FormatEnv); name != "" {
		if _, ok := output.Lookup(name); !ok {
			return "", Usagef("%s=%s: unknown output format: use %s", FormatEnv, name, strings.Join(output.Names(), ", "))
		}
		return name, nil
	}
	if e.Streams.OutTerminal {
		return output.FormatTable, nil
	}
	return output.FormatPlain, nil
}

func (e *Env) color(cmd *cobra.Command, format string) (bool, error) {
	mode := ColorAuto
	if f := globalFlag(cmd, FlagColor); f != nil {
		mode = f.Value.String()
	}
	switch mode {
	case ColorAlways:
		return true, nil
	case ColorNever:
		return false, nil
	case ColorAuto:
		return format == output.FormatTable && e.Streams.OutTerminal && e.Streams.OutColor &&
			e.Streams.getenv(NoColorEnv) == "" && e.Streams.getenv(TermEnv) != "dumb", nil
	default:
		return false, Usagef("--color %s: use auto, always or never", mode)
	}
}

// globalFlag is the root's persistent flag name, which a command's own flag
// of the same name (text --format) leaves untouched.
func globalFlag(cmd *cobra.Command, name string) *pflag.Flag {
	return cmd.Root().PersistentFlags().Lookup(name)
}

// JSON reports whether cmd's answer is JSON: --json, --format json or
// $RMCTL_FORMAT=json.
func (e *Env) JSON(cmd *cobra.Command) bool {
	o, err := e.Output(cmd)
	return err == nil && o.Format == output.FormatJSON
}

// Emit writes r in cmd's output format.
func (e *Env) Emit(cmd *cobra.Command, r output.Result) error {
	o, err := e.Output(cmd)
	if err != nil {
		return err
	}
	return o.Renderer.Render(cmd.OutOrStdout(), r, o.Style)
}

// Stream is a stream of rows with cols in cmd's output format. It starts —
// and a table writes its header — with the first row, or at End when no row
// came, so a command that fails before its first answer prints nothing.
func (e *Env) Stream(cmd *cobra.Command, cols []output.Column) output.Emitter {
	return &lazyStream{start: func() (output.Emitter, error) {
		o, err := e.Output(cmd)
		if err != nil {
			return nil, err
		}
		return output.Stream(cmd.OutOrStdout(), o.Renderer, cols, o.Style)
	}}
}

type lazyStream struct {
	start func() (output.Emitter, error)
	em    output.Emitter
}

func (l *lazyStream) begin() error {
	if l.em != nil {
		return nil
	}
	em, err := l.start()
	l.em = em
	return err
}

func (l *lazyStream) Row(r output.Row) error {
	if err := l.begin(); err != nil {
		return err
	}
	return l.em.Row(r)
}

func (l *lazyStream) End(final any) error {
	if l.em == nil && final == nil {
		return nil
	}
	if err := l.begin(); err != nil {
		return err
	}
	return l.em.End(final)
}
