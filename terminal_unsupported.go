//go:build !linux && !darwin

package main

import "fmt"

type terminalState struct{}

func getTerminalState(int) (*terminalState, error) {
	return nil, fmt.Errorf("terminal color queries are not supported on this platform")
}

func setTerminalState(int, *terminalState) error {
	return nil
}

func configureTerminalRawish(*terminalState) {}
