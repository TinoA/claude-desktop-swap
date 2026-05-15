package cmd

import (
	"fmt"

	"github.com/FranCalveyra/claude-desktop-swap/internal/platform"
	"github.com/FranCalveyra/claude-desktop-swap/internal/profile"
	"github.com/spf13/cobra"
)

var cmdSave = &cobra.Command{
	Use:   "save <name>",
	Short: "Save the current session as a named profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		p := platform.Current()
		appData, err := p.AppDataPath()
		if err != nil {
			return err
		}

		running, err := p.IsRunning()
		if err != nil {
			return err
		}
		if running {
			force, _ := cmd.Flags().GetBool("force")
			if !force {
				return fmt.Errorf("Claude Desktop is running — local storage snapshot may be inconsistent\nQuit Claude first, or use --force to snapshot anyway")
			}
		}

		store, err := profile.NewStore()
		if err != nil {
			return err
		}

		if err := store.Save(name, appData); err != nil {
			return err
		}

		fmt.Printf("Profile %q saved.\n", name)
		return nil
	},
}

func init() {
	cmdSave.Flags().Bool("force", false, "snapshot even if Claude Desktop is running")
}
