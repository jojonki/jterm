package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// appVersion は現在のバージョン。リリース時に wails.json の productVersion と合わせて更新する。
const appVersion = "0.1.4"

const latestReleaseAPI = "https://api.github.com/repos/jojonki/jterm/releases/latest"

// UpdateInfo は最新リリースの確認結果。
type UpdateInfo struct {
	Available bool   `json:"available"`
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Notes     string `json:"notes"`
	PageURL   string `json:"pageUrl"`
	AssetName string `json:"assetName"` // この OS 向けの配布ファイル。無ければ空
}

type ghRelease struct {
	TagName string `json:"tag_name"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// AppVersion は実行中のバージョンを返す。
func (a *App) AppVersion() string { return appVersion }

func fetchLatest(ctx context.Context) (*ghRelease, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "jterm/"+appVersion)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub returned %s", resp.Status)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// assetSuffix は OS ごとの配布ファイルの末尾。macOS は dmg、Windows は zip。
func assetSuffix() string {
	if goruntime.GOOS == "darwin" {
		return ".dmg"
	}
	return "-windows-amd64.zip"
}

func (r *ghRelease) asset() (name, url string) {
	if goruntime.GOOS != "darwin" && goruntime.GOOS != "windows" {
		return "", ""
	}
	for _, as := range r.Assets {
		if strings.HasSuffix(as.Name, assetSuffix()) {
			return as.Name, as.URL
		}
	}
	return "", ""
}

// versionNewer は a が b より新しい "x.y.z" かどうかを返す。解釈できなければ false。
func versionNewer(a, b string) bool {
	pa, pb := parseVersion(a), parseVersion(b)
	if pa == nil || pb == nil {
		return false
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parseVersion(s string) []int {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(s), "v"), ".")
	if len(parts) != 3 {
		return nil
	}
	out := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		out[i] = n
	}
	return out
}

// CheckForUpdate は GitHub の最新リリースを調べ、現在より新しければ Available を立てて返す。
func (a *App) CheckForUpdate() (UpdateInfo, error) {
	info := UpdateInfo{Current: appVersion}
	rel, err := fetchLatest(a.ctx)
	if err != nil {
		return info, err
	}
	info.Latest = strings.TrimPrefix(rel.TagName, "v")
	info.Notes = rel.Body
	info.PageURL = rel.HTMLURL
	info.AssetName, _ = rel.asset()
	info.Available = versionNewer(info.Latest, appVersion)
	return info, nil
}

// latestAsset は最新リリースのこの OS 向け配布ファイルの名前と URL を返す。
func latestAsset(ctx context.Context) (name, url string, err error) {
	rel, err := fetchLatest(ctx)
	if err != nil {
		return "", "", err
	}
	name, url = rel.asset()
	if url == "" {
		return "", "", fmt.Errorf("no download for this platform in %s", rel.TagName)
	}
	// ファイル名はリリース側の値なので、パス区切りを含んでいても保存先から出ないようにする
	return filepath.Base(name), url, nil
}

// progressWriter は書き込んだ量を数え、1% 進むごとに通知する。
type progressWriter struct {
	done, total int64
	last        int
	onPercent   func(int)
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.done += int64(len(p))
	if w.total > 0 {
		if pct := int(w.done * 100 / w.total); pct != w.last {
			w.last = pct
			w.onPercent(pct)
		}
	}
	return len(p), nil
}

// download は url を dir/name に保存し、そのパスを返す。進み具合は update:progress イベントで知らせる。
func (a *App) download(url, dir, name string) (string, error) {
	req, err := http.NewRequestWithContext(a.ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "jterm/"+appVersion)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: %s", resp.Status)
	}

	// 途中で失敗した不完全なファイルを残さないよう、一時ファイルに書いてから置き換える
	dest := filepath.Join(dir, name)
	tmp, err := os.CreateTemp(dir, name+".part-*")
	if err != nil {
		return "", err
	}
	pw := &progressWriter{total: resp.ContentLength, last: -1, onPercent: func(pct int) {
		runtime.EventsEmit(a.ctx, "update:progress", pct)
	}}
	_, err = io.Copy(io.MultiWriter(tmp, pw), resp.Body)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), dest)
	}
	if err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return dest, nil
}

// InstallUpdate は最新リリースをダウンロードして今のアプリと置き換え、jterm を再起動する。
// dmg の中から起動しているときなど、置き換えられないときはエラーを返す(画面側は DownloadUpdate に切り替える)。
func (a *App) InstallUpdate() error {
	name, url, err := latestAsset(a.ctx)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "jterm-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	file, err := a.download(url, dir, name)
	if err != nil {
		return err
	}
	runtime.EventsEmit(a.ctx, "update:installing")
	relaunch, err := installUpdate(file)
	if err != nil {
		return err
	}
	if err := relaunch.Start(); err != nil {
		return fmt.Errorf("updated, but could not restart jterm (please start it again): %w", err)
	}
	runtime.Quit(a.ctx)
	return nil
}

// DownloadUpdate は最新リリースの配布ファイルをダウンロードフォルダに保存し、開く。
// macOS では dmg がマウントされ、Windows ではエクスプローラーで zip が選択される。
// 保存先のパスを返す。
func (a *App) DownloadUpdate() (string, error) {
	name, url, err := latestAsset(a.ctx)
	if err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest, err := a.download(url, dir, name)
	if err != nil {
		return "", err
	}

	switch goruntime.GOOS {
	case "darwin":
		_ = exec.Command("open", dest).Start()
	case "windows":
		_ = exec.Command("explorer", "/select,", dest).Start()
	}
	return dest, nil
}

// OpenReleasePage は既定のブラウザでリリースページを開く。
func (a *App) OpenReleasePage() {
	runtime.BrowserOpenURL(a.ctx, "https://github.com/jojonki/jterm/releases/latest")
}
