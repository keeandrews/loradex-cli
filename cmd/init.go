package cmd

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/keeandrews/loradex-cli/internal/catalog"
	"github.com/keeandrews/loradex-cli/internal/config"
	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/keeandrews/loradex-cli/internal/project"
	"github.com/keeandrews/loradex-cli/internal/ref"
	"github.com/keeandrews/loradex-cli/internal/safetensors"
	"github.com/keeandrews/loradex-cli/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	initName    string
	initBase    string
	initDesc    string
	initTrigger string
	initFormat  string
	initLicense string
	initPrivate bool
	initFrom    string
	initForce   bool
	initHere    bool
)

var initCmd = &cobra.Command{
	Use:   "init [name]",
	Short: "Create a project (a managed workspace) via a short wizard",
	Long: `Create a loradex project. By default the project is managed under
~/.loradex/projects/<name>/ (alongside everything else loradex owns) and becomes
your active project, so build/import/push/convert work from anywhere.

Run with no flags for an interactive wizard, or pass flags to skip it. Use
--here to scaffold a workspace in the current directory instead.

Examples:
  loradex init                                   # wizard
  loradex init my-portraits --base flux2-klein   # named, skip most prompts
  loradex init . --here --base sdxl              # workspace in the current dir`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		p := output.New(g.json, g.quiet, g.verbose, g.noColor)
		if initHere {
			return initInPlace(p, args)
		}
		return initManagedProject(p, args)
	},
}

// initManagedProject scaffolds a project under ~/.loradex/projects and makes it active.
func initManagedProject(p *output.Printer, args []string) error {
	in := projectInputs{
		name: initName, base: initBase, desc: initDesc, trigger: initTrigger,
		visibility: visFromFlags(), license: initLicense,
	}
	if len(args) == 1 {
		in.name = args[0]
	}
	interactive := p.IsTTY() && !g.yes && !g.json
	if interactive {
		runProjectWizard(p, &in)
	}
	if in.name == "" {
		return output.Usage("a project name is required (e.g. `loradex init my-portraits`)")
	}
	slug := slugify(in.name)
	if err := ref.ValidateSlug(slug); err != nil {
		return output.Validation("project name %q: %v", in.name, err)
	}

	pd, err := config.ProjectsDir()
	if err != nil {
		return err
	}
	root := filepath.Join(pd, slug)
	if workspace.IsWorkspace(root) && !initForce {
		return output.Errorf(output.ExitValidation, "exists", "use --force or `loradex use "+slug+"`", "project %q already exists", slug)
	}
	if err := scaffoldWorkspace(root, in.base); err != nil {
		return err
	}
	if in.base != "" {
		if err := scaffoldFirstModel(root, slug, in); err != nil {
			return err
		}
	}
	if err := config.SetCurrentProject(slug); err != nil {
		return err
	}

	if g.json {
		return p.JSONOut(map[string]any{"project": slug, "root": root, "base": in.base, "active": true})
	}
	p.Success("Created project %q → %s", slug, root)
	p.Info("  it's now your active project (use `loradex use <name>` to switch)")
	p.Info("next:")
	if in.base != "" {
		p.Printf("  loradex build ./images --base %s --trigger %s\n", in.base, dashOr(in.trigger))
	} else {
		p.Info("  loradex build ./images --base flux2-klein --trigger <word>")
	}
	return nil
}

// initInPlace scaffolds a workspace in the current directory (back-compat).
func initInPlace(p *output.Printer, args []string) error {
	dir := "."
	if len(args) == 1 {
		dir = args[0]
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if workspace.IsWorkspace(abs) && !initForce {
		return output.Errorf(output.ExitValidation, "exists", "use --force to overwrite", "%s is already a workspace", dir)
	}
	if err := scaffoldWorkspace(abs, initBase); err != nil {
		return err
	}
	if initBase != "" || initName != "" {
		slug := orDefault(initName, filepath.Base(abs)+"-"+orDefault(initBase, "flux2-klein"))
		if err := scaffoldFirstModel(abs, slug, projectInputs{
			name: slug, base: orDefault(initBase, "flux2-klein"), trigger: initTrigger,
			visibility: visFromFlags(), license: initLicense,
		}); err != nil {
			return err
		}
	}
	p.Success("Initialized loradex workspace %q in %s", filepath.Base(abs), dir)
	p.Info("next:")
	p.Info("  loradex build ./images --base %s", orDefault(initBase, "flux2-klein"))
	return nil
}

// projectInputs holds the wizard-collected project settings.
type projectInputs struct {
	name, base, desc, trigger, visibility, license string
}

// runProjectWizard fills any blank fields interactively.
func runProjectWizard(p *output.Printer, in *projectInputs) {
	sc := bufio.NewScanner(os.Stdin)
	ask := func(label, def string) string {
		if def != "" {
			p.Printf("  %s [%s]: ", label, def)
		} else {
			p.Printf("  %s: ", label)
		}
		if !sc.Scan() {
			return def
		}
		if v := strings.TrimSpace(sc.Text()); v != "" {
			return v
		}
		return def
	}
	p.Info("")
	p.Info("  New loradex project")
	p.Info("  ───────────────────────────────────────────")
	in.name = ask("project name", in.name)
	in.desc = ask("description (optional)", in.desc)
	in.base = ask("base model (flux2-klein | flux1 | sdxl | sd15)", orDefault(in.base, "flux2-klein"))
	in.trigger = ask("trigger word (optional)", in.trigger)
	if in.visibility == "" {
		in.visibility = "public"
	}
	in.visibility = ask("visibility (public | private)", in.visibility)
	p.Info("  ───────────────────────────────────────────")
}

// scaffoldFirstModel creates the first models/<base> repo for a project.
func scaffoldFirstModel(root, slug string, in projectInputs) error {
	base := in.base
	name := slug
	if err := ref.ValidateSlug(name); err != nil {
		return output.Validation("%v", err)
	}
	trig := []string{}
	if in.trigger != "" {
		trig = []string{in.trigger}
	}
	cat := &catalog.Catalog{
		Name: name, Description: in.desc, Visibility: orDefault(in.visibility, "public"),
		BaseModel: base, Format: orDefault(initFormat, "safetensors"),
		License: orDefault(in.license, "CreativeML-OpenRAIL-M"), Weights: name + ".safetensors",
		TriggerWords: trig, Tags: []string{}, RecommendedWeight: 0.8,
	}
	if initFrom != "" {
		if h, err := readHeaderFile(initFrom); err == nil {
			cat.NetworkRank, cat.NetworkDim = h.NetworkRank, h.NetworkDim
		}
	}
	if err := os.MkdirAll(workspace.ModelDir(root, base), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(workspace.RepoYAMLPath(root, base), []byte(project.RenderCatalog(cat)), 0o644); err != nil {
		return err
	}
	_ = os.WriteFile(workspace.ReadmePath(root, base), []byte(project.RenderReadme(cat)), 0o644)

	if initFrom != "" {
		vdir := workspace.VersionDir(root, base, "v1")
		if err := os.MkdirAll(vdir, 0o755); err != nil {
			return err
		}
		if err := copyInto(initFrom, filepath.Join(vdir, cat.Weights)); err != nil {
			return err
		}
		_ = os.WriteFile(filepath.Join(vdir, "README.md"), []byte(project.RenderReadme(cat)), 0o644)
		proj, _ := workspace.Load(root)
		proj.UpsertModel(base, name, "v1")
		_ = workspace.Save(root, proj)
	}
	return nil
}

// scaffoldWorkspace creates the base workspace skeleton.
func scaffoldWorkspace(root, defaultBase string) error {
	if err := os.MkdirAll(workspace.DatasetDir(root), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(workspace.ModelsDir(root), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(workspace.DotDir(root), 0o755); err != nil {
		return err
	}
	_ = os.WriteFile(filepath.Join(workspace.DotDir(root), ".gitignore"), []byte(workspace.CacheGitignore), 0o644)
	return workspace.Save(root, &workspace.Project{
		Version: 1, Name: filepath.Base(root), DefaultBase: defaultBase,
		DefaultTrainer: "ai-toolkit", CreatedAt: workspace.Now(), Models: []workspace.ModelEntry{},
	})
}

func visFromFlags() string {
	if initPrivate {
		return "private"
	}
	return "public"
}

func readHeaderFile(path string) (safetensors.Header, error) {
	f, err := os.Open(path)
	if err != nil {
		return safetensors.Header{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return safetensors.Header{}, err
	}
	return safetensors.ReadHeader(f, fi.Size())
}

func copyInto(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// slugify lowercases and hyphenates a free-text name into a safe slug.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonSlug.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func init() {
	f := initCmd.Flags()
	f.StringVar(&initName, "name", "", "project name/slug")
	f.StringVar(&initBase, "base", "", "base model for the first model")
	f.StringVar(&initDesc, "description", "", "project description")
	f.StringVar(&initTrigger, "trigger", "", "trigger word")
	f.StringVar(&initFormat, "format", "", "format")
	f.StringVar(&initLicense, "license", "", "license id")
	f.BoolVar(&initPrivate, "private", false, "make the first model private")
	f.Bool("public", true, "make the first model public (default)")
	f.StringVar(&initFrom, "from", "", "import an existing .safetensors as v1")
	f.BoolVar(&initForce, "force", false, "overwrite an existing project/workspace")
	f.BoolVar(&initHere, "here", false, "scaffold a workspace in the current directory instead of the managed projects folder")
	rootCmd.AddCommand(initCmd)
}
