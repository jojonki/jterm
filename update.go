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
const appVersion = "0.1.2"

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

// DownloadUpdate は最新リリースの配布ファイルをダウンロードフォルダに保存し、開く。
// macOS では dmg がマウントされ、Windows ではエクスプローラーで zip が選択される。
// 保存先のパスを返す。
func (a *App) DownloadUpdate() (string, error) {
	rel, err := fetchLatest(a.ctx)
	if err != nil {
		return "", err
	}
	name, url := rel.asset()
	if url == "" {
		return "", fmt.Errorf("no download for this platform in %s", rel.TagName)
	}
	// ファイル名はリリース側の値なので、パス区切りを含んでいても保存先から出ないようにする
	name = filepath.Base(name)

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, name)

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
	tmp, err := os.CreateTemp(dir, name+".part-*")
	if err != nil {
		return "", err
	}
	_, err = io.Copy(tmp, resp.Body)
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
