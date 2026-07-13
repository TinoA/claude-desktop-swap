package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/FranCalveyra/claude-desktop-swap/internal/platform"
	"github.com/FranCalveyra/claude-desktop-swap/internal/profile"
	"github.com/spf13/cobra"
)

var cmdSave = &cobra.Command{
	Use:   "save <name>",
	Short: "Save the current session as a named profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if dryRun {
			return printDryRun("save/checkpoint + optional Desktop restart", args[0])
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
		return saveProfileWith(args[0], store, platform.Current(), os.Stdout)
	},
}

type saveStore interface {
	Checkpoint(string, string) error
}

type pathCheckpointer interface {
	CheckpointAt(string, string, string) error
}

func saveProfileWith(name string, store saveStore, p platform.Platform, out io.Writer) error {
	appData, err := p.AppDataPath()
	if err != nil {
		return err
	}
	running, err := p.IsRunning()
	if err != nil {
		return err
	}
	stopped := false
	relaunchOnError := func(operationErr error) error {
		if !stopped {
			return operationErr
		}
		if launchErr := p.LaunchApp(); launchErr != nil {
			return fmt.Errorf("%w; Claude could not be relaunched: %v", operationErr, launchErr)
		}
		return operationErr
	}
	if running {
		fmt.Fprintln(out, "Stopping Claude Desktop...")
		if err := p.KillApp(); err != nil {
			return fmt.Errorf("stop Claude: %w", err)
		}
		stopped = true
	}

	var checkpointErr error
	if routed, ok := store.(pathCheckpointer); ok {
		checkpointErr = routed.CheckpointAt(name, appData, platform.CookiesPath(appData))
	} else {
		checkpointErr = store.Checkpoint(name, appData)
	}
	if checkpointErr != nil {
		return relaunchOnError(checkpointErr)
	}
	if setter, ok := store.(interface{ SetCurrent(string) error }); ok {
		if err := setter.SetCurrent(name); err != nil {
			return relaunchOnError(fmt.Errorf("profile %q saved but active tracking failed: %w", name, err))
		}
	}
	fmt.Fprintf(out, "Profile %q saved.\n", name)

	if running {
		fmt.Fprintln(out, "Starting Claude Desktop...")
		if err := p.LaunchApp(); err != nil {
			if retryErr := p.LaunchApp(); retryErr != nil {
				return fmt.Errorf("profile %q saved but Claude could not restart; launch manually: %w", name, err)
			}
		}
	}
	return nil
}
