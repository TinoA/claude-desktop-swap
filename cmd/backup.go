package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/FranCalveyra/claude-desktop-swap/internal/platform"
	"github.com/FranCalveyra/claude-desktop-swap/internal/profile"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var exportPassword string
var importPassword string
var exportLocal bool

var cmdExport = &cobra.Command{
	Use:   "export <file>",
	Short: "Export all saved accounts to an encrypted backup",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := profile.NewStore()
		if err != nil {
			return err
		}
		password := ""
		if !exportLocal {
			password, err = backupPassword(exportPassword, "Backup password: ")
			if err != nil {
				return err
			}
		}
		lock, err := acquireOperationLock("operation")
		if err != nil {
			return err
		}
		defer lock.Release()
		if err := prepareBackupProfiles(store, platform.Current(), nil, os.Stdout); err != nil {
			return err
		}
		if exportLocal {
			if err := store.ExportLocal(args[0]); err != nil {
				return err
			}
			fmt.Printf("Windows-user-protected backup exported to %s.\n", args[0])
			return nil
		}
		if err := store.Export(args[0], password); err != nil {
			return err
		}
		fmt.Printf("Encrypted backup exported to %s.\n", args[0])
		return nil
	},
}

var cmdImport = &cobra.Command{
	Use:   "import <file>",
	Short: "Import all saved accounts from an encrypted backup",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		protection, err := profile.DetectBackupProtection(args[0])
		if err != nil {
			return err
		}
		password := ""
		if protection == profile.BackupProtectionPassword {
			password, err = backupPassword(importPassword, "Backup password: ")
			if err != nil {
				return err
			}
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
		if err := store.ImportAuto(args[0], password); err != nil {
			return err
		}
		fmt.Println("Accounts imported. Switch to one of them to activate it in Claude Desktop.")
		if incomplete, err := store.IncompleteProfiles(); err == nil && len(incomplete) > 0 {
			fmt.Printf("Warning: refresh these legacy profiles before creating a new backup: %s.\n", strings.Join(incomplete, ", "))
		}
		return nil
	},
}

type backupPreparationStore interface {
	Current() (string, error)
	Exists(string) bool
	MatchLiveAt(string) (string, profile.Health)
	CheckpointAt(string, string, string) error
	SetCurrent(string) error
	IncompleteProfiles() ([]string, error)
}

type backupProfileResolver func(current string) (string, error)

func prepareBackupProfiles(store backupPreparationStore, p platform.Platform, resolve backupProfileResolver, out io.Writer) (resultErr error) {
	appData, err := p.AppDataPath()
	if err != nil {
		if detector, ok := p.(platform.InstallationDetector); ok && !detector.IsInstalled() {
			return requireCompleteBackupProfiles(store)
		}
		return err
	}
	running, err := p.IsRunning()
	if err != nil {
		return fmt.Errorf("detect Claude Desktop: %w", err)
	}
	if running {
		fmt.Fprintln(out, "Stopping Claude Desktop to refresh the active profile...")
		if err := p.KillApp(); err != nil {
			return fmt.Errorf("stop Claude: %w", err)
		}
		defer func() {
			fmt.Fprintln(out, "Starting Claude Desktop...")
			if launchErr := p.LaunchApp(); launchErr != nil {
				if resultErr == nil {
					resultErr = fmt.Errorf("claude could not restart; launch manually: %w", launchErr)
				} else {
					resultErr = fmt.Errorf("%w; claude could not restart: %v", resultErr, launchErr)
				}
			}
		}()
	}

	live := platform.CookiesPath(appData)
	matched, health := store.MatchLiveAt(live)
	if health == profile.HealthUnknown {
		return errors.New("live Claude session cannot be verified; backup cancelled without changing saved profiles")
	}
	if health == profile.HealthUsable {
		name := matched
		if name == "" {
			current, _ := store.Current()
			if resolve == nil {
				return errors.New("live Claude session is not represented by a saved profile; run save <name> before export")
			}
			name, err = resolve(current)
			if err != nil {
				return err
			}
			if strings.TrimSpace(name) == "" {
				return errors.New("backup cancelled because the active account was not named")
			}
		}
		if err := store.CheckpointAt(name, appData, live); err != nil {
			return fmt.Errorf("refresh active profile %q: %w", name, err)
		}
		if err := store.SetCurrent(name); err != nil {
			return fmt.Errorf("track refreshed profile %q: %w", name, err)
		}
		fmt.Fprintf(out, "Profile %q refreshed.\n", name)
	}

	return requireCompleteBackupProfiles(store)
}

func requireCompleteBackupProfiles(store backupPreparationStore) error {
	incomplete, err := store.IncompleteProfiles()
	if err != nil {
		return err
	}
	if len(incomplete) > 0 {
		return fmt.Errorf("refresh these legacy profiles before backup: %s", strings.Join(incomplete, ", "))
	}
	return nil
}

func init() {
	cmdExport.Flags().StringVar(&exportPassword, "password", "", "backup password (omit to enter it securely)")
	cmdExport.Flags().BoolVar(&exportLocal, "local", false, "protect the backup with the current Windows user (no password)")
	cmdImport.Flags().StringVar(&importPassword, "password", "", "backup password (omit to enter it securely)")
}

func backupPassword(value, prompt string) (string, error) {
	if strings.TrimSpace(value) != "" {
		return value, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("backup password is required; use --password when stdin is not interactive")
	}
	fmt.Fprint(os.Stderr, prompt)
	data, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", errors.New("backup password cannot be empty")
	}
	return string(data), nil
}
