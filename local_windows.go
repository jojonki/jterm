//go:build windows

package main

import "errors"

func (a *App) localShell(c *conn, shell string) error {
	return errors.New("Local shell is not supported on Windows yet")
}
