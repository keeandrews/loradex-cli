package cmd

import (
	"fmt"
	"strings"

	"github.com/keeandrews/loradex-cli/internal/api"
	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	searchBase   []string
	searchFormat []string
	searchTag    []string
	searchSort   string
	searchLimit  int
	searchPage   int
)

var searchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Compatibility-first + full-text discovery",
	Long: `Search public repositories, filtered to your exact stack.

Examples:
  loradex search "photorealistic portrait"
  loradex search --base flux2-klein --format safetensors "portrait"
  loradex search --tag anime --sort stars --json`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		app, err := newApp()
		if err != nil {
			return err
		}
		query := ""
		if len(args) == 1 {
			query = args[0]
		}
		p := api.SearchParams{
			Query: query, Base: searchBase, Format: searchFormat, Tag: searchTag,
			Sort: searchSort, Limit: clampLimit(searchLimit), Page: clampPage(searchPage),
		}
		res, err := app.Client.Search(cmd.Context(), p)
		if err != nil {
			return err
		}
		if g.json {
			return app.P.JSONOut(res)
		}

		var filters []string
		filters = append(filters, searchBase...)
		filters = append(filters, searchFormat...)
		filters = append(filters, searchTag...)
		line := fmt.Sprintf("%d repositories", res.Total)
		if len(filters) > 0 {
			line += " · filtered by " + strings.Join(filters, ", ")
		}
		app.P.Info("%s", line)

		if len(res.Items) == 0 {
			app.P.Printf("no matches\n")
			return nil
		}
		tw := app.P.Table()
		fmt.Fprintln(tw, "repository\tbase\tformat\t↓\t★\tupdated")
		for _, r := range res.Items {
			fmt.Fprintf(tw, "%s/%s\t%s\t%s\t%s\t%s\t%s\n",
				r.Owner, r.Name, dash(r.BaseModel), dash(r.Format),
				output.HumanCount(r.Downloads), output.HumanCount(r.Stars), shortDate(r.UpdatedAt))
		}
		tw.Flush()
		return nil
	},
}

func init() {
	f := searchCmd.Flags()
	f.StringArrayVar(&searchBase, "base", nil, "filter by base model (repeatable)")
	f.StringArrayVar(&searchFormat, "format", nil, "filter by format (repeatable)")
	f.StringArrayVar(&searchTag, "tag", nil, "filter by tag (repeatable)")
	f.StringVar(&searchSort, "sort", "downloads", "downloads | stars | updated | new")
	f.IntVar(&searchLimit, "limit", 30, "max results (1–100)")
	f.IntVar(&searchPage, "page", 1, "page number")
	rootCmd.AddCommand(searchCmd)
}
