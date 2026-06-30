package cmd

import (
	"fmt"
	"strings"

	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/keeandrews/loradex-cli/internal/ref"
	"github.com/spf13/cobra"
)

var (
	viewVersion  string
	viewVersions bool
	viewFiles    bool
)

var viewCmd = &cobra.Command{
	Use:   "view <owner/repo>[@version]",
	Short: "Show a repository page in the terminal",
	Long: `Render a repository's metadata and README in the terminal.

Examples:
  loradex view keenan/flux2-klein-portrait
  loradex view keenan/flux2-klein-portrait --versions
  loradex view keenan/flux2-klein-portrait --files --json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		app, err := newApp()
		if err != nil {
			return err
		}
		r, err := ref.ParseAllowBareRepo(args[0])
		if err != nil {
			return output.Usage("%v", err)
		}
		if r.Owner == "" {
			if !app.HasToken {
				return output.Usage("specify owner/repo (or `loradex login` to infer your owner)")
			}
			r.Owner = app.Handle
		}
		version, err := ref.ReconcileVersion(r.Version, viewVersion)
		if err != nil {
			return output.Usage("%v", err)
		}
		ctx := cmd.Context()

		if viewVersions {
			vs, err := app.Client.ListVersions(ctx, r.Owner, r.Repo)
			if err != nil {
				return err
			}
			if g.json {
				return app.P.JSONOut(vs)
			}
			tw := app.P.Table()
			fmt.Fprintln(tw, "version\treleased\tsize\tnotes")
			for _, v := range vs {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", v.Tag, shortDate(v.CreatedAt), output.HumanSize(v.Size), v.Notes)
			}
			tw.Flush()
			return nil
		}

		if viewFiles {
			ver, files, err := app.Client.ListFiles(ctx, r.Owner, r.Repo, version)
			if err != nil {
				return err
			}
			if g.json {
				return app.P.JSONOut(map[string]any{"version": ver, "files": files})
			}
			app.P.Info("files · %s", ver)
			tw := app.P.Table()
			fmt.Fprintln(tw, "name\ttype\tsize")
			for _, f := range files {
				fmt.Fprintf(tw, "%s\t%s\t%s\n", f.Name, dash(f.Type), output.HumanSize(f.Size))
			}
			tw.Flush()
			return nil
		}

		repo, err := app.Client.GetRepo(ctx, r.Owner, r.Repo)
		if err != nil {
			return err
		}
		if g.json {
			return app.P.JSONOut(repo)
		}
		app.P.Printf("%s\n", app.P.Bold(repo.Owner+"/"+repo.Name))
		app.P.Printf("%s · %s · %s\n", repo.Visibility, dash(repo.BaseModel), dash(repo.Format))
		if repo.Description != "" {
			app.P.Printf("\n%s\n", repo.Description)
		}
		app.P.Printf("\ntrigger words   %s\n", dash(strings.Join(repo.TriggerWords, ", ")))
		app.P.Printf("rank / dim      %d / %d\n", repo.NetworkRank, repo.NetworkDim)
		app.P.Printf("recommended     %.2f\n", repo.RecommendedWeight)
		app.P.Printf("size            %s\n", output.HumanSize(repo.Size))
		app.P.Printf("license         %s\n", dash(repo.License))
		app.P.Printf("downloads       %s · ★ %s\n", output.HumanCount(repo.Downloads), output.HumanCount(repo.Stars))
		app.P.Printf("latest          %s · updated %s\n", dash(repo.LatestVersion), shortDate(repo.UpdatedAt))
		if repo.Readme != "" {
			app.P.Printf("\n%s\n", renderMarkdown(app.P, repo.Readme))
		}
		return nil
	},
}

// renderMarkdown does a minimal markdown→ANSI pass (headings bold, code dim, lists bulleted).
func renderMarkdown(p *output.Printer, md string) string {
	var b strings.Builder
	inFence := false
	for _, line := range strings.Split(md, "\n") {
		switch {
		case strings.HasPrefix(line, "```"):
			inFence = !inFence
			continue
		case inFence:
			b.WriteString("    " + line + "\n")
		case strings.HasPrefix(line, "#"):
			t := strings.TrimLeft(line, "# ")
			b.WriteString(p.Bold(t) + "\n")
		case strings.HasPrefix(strings.TrimSpace(line), "- "), strings.HasPrefix(strings.TrimSpace(line), "* "):
			b.WriteString("  • " + strings.TrimSpace(line)[2:] + "\n")
		default:
			b.WriteString(line + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func init() {
	f := viewCmd.Flags()
	f.StringVar(&viewVersion, "version", "", "version to view (default latest)")
	f.BoolVar(&viewVersions, "versions", false, "show version history instead")
	f.BoolVar(&viewFiles, "files", false, "show the file list instead")
	rootCmd.AddCommand(viewCmd)
}
