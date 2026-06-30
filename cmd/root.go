// Package cmd wires the cobra commands. Each command file holds wiring only;
// real logic lives in internal/*.
package cmd

import (
	"context"
	"os"
	"os/signal"

	"github.com/keeandrews/loradex-cli/internal/api"
	"github.com/keeandrews/loradex-cli/internal/config"
	"github.com/keeandrews/loradex-cli/internal/credstore"
	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/spf13/cobra"
)

// globalFlags holds values bound to root persistent flags.
type globalFlags struct {
	endpoint string
	web      string
	token    string
	profile  string
	json     bool
	yes      bool
	quiet    bool
	verbose  bool
	noColor  bool
	insecure bool
	models   bool
}

var g globalFlags

var rootCmd = &cobra.Command{
	Use:   "loradex",
	Short: "Git for LoRAs — version, search, and share LoRA models from the command line",
	Long: `loradex is the command-line tool for the loradex platform.

Authenticate, discover, pull, clone, scaffold, and publish versioned LoRA
model files. Every read command supports --json; every write command supports
-y/--yes.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// `loradex --models` is a shortcut into the base-model browser.
		if g.models {
			return modelsCmd.RunE(cmd, nil)
		}
		return cmd.Help()
	},
}

// Execute runs the root command and maps errors to exit codes.
func Execute() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	rootCmd.SetContext(ctx)

	if err := rootCmd.Execute(); err != nil {
		p := output.New(g.json, g.quiet, g.verbose, g.noColor)
		os.Exit(p.EmitError(err))
	}
}

func init() {
	f := rootCmd.PersistentFlags()
	f.StringVar(&g.endpoint, "endpoint", "", "API base URL (env LORADEX_ENDPOINT)")
	f.StringVar(&g.web, "web", "", "web base URL for login (env LORADEX_WEB)")
	f.StringVar(&g.token, "token", "", "auth token override for CI (env LORADEX_TOKEN; never persisted)")
	f.StringVar(&g.profile, "profile", "", "named credential/config profile (env LORADEX_PROFILE)")
	f.BoolVar(&g.json, "json", false, "machine-readable JSON output")
	f.BoolVarP(&g.yes, "yes", "y", false, "assume yes; skip confirmation prompts")
	f.BoolVarP(&g.quiet, "quiet", "q", false, "suppress progress and non-essential output")
	f.BoolVarP(&g.verbose, "verbose", "v", false, "debug logging to stderr (secrets redacted)")
	f.BoolVar(&g.noColor, "no-color", false, "disable ANSI color (env NO_COLOR)")
	f.BoolVar(&g.insecure, "insecure", false, "allow http:// for loopback endpoints only (never disables TLS verification)")
	f.BoolVar(&g.models, "models", false, "open the base-model browser (same as `loradex models`)")
}

// App bundles the resolved printer + API client for a command run.
type App struct {
	P        *output.Printer
	Client   *api.Client
	Endpoint string
	Web      string
	Profile  string
	Handle   string // from stored creds (may be empty)
	HasToken bool
}

// newApp resolves config/creds and builds the API client.
func newApp() (*App, error) {
	file, _ := config.Load()

	endpoint := config.Resolve(g.endpoint, os.Getenv("LORADEX_ENDPOINT"), file.Endpoint, config.DefaultEndpoint)
	web := config.Resolve(g.web, os.Getenv("LORADEX_WEB"), file.Web, config.DefaultWeb)
	profile := config.Resolve(g.profile, os.Getenv("LORADEX_PROFILE"), file.Profile, config.DefaultProfile)

	p := output.New(g.json, g.quiet, g.verbose, g.noColor)

	// Resolve token: explicit flag/env is in-memory only; otherwise endpoint-keyed store.
	token := g.token
	if token == "" {
		token = os.Getenv("LORADEX_TOKEN")
	}
	handle, hasToken := "", token != ""
	if token == "" {
		if cred, ok, _ := credstore.Get(credstore.Key(profile, endpoint)); ok {
			token, handle, hasToken = cred.Token, cred.Handle, true
		}
	}

	client := api.New(endpoint, web, token, g.insecure, p)
	if err := client.CheckEndpoint(); err != nil {
		return nil, err
	}
	return &App{P: p, Client: client, Endpoint: endpoint, Web: web, Profile: profile, Handle: handle, HasToken: hasToken}, nil
}

// requireAuth returns a CLIError (exit 3) when no token is available for the endpoint.
func (a *App) requireAuth() error {
	if !a.HasToken {
		return output.Errorf(output.ExitUnauth, "unauthenticated",
			"run `loradex login`", "not logged in to %s", credstore.HostOf(a.Endpoint))
	}
	return nil
}
