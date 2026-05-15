package cmd

import (
	"fmt"

	"github.com/FranCalveyra/claude-desktop-swap/internal/profile"
	"github.com/spf13/cobra"
)

var cmdDelete = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a saved profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		store, err := profile.NewStore()
		if err != nil {
			return err
		}

		if err := store.Delete(name); err != nil {
			return err
		}

		fmt.Printf("Profile %q deleted.\n", name)
		return nil
	},
}
