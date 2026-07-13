package cmd

import (
	"fmt"
	"os"

	"github.com/FranCalveyra/claude-desktop-swap/internal/platform"
	"github.com/FranCalveyra/claude-desktop-swap/internal/profile"
	"github.com/spf13/cobra"
)

var cmdDelete = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a saved profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if dryRun {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			fmt.Printf("DRY RUN: delete profile %q at %s\\.claude-swap\\profiles\\%s; no data will be modified.\n", name, home, name)
			return nil
		}

		store, err := profile.NewStore()
		if err != nil {
			return err
		}
		lock, err := acquireOperationLock("operation")
		if err != nil {
			return err
		}
		defer lock.Release()
		if current, _ := store.Current(); current == name {
			return fmt.Errorf("cannot delete active profile %q; switch to another account first", name)
		}
		if appData, pathErr := platform.Current().AppDataPath(); pathErr == nil {
			if liveName, _ := store.MatchLiveAt(platform.CookiesPath(appData)); liveName == name {
				return fmt.Errorf("cannot delete active profile %q; switch to another account first", name)
			}
		}

		if err := store.Delete(name); err != nil {
			return err
		}

		fmt.Printf("Profile %q deleted.\n", name)
		return nil
	},
}
