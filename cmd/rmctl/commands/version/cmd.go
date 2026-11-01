// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package version

import (
	"fmt"
	"io"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/go-atlas-core/runtime/appinfo"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
)

// command wraps [cobra.Command] for the "version" subcommand.
type command struct {
	*cobra.Command
}

// New returns the "version" [cobra.Command] with --full/-f and --output/-o
// flags. The output formats are "text" (default) and "json"; the persistent
// --json selects json as well.
func New() *cobra.Command {
	version := &command{
		Command: &cobra.Command{
			Use:   "version",
			Short: "Display version information",
			Long: heredoc.Doc(`
				Display version information for rmctl.

				By default, only the version number is shown. Use --full to display
				additional build information including commit hash, build time, and
				platform.

				Output can be formatted as plain text (default) or JSON for
				programmatic consumption.
			`),
			Example: heredoc.Doc(`
				# Display the current version
				rmctl version

				# Display full version information in JSON format
				rmctl version --full --output json
			`),
			Args:         cliutil.Args(cobra.NoArgs),
			SilenceUsage: true,
		},
	}
	version.configure()
	return version.Command
}

func (c *command) configure() {
	c.Flags().BoolP("full", "f", false, "Display full version information")
	c.Flags().StringP("output", "o", "text", "Output format: text (default), json")
	c.RunE = c.run
}

func (c *command) run(cmd *cobra.Command, _ []string) error {
	output, _ := cmd.Flags().GetString("output") //nolint:errcheck // the flag exists
	full, _ := cmd.Flags().GetBool("full")       //nolint:errcheck // the flag exists
	if cliutil.JSON(cmd) {
		output = "json"
	}
	switch output {
	case "json":
		return printJSON(cmd.OutOrStdout(), full)
	case "text":
		return printText(cmd.OutOrStdout(), full)
	default:
		return cliutil.Usagef("invalid output format: %s. Valid formats are: text, json", output)
	}
}

// printText writes the version; with full, the multi-line build summary of
// [appinfo.AppVersion].
func printText(w io.Writer, full bool) error {
	text := appinfo.Version
	if full {
		text = appinfo.AppVersion()
	}
	_, err := fmt.Fprintln(w, text)
	return err
}

// fullView is version --full as JSON.
type fullView struct {
	Version   string `json:"version"`
	BuildTime string `json:"build_time"`
	Commit    string `json:"commit"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
}

// printJSON writes the version as indented JSON; with full, the build
// information too.
func printJSON(w io.Writer, full bool) error {
	if !full {
		return cliutil.WriteJSON(w, struct {
			Version string `json:"version"`
		}{Version: appinfo.Version})
	}
	return cliutil.WriteJSON(w, fullView{
		Version:   appinfo.Version,
		BuildTime: appinfo.BuildTime,
		Commit:    appinfo.Commit,
		GoVersion: appinfo.BuildGoVersion(),
		Platform:  appinfo.BuildPlatform(),
	})
}
