//go:build !darwin && !windows

package main

import (
	"errors"
	"os/exec"
)

func installUpdate(string) (*exec.Cmd, error) {
	return nil, errors.New("automatic update is not supported on this platform")
}

func cleanupOldUpdate() {}
