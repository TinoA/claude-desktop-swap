package cmd

import (
	"fmt"

	"github.com/FranCalveyra/claude-desktop-swap/internal/profile"
	"github.com/spf13/cobra"
)

var cmdStatus = &cobra.Command{
	Use:   "status",
	Short: "Show the currently active profile",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := profile.NewStore()
		if err != nil {
			return err
		}

		current, err := store.Current()
		if err != nil || current == "" {
			fmt.Println("No active profile.")
			return nil
		}

		fmt.Printf("Active profile: %s\n", current)
		return nil
	},
}
