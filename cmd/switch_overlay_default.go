//go:build !windows

package cmd

type switchOverlay struct{}

func startSwitchOverlay() *switchOverlay            { return &switchOverlay{} }
func startAddPreparationOverlay() *switchOverlay    { return &switchOverlay{} }
func startAddSuccessOverlay() *switchOverlay        { return &switchOverlay{} }
func startBackupPreparationOverlay() *switchOverlay { return &switchOverlay{} }
func (*switchOverlay) Close()                       {}
