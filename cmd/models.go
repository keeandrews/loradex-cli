package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/keeandrews/loradex-cli/internal/basemodel"
	"github.com/keeandrews/loradex-cli/internal/config"
	"github.com/keeandrews/loradex-cli/internal/hfcli"
	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	mAddID, mAddName, mAddArch, mAddRepo, mAddURL, mAddFormat, mAddSHA, mAddLicense string
	mAddGated                                                                       bool
	mAddSize                                                                        float64
)

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Browse and download base models for training",
	Long: `Browse a catalog of popular base models, download them into the loradex
models directory, and catalog your own.

  loradex models                 # interactive menu (or a list when non-interactive)
  loradex models list            # list every model + download status
  loradex models pull flux2-klein
  loradex models add             # catalog a custom model (the "other" option)
  loradex models rm <id>
  loradex models path <id>       # print the local path (for scripting)`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		p := output.New(g.json, g.quiet, g.verbose, g.noColor)
		if g.json || !p.IsTTY() {
			return runModelsList(p)
		}
		return runModelsMenu(cmd, p)
	},
}

var modelsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List base models and their download status",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runModelsList(output.New(g.json, g.quiet, g.verbose, g.noColor))
	},
}

var modelsPullCmd = &cobra.Command{
	Use:   "pull <id>...",
	Short: "Download one or more base models into the loradex models directory",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		p := output.New(g.json, g.quiet, g.verbose, g.noColor)
		for _, id := range args {
			if _, err := pullModel(cmd, p, id, mPullForce); err != nil {
				return err
			}
		}
		return nil
	},
}

var mPullForce bool

var modelsAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Catalog a custom base model (HuggingFace repo or direct https URL)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		p := output.New(g.json, g.quiet, g.verbose, g.noColor)
		c := config.CustomModel{
			ID: mAddID, Name: mAddName, Arch: mAddArch, Repo: mAddRepo, URL: mAddURL,
			Format: mAddFormat, SHA256: mAddSHA, License: mAddLicense, SizeGB: mAddSize, Gated: mAddGated,
		}
		if c.ID == "" { // interactive fill when no flags given
			if !p.IsTTY() {
				return output.Usage("provide --id (and --repo or --url) for non-interactive add")
			}
			c = promptCustomModel(p)
		}
		if err := basemodel.AddCustom(c); err != nil {
			return output.Validation("%v", err)
		}
		p.Success("cataloged custom model %q (source: %s)", c.ID, orDefault(c.Repo, c.URL))
		p.Printf("  loradex models pull %s\n", c.ID)
		return nil
	},
}

var modelsRmCmd = &cobra.Command{
	Use:   "rm <id>",
	Short: "Remove a downloaded model (and its custom catalog entry, if any)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		p := output.New(g.json, g.quiet, g.verbose, g.noColor)
		id := args[0]
		e, ok := basemodel.Find(id)
		if !ok {
			return output.Errorf(output.ExitNotFound, "not_found", "see `loradex models list`", "unknown model %q", id)
		}
		if basemodel.IsDownloaded(e) {
			if !g.yes && !confirm(p, fmt.Sprintf("Delete downloaded files for %q?", id)) {
				return output.Errorf(output.ExitError, "aborted", "", "aborted")
			}
			if err := basemodel.Remove(e); err != nil {
				return err
			}
			p.Success("removed downloaded files for %q", id)
		}
		if e.Custom {
			if _, err := basemodel.RemoveCustom(id); err != nil {
				return err
			}
			p.Success("removed custom catalog entry %q", id)
		}
		return nil
	},
}

var modelsPathCmd = &cobra.Command{
	Use:   "path <id>",
	Short: "Print the local path of a downloaded model",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		p := output.New(g.json, g.quiet, g.verbose, g.noColor)
		e, ok := basemodel.Find(args[0])
		if !ok {
			return output.Errorf(output.ExitNotFound, "not_found", "", "unknown model %q", args[0])
		}
		path, err := basemodel.LocalPath(e)
		if err != nil {
			return err
		}
		if !basemodel.IsDownloaded(e) {
			return output.Errorf(output.ExitNotFound, "not_downloaded", "run `loradex models pull "+args[0]+"`", "%q is not downloaded", args[0])
		}
		p.Printf("%s\n", path)
		return nil
	},
}

// --- shared logic ---

func runModelsList(p *output.Printer) error {
	all, err := basemodel.All()
	if err != nil {
		return err
	}
	if g.json {
		rows := make([]map[string]any, 0, len(all))
		for _, e := range all {
			rows = append(rows, map[string]any{
				"id": e.ID, "name": e.Name, "arch": e.Arch, "source": e.Source(),
				"format": e.Format, "gated": e.Gated, "size_gb": e.SizeGB,
				"custom": e.Custom, "downloaded": basemodel.IsDownloaded(e),
			})
		}
		return p.JSONOut(map[string]any{"models": rows})
	}
	printModelsTable(p, all)
	return nil
}

func printModelsTable(p *output.Printer, all []basemodel.Entry) {
	tw := p.Table()
	fmt.Fprintln(tw, "  #\tID\tARCH\tSIZE\tGATED\tSTATUS\tSOURCE")
	for i, e := range all {
		status := "—"
		if basemodel.IsDownloaded(e) {
			status = "downloaded"
		}
		gated := ""
		if e.Gated {
			gated = "gated"
		}
		size := "?"
		if e.SizeGB > 0 {
			size = fmt.Sprintf("~%gGB", e.SizeGB)
		}
		tag := e.ID
		if e.Custom {
			tag += " *"
		}
		fmt.Fprintf(tw, "  %d\t%s\t%s\t%s\t%s\t%s\t%s\n", i+1, tag, e.Arch, size, gated, status, e.Source())
	}
	tw.Flush()
	p.Info("  * = custom    download: loradex models pull <id>    add your own: loradex models add")
}

// runModelsMenu is the interactive browser invoked by `loradex models` / `--models`.
func runModelsMenu(cmd *cobra.Command, p *output.Printer) error {
	sc := bufio.NewScanner(os.Stdin)
	for {
		all, err := basemodel.All()
		if err != nil {
			return err
		}
		p.Info("")
		printModelsTable(p, all)
		fmt.Fprintf(p.Err, "\nEnter a number or model id to download, 'a' to add a custom model, 'q' to quit: ")
		if !sc.Scan() {
			return nil
		}
		in := strings.ToLower(strings.TrimSpace(sc.Text()))
		switch {
		case in == "" || in == "q" || in == "quit":
			return nil
		case in == "a" || in == "add":
			c := promptCustomModel(p)
			if err := basemodel.AddCustom(c); err != nil {
				p.Info("  error: %v", err)
				continue
			}
			p.Success("cataloged %q", c.ID)
		default:
			e, ok := resolveMenuChoice(in, all)
			if !ok {
				p.Info("  not a valid choice — enter a number (e.g. 3) or a model id (e.g. flux2-klein)")
				continue
			}
			if basemodel.IsDownloaded(e) {
				p.Info("  %q is already downloaded", e.ID)
				continue
			}
			if _, err := pullModel(cmd, p, e.ID, false); err != nil {
				p.Info("  error: %v", err)
			}
		}
	}
}

// resolveMenuChoice maps a menu line to a model entry. It accepts a 1-based
// index, a model id, or a pasted command like "loradex models pull flux2-klein"
// (the loradex/models/pull/download/get prefix tokens are ignored).
func resolveMenuChoice(in string, all []basemodel.Entry) (basemodel.Entry, bool) {
	fields := strings.Fields(in)
	for len(fields) > 0 {
		switch fields[0] {
		case "loradex", "models", "pull", "download", "get":
			fields = fields[1:]
		default:
			goto done
		}
	}
done:
	if len(fields) == 0 {
		return basemodel.Entry{}, false
	}
	tok := fields[0]
	if n, err := strconv.Atoi(tok); err == nil {
		if n >= 1 && n <= len(all) {
			return all[n-1], true
		}
		return basemodel.Entry{}, false
	}
	for _, e := range all {
		if strings.ToLower(e.ID) == tok {
			return e, true
		}
	}
	return basemodel.Entry{}, false
}

// ensureHFAuth makes sure the user is logged in to HuggingFace before pulling a
// gated model. Interactively it offers to run `hf auth login`; otherwise it
// returns a clear error rather than attempting a download that will 401.
func ensureHFAuth(cmd *cobra.Command, p *output.Printer, e basemodel.Entry) error {
	if !e.Gated || e.Repo == "" || hfcli.LoggedIn() {
		return nil
	}
	p.Info("%q is gated — it needs a (free) HuggingFace login:", e.ID)
	p.Info("  1. accept the license:  https://huggingface.co/%s", e.Repo)
	p.Info("  2. create a token:       https://huggingface.co/settings/tokens")
	interactive := p.IsTTY() && !g.yes && !g.json
	hf := hfcli.Detect()
	switch {
	case interactive && hf.Installed:
		if confirm(p, "Run `hf auth login` now?") {
			if err := hfcli.Login(cmd.Context()); err != nil {
				return err
			}
			if hfcli.LoggedIn() {
				return nil
			}
		}
	case interactive && !hf.Installed:
		p.Info("  the HuggingFace CLI isn't installed — run `loradex setup --install-hf` first")
	}
	return output.Errorf(output.ExitUnauth, "hf_not_authenticated",
		"log in to HuggingFace (accept the license + `hf auth login`), then re-run",
		"not logged in to HuggingFace — cannot pull gated model %q", e.ID)
}

// pullModel downloads a registry entry by id and reports the result. With force,
// any existing copy is removed first; otherwise a complete copy is reused and a
// partial one resumes.
func pullModel(cmd *cobra.Command, p *output.Printer, id string, force bool) (string, error) {
	e, ok := basemodel.Find(id)
	if !ok {
		return "", output.Errorf(output.ExitNotFound, "not_found", "see `loradex models list`", "unknown model %q", id)
	}
	switch {
	case force:
		if err := basemodel.Remove(e); err != nil {
			return "", err
		}
		p.Info("removed existing copy of %q — re-downloading", id)
	case basemodel.IsDownloaded(e):
		path, _ := basemodel.LocalPath(e)
		// Offer to re-download: delete the existing copy first, with confirmation.
		if p.IsTTY() && !g.yes && !g.json {
			if !confirm(p, fmt.Sprintf("%q is already downloaded. Delete it and download again?", id)) {
				p.Info("keeping existing copy: %s", path)
				return path, nil
			}
			if err := basemodel.Remove(e); err != nil {
				return "", err
			}
			p.Info("removed existing copy — re-downloading")
		} else {
			p.Info("%q already downloaded: %s (use --force to re-download)", id, path)
			return path, nil
		}
	case basemodel.PartiallyDownloaded(e):
		p.Info("resuming a previous partial download of %q…", id)
	}
	if err := ensureHFAuth(cmd, p, e); err != nil {
		return "", err
	}
	_, python := trainerLocation()
	p.Info("downloading %s (%s, ~%gGB)…", e.Name, e.Source(), e.SizeGB)
	path, err := basemodel.Download(cmd.Context(), e, python, p)
	if err != nil {
		return "", err
	}
	p.Success("downloaded %q → %s", id, path)
	switch {
	case basemodel.IsKnownBase(e.ID):
		// id is itself a trainable base — build auto-resolves the local copy.
		p.Printf("  use it:  loradex build ./images --base %s\n", e.ID)
	case basemodel.BaseForArch(e.Arch) != "":
		// custom model with a known architecture: pick its base + point at the file.
		p.Printf("  use it:  loradex build ./images --base %s --checkpoint %s\n", basemodel.BaseForArch(e.Arch), path)
	default:
		p.Printf("  use it:  loradex build ./images --base <base> --checkpoint %s\n", path)
	}
	return path, nil
}

// promptCustomModel collects a custom model definition interactively.
func promptCustomModel(p *output.Printer) config.CustomModel {
	sc := bufio.NewScanner(os.Stdin)
	ask := func(label, def string) string {
		if def != "" {
			fmt.Fprintf(p.Err, "  %s [%s]: ", label, def)
		} else {
			fmt.Fprintf(p.Err, "  %s: ", label)
		}
		if !sc.Scan() {
			return def
		}
		v := strings.TrimSpace(sc.Text())
		if v == "" {
			return def
		}
		return v
	}
	p.Info("Catalog a custom base model. Provide a HuggingFace repo OR a direct https URL.")
	c := config.CustomModel{}
	c.ID = ask("id (slug, e.g. my-flux)", "")
	c.Name = ask("display name", c.ID)
	c.Arch = ask("arch ("+strings.Join(basemodel.KnownArchs, "/")+")", "other")
	c.Repo = ask("HuggingFace repo id (blank if using a URL)", "")
	if c.Repo == "" {
		c.URL = ask("direct https URL", "")
		c.SHA256 = ask("sha256 (optional, recommended)", "")
	}
	c.Format = ask("format (diffusers/safetensors)", defaultFormat(c))
	c.License = ask("license (optional)", "")
	return c
}

func defaultFormat(c config.CustomModel) string {
	if c.Repo != "" {
		return "diffusers"
	}
	return "safetensors"
}

func init() {
	f := modelsAddCmd.Flags()
	f.StringVar(&mAddID, "id", "", "unique slug / base id")
	f.StringVar(&mAddName, "name", "", "display name")
	f.StringVar(&mAddArch, "arch", "", "flux2 | flux1 | sdxl | sd15 | other")
	f.StringVar(&mAddRepo, "repo", "", "HuggingFace repo id")
	f.StringVar(&mAddURL, "url", "", "direct https URL (single file)")
	f.StringVar(&mAddFormat, "format", "", "diffusers | safetensors")
	f.StringVar(&mAddSHA, "sha256", "", "integrity digest for URL downloads")
	f.StringVar(&mAddLicense, "license", "", "license id")
	f.Float64Var(&mAddSize, "size-gb", 0, "approximate size in GB")
	f.BoolVar(&mAddGated, "gated", false, "requires HuggingFace auth")

	modelsPullCmd.Flags().BoolVar(&mPullForce, "force", false, "remove any existing copy and re-download from scratch")
	modelsCmd.AddCommand(modelsListCmd, modelsPullCmd, modelsAddCmd, modelsRmCmd, modelsPathCmd)
	rootCmd.AddCommand(modelsCmd)
}
