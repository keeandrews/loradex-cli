package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/keeandrews/loradex-cli/internal/catalog"
	"github.com/keeandrews/loradex-cli/internal/drawthings"
	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/keeandrews/loradex-cli/internal/project"
	"github.com/keeandrews/loradex-cli/internal/ref"
	"github.com/keeandrews/loradex-cli/internal/trainerreg"
	"github.com/keeandrews/loradex-cli/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	imPath, imBase, imName, imTrigger, imLicense string
	imRank                                       int
	imPrivate, imList                            bool
)

// drawThingsModelsDir returns the recorded/detected Draw Things Models folder.
func drawThingsModelsDir() string {
	return trainerreg.Detect(trainerreg.DrawThings).ModelsDir
}

// resolveDrawThingsPath resolves a bare filename against the Draw Things Models
// folder so users can `loradex import keenan_v4_1500_lora_f32.ckpt` without the
// long container path. An existing path is returned unchanged.
func resolveDrawThingsPath(arg string) string {
	if _, err := os.Stat(arg); err == nil {
		return arg
	}
	if strings.ContainsAny(arg, "/\\") {
		return arg // looks like a path already; let validation report it
	}
	dir := drawThingsModelsDir()
	if dir == "" {
		return arg
	}
	for _, cand := range []string{arg, arg + ".ckpt"} {
		p := filepath.Join(dir, cand)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return arg
}

// listDrawThingsLoRAs lists candidate LoRA checkpoints in the Draw Things dir.
func listDrawThingsLoRAs(p *output.Printer) error {
	dir := drawThingsModelsDir()
	if dir == "" {
		return output.Errorf(output.ExitValidation, "no_drawthings",
			"run `loradex setup` to detect Draw Things", "Draw Things models folder not found")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	type row struct {
		name string
		size int64
		lora bool
	}
	var rows []row
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ckpt") {
			continue
		}
		fi, _ := e.Info()
		rows = append(rows, row{e.Name(), sizeOf(fi), strings.Contains(strings.ToLower(e.Name()), "lora")})
	}
	if g.json {
		out := make([]map[string]any, 0, len(rows))
		for _, r := range rows {
			out = append(out, map[string]any{"name": r.name, "size": r.size, "is_lora": r.lora})
		}
		return p.JSONOut(map[string]any{"dir": dir, "files": out})
	}
	p.Info("Draw Things models in %s:", dir)
	tw := p.Table()
	for _, r := range rows {
		kind := "model"
		if r.lora {
			kind = "LoRA"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", r.name, output.HumanSize(r.size), kind)
	}
	tw.Flush()
	p.Info("import one: loradex import <name>")
	return nil
}

func sizeOf(fi os.FileInfo) int64 {
	if fi == nil {
		return 0
	}
	return fi.Size()
}

var importCmd = &cobra.Command{
	Use:   "import <draw-things.ckpt>",
	Short: "Import a Draw Things LoRA into the workspace, ready to push",
	Long: `Catalog a LoRA you trained in Draw Things (a .ckpt SQLite checkpoint) as a
loradex model version, ready to publish with ` + "`loradex push`" + `. The base model,
network rank, trigger, and name are auto-detected and can be overridden.

Draw Things checkpoints are kept in their native "drawthings" format — they are
published as-is, not converted.

Examples:
  loradex import ~/Library/Containers/com.liuliu.draw-things/Data/Documents/Models/keenan_v4_1500_lora_f32.ckpt
  loradex import ./my_lora.ckpt --base flux2-klein --trigger ohwxman --name keenan-v4`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		p := output.New(g.json, g.quiet, g.verbose, g.noColor)

		if imList {
			return listDrawThingsLoRAs(p)
		}
		if len(args) == 0 {
			return output.Usage("provide a Draw Things .ckpt path or name (or --list to see detected ones)")
		}
		// Resolve a bare filename against the recorded Draw Things models dir.
		src := resolveDrawThingsPath(args[0])

		// 1. Validate it's a Draw Things (SQLite) checkpoint.
		ok, err := drawthings.IsCheckpoint(src)
		if err != nil {
			return output.Errorf(output.ExitValidation, "not_found", "", "cannot read %q: %v", src, err)
		}
		if !ok {
			return output.Errorf(output.ExitValidation, "not_drawthings",
				"Draw Things LoRAs are SQLite .ckpt files; for safetensors use `loradex init --from`",
				"%q is not a Draw Things checkpoint (no SQLite header)", src)
		}

		// 2. Detect metadata, apply overrides.
		meta := drawthings.Detect(cmd.Context(), src)
		base := orDefault(imBase, meta.Base)
		if base == "" {
			return output.Usage("could not detect the base model — pass --base (e.g. flux2-klein)")
		}
		name := orDefault(imName, meta.Name)
		if name == "" {
			name = slugFromFilename(src)
		}
		if err := ref.ValidateSlug(name); err != nil {
			return output.Validation("name %q: %v (pass --name)", name, err)
		}
		trigger := orDefault(imTrigger, meta.Trigger)
		rank := imRank
		if rank == 0 {
			rank = meta.Rank
		}

		// 3. Locate the workspace (active project / CWD), or scaffold one in place.
		root, err := resolveWorkspaceRoot(imPath)
		if err != nil {
			root = orDefault(imPath, ".")
			if err := scaffoldWorkspace(root, base); err != nil {
				return err
			}
			p.Info("initialized workspace in %s", root)
		}

		// 4. Catalog the model (drawthings format, native weights).
		weightsName := filepath.Base(src)
		trig := []string{}
		if trigger != "" {
			trig = []string{trigger}
		}
		cat := &catalog.Catalog{
			Name: name, Visibility: importVisibility(), BaseModel: base, Format: "drawthings",
			License: orDefault(imLicense, "CreativeML-OpenRAIL-M"), Weights: weightsName,
			TriggerWords: trig, NetworkRank: rank, NetworkDim: rank, RecommendedWeight: 0.8, Tags: []string{},
		}
		if res := catalog.Validate(cat); !res.OK() {
			for _, e := range res.Errors {
				p.Info("  · %s", e.String())
			}
			return output.Errorf(output.ExitValidation, "invalid_catalog", "override with flags", "%d validation error(s)", len(res.Errors))
		}
		if err := os.MkdirAll(workspace.ModelDir(root, base), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(workspace.RepoYAMLPath(root, base), []byte(project.RenderCatalog(cat)), 0o644); err != nil {
			return err
		}
		_ = os.WriteFile(workspace.ReadmePath(root, base), []byte(project.RenderReadme(cat)), 0o644)

		// 5. Copy the checkpoint into a fresh version folder.
		v := workspace.NextVersion(root, base)
		vdir := workspace.VersionDir(root, base, v)
		if err := os.MkdirAll(vdir, 0o755); err != nil {
			return err
		}
		dst := filepath.Join(vdir, weightsName)
		size, err := streamCopy(src, dst)
		if err != nil {
			return err
		}
		_ = os.WriteFile(filepath.Join(vdir, "README.md"), []byte(project.RenderReadme(cat)), 0o644)
		writeImportProvenance(vdir, src, base, meta, rank, size)

		proj, _ := workspace.Load(root)
		proj.UpsertModel(base, name, v)
		_ = workspace.Save(root, proj)

		if g.json {
			return p.JSONOut(map[string]any{
				"name": name, "base": base, "version": v, "format": "drawthings",
				"weights": dst, "rank": rank, "trigger": trigger, "size": size,
			})
		}
		p.Success("Imported %s → models/%s/versions/%s/ (%s)", weightsName, base, v, output.HumanSize(size))
		p.Info("  base %s · rank %d · trigger %s · format drawthings", base, rank, dashOr(trigger))
		p.Info("next:")
		p.Printf("  loradex push models/%s\n", base)
		return nil
	},
}

func writeImportProvenance(vdir, src, base string, meta drawthings.Meta, rank int, size int64) {
	prov := map[string]any{
		"version": 1, "imported_from": "draw-things", "source_file": filepath.Base(src),
		"base_model": base, "format": "drawthings",
		"detected": map[string]any{"arch": meta.Arch, "base": meta.Base, "name": meta.Name, "trigger": meta.Trigger, "rank": meta.Rank},
		"network":  map[string]any{"type": "lora", "rank": rank},
		"output":   map[string]any{"file": filepath.Base(src), "size": size},
	}
	if data, err := yaml.Marshal(prov); err == nil {
		_ = os.WriteFile(filepath.Join(vdir, "training.yaml"), data, 0o644)
	}
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// slugFromFilename derives a model slug from a Draw Things filename, stripping
// the common trailing step/precision suffixes (e.g. _1500_lora_f32).
func slugFromFilename(path string) string {
	s := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	s = strings.ToLower(s)
	for _, suf := range []string{"_lora_f32", "_lora_f16", "_lora"} {
		s = strings.TrimSuffix(s, suf)
	}
	s = nonSlug.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func streamCopy(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return n, err
}

func importVisibility() string {
	if imPrivate {
		return "private"
	}
	return "public"
}

func dashOr(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func init() {
	f := importCmd.Flags()
	f.StringVar(&imPath, "path", ".", "workspace root (scaffolded if missing)")
	f.StringVar(&imBase, "base", "", "base model id (default: auto-detect)")
	f.StringVar(&imName, "name", "", "model/repo slug (default: auto-detect)")
	f.StringVar(&imTrigger, "trigger", "", "trigger token (default: auto-detect)")
	f.StringVar(&imLicense, "license", "", "license id")
	f.IntVar(&imRank, "rank", 0, "LoRA network rank (default: auto-detect)")
	f.BoolVar(&imPrivate, "private", false, "make the model private")
	f.BoolVar(&imList, "list", false, "list Draw Things LoRAs detected on this machine")
	rootCmd.AddCommand(importCmd)
}
