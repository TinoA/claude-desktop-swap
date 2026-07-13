//go:build !windows

package cmd

import (
	"errors"

	"github.com/spf13/cobra"
)

var cmdTray = &cobra.Command{
	Use:   "tray",
	Short: "Run the Claude account switcher in the system tray",
	RunE: func(cmd *cobra.Command, args []string) error {
		return errors.New("the system tray UI is currently supported on Windows only")
	},
}
