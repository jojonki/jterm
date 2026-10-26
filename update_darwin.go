package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// installUpdate は dmg の中の jterm.app で、いま動いている .app を置き換える。
// 戻り値は、jterm の終了を待って古い版を消し、新しい版を開くコマンド。
func installUpdate(dmg string) (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return nil, err
	}
	// .../jterm.app/Contents/MacOS/jterm
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if !strings.HasSuffix(bundle, ".app") {
		return nil, errors.New("jterm is not running from an app bundle")
	}
	if strings.HasPrefix(bundle, "/Volumes/") || strings.Contains(bundle, "/AppTranslocation/") {
		return nil, errors.New("move jterm.app to the Applications folder first, then update")
	}

	mnt, err := os.MkdirTemp("", "jterm-dmg-")
	if err != nil {
		return nil, err
	}
	defer os.Remove(mnt)
	if out, err := exec.Command("hdiutil", "attach", "-nobrowse", "-readonly", "-noautoopen", "-mountpoint", mnt, dmg).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("could not open the disk image: %v: %s", err, strings.TrimSpace(string(out)))
	}
	defer exec.Command("hdiutil", "detach", "-force", mnt).Run()
	apps, _ := filepath.Glob(filepath.Join(mnt, "*.app"))
	if len(apps) != 1 {
		return nil, errors.New("the disk image does not contain jterm.app")
	}

	// 置き換えを rename で済ませられるよう、同じフォルダに展開する
	stage, err := os.MkdirTemp(filepath.Dir(bundle), ".jterm-update-")
	if err != nil {
		return nil, fmt.Errorf("cannot write to %s: %w", filepath.Dir(bundle), err)
	}
	ok := false
	defer func() {
		if !ok {
			os.RemoveAll(stage)
		}
	}()
	newApp := filepath.Join(stage, "jterm.app")
	if out, err := exec.Command("ditto", apps[0], newApp).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("could not copy the new version: %v: %s", err, strings.TrimSpace(string(out)))
	}
	if err := checkSameSigner(bundle, newApp); err != nil {
		return nil, err
	}

	old := filepath.Join(stage, "old.app")
	if err := os.Rename(bundle, old); err != nil {
		return nil, err
	}
	if err := os.Rename(newApp, bundle); err != nil {
		os.Rename(old, bundle)
		return nil, err
	}
	ok = true

	const script = `while kill -0 "$1" 2>/dev/null; do sleep 0.2; done; rm -rf "$2"; open "$3"`
	cmd := exec.Command("/bin/sh", "-c", script, "sh", strconv.Itoa(os.Getpid()), stage, bundle)
	// jterm の終了に巻き込まれないよう、別のプロセスグループで動かす
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd, nil
}

// checkSameSigner は、新しい .app が今の .app と同じ開発者の署名を持つか確かめる。
// 今の .app が Developer ID で署名されていない開発版のときは確かめない。
func checkSameSigner(cur, next string) error {
	out, err := exec.Command("codesign", "-d", "-r-", cur).CombinedOutput()
	if err != nil {
		return nil
	}
	_, req, found := strings.Cut(string(out), "designated => ")
	req = strings.TrimSpace(req)
	if !found || !strings.Contains(req, "anchor apple generic") {
		return nil
	}
	if out, err := exec.Command("codesign", "--verify", "--deep", "--strict", "-R="+req, next).CombinedOutput(); err != nil {
		return fmt.Errorf("the downloaded app is not signed by the jterm developer: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func cleanupOldUpdate() {}
