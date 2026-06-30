package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/keeandrews/loradex-cli/internal/api"
	"github.com/keeandrews/loradex-cli/internal/catalog"
	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/keeandrews/loradex-cli/internal/pathsafe"
	"github.com/keeandrews/loradex-cli/internal/project"
	"github.com/keeandrews/loradex-cli/internal/ref"
	"github.com/keeandrews/loradex-cli/internal/transfer"
	"github.com/spf13/cobra"
)

var (
	cloneVersion   string
	cloneOutput    string
	cloneNoSamples bool
	cloneForce     bool
)

var cloneCmd = &cobra.Command{
	Use:   "clone <owner/repo>[@version] [dir]",
	Short: "Create a full, editable working copy",
	Long: `Clone a repo into an editable working copy (weights, README, loradex.yaml,
samples, and a .loradex/ linked to the remote) so you can edit and push.

Examples:
  loradex clone keenan/flux2-klein-portrait
  loradex clone bob/film-look@v2 ./film-look --no-samples`,
	Args: cobra.RangeArgs(1, 2),
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
		version, err := ref.ReconcileVersion(r.Version, cloneVersion)
		if err != nil {
			return output.Usage("%v", err)
		}
		ctx := cmd.Context()

		outDir := cloneOutput
		if len(args) == 2 {
			outDir = args[1]
		}
		if outDir == "" {
			outDir = "./" + r.Repo
		}
		if err := ensureEmptyDir(outDir, cloneForce); err != nil {
			return err
		}

		repo, err := app.Client.GetRepo(ctx, r.Owner, r.Repo)
		if err != nil {
			return err
		}
		resp, err := app.Client.Download(ctx, r.Owner, r.Repo, version, "all")
		if err != nil {
			return err
		}

		weightsName, weightsSHA := "", ""
		for _, f := range resp.Files {
			if cloneNoSamples && strings.HasPrefix(filepath.ToSlash(f.Name), "samples/") {
				continue
			}
			dest, err := pathsafe.SafeJoin(outDir, f.Name)
			if err != nil {
				return output.Errorf(output.ExitValidation, "unsafe_path", "", "%v", err)
			}
			if err := transfer.Download(ctx, app.Client.HTTP, f, dest, app.P); err != nil {
				return err
			}
			if strings.HasSuffix(f.Name, ".safetensors") || strings.HasSuffix(f.Name, ".ckpt") {
				weightsName, weightsSHA = f.Name, f.SHA256
			}
		}

		// Reconstruct loradex.yaml from metadata (only if the server didn't ship one).
		if _, err := os.Stat(project.CatalogPath(outDir)); err != nil {
			cat := repoToCatalog(repo, weightsName)
			_ = os.WriteFile(project.CatalogPath(outDir), []byte(project.RenderCatalog(cat)), 0o644)
		}

		// Link .loradex/ to the remote.
		cfg := &project.Config{Version: 1, Endpoint: app.Endpoint, Owner: r.Owner, Repo: r.Repo}
		if weightsSHA != "" {
			cfg.LastPush = &project.LastPush{Version: resp.Version, SHA256: weightsSHA, PushedAt: time.Now().UTC().Format(time.RFC3339)}
		}
		if err := project.SaveConfig(outDir, cfg); err != nil {
			return err
		}

		if g.json {
			return app.P.JSONOut(map[string]any{"owner": r.Owner, "repo": r.Repo, "version": resp.Version, "dir": outDir})
		}
		app.P.Success("Cloned %s/%s@%s into %s", r.Owner, r.Repo, resp.Version, outDir)
		app.P.Info("edit loradex.yaml / README.md, then `loradex push`")
		return nil
	},
}

func repoToCatalog(r *api.Repo, weightsName string) *catalog.Catalog {
	if weightsName == "" {
		weightsName = r.Name + ".safetensors"
	}
	return &catalog.Catalog{
		Name: r.Name, Description: r.Description, Visibility: r.Visibility,
		BaseModel: r.BaseModel, Format: r.Format, License: r.License,
		Weights: weightsName, TriggerWords: r.TriggerWords,
		NetworkRank: r.NetworkRank, NetworkDim: r.NetworkDim,
		RecommendedWeight: r.RecommendedWeight, Tags: r.Tags,
	}
}

func ensureEmptyDir(dir string, force bool) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) > 0 && !force {
		return output.Errorf(output.ExitValidation, "not_empty", "use --force to clone into it", "%s is not empty", dir)
	}
	return nil
}

func init() {
	f := cloneCmd.Flags()
	f.StringVar(&cloneVersion, "version", "", "version to clone (default latest)")
	f.StringVarP(&cloneOutput, "output", "o", "", "output directory (default ./<repo>)")
	f.BoolVar(&cloneNoSamples, "no-samples", false, "skip sample images")
	f.BoolVar(&cloneForce, "force", false, "clone into a non-empty directory")
	rootCmd.AddCommand(cloneCmd)
}
