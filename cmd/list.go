package cmd

import (
	"fmt"

	"github.com/keeandrews/loradex-cli/internal/api"
	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	listVisibility string
	listOwner      string
	listStarred    bool
	listSort       string
	listLimit      int
	listPage       int
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List your repositories",
	Long: `List repositories (your dashboard).

Examples:
  loradex list
  loradex list --visibility private --sort updated
  loradex list --owner mira          # someone else's public repos
  loradex list --starred --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		app, err := newApp()
		if err != nil {
			return err
		}
		// Listing your own repos or starred requires auth; an explicit --owner is public.
		if listOwner == "" {
			if err := app.requireAuth(); err != nil {
				return err
			}
		}
		vis := listVisibility
		if vis == "all" {
			vis = ""
		}
		p := api.ListReposParams{
			Owner: listOwner, Visibility: vis, Starred: listStarred,
			Sort: listSort, Limit: clampLimit(listLimit), Page: clampPage(listPage),
		}
		res, err := app.Client.ListRepos(cmd.Context(), p)
		if err != nil {
			return err
		}
		if g.json {
			return app.P.JSONOut(res)
		}
		renderRepoTable(app.P, res.Items)
		app.P.Info("%d of %d", len(res.Items), res.Total)
		return nil
	},
}

func renderRepoTable(p *output.Printer, repos []api.Repo) {
	if len(repos) == 0 {
		p.Printf("no repositories\n")
		return
	}
	tw := p.Table()
	fmt.Fprintln(tw, "repository\tvis\tbase\tlatest\tsize\tupdated\t↓")
	for _, r := range repos {
		fmt.Fprintf(tw, "%s/%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Owner, r.Name, r.Visibility, dash(r.BaseModel), dash(r.LatestVersion),
			output.HumanSize(r.Size), shortDate(r.UpdatedAt), output.HumanCount(r.Downloads))
	}
	tw.Flush()
}

func init() {
	f := listCmd.Flags()
	f.StringVar(&listVisibility, "visibility", "all", "public | private | all")
	f.StringVar(&listOwner, "owner", "", "list a specific owner's public repos")
	f.BoolVar(&listStarred, "starred", false, "list your starred repos")
	f.StringVar(&listSort, "sort", "", "downloads | stars | updated | new")
	f.IntVar(&listLimit, "limit", 30, "max results (1–100)")
	f.IntVar(&listPage, "page", 1, "page number")
	rootCmd.AddCommand(listCmd)
}
