package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/keeandrews/loradex-cli/internal/api"
	"github.com/keeandrews/loradex-cli/internal/catalog"
	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/keeandrews/loradex-cli/internal/transfer"
	"github.com/keeandrews/loradex-cli/internal/workspace"
	"github.com/spf13/cobra"
)

// Client-side size guards (server enforces the real limits).
const (
	maxWeightsBytes  = 8_000_000_000 // 8 GB hard cap
	softWeightsBytes = 2_000_000_000 // 2 GB warn threshold
	maxSamples       = 12
	maxSampleBytes   = 10_000_000
	maxTotalSamples  = 50_000_000
)

var (
	pushPath           string
	pushMessage        string
	pushIncludeSamples bool
	pushCreate         bool
	pushForceWeights   bool
	pushDryRun         bool
)

type uploadItem struct {
	name   string
	path   string
	size   int64
	sha256 string
}

var pushCmd = &cobra.Command{
	Use:   "push [models/<base>[@vN]]",
	Short: "Publish a model version from the workspace",
	Long: `Publish a version folder (weights + README + config.json + samples) as a new
immutable remote version. Identical content no-ops.

Examples:
  loradex push                       # single-model workspace → latest version
  loradex push models/flux2-klein    # latest version of that base
  loradex push models/sdxl@v2
  loradex push models/flux2-klein --dry-run`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		app, err := newApp()
		if err != nil {
			return err
		}
		root, err := resolveWorkspaceRoot(pushPath)
		if err != nil {
			return err
		}
		arg := ""
		if len(args) == 1 {
			arg = args[0]
		}
		tgt, err := workspace.ResolveTarget(root, arg)
		if err != nil {
			return output.Errorf(output.ExitValidation, "bad_target", "", "%v", err)
		}

		cat, err := catalog.Load(workspace.RepoYAMLPath(root, tgt.Base))
		if err != nil {
			return output.Errorf(output.ExitValidation, "no_repo_yaml", "", "missing repo.yaml for %s", tgt.Base)
		}

		// 1. Validate catalog.
		res := catalog.Validate(cat)
		for _, w := range res.Warnings {
			app.P.Info("warning: %s", w)
		}
		if !res.OK() {
			for _, e := range res.Errors {
				app.P.Info("  · %s", e.String())
			}
			return output.Errorf(output.ExitValidation, "invalid_catalog", "fix repo.yaml", "%d validation error(s)", len(res.Errors))
		}
		if len([]rune(pushMessage)) > catalog.MaxMessage {
			return output.Validation("release message too long (max %d chars)", catalog.MaxMessage)
		}

		// 2. Resolve + check weights in the version folder.
		weightsPath := filepath.Join(tgt.Dir, cat.Weights)
		if _, err := os.Stat(weightsPath); err != nil {
			matches, _ := filepath.Glob(filepath.Join(tgt.Dir, "*.safetensors"))
			if len(matches) != 1 {
				return output.Validation("no weights file in %s", tgt.Dir)
			}
			weightsPath = matches[0]
		}
		if err := checkWeights(app.P, tgt.Dir, weightsPath); err != nil {
			return err
		}

		if err := app.requireAuth(); err != nil {
			return err
		}
		owner := app.Handle

		app.P.Info("hashing %s…", filepath.Base(weightsPath))
		weightsSHA, weightsSize, err := transfer.HashFile(weightsPath)
		if err != nil {
			return err
		}
		extras, cleanup, err := assembleExtras(app.P, tgt.Dir, cat, pushIncludeSamples)
		if err != nil {
			return err
		}
		defer cleanup()

		totalSize := weightsSize
		var extraMeta []api.ExtraFile
		for _, e := range extras {
			totalSize += e.size
			extraMeta = append(extraMeta, api.ExtraFile{Name: e.name, Size: e.size, SHA256: e.sha256})
		}

		me, err := app.Client.Me(cmd.Context())
		if err != nil {
			return err
		}
		if owner == "" {
			owner = me.Handle
		}
		if me.StorageQuota > 0 && me.StorageUsed+totalSize > me.StorageQuota {
			return output.Errorf(output.ExitQuota, "quota_exceeded", "free up space or upgrade",
				"upload of %s would exceed your quota (%s of %s used)",
				output.HumanSize(totalSize), output.HumanSize(me.StorageUsed), output.HumanSize(me.StorageQuota))
		}

		if pushDryRun {
			printPlan(app.P, owner, cat, tgt.Version, weightsPath, weightsSHA, weightsSize, extras, totalSize)
			app.P.Info("dry-run — no changes made")
			return nil
		}

		if _, err := app.Client.GetRepo(cmd.Context(), owner, cat.Name); err != nil {
			var ce *output.CLIError
			if errors.As(err, &ce) && ce.Code == output.ExitNotFound {
				if !pushCreate {
					return output.Errorf(output.ExitNotFound, "no_repo", "drop --create=false", "repo %s/%s does not exist", owner, cat.Name)
				}
				app.P.Info("creating repo %s/%s…", owner, cat.Name)
				if _, err := app.Client.CreateRepo(cmd.Context(), owner, catalogToCreateBody(cat)); err != nil {
					return err
				}
			} else {
				return err
			}
		}

		if !g.yes {
			printPlan(app.P, owner, cat, tgt.Version, weightsPath, weightsSHA, weightsSize, extras, totalSize)
			if !confirm(app.P, "Publish this version?") {
				return output.Errorf(output.ExitError, "aborted", "", "aborted")
			}
		}

		init, err := app.Client.InitiateUpload(cmd.Context(), owner, cat.Name, api.InitiateBody{
			SHA256: weightsSHA, Size: weightsSize, Filename: filepath.Base(weightsPath), ExtraFiles: extraMeta,
		})
		if err != nil {
			return err
		}
		if init.DuplicateOf != nil && *init.DuplicateOf != "" {
			app.P.Success("Identical content already published as %s — nothing to do", *init.DuplicateOf)
			writePushed(tgt.Dir, *init.DuplicateOf, weightsSHA)
			return nil
		}

		local := map[string]uploadItem{filepath.Base(weightsPath): {path: weightsPath, size: weightsSize}}
		for _, e := range extras {
			local[e.name] = e
		}
		var weightsParts []api.PartETag
		for _, t := range init.Uploads {
			it, ok := local[t.Name]
			if !ok {
				return output.Errorf(output.ExitError, "upload_mismatch", "", "server asked for an unexpected file: %s", t.Name)
			}
			if len(t.Parts) > 0 {
				parts, err := transfer.UploadParts(cmd.Context(), app.Client.HTTP, t, it.path, app.P)
				if err != nil {
					return err
				}
				if err := transfer.CompleteMultipart(cmd.Context(), app.Client.HTTP, t.CompleteURL, parts); err != nil {
					return err
				}
				if t.Name == filepath.Base(weightsPath) {
					weightsParts = parts
				}
			} else if _, err := transfer.UploadSingle(cmd.Context(), app.Client.HTTP, t, it.path, it.size, app.P); err != nil {
				return err
			}
		}

		fin, err := app.Client.FinalizeUpload(cmd.Context(), owner, cat.Name, api.FinalizeBody{
			UploadID: init.UploadID, SHA256: weightsSHA, Parts: weightsParts,
		})
		if err != nil {
			return err
		}
		writePushed(tgt.Dir, fin.Version, weightsSHA)
		if g.json {
			return app.P.JSONOut(map[string]any{"version": fin.Version, "size": weightsSize, "sha256": weightsSHA})
		}
		app.P.Success("Published %s/%s %s (%s)", owner, cat.Name, fin.Version, output.HumanSize(weightsSize))
		app.P.Printf("  loradex pull %s/%s@%s\n", owner, cat.Name, fin.Version)
		return nil
	},
}

func checkWeights(p *output.Printer, dir, path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return output.Validation("weights file %q not found", path)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			return output.Validation("could not resolve symlink %q", path)
		}
		absDir, _ := filepath.Abs(dir)
		if !strings.HasPrefix(real, absDir) {
			p.Info("warning: %s points outside the version folder (%s)", filepath.Base(path), real)
		}
		if home, err := os.UserHomeDir(); err == nil && !strings.HasPrefix(real, home) && !pushForceWeights {
			return output.Errorf(output.ExitValidation, "outside_home", "pass --force", "weights resolve outside your home: %s", real)
		}
		path = real
		if fi, err = os.Stat(path); err != nil {
			return output.Validation("weights file %q not found", path)
		}
	}
	if !fi.Mode().IsRegular() {
		return output.Validation("weights %q is not a regular file", path)
	}
	if fi.Size() > maxWeightsBytes {
		return output.Errorf(output.ExitQuota, "too_large", "", "weights file is %s (max %s)", output.HumanSize(fi.Size()), output.HumanSize(maxWeightsBytes))
	}
	if fi.Size() > softWeightsBytes {
		p.Info("note: weights file is large (%s)", output.HumanSize(fi.Size()))
	}
	return nil
}

func assembleExtras(p *output.Printer, versionDir string, cat *catalog.Catalog, includeSamples bool) ([]uploadItem, func(), error) {
	var items []uploadItem
	cleanup := func() {}

	readme := filepath.Join(versionDir, "README.md")
	if fi, err := os.Stat(readme); err == nil && fi.Mode().IsRegular() {
		sha, size, err := transfer.HashFile(readme)
		if err != nil {
			return nil, cleanup, err
		}
		items = append(items, uploadItem{name: "README.md", path: readme, size: size, sha256: sha})
	}

	cfgBytes, _ := json.MarshalIndent(catalogToCreateBody(cat), "", "  ")
	tmp, err := os.CreateTemp(versionDir, ".loradex-config-*.json")
	if err != nil {
		return nil, cleanup, err
	}
	tmpName := tmp.Name()
	cleanup = func() { os.Remove(tmpName) }
	if _, err := tmp.Write(cfgBytes); err != nil {
		tmp.Close()
		return nil, cleanup, err
	}
	tmp.Close()
	sha, size, err := transfer.HashFile(tmpName)
	if err != nil {
		return nil, cleanup, err
	}
	items = append(items, uploadItem{name: "config.json", path: tmpName, size: size, sha256: sha})

	if includeSamples {
		samples, err := collectSamples(p, filepath.Join(versionDir, "samples"))
		if err != nil {
			return nil, cleanup, err
		}
		items = append(items, samples...)
	}
	return items, cleanup, nil
}

func collectSamples(p *output.Printer, samplesDir string) ([]uploadItem, error) {
	entries, err := os.ReadDir(samplesDir)
	if err != nil {
		return nil, nil
	}
	allowed := map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true, "image/gif": true}
	var items []uploadItem
	var total int64
	for _, e := range entries {
		name := e.Name()
		full := filepath.Join(samplesDir, name)
		if strings.HasPrefix(name, ".") || e.IsDir() {
			continue
		}
		fi, err := os.Lstat(full)
		if err != nil || fi.Mode()&os.ModeSymlink != 0 {
			p.Info("skipping sample %s (symlink/unreadable)", name)
			continue
		}
		if fi.Size() > maxSampleBytes {
			p.Info("skipping sample %s (too large)", name)
			continue
		}
		ct, err := sniffType(full)
		if err != nil || !allowed[ct] {
			p.Info("skipping sample %s (not an image)", name)
			continue
		}
		if len(items) >= maxSamples || total+fi.Size() > maxTotalSamples {
			p.Info("skipping sample %s (sample caps reached)", name)
			continue
		}
		sha, size, err := transfer.HashFile(full)
		if err != nil {
			continue
		}
		total += size
		items = append(items, uploadItem{name: "samples/" + name, path: full, size: size, sha256: sha})
	}
	return items, nil
}

func sniffType(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	return strings.SplitN(http.DetectContentType(buf[:n]), ";", 2)[0], nil
}

func catalogToCreateBody(c *catalog.Catalog) api.CreateRepoBody {
	return api.CreateRepoBody{
		Name: c.Name, Description: c.Description, Visibility: c.Visibility,
		BaseModel: c.BaseModel, Format: c.Format, License: c.License,
		TriggerWords: c.TriggerWords, NetworkRank: c.NetworkRank, NetworkDim: c.NetworkDim,
		RecommendedWeight: c.RecommendedWeight, Tags: c.Tags,
	}
}

func printPlan(p *output.Printer, owner string, cat *catalog.Catalog, version, weightsPath, sha string, size int64, extras []uploadItem, total int64) {
	p.Info("push plan:")
	p.Info("  repo       %s/%s (%s)", owner, cat.Name, cat.Visibility)
	p.Info("  version    %s  (server-assigned tag on publish)", version)
	p.Info("  weights    %s  %s  sha256:%s", filepath.Base(weightsPath), output.HumanSize(size), sha[:16])
	for _, e := range extras {
		p.Info("  +          %s  %s", e.name, output.HumanSize(e.size))
	}
	p.Info("  total      %s", output.HumanSize(total))
}

func confirm(p *output.Printer, prompt string) bool {
	fmt.Fprintf(p.Err, "%s [y/N] ", prompt)
	sc := bufio.NewScanner(os.Stdin)
	if sc.Scan() {
		ans := strings.ToLower(strings.TrimSpace(sc.Text()))
		return ans == "y" || ans == "yes"
	}
	return false
}

// writePushed records the remote tag + hash for a version (no secrets).
func writePushed(versionDir, remoteVersion, sha string) {
	data, _ := json.MarshalIndent(map[string]string{
		"remote_version": remoteVersion, "sha256": sha, "pushed_at": time.Now().UTC().Format(time.RFC3339),
	}, "", "  ")
	_ = os.WriteFile(filepath.Join(versionDir, ".pushed.json"), data, 0o644)
}

func init() {
	f := pushCmd.Flags()
	f.StringVar(&pushPath, "path", ".", "workspace directory")
	f.StringVarP(&pushMessage, "message", "m", "", "release notes")
	f.BoolVar(&pushIncludeSamples, "include-samples", false, "also upload images under the version's samples/")
	f.BoolVar(&pushCreate, "create", true, "auto-create the repo if missing")
	f.BoolVar(&pushForceWeights, "force", false, "allow weights resolving outside your home directory")
	f.BoolVar(&pushDryRun, "dry-run", false, "print the plan without uploading or mutating")
	rootCmd.AddCommand(pushCmd)
}
