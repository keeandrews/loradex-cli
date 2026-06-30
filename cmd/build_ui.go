package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/keeandrews/loradex-cli/internal/trainer"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

// confirmCaptions previews the generated captions (filename → caption) and asks
// the user to confirm before training.
func confirmCaptions(p *output.Printer, dsDir string) bool {
	entries, err := os.ReadDir(dsDir)
	if err != nil {
		return true
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".txt") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return true
	}
	p.Info("")
	p.Info("  Caption preview:")
	tw := p.Table()
	for _, n := range names {
		data, _ := os.ReadFile(filepath.Join(dsDir, n))
		cap := strings.ReplaceAll(strings.TrimSpace(string(data)), "\n", " ")
		if len(cap) > 110 {
			cap = cap[:110] + "…"
		}
		fmt.Fprintf(tw, "  %s\t%s\n", strings.TrimSuffix(n, ".txt"), cap)
	}
	tw.Flush()
	p.Info("  captions are saved next to each image as <name>.txt — edit any and re-run with --caption keep")
	return confirm(p, "Use these captions?")
}

// showOutputPreview lists the files the build will write, with full local paths.
func showOutputPreview(p *output.Printer, versionDir, weightsPath string) {
	p.Info("")
	p.Info("  These files will be generated:")
	for _, rel := range []string{filepath.Base(weightsPath), "training.yaml", "metrics.json", "README.md"} {
		p.Info("    %s", filepath.Join(versionDir, rel))
	}
}

// trainWithProgress runs training, rendering a step progress bar with count and
// estimated time remaining.
func trainWithProgress(cmd *cobra.Command, p *output.Printer, tr trainer.AIToolkit, plan trainer.Plan) (trainer.Result, error) {
	total := plan.Steps
	if total <= 0 {
		total = plan.Req.Profile.Steps
	}
	bar := progressbar.NewOptions(total,
		progressbar.OptionSetDescription("  training"),
		progressbar.OptionSetWriter(p.Err),
		progressbar.OptionShowCount(),
		progressbar.OptionShowElapsedTimeOnFinish(),
		progressbar.OptionSetPredictTime(true),
		progressbar.OptionSetVisibility(p.ProgressEnabled()),
		progressbar.OptionClearOnFinish(),
	)
	res, err := tr.Train(cmd.Context(), plan, func(pr trainer.Progress) {
		if pr.TotalSteps > 0 {
			bar.ChangeMax(pr.TotalSteps)
		}
		if pr.Step > 0 {
			_ = bar.Set(pr.Step)
		}
	})
	_ = bar.Finish()
	if p.ProgressEnabled() {
		fmt.Fprintln(p.Err)
	}
	return res, err
}
