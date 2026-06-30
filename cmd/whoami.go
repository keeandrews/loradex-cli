package cmd

import (
	"github.com/keeandrews/loradex-cli/internal/credstore"
	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/spf13/cobra"
)

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show the authenticated identity, plan, and storage usage",
	Example: `  loradex whoami
  loradex whoami --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		app, err := newApp()
		if err != nil {
			return err
		}
		if err := app.requireAuth(); err != nil {
			return err
		}
		me, err := app.Client.Me(cmd.Context())
		if err != nil {
			return err
		}
		host := credstore.HostOf(app.Endpoint)
		if g.json {
			return app.P.JSONOut(map[string]any{
				"handle": me.Handle, "plan": me.Plan, "endpoint": host,
				"storage_used": me.StorageUsed, "storage_quota": me.StorageQuota,
			})
		}
		pct := 0
		if me.StorageQuota > 0 {
			pct = int(me.StorageUsed * 100 / me.StorageQuota)
		}
		app.P.Printf("%s · %s plan\n", app.P.Bold(me.Handle), me.Plan)
		app.P.Printf("endpoint  %s\n", host)
		app.P.Printf("storage   %s / %s  (%d%%)\n",
			output.HumanSize(me.StorageUsed), output.HumanSize(me.StorageQuota), pct)
		return nil
	},
}

func init() { rootCmd.AddCommand(whoamiCmd) }
