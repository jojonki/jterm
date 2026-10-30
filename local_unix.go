//go:build !windows

package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// localShell はローカルのシェルを PTY 上で起動し、終了するまで待つ。
// shell が空なら $SHELL をログインシェルとして起動する。
func (a *App) localShell(c *conn, shell string) error {
	argv := strings.Fields(shell)
	login := false
	if len(argv) == 0 {
		sh := os.Getenv("SHELL")
		if sh == "" {
			sh = "/bin/sh"
		}
		argv, login = []string{sh}, true
	}

	cmd := exec.CommandContext(c.ctx, argv[0], argv[1:]...)
	if login {
		cmd.Args[0] = "-" + filepath.Base(argv[0])
	}
	if home, err := os.UserHomeDir(); err == nil {
		cmd.Dir = home
	}
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGHUP) }
	cmd.WaitDelay = 2 * time.Second

	c.mu.Lock()
	cols, rows := c.cols, c.rows
	c.mu.Unlock()
	if cols <= 0 || rows <= 0 {
		cols, rows = 80, 24
	}
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		return err
	}
	defer ptmx.Close()

	c.mu.Lock()
	c.stdin = ptmx
	c.resize = func(cols, rows int) {
		pty.Setsize(ptmx, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	}
	c.mu.Unlock()
	a.emitStatus(c.id, "connected", "")

	io.Copy(&eventWriter{a: a, id: c.id}, ptmx) // シェル終了で EIO/EOF になる
	err = cmd.Wait()
	var exitErr *exec.ExitError
	if err == nil || c.ctx.Err() != nil || errors.As(err, &exitErr) {
		return nil
	}
	return err
}
