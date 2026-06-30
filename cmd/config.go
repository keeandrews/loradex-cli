package cmd

import (
	"github.com/keeandrews/loradex-cli/internal/basemodel"
	"github.com/keeandrews/loradex-cli/internal/config"
	"github.com/keeandrews/loradex-cli/internal/interpreter"
	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "View loradex configuration and set global defaults",
	Long: `Show the loradex home, paths, and defaults; set the default base model and
default caption model used when a command doesn't specify one.

  loradex config                                  # show current config
  loradex config set default-base flux2-klein
  loradex config set default-interpreter qwen3-vl-4b`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		p := output.New(g.json, g.quiet, g.verbose, g.noColor)
		f, err := config.Load()
		if err != nil {
			return err
		}
		home, _ := config.Dir()
		models, _ := config.ModelsDir()
		interps, _ := config.InterpretersDir()
		if g.json {
			return p.JSONOut(map[string]any{
				"home": home, "endpoint": f.Endpoint, "default_base": f.DefaultBase,
				"default_interpreter": f.DefaultInterpreter, "current_project": f.CurrentProject,
				"models_dir": models, "interpreters_dir": interps,
			})
		}
		p.Info("  loradex config")
		p.Info("  ───────────────────────────────────────────")
		p.Info("  Home                 %s", home)
		p.Info("  Endpoint             %s", orDefault(f.Endpoint, config.DefaultEndpoint))
		p.Info("  Default base         %s", dash(f.DefaultBase))
		p.Info("  Default interpreter  %s", dash(f.DefaultInterpreter))
		p.Info("  Current project      %s", dash(f.CurrentProject))
		p.Info("  Models dir           %s", models)
		p.Info("  Interpreters dir     %s", interps)
		p.Info("  ───────────────────────────────────────────")
		p.Info("  set a default: loradex config set default-base|default-interpreter <id>")
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a config value (default-base, default-interpreter, endpoint)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		p := output.New(g.json, g.quiet, g.verbose, g.noColor)
		key, val := args[0], args[1]
		f, err := config.Load()
		if err != nil {
			return err
		}
		switch key {
		case "default-base", "default_base":
			if _, ok := basemodel.Find(val); !ok {
				p.Info("note: %q isn't a known base — setting it anyway (custom/unlisted)", val)
			}
			f.DefaultBase = val
		case "default-interpreter", "default_interpreter":
			if _, ok := interpreter.Find(val); !ok {
				p.Info("note: %q isn't a known interpreter — setting it anyway (custom/unlisted)", val)
			}
			f.DefaultInterpreter = val
		case "endpoint":
			f.Endpoint = val
		default:
			return output.Usage("unknown key %q (default-base, default-interpreter, endpoint)", key)
		}
		if err := config.Save(f); err != nil {
			return err
		}
		p.Success("set %s = %s", key, val)
		return nil
	},
}

func init() {
	configCmd.AddCommand(configSetCmd)
	rootCmd.AddCommand(configCmd)
}
