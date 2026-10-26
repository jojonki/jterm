package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// installUpdate は zip の中の jterm.exe で、いま動いている exe を置き換える。
// 動いている exe は消せないが名前は変えられるので、.old に退避して次回起動時に消す。
// 戻り値は、jterm の終了を待って新しい版を起動するコマンド。
func installUpdate(zipPath string) (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var src *zip.File
	for _, f := range zr.File {
		if strings.EqualFold(filepath.Base(f.Name), "jterm.exe") {
			src = f
			break
		}
	}
	if src == nil {
		return nil, errors.New("the zip does not contain jterm.exe")
	}

	newExe := exe + ".new"
	if err := extract(src, newExe); err != nil {
		os.Remove(newExe)
		return nil, fmt.Errorf("cannot write to %s: %w", filepath.Dir(exe), err)
	}
	old := exe + ".old"
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		os.Remove(newExe)
		return nil, err
	}
	if err := os.Rename(newExe, exe); err != nil {
		os.Rename(old, exe)
		return nil, err
	}

	script := fmt.Sprintf("Wait-Process -Id %d -ErrorAction SilentlyContinue; Start-Process -FilePath '%s'",
		os.Getpid(), strings.ReplaceAll(exe, "'", "''"))
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return cmd, nil
}

func extract(f *zip.File, dest string) error {
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := os.Create(dest)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	return err
}

// cleanupOldUpdate は前回の更新で退避した古い exe を消す。
func cleanupOldUpdate() {
	if exe, err := os.Executable(); err == nil {
		os.Remove(exe + ".old")
	}
}
