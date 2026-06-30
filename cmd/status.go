package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/keeandrews/loradex-cli/internal/catalog"
	"github.com/keeandrews/loradex-cli/internal/transfer"
	"github.com/keeandrews/loradex-cli/internal/workspace"
	"github.com/spf13/cobra"
)

var statusPath string

type versionStatus struct {
	Version       string `json:"version"`
	Pushed        bool   `json:"pushed"`
	RemoteVersion string `json:"remote_version,omitempty"`
	Changed       bool   `json:"changed_since_push"`
}

type modelStatus struct {
	Base         string          `json:"base"`
	Slug         string          `json:"slug"`
	RemoteLatest string          `json:"remote_latest,omitempty"`
	Versions     []versionStatus `json:"versions"`
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show workspace models, versions, and push state",
	Long: `Inside a workspace, list models and their versions, which are pushed vs
local-only, and whether a version changed since it was pushed.

Examples:
  loradex status
  loradex status --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		app, err := newApp()
		if err != nil {
			return err
		}
		root, err := resolveWorkspaceRoot(statusPath)
		if err != nil {
			return err
		}
		proj, err := workspace.Load(root)
		if err != nil {
			return err
		}

		var models []modelStatus
		for _, base := range workspace.DiscoverModels(root) {
			cat, _ := catalog.Load(workspace.RepoYAMLPath(root, base))
			slug := base
			if cat != nil {
				slug = cat.Name
			}
			ms := modelStatus{Base: base, Slug: slug}

			if app.HasToken {
				if repo, err := app.Client.GetRepo(cmd.Context(), app.Handle, slug); err == nil {
					ms.RemoteLatest = repo.LatestVersion
				}
			}
			for _, v := range workspace.DiscoverVersions(root, base) {
				vdir := workspace.VersionDir(root, base, v)
				vs := versionStatus{Version: v}
				pushed := readPushed(vdir)
				if pushed != nil {
					vs.Pushed = true
					vs.RemoteVersion = pushed["remote_version"]
					if local := hashVersionWeights(vdir, cat); local != "" && local != pushed["sha256"] {
						vs.Changed = true
					}
				}
				ms.Versions = append(ms.Versions, vs)
			}
			models = append(models, ms)
		}

		if g.json {
			return app.P.JSONOut(map[string]any{"project": proj.Name, "endpoint": app.Endpoint, "models": models})
		}
		app.P.Printf("%s  (%s)\n", proj.Name, app.Endpoint)
		if len(models) == 0 {
			app.P.Printf("no models yet — run `loradex build`\n")
			return nil
		}
		for _, m := range models {
			app.P.Printf("\n%s  →  %s\n", app.P.Bold(m.Base), m.Slug)
			for _, v := range m.Versions {
				switch {
				case !v.Pushed:
					app.P.Printf("  %-5s local-only (not pushed)\n", v.Version)
				case v.Changed:
					app.P.Printf("  %-5s pushed as %s · changed since push\n", v.Version, v.RemoteVersion)
				default:
					app.P.Printf("  %-5s pushed as %s\n", v.Version, v.RemoteVersion)
				}
			}
			if m.RemoteLatest != "" {
				app.P.Printf("  remote latest: %s\n", m.RemoteLatest)
			}
		}
		if !app.HasToken {
			app.P.Info("(not logged in — remote not checked)")
		}
		return nil
	},
}

func readPushed(versionDir string) map[string]string {
	data, err := os.ReadFile(filepath.Join(versionDir, ".pushed.json"))
	if err != nil {
		return nil
	}
	var m map[string]string
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	return m
}

func hashVersionWeights(versionDir string, cat *catalog.Catalog) string {
	var path string
	if cat != nil && cat.Weights != "" {
		path = filepath.Join(versionDir, cat.Weights)
	}
	if _, err := os.Stat(path); err != nil {
		matches, _ := filepath.Glob(filepath.Join(versionDir, "*.safetensors"))
		if len(matches) == 1 {
			path = matches[0]
		} else {
			return ""
		}
	}
	sha, _, err := transfer.HashFile(path)
	if err != nil {
		return ""
	}
	return sha
}

func init() {
	statusCmd.Flags().StringVar(&statusPath, "path", ".", "workspace directory")
	rootCmd.AddCommand(statusCmd)
}
