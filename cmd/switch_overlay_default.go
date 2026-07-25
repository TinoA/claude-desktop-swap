//go:build !windows

package cmd

type switchOverlay struct{}

func startSwitchOverlay(...string) *switchOverlay         { return &switchOverlay{} }
func startAddPreparationOverlay(...string) *switchOverlay { return &switchOverlay{} }
func startAddSuccessOverlay(...string) *switchOverlay     { return &switchOverlay{} }
func (*switchOverlay) Close()                             {}
