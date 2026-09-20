//go:build linux

package main

import "golang.org/x/sys/unix"

type terminalState = unix.Termios

func getTerminalState(fd int) (*terminalState, error) {
	return unix.IoctlGetTermios(fd, unix.TCGETS)
}

func setTerminalState(fd int, state *terminalState) error {
	return unix.IoctlSetTermios(fd, unix.TCSETS, state)
}

func configureTerminalRawish(state *terminalState) {
	state.Lflag &^= unix.ECHO | unix.ICANON
	state.Cc[unix.VMIN] = 0
	state.Cc[unix.VTIME] = 1 // 100ms timeout per byte
}
