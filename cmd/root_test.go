package cmd

import (
	"slices"
	"testing"
)

func TestStartupArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		goos string
		want []string
	}{
		{name: "Windows double click opens tray", goos: "windows", want: []string{"tray"}},
		{name: "Windows explicit command is preserved", args: []string{"list"}, goos: "windows", want: []string{"list"}},
		{name: "Other systems keep existing behavior", goos: "linux"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := startupArgs(test.args, test.goos); !slices.Equal(got, test.want) {
				t.Fatalf("startupArgs(%v, %q) = %v, want %v", test.args, test.goos, got, test.want)
			}
		})
	}
}
