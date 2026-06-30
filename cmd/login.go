package cmd

import (
	"bufio"
	"context"
	"os"
	"strings"
	"time"

	"github.com/keeandrews/loradex-cli/internal/api"
	"github.com/keeandrews/loradex-cli/internal/credstore"
	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/spf13/cobra"
)

var loginWithBrowser bool

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate and store credentials for the targeted endpoint",
	Long: `Authenticate to loradex. By default this opens your browser; you approve
the CLI while already signed in on the web. Credentials are stored in your OS
keychain (file fallback) and bound to the endpoint host.

Examples:
  loradex login
  loradex login --token "$LORADEX_TOKEN"               # CI / non-interactive
  loradex login --endpoint https://staging.api.loradex.ai`,
	RunE: func(cmd *cobra.Command, args []string) error {
		app, err := newApp()
		if err != nil {
			return err
		}
		ctx := cmd.Context()

		// Token flow (explicit token, or --with-browser=false).
		token := g.token
		if token == "" {
			token = os.Getenv("LORADEX_TOKEN")
		}
		if token == "" && !loginWithBrowser {
			app.P.Info("Paste a CLI token for %s:", credstore.HostOf(app.Endpoint))
			sc := bufio.NewScanner(os.Stdin)
			if sc.Scan() {
				token = strings.TrimSpace(sc.Text())
			}
			if token == "" {
				return output.Usage("no token provided")
			}
		}
		if token != "" {
			return finishLogin(app, ctx, token, "")
		}

		// Browser loopback exchange.
		pkce := api.NewPKCE()
		state := api.RandomState()
		srv, err := api.StartLoopback()
		if err != nil {
			return output.Errorf(output.ExitError, "loopback_failed", "try `loradex login --token <tok>`", "could not start local callback server: %v", err)
		}
		defer srv.Shutdown()

		authURL := app.Client.AuthorizeURL(srv.CallbackURL(), state, pkce.Challenge)
		app.P.Info("Opening your browser to authorize loradex…")
		app.P.Info("If it doesn't open, visit:\n  %s", authURL)
		_ = api.OpenBrowser(authURL)

		code, err := srv.Wait(ctx, state, 3*time.Minute)
		if err != nil {
			return err
		}
		res, err := app.Client.Exchange(ctx, code, pkce.Verifier)
		if err != nil {
			return err
		}
		return finishLogin(app, ctx, res.Token, res.Handle)
	},
}

// finishLogin validates the token (when handle unknown) and stores it endpoint-keyed.
func finishLogin(app *App, ctx context.Context, token, handle string) error {
	// Validate by calling /v1/me with the new token.
	client := api.New(app.Endpoint, app.Web, token, g.insecure, app.P)
	me, err := client.Me(ctx)
	if err != nil {
		return err
	}
	if handle == "" {
		handle = me.Handle
	}
	if err := credstore.Set(credstore.Key(app.Profile, app.Endpoint), credstore.Credential{Token: token, Handle: handle}); err != nil {
		return output.Errorf(output.ExitError, "store_failed", "", "could not store credentials: %v", err)
	}
	app.P.Success("Logged in to %s as %s", credstore.HostOf(app.Endpoint), handle)
	return nil
}

func init() {
	loginCmd.Flags().BoolVar(&loginWithBrowser, "with-browser", true, "use the browser flow (false forces a token prompt)")
	rootCmd.AddCommand(loginCmd)
}
