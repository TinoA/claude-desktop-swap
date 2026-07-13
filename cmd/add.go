package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/FranCalveyra/claude-desktop-swap/internal/platform"
	"github.com/FranCalveyra/claude-desktop-swap/internal/profile"
	"github.com/spf13/cobra"
)

var cmdAdd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add a new account interactively without manual logout",
	Long: `Snapshots your current session, launches Claude Desktop with a clean
state, waits for you to log in as a new account, then snapshots that
new session as <name>. No manual logout required.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if dryRun {
			return printDryRun("add: stop, wipe session, launch, checkpoint; not executed", name)
		}

		p := platform.Current()
		store, err := profile.NewStore()
		if err != nil {
			return err
		}
		workflow, err := newAddWorkflow(store, p)
		if err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		preparation := startAddPreparationOverlay()
		err = workflow.Begin(name)
		if err == nil {
			err = waitForClaudeLoginWindow(ctx, p)
		}
		preparation.Close()
		if err != nil {
			return err
		}

		fmt.Printf("Claude Desktop is ready. Log in as the new account; registration will finish automatically for %q. Press Ctrl+C to cancel: ", name)
		if err := workflow.WaitAndComplete(ctx); err != nil {
			if errors.Is(err, context.Canceled) {
				return errAddCancelled
			}
			return err
		}
		success := startAddSuccessOverlay()
		defer success.Close()
		// Keep the confirmation visible long enough to be noticed while Claude
		// has already been relaunched with the new profile.
		select {
		case <-ctx.Done():
		case <-time.After(1500 * time.Millisecond):
		}
		fmt.Printf("Profile %q saved and Claude Desktop restarted.\n", name)
		return nil
	},
}

func waitForClaudeLoginWindow(ctx context.Context, p platform.Platform) error {
	waiter, ok := p.(platform.LoginWindowWaiter)
	if !ok {
		return nil
	}
	waitCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if err := waiter.WaitForLoginWindow(waitCtx); errors.Is(err, context.DeadlineExceeded) {
		return nil
	} else {
		return err
	}
}

func confirmAddFromInput(input io.Reader, workflow *addWorkflow) error {
	if _, err := bufio.NewReader(input).ReadString('\n'); err != nil {
		cancelErr := workflow.Cancel()
		if cancelErr != nil {
			return fmt.Errorf("login confirmation failed: %w; recovery failed: %v", err, cancelErr)
		}
		return fmt.Errorf("login confirmation cancelled: %w", err)
	}
	return nil
}

type trackedCheckpointer interface {
	Current() (string, error)
	Checkpoint(string, string) error
}

func checkpointTrackedSession(store trackedCheckpointer, appData string) error {
	current, _ := store.Current()
	if current == "" {
		return fmt.Errorf("active session has no tracked profile; save it before continuing")
	}
	if routed, ok := store.(pathCheckpointer); ok {
		return routed.CheckpointAt(current, appData, platform.CookiesPath(appData))
	}
	return store.Checkpoint(current, appData)
}
