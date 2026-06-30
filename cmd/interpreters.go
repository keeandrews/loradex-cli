package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/keeandrews/loradex-cli/internal/config"
	"github.com/keeandrews/loradex-cli/internal/interpreter"
	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/spf13/cobra"
)

var interpretersCmd = &cobra.Command{
	Use:     "interpreters",
	Aliases: []string{"interpreter"},
	Short:   "Browse and download caption models (vision-language interpreters)",
	Long: `Caption models ("interpreters") describe your dataset images before training,
so each photo gets a detailed caption. Pull one (downloaded to ~/.loradex/
interpreters); the first one you pull becomes your default captioner.

  loradex interpreters                 # interactive browser (or a list)
  loradex interpreters list
  loradex interpreters pull qwen3-vl-4b
  loradex interpreters add --id my-vlm --repo org/My-VLM
  loradex interpreters rm <id>
  loradex interpreters path <id>`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		p := output.New(g.json, g.quiet, g.verbose, g.noColor)
		if g.json || !p.IsTTY() {
			return runInterpretersList(p)
		}
		return runInterpretersMenu(cmd, p)
	},
}

var interpretersListCmd = &cobra.Command{
	Use: "list", Short: "List caption models and their download status", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInterpretersList(output.New(g.json, g.quiet, g.verbose, g.noColor))
	},
}

var interpretersPullCmd = &cobra.Command{
	Use: "pull <id>...", Short: "Download caption model(s) into ~/.loradex/interpreters", Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		p := output.New(g.json, g.quiet, g.verbose, g.noColor)
		for _, id := range args {
			if _, err := pullInterpreter(cmd, p, id, intPullForce); err != nil {
				return err
			}
		}
		return nil
	},
}

var intPullForce bool

var interpretersAddCmd = &cobra.Command{
	Use: "add", Short: "Catalog a custom caption model (HuggingFace repo)", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		p := output.New(g.json, g.quiet, g.verbose, g.noColor)
		c := config.CustomModel{ID: intAddID, Name: intAddName, Repo: intAddRepo, SizeGB: intAddSize, Gated: intAddGated}
		if c.ID == "" {
			if !p.IsTTY() {
				return output.Usage("provide --id and --repo for non-interactive add")
			}
			c = promptCustomInterpreter(p)
		}
		if err := interpreter.AddCustom(c); err != nil {
			return output.Validation("%v", err)
		}
		p.Success("cataloged interpreter %q (%s)", c.ID, c.Repo)
		p.Printf("  loradex interpreters pull %s\n", c.ID)
		return nil
	},
}

var interpretersRmCmd = &cobra.Command{
	Use: "rm <id>", Short: "Remove a downloaded caption model (+ custom entry)", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		p := output.New(g.json, g.quiet, g.verbose, g.noColor)
		id := args[0]
		e, ok := interpreter.Find(id)
		if !ok {
			return output.Errorf(output.ExitNotFound, "not_found", "see `loradex interpreters list`", "unknown interpreter %q", id)
		}
		if interpreter.IsDownloaded(e) {
			if !g.yes && !confirm(p, fmt.Sprintf("Delete downloaded files for %q?", id)) {
				return output.Errorf(output.ExitError, "aborted", "", "aborted")
			}
			if err := interpreter.Remove(e); err != nil {
				return err
			}
			p.Success("removed downloaded files for %q", id)
		}
		if e.Custom {
			if _, err := interpreter.RemoveCustom(id); err != nil {
				return err
			}
			p.Success("removed custom catalog entry %q", id)
		}
		return nil
	},
}

var interpretersPathCmd = &cobra.Command{
	Use: "path <id>", Short: "Print the local path of a downloaded caption model", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		p := output.New(g.json, g.quiet, g.verbose, g.noColor)
		e, ok := interpreter.Find(args[0])
		if !ok {
			return output.Errorf(output.ExitNotFound, "not_found", "", "unknown interpreter %q", args[0])
		}
		if !interpreter.IsDownloaded(e) {
			return output.Errorf(output.ExitNotFound, "not_downloaded", "run `loradex interpreters pull "+args[0]+"`", "%q is not downloaded", args[0])
		}
		path, _ := interpreter.LocalPath(e)
		p.Printf("%s\n", path)
		return nil
	},
}

func runInterpretersList(p *output.Printer) error {
	all, err := interpreter.All()
	if err != nil {
		return err
	}
	f, _ := config.Load()
	def := f.DefaultInterpreter
	if g.json {
		rows := make([]map[string]any, 0, len(all))
		for _, e := range all {
			rows = append(rows, map[string]any{
				"id": e.ID, "name": e.Name, "repo": e.Repo, "size_gb": e.SizeGB,
				"downloaded": interpreter.IsDownloaded(e), "default": e.ID == def, "custom": e.Custom,
			})
		}
		return p.JSONOut(map[string]any{"interpreters": rows, "default": def})
	}
	printInterpretersTable(p, all, def)
	return nil
}

func printInterpretersTable(p *output.Printer, all []interpreter.Entry, def string) {
	tw := p.Table()
	fmt.Fprintln(tw, "  #\tID\tSIZE\tSTATUS\tREPO")
	for i, e := range all {
		status := "—"
		if interpreter.IsDownloaded(e) {
			status = "downloaded"
		}
		tag := e.ID
		if e.ID == def {
			tag += " (default)"
		}
		if e.Custom {
			tag += " *"
		}
		size := "?"
		if e.SizeGB > 0 {
			size = fmt.Sprintf("~%gGB", e.SizeGB)
		}
		fmt.Fprintf(tw, "  %d\t%s\t%s\t%s\t%s\n", i+1, tag, size, status, e.Repo)
	}
	tw.Flush()
	p.Info("  download: loradex interpreters pull <id>   ·   set default: loradex config set default-interpreter <id>")
}

func runInterpretersMenu(cmd *cobra.Command, p *output.Printer) error {
	sc := bufio.NewScanner(os.Stdin)
	for {
		all, err := interpreter.All()
		if err != nil {
			return err
		}
		f, _ := config.Load()
		p.Info("")
		printInterpretersTable(p, all, f.DefaultInterpreter)
		fmt.Fprintf(p.Err, "\nEnter a number or id to download, 'q' to quit: ")
		if !sc.Scan() {
			return nil
		}
		in := strings.ToLower(strings.TrimSpace(sc.Text()))
		if in == "" || in == "q" || in == "quit" {
			return nil
		}
		var e interpreter.Entry
		ok := false
		if n, err := strconv.Atoi(in); err == nil && n >= 1 && n <= len(all) {
			e, ok = all[n-1], true
		} else {
			e, ok = interpreter.Find(in)
		}
		if !ok {
			p.Info("  not a valid choice")
			continue
		}
		if interpreter.IsDownloaded(e) {
			p.Info("  %q is already downloaded", e.ID)
			continue
		}
		if _, err := pullInterpreter(cmd, p, e.ID, false); err != nil {
			p.Info("  error: %v", err)
		}
	}
}

// pullInterpreter downloads an interpreter and makes it the default if none is set.
func pullInterpreter(cmd *cobra.Command, p *output.Printer, id string, force bool) (string, error) {
	e, ok := interpreter.Find(id)
	if !ok {
		return "", output.Errorf(output.ExitNotFound, "not_found", "see `loradex interpreters list`", "unknown interpreter %q", id)
	}
	switch {
	case force:
		if err := interpreter.Remove(e); err != nil {
			return "", err
		}
		p.Info("removed existing copy of %q — re-downloading", id)
	case interpreter.IsDownloaded(e):
		path, _ := interpreter.LocalPath(e)
		p.Info("%q already downloaded: %s", id, path)
		setDefaultInterpreterIfUnset(p, id)
		return path, nil
	case interpreter.PartiallyDownloaded(e):
		p.Info("resuming a previous partial download of %q…", id)
	}
	_, python := trainerLocation()
	p.Info("downloading %s (%s, ~%gGB)…", e.Name, e.Repo, e.SizeGB)
	path, err := interpreter.Download(cmd.Context(), e, python, p)
	if err != nil {
		return "", err
	}
	p.Success("downloaded %q → %s", id, path)
	setDefaultInterpreterIfUnset(p, id)
	return path, nil
}

// setDefaultInterpreterIfUnset makes id the default captioner when none is set.
func setDefaultInterpreterIfUnset(p *output.Printer, id string) {
	f, err := config.Load()
	if err != nil || f.DefaultInterpreter != "" {
		return
	}
	f.DefaultInterpreter = id
	if config.Save(f) == nil {
		p.Info("  set as your default captioner (change with `loradex config set default-interpreter <id>`)")
	}
}

func promptCustomInterpreter(p *output.Printer) config.CustomModel {
	sc := bufio.NewScanner(os.Stdin)
	ask := func(label string) string {
		fmt.Fprintf(p.Err, "  %s: ", label)
		if sc.Scan() {
			return strings.TrimSpace(sc.Text())
		}
		return ""
	}
	p.Info("Catalog a custom caption model (a HuggingFace vision-language repo).")
	c := config.CustomModel{}
	c.ID = ask("id (slug, e.g. my-vlm)")
	c.Name = ask("display name")
	c.Repo = ask("HuggingFace repo id")
	return c
}

var (
	intAddID, intAddName, intAddRepo string
	intAddSize                       float64
	intAddGated                      bool
)

func init() {
	af := interpretersAddCmd.Flags()
	af.StringVar(&intAddID, "id", "", "unique slug / interpreter id")
	af.StringVar(&intAddName, "name", "", "display name")
	af.StringVar(&intAddRepo, "repo", "", "HuggingFace repo id")
	af.Float64Var(&intAddSize, "size-gb", 0, "approximate size in GB")
	af.BoolVar(&intAddGated, "gated", false, "requires HuggingFace auth")
	interpretersPullCmd.Flags().BoolVar(&intPullForce, "force", false, "remove any existing copy and re-download")
	interpretersCmd.AddCommand(interpretersListCmd, interpretersPullCmd, interpretersAddCmd, interpretersRmCmd, interpretersPathCmd)
	rootCmd.AddCommand(interpretersCmd)
}
