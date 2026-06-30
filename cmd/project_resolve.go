package cmd

import (
	"os"
	"path/filepath"

	"github.com/keeandrews/loradex-cli/internal/config"
	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/keeandrews/loradex-cli/internal/workspace"
)

// resolveWorkspaceRoot finds the workspace a command should act on, in order:
//  1. an explicit --path (must be a workspace),
//  2. the current directory (walking up) — a CWD workspace wins,
//  3. the active managed project under <home>/projects (config.current_project).
//
// pathArg is the command's --path flag value ("" or "." means "auto").
func resolveWorkspaceRoot(pathArg string) (string, error) {
	if pathArg != "" && pathArg != "." {
		if workspace.IsWorkspace(pathArg) {
			return pathArg, nil
		}
		if root, err := workspace.FindRoot(pathArg); err == nil {
			return root, nil
		}
		return "", output.Errorf(output.ExitValidation, "no_workspace", "run `loradex init` first", "%q is not a loradex workspace", pathArg)
	}
	if root, err := workspace.FindRoot("."); err == nil {
		return root, nil
	}
	if root, ok := currentProjectRoot(); ok {
		return root, nil
	}
	return "", output.Errorf(output.ExitValidation, "no_active_project",
		"run `loradex init` to create a project, or `loradex use <name>` to pick one",
		"no workspace here and no active project set")
}

// currentProjectRoot returns the active managed project's root, if it exists.
func currentProjectRoot() (string, bool) {
	f, err := config.Load()
	if err != nil || f.CurrentProject == "" {
		return "", false
	}
	pd, err := config.ProjectsDir()
	if err != nil {
		return "", false
	}
	root := filepath.Join(pd, f.CurrentProject)
	if workspace.IsWorkspace(root) {
		return root, true
	}
	return "", false
}

// listProjects returns the slugs of managed projects under <home>/projects.
func listProjects() ([]string, error) {
	pd, err := config.ProjectsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(pd)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && workspace.IsWorkspace(filepath.Join(pd, e.Name())) {
			out = append(out, e.Name())
		}
	}
	return out, nil
}
