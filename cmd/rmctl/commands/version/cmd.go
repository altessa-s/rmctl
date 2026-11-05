// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package version

import (
	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/go-atlas-core/runtime/appinfo"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"

	out "github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
)

// command wraps [cobra.Command] for the "version" subcommand.
type command struct {
	*cobra.Command
	env *cliutil.Env
}

// New returns the "version" [cobra.Command] with --full/-f and --output/-o
// flags. --output text or json chooses the format; without it the global
// --format, --json and $RMCTL_FORMAT do.
func New(env *cliutil.Env) *cobra.Command {
	version := &command{
		env: env,
		Command: &cobra.Command{
			Use:   "version",
			Short: "Display version information",
			Long: heredoc.Doc(`
				Display version information for rmctl.

				By default, only the version number is shown. Use --full to display
				additional build information including commit hash, build time,
				platform, and whether this build renders documents: render is no in
				rmctl-lite, the build without the renderer, and in a build without cgo.

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
	full, _ := cmd.Flags().GetBool("full") //nolint:errcheck // the flag exists
	r := result(full, c.env.Streams.Renders)
	if !cmd.Flags().Changed("output") {
		return c.env.Emit(cmd, r)
	}
	output, _ := cmd.Flags().GetString("output")                    //nolint:errcheck // the flag exists
	if asJSON, _ := cmd.Flags().GetBool(cliutil.FlagJSON); asJSON { //nolint:errcheck // the flag is persistent on the root
		output = "json"
	}
	switch output {
	case "json":
		return out.JSONRenderer{}.Render(cmd.OutOrStdout(), r, out.Style{})
	case "text":
		return out.PlainRenderer{}.Render(cmd.OutOrStdout(), r, out.Style{})
	default:
		return cliutil.Usagef("invalid output format: %s. Valid formats are: text, json", output)
	}
}

// result is the version, or with full the build information as a record;
// renders says this build draws documents.
func result(full, renders bool) out.Result {
	if !full {
		return out.Result{
			JSON: struct {
				Version string `json:"version"`
			}{Version: appinfo.Version},
			Doc: out.Doc{out.Body{Text: appinfo.Version}},
		}
	}
	v := fullView{
		Version:   appinfo.Version,
		BuildTime: appinfo.BuildTime,
		Commit:    appinfo.Commit,
		GoVersion: appinfo.BuildGoVersion(),
		Platform:  appinfo.BuildPlatform(),
		Render:    renders,
	}
	return out.Result{JSON: v, Doc: out.Doc{out.Record{Fields: []out.Field{
		{Key: "version", Value: out.Text(v.Version)},
		{Key: "build_time", Value: out.Text(v.BuildTime)},
		{Key: "commit", Value: out.ID(v.Commit)},
		{Key: "go_version", Value: out.Text(v.GoVersion)},
		{Key: "platform", Value: out.Text(v.Platform)},
		{Key: "render", Value: out.Text(yesNo(v.Render))},
	}}}}
}

// yesNo is on as yes or no.
func yesNo(on bool) string {
	if on {
		return "yes"
	}
	return "no"
}

// fullView is version --full as JSON.
type fullView struct {
	Version   string `json:"version"`
	BuildTime string `json:"build_time"`
	Commit    string `json:"commit"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
	// Render says this build draws documents: false in rmctl-lite.
	Render bool `json:"render"`
}
