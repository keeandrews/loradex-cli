package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/keeandrews/loradex-cli/internal/convert"
	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/keeandrews/loradex-cli/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	convTo     string
	convFrom   string
	convOutput string
)

var convertCmd = &cobra.Command{
	Use:   "convert [file]",
	Short: "Convert a LoRA between formats (safetensors, MLX, diffusers, Draw Things)",
	Long: `Convert a LoRA file to one or more formats. With no file, pick one from the
active project interactively. Output is written next to the source under a
per-format subfolder, e.g. models/<base>/versions/vN/mlx/<file>.

Reliability: safetensors↔MLX are faithful; diffusers is a best-effort key remap;
Draw Things read/write are experimental (float32 LoRAs only).

Examples:
  loradex convert                                  # pick a file + targets
  loradex convert path/to/lora.safetensors --to mlx
  loradex convert lora.safetensors --to mlx,diffusers
  loradex convert keenan.ckpt --to safetensors     # Draw Things → safetensors`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		p := output.New(g.json, g.quiet, g.verbose, g.noColor)

		// 1. Resolve the source file (arg or interactive picker).
		src := ""
		if len(args) == 1 {
			src = args[0]
		} else {
			picked, err := pickConvertFile(p)
			if err != nil {
				return err
			}
			src = picked
		}
		if fi, err := os.Stat(src); err != nil || fi.IsDir() {
			return output.Errorf(output.ExitValidation, "no_file", "", "no such file: %s", src)
		}

		// 2. Source format.
		srcFmt := convert.Format(convFrom)
		if srcFmt == "" {
			f, err := convert.DetectFormat(src)
			if err != nil {
				return err
			}
			srcFmt = f
		}

		// 3. Target formats (flag or prompt).
		targets, err := resolveTargets(p, srcFmt)
		if err != nil {
			return err
		}

		// 4. Convert into per-format subfolders.
		outRoot := convOutput
		if outRoot == "" {
			outRoot = filepath.Dir(src)
		}
		base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
		results := map[string]string{}
		for _, t := range targets {
			outPath := filepath.Join(outRoot, string(t), base+t.Ext())
			p.Info("converting %s → %s …", filepath.Base(src), t)
			res, err := convert.Convert(cmd.Context(), src, srcFmt, t, outPath, p)
			if err != nil {
				return err
			}
			for _, w := range res.Warnings {
				p.Info("  note: %s", w)
			}
			results[string(t)] = outPath
			p.Success("%s → %s  (%d tensors, %s)", t, outPath, res.Tensors, res.Quality)
		}

		if g.json {
			return p.JSONOut(map[string]any{"source": src, "from": srcFmt, "outputs": results})
		}
		return nil
	},
}

// resolveTargets returns the requested target formats, prompting if --to is unset.
func resolveTargets(p *output.Printer, src convert.Format) ([]convert.Format, error) {
	raw := convTo
	if raw == "" && p.IsTTY() && !g.yes && !g.json {
		p.Info("convert to which format(s)? %s", formatList(src))
		fmt.Fprintf(p.Err, "  comma-separated [mlx]: ")
		sc := bufio.NewScanner(os.Stdin)
		if sc.Scan() {
			raw = strings.TrimSpace(sc.Text())
		}
	}
	if raw == "" {
		raw = "mlx"
	}
	var out []convert.Format
	for _, tok := range strings.Split(raw, ",") {
		tok = strings.ToLower(strings.TrimSpace(tok))
		if tok == "" {
			continue
		}
		f := convert.Format(tok)
		if !validTarget(f) {
			return nil, output.Usage("unknown target format %q (choose from %s)", tok, formatList(src))
		}
		if f == src {
			p.Info("note: skipping %s (same as the source format)", f)
			continue
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		return nil, output.Usage("no target format selected")
	}
	return out, nil
}

func validTarget(f convert.Format) bool {
	for _, t := range convert.Targets {
		if t == f {
			return true
		}
	}
	return false
}

func formatList(exclude convert.Format) string {
	var names []string
	for _, t := range convert.Targets {
		if t != exclude {
			names = append(names, string(t))
		}
	}
	return strings.Join(names, " | ")
}

// convFile is a convertible weights file discovered in a project.
type convFile struct {
	path, base, version string
}

// pickConvertFile lists convertible weights in the active project and prompts.
func pickConvertFile(p *output.Printer) (string, error) {
	root, err := resolveWorkspaceRoot("")
	if err != nil {
		return "", output.Errorf(output.ExitValidation, "no_source",
			"pass a file path, or run this inside a project", "no file given and no active project to pick from")
	}
	files := convertibleFiles(root)
	if len(files) == 0 {
		return "", output.Errorf(output.ExitValidation, "no_files",
			"build or import a LoRA first", "no convertible LoRA files found in this project")
	}
	if !p.IsTTY() || g.yes || g.json {
		return "", output.Usage("specify a file to convert (interactive picker needs a terminal)")
	}
	p.Info("")
	p.Info("  Convertible LoRAs in this project:")
	tw := p.Table()
	for i, f := range files {
		fmt.Fprintf(tw, "  %d\t%s/%s\t%s\n", i+1, f.base, f.version, filepath.Base(f.path))
	}
	tw.Flush()
	fmt.Fprintf(p.Err, "\n  pick a number: ")
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return "", output.Usage("no selection")
	}
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(sc.Text()), "%d", &n); err != nil || n < 1 || n > len(files) {
		return "", output.Usage("not a valid choice")
	}
	return files[n-1].path, nil
}

// convertibleLoRA extensions live at the top level of a version folder.
func convertibleFiles(root string) []convFile {
	var out []convFile
	for _, base := range workspace.DiscoverModels(root) {
		for _, v := range workspace.DiscoverVersions(root, base) {
			vdir := workspace.VersionDir(root, base, v)
			entries, err := os.ReadDir(vdir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				ext := strings.ToLower(filepath.Ext(e.Name()))
				if ext == ".safetensors" || ext == ".ckpt" {
					out = append(out, convFile{path: filepath.Join(vdir, e.Name()), base: base, version: v})
				}
			}
		}
	}
	return out
}

func init() {
	f := convertCmd.Flags()
	f.StringVar(&convTo, "to", "", "target format(s), comma-separated: mlx,diffusers,drawthings,safetensors")
	f.StringVar(&convFrom, "from", "", "override the detected source format")
	f.StringVar(&convOutput, "output", "", "output base dir (default: alongside the source)")
	rootCmd.AddCommand(convertCmd)
}
