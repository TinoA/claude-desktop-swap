package cmd

import (
	"fmt"
	"os"

	"github.com/FranCalveyra/claude-desktop-swap/internal/platform"
	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags.
var Version = "dev"
var dryRun bool

const ProductName = "Windows Claude Swap"

var root = &cobra.Command{
	Use:     "claude-desktop-swap",
	Short:   "Windows Claude Swap: switch Claude Desktop accounts without logging out",
	Version: Version,
}

func Execute() {
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	root.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "show paths and actions without changing data")
	root.AddCommand(cmdSave, cmdAdd, cmdUse, cmdList, cmdDelete, cmdStatus, cmdTray, cmdExport, cmdImport)
}

func printDryRun(action string, name string) error {
	p := platform.Current()
	appData, err := p.AppDataPath()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "DRY RUN: %s\napp-data: %s\nCookies: %s\nprofile: %s\nNo data, processes, or sessions will be modified.\n", action, appData, platform.CookiesPath(appData), name)
	return nil
}
