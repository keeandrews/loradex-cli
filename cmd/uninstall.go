package cmd

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/keeandrews/loradex-cli/internal/config"
	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove loradex and all its files (binary, config, credentials, models, trainers)",
	Long: `Permanently remove the loradex home folder(s) — the binary, config,
credentials, downloaded base models, and installed trainer backends — and clean
the lines loradex added to your shell profile.

This is the same teardown the installer's "uninstall" / "reinstall" options use.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		p := output.New(g.json, g.quiet, g.verbose, g.noColor)
		targets := uninstallTargets()
		if len(targets) == 0 {
			p.Info("nothing to remove — no loradex home found")
			return nil
		}

		p.Info("This will permanently delete:")
		var total int64
		for _, t := range targets {
			sz := dirSize(t)
			total += sz
			p.Info("  %s  (%s)", t, output.HumanSize(sz))
		}
		rc := shellRCForHost()
		if rc != "" {
			p.Info("and remove loradex lines from %s", rc)
		}
		p.Info("  total: %s", output.HumanSize(total))

		if !g.yes && !confirm(p, "Remove everything above? This cannot be undone.") {
			return output.Errorf(output.ExitError, "aborted", "", "aborted")
		}

		// Clean the shell profile first (before the binary may delete itself).
		if cleaned, ok := cleanShellRC(targets); ok {
			p.Info("cleaned %s", cleaned)
		}
		for _, t := range targets {
			if err := os.RemoveAll(t); err != nil {
				return output.Errorf(output.ExitError, "remove_failed", "", "could not remove %s: %v", t, err)
			}
		}
		p.Success("loradex removed.")
		p.Info("  open a new terminal to clear the stale PATH/LORADEX_HOME from this shell")
		return nil
	},
}

// uninstallTargets returns the existing loradex home directories (active home
// from $LORADEX_HOME plus the default home), without creating anything.
func uninstallTargets() []string {
	seen := map[string]bool{}
	var out []string
	add := func(d string) {
		d = strings.TrimSpace(d)
		if d == "" {
			return
		}
		abs, err := filepath.Abs(d)
		if err != nil {
			return
		}
		if seen[abs] {
			return
		}
		if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
			seen[abs] = true
			out = append(out, abs)
		}
	}
	add(os.Getenv("LORADEX_HOME"))
	add(config.DefaultHome())
	return out
}

// cleanShellRC removes the lines `loradex setup` added (LORADEX_HOME export and
// PATH exports for the removed homes' bin dirs) from the shell profile.
func cleanShellRC(targets []string) (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	rc := shellRCPath(home)
	data, err := os.ReadFile(rc)
	if err != nil {
		return rc, false
	}
	binDirs := map[string]bool{}
	for _, t := range targets {
		binDirs[filepath.Join(t, "bin")] = true
	}
	var out []string
	for _, ln := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(ln)
		if t == "# Added by `loradex setup`" || strings.HasPrefix(t, "export LORADEX_HOME=") {
			continue
		}
		if strings.HasPrefix(t, "export PATH=") {
			skip := false
			for bd := range binDirs {
				if strings.Contains(ln, bd) {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
		}
		out = append(out, ln)
	}
	if err := os.WriteFile(rc, []byte(strings.Join(out, "\n")), 0o644); err != nil {
		return rc, false
	}
	return rc, true
}

func shellRCForHost() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return shellRCPath(home)
}

// dirSize returns the total bytes under a directory (best effort).
func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, e := d.Info(); e == nil {
				total += fi.Size()
			}
		}
		return nil
	})
	return total
}

func init() { rootCmd.AddCommand(uninstallCmd) }
