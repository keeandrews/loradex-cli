package cmd

import (
	"github.com/keeandrews/loradex-cli/internal/credstore"
	"github.com/spf13/cobra"
)

var logoutAll bool

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove stored credentials for the targeted endpoint/profile",
	Long: `Remove stored credentials. Idempotent — no error if already logged out.

Examples:
  loradex logout
  loradex logout --all          # remove every stored credential`,
	RunE: func(cmd *cobra.Command, args []string) error {
		app, err := newApp()
		if err != nil {
			return err
		}
		if logoutAll {
			if err := credstore.DeleteAll(); err != nil {
				return err
			}
			app.P.Success("Removed all stored credentials")
			return nil
		}
		key := credstore.Key(app.Profile, app.Endpoint)
		if err := credstore.Delete(key); err != nil {
			return err
		}
		app.P.Success("Logged out of %s", credstore.HostOf(app.Endpoint))
		return nil
	},
}

func init() {
	logoutCmd.Flags().BoolVar(&logoutAll, "all", false, "remove every stored credential across endpoints/profiles")
	rootCmd.AddCommand(logoutCmd)
}
