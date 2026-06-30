package cmd

import (
	"github.com/keeandrews/loradex-cli/internal/buildinfo"
	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the CLI version and build info",
	RunE: func(cmd *cobra.Command, args []string) error {
		p := output.New(g.json, g.quiet, g.verbose, g.noColor)
		info := map[string]string{
			"version":  buildinfo.Version,
			"commit":   buildinfo.Commit,
			"date":     buildinfo.Date,
			"go":       buildinfo.GoVersion(),
			"platform": buildinfo.Platform(),
		}
		if g.json {
			return p.JSONOut(info)
		}
		p.Printf("loradex %s\n", buildinfo.Version)
		p.Printf("  commit   %s\n", buildinfo.Commit)
		p.Printf("  built    %s\n", buildinfo.Date)
		p.Printf("  go       %s\n", buildinfo.GoVersion())
		p.Printf("  platform %s\n", buildinfo.Platform())
		return nil
	},
}

func init() { rootCmd.AddCommand(versionCmd) }
