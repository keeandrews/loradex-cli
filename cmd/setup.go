package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/keeandrews/loradex-cli/internal/config"
	"github.com/keeandrews/loradex-cli/internal/hfcli"
	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/keeandrews/loradex-cli/internal/trainerreg"
	"github.com/spf13/cobra"
)

var (
	setupAddPath        bool
	setupInstallAitk    bool
	setupInstallHF      bool
	setupReinstallAitk  bool
	setupNonInteractive bool
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Configure loradex: detect/install trainers and add to PATH",
	Long: `Interactive setup wizard. Detects components already on this machine
(ai-toolkit, Draw Things, ComfyUI, and the HuggingFace CLI), records their
locations, installs the ones you select (ai-toolkit under the loradex home; the
HuggingFace CLI via uv/pipx/pip), and optionally adds loradex to your PATH.

Safe to re-run at any time. Everything loradex owns lives under one home folder
(override with $LORADEX_HOME). To remove everything, use ` + "`loradex uninstall`" + `.

Examples:
  loradex setup                                      # interactive checklist
  loradex setup --install-aitoolkit --install-hf --add-path -y   # non-interactive
  LORADEX_HOME=~/loradex loradex setup               # use a custom home`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		p := output.New(g.json, g.quiet, g.verbose, g.noColor)
		home, err := config.Dir()
		if err != nil {
			return err
		}
		models, _ := config.ModelsDir()
		trainers, _ := config.TrainersDir()

		p.Info("")
		p.Info("  loradex setup")
		p.Info("  ───────────────────────────────────────────")
		p.Info("  Home      %s%s", home, homeSourceNote())
		p.Info("  Models    %s", models)
		p.Info("  Trainers  %s", trainers)
		p.Info("  ───────────────────────────────────────────")

		items := buildSetupItems()
		interactive := !setupNonInteractive && !g.yes && !g.json && p.IsTTY()

		var selected map[string]bool
		if interactive {
			sel, apply := runChecklist(p, items)
			if !apply {
				p.Info("cancelled — no changes made")
				return nil
			}
			selected = sel
		} else {
			selected = defaultSelection(items)
			if setupInstallAitk {
				selected[trainerreg.AIToolkit] = true
			}
			if setupInstallHF {
				selected[itemHuggingFace] = true
			}
		}

		// Apply: install selected-but-missing (auto-installable), record the rest.
		if err := applySelection(cmd, p, items, selected); err != nil {
			return err
		}

		// Persist a custom home so future shells find the same single folder.
		maybePersistHome(p)

		// PATH step.
		if err := maybeAddToPath(p, interactive); err != nil {
			return err
		}

		warnIfShadowed(p)

		p.Success("Setup complete.")
		if st := trainerreg.Detect(trainerreg.AIToolkit); st.Installed {
			p.Info("  train:  loradex build ./images --base flux2-klein --trigger <word>")
		}
		p.Info("  models: loradex models")
		return nil
	},
}

// itemHuggingFace is the synthetic component id for the HuggingFace CLI.
const itemHuggingFace = "huggingface"

// setupItem is one selectable component in the wizard (a trainer or a tool).
type setupItem struct {
	id, name, desc, detail string
	installed, canInstall  bool
}

// buildSetupItems assembles the wizard's components: detected trainers plus the
// HuggingFace CLI.
func buildSetupItems() []setupItem {
	var items []setupItem
	for _, s := range trainerreg.DetectAll() {
		detail := s.Detail
		if s.Installed {
			detail = "found: " + s.Detail
		} else if s.AutoInstallable {
			detail = "not installed — will install under the loradex home if selected"
		}
		items = append(items, setupItem{
			id: s.ID, name: s.Name, desc: s.Desc, detail: detail,
			installed: s.Installed, canInstall: s.AutoInstallable,
		})
	}
	hf := hfcli.Detect()
	detail := "not installed — needed to pull gated base models (FLUX); will install if selected"
	if hf.Installed {
		detail = "found: " + hf.Path
		if hf.LoggedIn {
			detail += "  (logged in)"
		} else {
			detail += "  (not logged in)"
		}
	}
	items = append(items, setupItem{
		id: itemHuggingFace, name: "HuggingFace CLI", canInstall: true,
		desc:   "Pulls base models from HuggingFace (`loradex models pull`). Gated models need a login.",
		detail: detail, installed: hf.Installed,
	})
	return items
}

// runChecklist renders an interactive checklist and returns the selection keyed
// by component id, plus whether to apply it (false = cancelled). Installed
// components start checked.
func runChecklist(p *output.Printer, items []setupItem) (map[string]bool, bool) {
	sel := defaultSelection(items)
	sc := bufio.NewScanner(os.Stdin)
	for {
		p.Info("")
		p.Info("  Components — toggle a number, then Enter to apply:")
		for i, it := range items {
			box := " "
			if sel[it.id] {
				box = "x"
			}
			p.Info("  %d. [%s] %-16s %s", i+1, box, it.name, it.detail)
			p.Info("        %s", it.desc)
		}
		fmt.Fprintf(p.Err, "\n  number to toggle · Enter to apply · q to cancel: ")
		if !sc.Scan() {
			return sel, true
		}
		in := strings.ToLower(strings.TrimSpace(sc.Text()))
		switch {
		case in == "":
			return sel, true
		case in == "q" || in == "quit":
			return nil, false // cancel → apply nothing
		default:
			n, err := strconv.Atoi(in)
			if err != nil || n < 1 || n > len(items) {
				p.Info("  not a valid choice")
				continue
			}
			id := items[n-1].id
			sel[id] = !sel[id]
		}
	}
}

func defaultSelection(items []setupItem) map[string]bool {
	sel := map[string]bool{}
	for _, it := range items {
		sel[it.id] = it.installed // pre-check anything already present
	}
	return sel
}

func applySelection(cmd *cobra.Command, p *output.Printer, items []setupItem, selected map[string]bool) error {
	f, err := config.Load()
	if err != nil {
		return err
	}
	for _, it := range items {
		want := selected[it.id]
		if !want {
			disableComponent(f, it.id)
			continue
		}
		if it.id == itemHuggingFace {
			if err := applyHuggingFace(cmd, p, f, it); err != nil {
				return err
			}
			continue
		}
		if err := applyTrainer(cmd, p, f, it); err != nil {
			return err
		}
	}
	return config.Save(f)
}

func applyTrainer(cmd *cobra.Command, p *output.Printer, f *config.File, it setupItem) error {
	st := trainerreg.Detect(it.id)
	switch {
	case st.Installed:
		f.SetTrainer(it.id, st.ToConfig(true))
		p.Info("✓ %s recorded: %s", it.name, st.Detail)
	case st.AutoInstallable:
		dest, derr := aitoolkitDest()
		if derr != nil {
			return derr
		}
		if setupReinstallAitk {
			p.Info("reinstalling ai-toolkit (removing %s)…", dest)
			_ = os.RemoveAll(dest)
		}
		newSt, ierr := trainerreg.InstallAIToolkit(cmd.Context(), dest, p)
		if ierr != nil {
			return ierr
		}
		f.SetTrainer(it.id, newSt.ToConfig(true))
		p.Success("installed %s → %s", it.name, newSt.Path)
	default:
		p.Info("• %s is not installed and loradex can't install it automatically — %s", it.name, installHint(it.id))
	}
	return nil
}

func applyHuggingFace(cmd *cobra.Command, p *output.Printer, f *config.File, it setupItem) error {
	hf := hfcli.Detect()
	if !hf.Installed {
		path, err := hfcli.Install(cmd.Context(), p)
		if err != nil {
			return err
		}
		hf = hfcli.Status{Installed: true, Path: path, LoggedIn: hfcli.LoggedIn()}
		p.Success("installed HuggingFace CLI → %s", path)
	} else {
		p.Info("✓ HuggingFace CLI recorded: %s", hf.Path)
	}
	f.HuggingFace = &config.ToolInfo{Path: hf.Path, Enabled: true}
	if !hf.LoggedIn {
		p.Info("  not logged in — run `hf auth login` (loradex will prompt you when you pull a gated model)")
	}
	return nil
}

// disableComponent marks a deselected component disabled, keeping its location.
func disableComponent(f *config.File, id string) {
	if id == itemHuggingFace {
		if f.HuggingFace != nil {
			f.HuggingFace.Enabled = false
		}
		return
	}
	if t, ok := f.Trainers[id]; ok {
		t.Enabled = false
		f.SetTrainer(id, t)
	}
}

func aitoolkitDest() (string, error) {
	td, err := config.TrainersDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(td, "ai-toolkit"), nil
}

func installHint(id string) string {
	switch id {
	case trainerreg.DrawThings:
		return "install Draw Things from the Mac App Store / drawthings.ai"
	case trainerreg.ComfyUI:
		return "clone ComfyUI to ~/ComfyUI"
	}
	return "see its project docs"
}

// maybeAddToPath offers to add the loradex binary's directory to the shell PATH.
func maybeAddToPath(p *output.Printer, interactive bool) error {
	exe, err := os.Executable()
	if err != nil {
		return nil // non-fatal
	}
	binDir := filepath.Dir(exe)
	if onPath(binDir) {
		return nil // already reachable
	}
	if interactive && !setupAddPath {
		if !confirm(p, fmt.Sprintf("Add loradex to your PATH (%s)?", binDir)) {
			p.Info("skipped PATH — run loradex with: %s", exe)
			return nil
		}
	} else if !setupAddPath {
		p.Info("note: %s is not on your PATH — re-run with --add-path to add it", binDir)
		return nil
	}

	rc, line, err := addDirToShellRC(binDir)
	if err != nil {
		p.Info("could not update your shell profile: %v", err)
		p.Info("add this line manually:\n  %s", line)
		return nil
	}
	p.Success("added loradex to PATH in %s", rc)
	p.Info("  run `source %s` (or open a new terminal) to refresh this shell", rc)
	return nil
}

// maybePersistHome writes `export LORADEX_HOME=…` to the shell profile when a
// non-default home is in effect, so the binary and its data stay in one folder
// across shells. Idempotent and best-effort.
func maybePersistHome(p *output.Printer) {
	h := strings.TrimSpace(os.Getenv("LORADEX_HOME"))
	if h == "" {
		return // using the default home; nothing to persist
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	rc := shellRCPath(home)
	if data, err := os.ReadFile(rc); err == nil && strings.Contains(string(data), "LORADEX_HOME") {
		return
	}
	f, err := os.OpenFile(rc, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "\n# Added by `loradex setup`\nexport LORADEX_HOME=\"%s\"\n", h); err == nil {
		p.Info("recorded LORADEX_HOME=%s in %s", h, rc)
	}
}

// warnIfShadowed alerts when a *different* loradex earlier on PATH shadows the
// binary that's running — a common source of "my change didn't take" confusion.
func warnIfShadowed(p *output.Printer) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return
	}
	found, err := exec.LookPath("loradex")
	if err != nil {
		return
	}
	if rp, err := filepath.EvalSymlinks(found); err == nil {
		found = rp
	}
	if found != exe {
		p.Info("")
		p.Info("⚠ another loradex shadows this one on your PATH:")
		p.Info("    running: %s", exe)
		p.Info("    PATH picks: %s", found)
		p.Info("  update or remove the shadowing copy so `loradex` uses this build")
	}
}

func onPath(dir string) bool {
	clean := filepath.Clean(dir)
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.Clean(d) == clean {
			return true
		}
	}
	return false
}

// addDirToShellRC appends a PATH export for dir to the user's shell profile,
// returning the profile path and the line written. Idempotent.
func addDirToShellRC(dir string) (rcPath, line string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	rcPath = shellRCPath(home)
	line = fmt.Sprintf("export PATH=\"%s:$PATH\"", dir)

	// Don't double-append if the dir is already referenced.
	if data, err := os.ReadFile(rcPath); err == nil && strings.Contains(string(data), dir) {
		return rcPath, line, nil
	}
	f, err := os.OpenFile(rcPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return rcPath, line, err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "\n# Added by `loradex setup`\n%s\n", line)
	return rcPath, line, err
}

// shellRCPath picks a shell profile based on $SHELL.
func shellRCPath(home string) string {
	sh := os.Getenv("SHELL")
	switch {
	case strings.Contains(sh, "zsh"):
		return filepath.Join(home, ".zshrc")
	case strings.Contains(sh, "bash"):
		if runtime.GOOS == "darwin" {
			return filepath.Join(home, ".bash_profile")
		}
		return filepath.Join(home, ".bashrc")
	case strings.Contains(sh, "fish"):
		return filepath.Join(home, ".config", "fish", "config.fish")
	default:
		return filepath.Join(home, ".profile")
	}
}

func homeSourceNote() string {
	if strings.TrimSpace(os.Getenv("LORADEX_HOME")) != "" {
		return "  ($LORADEX_HOME)"
	}
	return ""
}

func init() {
	f := setupCmd.Flags()
	f.BoolVar(&setupAddPath, "add-path", false, "add loradex to PATH without prompting")
	f.BoolVar(&setupInstallAitk, "install-aitoolkit", false, "install ai-toolkit (non-interactive)")
	f.BoolVar(&setupInstallHF, "install-hf", false, "install the HuggingFace CLI (non-interactive)")
	f.BoolVar(&setupReinstallAitk, "reinstall-aitoolkit", false, "remove and reinstall ai-toolkit")
	f.BoolVar(&setupNonInteractive, "non-interactive", false, "no prompts; record detected trainers")
	rootCmd.AddCommand(setupCmd)
}
