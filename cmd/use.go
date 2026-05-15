package cmd

import (
	"fmt"

	"github.com/FranCalveyra/claude-desktop-swap/internal/platform"
	"github.com/FranCalveyra/claude-desktop-swap/internal/profile"
	"github.com/spf13/cobra"
)

var cmdUse = &cobra.Command{
	Use:   "use <name>",
	Short: "Switch to a saved profile (kills and restarts Claude Desktop)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		p := platform.Current()
		appData, err := p.AppDataPath()
		if err != nil {
			return err
		}

		store, err := profile.NewStore()
		if err != nil {
			return err
		}

		if !store.Exists(name) {
			return fmt.Errorf("profile %q not found — run 'claude-swap list' to see available profiles", name)
		}

		fmt.Println("Stopping Claude Desktop...")
		if err := p.KillApp(); err != nil {
			return fmt.Errorf("stop Claude: %w", err)
		}

		fmt.Printf("Restoring profile %q...\n", name)
		if err := store.Restore(name, appData); err != nil {
			return fmt.Errorf("restore profile: %w", err)
		}

		fmt.Println("Starting Claude Desktop...")
		if err := p.LaunchApp(); err != nil {
			return fmt.Errorf("launch Claude: %w", err)
		}

		fmt.Printf("Switched to %q.\n", name)
		return nil
	},
}
