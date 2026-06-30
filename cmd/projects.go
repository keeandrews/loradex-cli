package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/keeandrews/loradex-cli/internal/config"
	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/keeandrews/loradex-cli/internal/workspace"
	"github.com/spf13/cobra"
)

var projectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "List managed projects (under ~/.loradex/projects)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		p := output.New(g.json, g.quiet, g.verbose, g.noColor)
		slugs, err := listProjects()
		if err != nil {
			return err
		}
		f, _ := config.Load()
		current := f.CurrentProject
		if g.json {
			return p.JSONOut(map[string]any{"projects": slugs, "current": current})
		}
		if len(slugs) == 0 {
			p.Info("no managed projects yet — create one with `loradex init`")
			return nil
		}
		tw := p.Table()
		fmt.Fprintln(tw, "  \tPROJECT\tMODELS\tACTIVE")
		pd, _ := config.ProjectsDir()
		for _, s := range slugs {
			marker, active := " ", ""
			if s == current {
				marker, active = "*", "active"
			}
			models := len(workspace.DiscoverModels(filepath.Join(pd, s)))
			fmt.Fprintf(tw, "  %s\t%s\t%d\t%s\n", marker, s, models, active)
		}
		tw.Flush()
		p.Info("switch with `loradex use <name>`")
		return nil
	},
}

var useCmd = &cobra.Command{
	Use:   "use <name>",
	Short: "Set the active managed project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		p := output.New(g.json, g.quiet, g.verbose, g.noColor)
		slug := args[0]
		pd, err := config.ProjectsDir()
		if err != nil {
			return err
		}
		if !workspace.IsWorkspace(filepath.Join(pd, slug)) {
			return output.Errorf(output.ExitNotFound, "not_found", "see `loradex projects`", "no managed project %q", slug)
		}
		if err := config.SetCurrentProject(slug); err != nil {
			return err
		}
		p.Success("active project: %s", slug)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(projectsCmd)
	rootCmd.AddCommand(useCmd)
}
