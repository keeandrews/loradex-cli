package cmd

import (
	"os"
	"path/filepath"

	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/keeandrews/loradex-cli/internal/pathsafe"
	"github.com/keeandrews/loradex-cli/internal/ref"
	"github.com/keeandrews/loradex-cli/internal/transfer"
	"github.com/spf13/cobra"
)

var (
	pullVersion        string
	pullOutput         string
	pullIncludeSamples bool
	pullForce          bool
)

var pullCmd = &cobra.Command{
	Use:   "pull <owner/repo>[@version]",
	Short: "Download model file(s) for use in a pipeline",
	Long: `Download the weights (and optionally samples) of a repo. No local project is
created. Every file is hash- and size-verified before being written.

Examples:
  loradex pull keenan/flux2-klein-portrait
  loradex pull bob/film-look@v2 -o ~/models/
  loradex pull keenan/flux2-klein-portrait --include-samples`,
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
				return output.Usage("specify owner/repo")
			}
			r.Owner = app.Handle
		}
		version, err := ref.ReconcileVersion(r.Version, pullVersion)
		if err != nil {
			return output.Usage("%v", err)
		}

		// include=weights by default; +samples adds preview images.
		include := "weights"
		if pullIncludeSamples {
			include = "weights,samples" // TODO(server-contract): confirm samples token
		}
		resp, err := app.Client.Download(cmd.Context(), r.Owner, r.Repo, version, include)
		if err != nil {
			return err
		}

		outDir := orDefault(pullOutput, ".")
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return err
		}
		var written []string
		for _, f := range resp.Files {
			dest, err := pathsafe.SafeJoin(outDir, f.Name)
			if err != nil {
				return output.Errorf(output.ExitValidation, "unsafe_path", "", "%v", err)
			}
			if !pullForce {
				if _, err := os.Lstat(dest); err == nil {
					return output.Errorf(output.ExitValidation, "exists", "use --force to overwrite", "%s already exists", rel(outDir, dest))
				}
			}
			if err := transfer.Download(cmd.Context(), app.Client.HTTP, f, dest, app.P); err != nil {
				return err
			}
			written = append(written, dest)
		}

		if g.json {
			rels := make([]string, len(written))
			for i, w := range written {
				rels[i] = rel(outDir, w)
			}
			return app.P.JSONOut(map[string]any{"version": resp.Version, "files": rels})
		}
		app.P.Success("Pulled %s/%s@%s", r.Owner, r.Repo, resp.Version)
		for _, w := range written {
			app.P.Printf("  %s\n", w)
		}
		return nil
	},
}

func rel(base, p string) string {
	if r, err := filepath.Rel(base, p); err == nil {
		return r
	}
	return filepath.Base(p)
}

func init() {
	f := pullCmd.Flags()
	f.StringVar(&pullVersion, "version", "", "version to pull (default latest)")
	f.StringVarP(&pullOutput, "output", "o", ".", "output directory")
	f.BoolVar(&pullIncludeSamples, "include-samples", false, "also download sample images")
	f.BoolVar(&pullForce, "force", false, "overwrite existing files")
	rootCmd.AddCommand(pullCmd)
}
