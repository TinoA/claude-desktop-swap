package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/FranCalveyra/claude-desktop-swap/internal/platform"
	"github.com/FranCalveyra/claude-desktop-swap/internal/profile"
	"github.com/spf13/cobra"
)

var cmdUse = &cobra.Command{
	Use:   "use [name]",
	Short: "Switch to a saved profile (kills and restarts Claude Desktop)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if dryRun {
			name := "<interactive>"
			if len(args) == 1 {
				name = args[0]
			}
			return printDryRun("use: checkpoint, atomic restore, Desktop restart", name)
		}
		store, err := profile.NewStore()
		if err != nil {
			return err
		}

		name, err := profileNameFromArgs(args, store)
		if err != nil {
			return err
		}
		if name == "" {
			return nil
		}

		return switchProfile(name, store)
	},
}

func profileNameFromArgs(args []string, store *profile.Store) (string, error) {
	if len(args) == 1 {
		return args[0], nil
	}

	profiles, err := store.List()
	if err != nil {
		return "", err
	}
	if len(profiles) == 0 {
		fmt.Println("No profiles saved. Run 'claude-desktop-swap save <name>' to create one.")
		return "", nil
	}

	current := ""
	if appData, err := platform.Current().AppDataPath(); err == nil {
		current, _ = store.MatchLiveAt(platform.CookiesPath(appData))
	}
	enrichLiveAccounts(store, profiles)
	return runPicker(profiles, current)
}

func switchProfile(name string, store *profile.Store) error {
	lock, err := acquireOperationLock("operation")
	if err != nil {
		return err
	}
	defer lock.Release()
	overlay := startSwitchOverlay()
	defer overlay.Close()
	err = switchProfileWith(name, store, platform.Current(), os.Stdout)
	if err == nil {
		if pending, pendingErr := loadPendingAdd(); pendingErr == nil && pending.Previous == name {
			_ = clearPendingAdd()
		}
	}
	return err
}

type switchStore interface {
	Exists(string) bool
	Inspect(string) profile.Inspection
	Current() (string, error)
	Checkpoint(string, string) error
	Restore(string, string) error
}

type pathSwitcher interface {
	CheckpointAt(string, string, string) error
	RestoreAt(string, string, string) error
}

type sessionUpdateConfirmer func(current, target string) bool

func switchProfileWith(name string, store switchStore, p platform.Platform, out io.Writer, confirmers ...sessionUpdateConfirmer) error {
	appData, err := p.AppDataPath()
	if err != nil {
		return err
	}

	if !store.Exists(name) {
		return fmt.Errorf("profile %q not found — run 'claude-desktop-swap list' to see available profiles", name)
	}
	inspection := store.Inspect(name)
	if inspection.Health != profile.HealthUsable {
		return fmt.Errorf("profile %q is %s; reauthentication is required before switching", name, inspection.Health)
	}

	wasRunning, err := p.IsRunning()
	if err != nil {
		return fmt.Errorf("detect Claude Desktop: %w", err)
	}
	current, currentErr := store.Current()
	liveName, liveHealth, hasLiveMatcher := matchLiveProfile(store, platform.CookiesPath(appData))
	if currentErr != nil {
		current = ""
	}

	if hasLiveMatcher && liveName == name && liveHealth == profile.HealthUsable {
		if err := markCurrentProfile(store, name); err != nil {
			return fmt.Errorf("track active profile: %w", err)
		}
		if wasRunning {
			fmt.Fprintf(out, "Profile %q is already active.\n", name)
			return nil
		}
		fmt.Fprintln(out, "Starting Claude Desktop...")
		if err := p.LaunchApp(); err != nil {
			return fmt.Errorf("profile %q is active but Claude could not start; launch manually: %w", name, err)
		}
		fmt.Fprintf(out, "Switched to %q.\n", name)
		return nil
	}

	fmt.Fprintln(out, "Stopping Claude Desktop...")
	stopped := false
	if wasRunning {
		if err := p.KillApp(); err != nil {
			return fmt.Errorf("stop Claude: %w", err)
		}
		stopped = true
	}
	relaunchPrevious := func(operationErr error) error {
		if !stopped {
			return operationErr
		}
		if launchErr := p.LaunchApp(); launchErr != nil {
			return fmt.Errorf("%w; could not relaunch previous Claude session: %v", operationErr, launchErr)
		}
		return operationErr
	}

	confirmedSessionUpdate := false
	if hasLiveMatcher {
		liveName, liveHealth, _ = matchLiveProfile(store, platform.CookiesPath(appData))
		switch liveHealth {
		case profile.HealthUsable:
			if liveName == "" {
				if current == "" || !store.Exists(current) || len(confirmers) == 0 || confirmers[0] == nil {
					return relaunchPrevious(errors.New("live Claude session is not recognized by a saved profile; refusing to overwrite it"))
				}
				if !confirmers[0](current, name) {
					return relaunchPrevious(errors.New("account switch cancelled; the live Claude session was not changed"))
				}
				liveName = current
				confirmedSessionUpdate = true
			}
		case profile.HealthUnknown:
			return relaunchPrevious(errors.New("live Claude session cannot be verified; refusing to overwrite it"))
		}
	}

	outgoing := ""
	if !hasLiveMatcher {
		outgoing = current
	} else if liveHealth == profile.HealthUsable {
		outgoing = liveName
	}
	if outgoing != "" && (outgoing != name || confirmedSessionUpdate) {
		fmt.Fprintf(out, "Checkpointing profile %q...\n", outgoing)
		var checkpointErr error
		if routed, ok := store.(pathSwitcher); ok {
			checkpointErr = routed.CheckpointAt(outgoing, appData, platform.CookiesPath(appData))
		} else {
			checkpointErr = store.Checkpoint(outgoing, appData)
		}
		if checkpointErr != nil {
			return relaunchPrevious(fmt.Errorf("checkpoint outgoing profile: %w", checkpointErr))
		}
	}

	if liveName == name && liveHealth == profile.HealthUsable {
		if err := markCurrentProfile(store, name); err != nil {
			return fmt.Errorf("track active profile: %w", err)
		}
		fmt.Fprintln(out, "Starting Claude Desktop...")
		if err := p.LaunchApp(); err != nil {
			return fmt.Errorf("profile %q is active but Claude could not start; launch manually: %w", name, err)
		}
		fmt.Fprintf(out, "Switched to %q.\n", name)
		return nil
	}

	fmt.Fprintf(out, "Restoring profile %q...\n", name)
	var restoreErr error
	if routed, ok := store.(pathSwitcher); ok {
		restoreErr = routed.RestoreAt(name, appData, platform.CookiesPath(appData))
	} else {
		restoreErr = store.Restore(name, appData)
	}
	if restoreErr != nil {
		return relaunchPrevious(fmt.Errorf("restore profile: %w", restoreErr))
	}

	fmt.Fprintln(out, "Starting Claude Desktop...")
	if err := p.LaunchApp(); err != nil {
		if retryErr := p.LaunchApp(); retryErr != nil {
			return fmt.Errorf("profile %q is active but Claude could not start; launch manually: %w", name, err)
		}
	}

	fmt.Fprintf(out, "Switched to %q.\n", name)
	return nil
}

func matchLiveProfile(store switchStore, cookies string) (name string, health profile.Health, supported bool) {
	matcher, ok := store.(interface {
		MatchLiveAt(string) (string, profile.Health)
	})
	if !ok {
		return "", profile.HealthUnknown, false
	}
	name, health = matcher.MatchLiveAt(cookies)
	return name, health, true
}

func markCurrentProfile(store switchStore, name string) error {
	setter, ok := store.(interface{ SetCurrent(string) error })
	if !ok {
		return nil
	}
	return setter.SetCurrent(name)
}
