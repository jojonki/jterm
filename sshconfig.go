package main

import (
	"bufio"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// sshConfigBlock は ssh_config の Host 行ひとつ分(先頭の共通部分は patterns が空)。
type sshConfigBlock struct {
	patterns []string
	opts     map[string]string // キーは小文字。同じキーは最初の値を採用する
}

// ImportResult は ~/.ssh/config の取り込み結果。
type ImportResult struct {
	Added       int `json:"added"`
	Skipped     int `json:"skipped"`     // 既に同じセッションがあるもの
	Unsupported int `json:"unsupported"` // ProxyJump / ProxyCommand が指定されているもの(接続はできない可能性がある)
}

// ImportSSHConfig は ~/.ssh/config の Host エイリアスをセッションとして取り込む。
func (a *App) ImportSSHConfig() (ImportResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return ImportResult{}, err
	}
	blocks, err := parseSSHConfig(filepath.Join(home, ".ssh", "config"), 0)
	if err != nil {
		return ImportResult{}, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	var res ImportResult
	for _, alias := range sshConfigAliases(blocks) {
		opts := resolveSSHConfig(blocks, alias)
		host := opts["hostname"]
		if host == "" {
			host = alias
		}
		host = strings.ReplaceAll(host, "%h", alias)
		port := 22
		if p, err := strconv.Atoi(opts["port"]); err == nil && p >= 1 && p <= 65535 {
			port = p
		}
		user := opts["user"]
		key := opts["identityfile"]
		if strings.Contains(key, "%") {
			key = ""
		}

		if a.store.hasImported(alias, host, port, user) {
			res.Skipped++
			continue
		}
		a.store.Sessions = append(a.store.Sessions, &Session{
			ID: newID(), Name: alias, Host: host, Port: port, User: user, KeyFile: key,
		})
		res.Added++
		if opts["proxyjump"] != "" || opts["proxycommand"] != "" {
			res.Unsupported++
		}
	}
	if res.Added > 0 {
		if err := a.store.save(); err != nil {
			return ImportResult{}, err
		}
		a.notifySessions()
	}
	return res, nil
}

func (s *Store) hasImported(name, host string, port int, user string) bool {
	for _, x := range s.Sessions {
		if x.Name == name && x.Host == host && x.Port == port && x.User == user {
			return true
		}
	}
	return false
}

// parseSSHConfig は設定ファイルを読み、Include も展開してブロック列を返す。
// ファイルが無いときは空を返す。
func parseSSHConfig(file string, depth int) ([]sshConfigBlock, error) {
	f, err := os.Open(file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	blocks := []sshConfigBlock{{opts: map[string]string{}}}
	skip := false // Match ブロックの中身は解釈しない
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, val := splitSSHConfigLine(sc.Text())
		switch key {
		case "":
		case "host":
			skip = false
			blocks = append(blocks, sshConfigBlock{patterns: splitSSHConfigFields(val), opts: map[string]string{}})
		case "match":
			skip = true
		case "include":
			if depth >= 5 {
				continue
			}
			for _, pat := range splitSSHConfigFields(val) {
				if !filepath.IsAbs(pat) && !strings.HasPrefix(pat, "~") {
					pat = filepath.Join(filepath.Dir(file), pat)
				}
				matches, _ := filepath.Glob(expandHome(pat))
				sort.Strings(matches)
				for _, m := range matches {
					sub, err := parseSSHConfig(m, depth+1)
					if err == nil {
						blocks = append(blocks, sub...)
					}
				}
			}
			// Include の後ろの設定が直前の Host に混ざらないよう、新しい共通ブロックを開く
			blocks = append(blocks, sshConfigBlock{opts: map[string]string{}})
		default:
			if skip {
				continue
			}
			b := &blocks[len(blocks)-1]
			if _, ok := b.opts[key]; !ok {
				b.opts[key] = unquote(val)
			}
		}
	}
	return blocks, sc.Err()
}

// splitSSHConfigLine は "Key value" と "Key=value" の両方を分解する。コメント行は空のキーを返す。
func splitSSHConfigLine(line string) (key, val string) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", ""
	}
	i := strings.IndexAny(line, " \t=")
	if i < 0 {
		return strings.ToLower(line), ""
	}
	key = strings.ToLower(line[:i])
	val = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line[i:]), "="))
	return key, val
}

// splitSSHConfigFields は空白区切りの値を、引用符を考慮して分割する。
func splitSSHConfigFields(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote, has := false, false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote, has = !inQuote, true
		case (r == ' ' || r == '\t') && !inQuote:
			if has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		case r == '#' && !inQuote && !has:
			return out
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	if has {
		out = append(out, cur.String())
	}
	return out
}

func unquote(s string) string {
	if f := splitSSHConfigFields(s); len(f) > 0 {
		return f[0]
	}
	return ""
}

// sshConfigAliases はワイルドカードや否定を含まない Host 名を、登場順に重複なしで返す。
func sshConfigAliases(blocks []sshConfigBlock) []string {
	var out []string
	seen := map[string]bool{}
	for _, b := range blocks {
		for _, p := range b.patterns {
			if strings.ContainsAny(p, "*?!") || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// resolveSSHConfig は alias に一致する全ブロックから、ssh と同じく最初に現れた値を採用する。
func resolveSSHConfig(blocks []sshConfigBlock, alias string) map[string]string {
	res := map[string]string{}
	for _, b := range blocks {
		if len(b.patterns) > 0 && !sshHostMatches(b.patterns, alias) {
			continue
		}
		for k, v := range b.opts {
			if _, ok := res[k]; !ok {
				res[k] = v
			}
		}
	}
	return res
}

func sshHostMatches(patterns []string, host string) bool {
	matched := false
	for _, p := range patterns {
		neg := strings.HasPrefix(p, "!")
		if neg {
			p = p[1:]
		}
		if ok, _ := path.Match(p, host); ok {
			if neg {
				return false
			}
			matched = true
		}
	}
	return matched
}
