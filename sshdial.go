package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

var errCanceled = errors.New("canceled")

type promptKind string

const (
	promptText     promptKind = "text"
	promptSecret   promptKind = "secret"
	promptPassword promptKind = "password" // secret に「保存する」の選択肢が付いたもの
	promptConfirm  promptKind = "confirm"
)

type promptRequest struct {
	Kind    promptKind
	Title   string
	Message string
}

type promptReply struct {
	value    string
	ok       bool
	remember bool
}

// prompter は接続中にユーザーへ問い合わせる手段。
// キャンセルされた場合は errCanceled を返す。
type prompter interface {
	prompt(req promptRequest) (promptReply, error)
	info(message string)
}

// dialResult は認証に使ったパスワードを保存すべきかどうかを含む。
type dialResult struct {
	client             *ssh.Client
	passwordToRemember string
}

// knownHostsMu は known_hosts の確認と追記を直列化する。
// 同じホストへ同時に接続したときに確認ダイアログが重複しないようにするため。
var knownHostsMu sync.Mutex

// dialSession は認証まで済ませた SSH クライアントを返す。
func dialSession(ctx context.Context, s Session, p prompter) (dialResult, error) {
	user := s.User
	if user == "" {
		r, err := p.prompt(promptRequest{Kind: promptText, Title: "Username", Message: "Username for " + s.Host})
		if err != nil {
			return dialResult{}, err
		}
		user = strings.TrimSpace(r.value)
	}
	port := s.Port
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(s.Host, strconv.Itoa(port))

	dir, err := configDir()
	if err != nil {
		return dialResult{}, err
	}
	khPath := filepath.Join(dir, "known_hosts")

	// パスワードは「保存済み → 入力」の順に使う。
	triedSaved, usedSaved := false, false
	remember := ""
	getPassword := func(message string) (string, error) {
		if !triedSaved && s.HasPassword {
			triedSaved = true
			if pw, err := getSecret(s.ID); err == nil {
				usedSaved = true
				return pw, nil
			}
		}
		if usedSaved {
			usedSaved = false
			message = "The saved password was rejected.\n" + message
		}
		r, err := p.prompt(promptRequest{Kind: promptPassword, Title: "Password", Message: message})
		if err != nil {
			return "", err
		}
		remember = ""
		if r.remember {
			remember = r.value
		}
		return r.value, nil
	}

	var auths []ssh.AuthMethod
	if signers := loadSigners(p, s.KeyFile); len(signers) > 0 {
		auths = append(auths, ssh.PublicKeys(signers...))
	}
	auths = append(auths,
		ssh.RetryableAuthMethod(ssh.PasswordCallback(func() (string, error) {
			return getPassword(fmt.Sprintf("Password for %s@%s", user, s.Host))
		}), 3),
		ssh.RetryableAuthMethod(ssh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
			header := strings.TrimSpace(name + "\n" + instruction)
			answers := make([]string, len(questions))
			for i, q := range questions {
				msg := strings.TrimSpace(header + "\n" + q)
				if echos[i] {
					r, err := p.prompt(promptRequest{Kind: promptText, Title: "Authentication", Message: msg})
					if err != nil {
						return nil, err
					}
					answers[i] = r.value
					continue
				}
				pw, err := getPassword(msg)
				if err != nil {
					return nil, err
				}
				answers[i] = pw
			}
			return answers, nil
		}), 3),
	)

	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            auths,
		HostKeyCallback: hostKeyCallback(p, khPath),
		Timeout:         15 * time.Second,
	}

	p.info(fmt.Sprintf("Connecting to %s ...", addr))
	d := net.Dialer{Timeout: cfg.Timeout}
	nc, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return dialResult{}, err
	}
	// 認証待ち(プロンプト表示中など)にキャンセルされたら接続を切る
	stop := context.AfterFunc(ctx, func() { nc.Close() })
	defer stop()

	cc, chans, reqs, err := ssh.NewClientConn(nc, addr, cfg)
	if err != nil {
		nc.Close()
		if ctx.Err() != nil {
			return dialResult{}, errCanceled
		}
		return dialResult{}, err
	}
	return dialResult{client: ssh.NewClient(cc, chans, reqs), passwordToRemember: remember}, nil
}

func defaultKeyPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var paths []string
	for _, n := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		paths = append(paths, filepath.Join(home, ".ssh", n))
	}
	return paths
}

// findKeyFiles は ~/.ssh にある秘密鍵らしきファイルを返す。
func findKeyFiles() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(home, ".ssh"))
	if err != nil {
		return nil
	}
	var keys []string
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".pub") {
			continue
		}
		path := filepath.Join(home, ".ssh", e.Name())
		if info, err := e.Info(); err != nil || info.Size() > 64<<10 {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if bytes.Contains(data, []byte("PRIVATE KEY-----")) || bytes.HasPrefix(data, []byte("PuTTY-User-Key-File")) {
			keys = append(keys, path)
		}
	}
	sort.Strings(keys)
	return keys
}

func loadSigners(p prompter, keyFile string) []ssh.Signer {
	paths := defaultKeyPaths()
	explicit := keyFile != ""
	if explicit {
		paths = []string{expandHome(keyFile)}
	}

	var signers []ssh.Signer
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			if explicit {
				p.info(fmt.Sprintf("Warning: cannot read private key: %v", err))
			}
			continue
		}
		if bytes.HasPrefix(data, []byte("PuTTY-User-Key-File")) {
			p.info(fmt.Sprintf("Warning: %s is a PuTTY (.ppk) key, which is not supported. "+
				"Convert it to OpenSSH format with PuTTYgen (Conversions > Export OpenSSH key).", filepath.Base(path)))
			continue
		}
		signer, err := ssh.ParsePrivateKey(data)
		var ppErr *ssh.PassphraseMissingError
		if errors.As(err, &ppErr) {
			signer, err = unlockKey(p, path, data, explicit)
		}
		if err != nil {
			if explicit {
				p.info(fmt.Sprintf("Warning: cannot use private key %s: %v", path, err))
			}
			continue
		}
		signers = append(signers, signer)
	}
	return signers
}

// passphraseSecretID は鍵ファイルごとのパスフレーズを OS の資格情報ストアに保存するときの名前。
func passphraseSecretID(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return "passphrase:" + path
}

// unlockKey はパスフレーズ付きの秘密鍵を開く。保存済みのパスフレーズ → 入力の順に使う。
// 自動検出した鍵(explicit=false)は、保存済みのパスフレーズで開けなければ聞かずにスキップする。
func unlockKey(p prompter, path string, data []byte, explicit bool) (ssh.Signer, error) {
	id := passphraseSecretID(path)
	msg := "Passphrase for " + filepath.Base(path)
	if saved, err := getSecret(id); err == nil {
		signer, err := ssh.ParsePrivateKeyWithPassphrase(data, []byte(saved))
		if err == nil {
			return signer, nil
		}
		deleteSecret(id)
		msg = "The saved passphrase was rejected.\n" + msg
	}
	if !explicit {
		// 自動検出した鍵のためにパスワード認証より先にパスフレーズを聞かない
		return nil, errors.New("passphrase required")
	}
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		r, perr := p.prompt(promptRequest{Kind: promptPassword, Title: "Passphrase", Message: msg})
		if perr != nil {
			return nil, perr
		}
		var signer ssh.Signer
		signer, err = ssh.ParsePrivateKeyWithPassphrase(data, []byte(r.value))
		if err == nil {
			if r.remember {
				if serr := setSecret(id, r.value); serr != nil {
					p.info(fmt.Sprintf("Warning: failed to save passphrase: %v", serr))
				}
			}
			return signer, nil
		}
		msg = "Wrong passphrase. Passphrase for " + filepath.Base(path)
	}
	return nil, err
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

func hostKeyCallback(p prompter, path string) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		knownHostsMu.Lock()
		defer knownHostsMu.Unlock()

		if f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0o600); err == nil {
			f.Close()
		}
		check, err := knownhosts.New(path)
		if err != nil {
			return err
		}
		err = check(hostname, remote, key)
		if err == nil {
			return nil
		}
		var ke *knownhosts.KeyError
		if !errors.As(err, &ke) {
			return err
		}
		fp := ssh.FingerprintSHA256(key)
		for _, want := range ke.Want {
			if want.Key.Type() == key.Type() {
				return fmt.Errorf("HOST KEY HAS CHANGED. Connection aborted: someone could be eavesdropping on you (man-in-the-middle attack).\r\n"+
					"Received key: %s %s\r\nSaved key: %s line %d\r\n"+
					"If this change is expected, remove that line and reconnect.",
					key.Type(), fp, want.Filename, want.Line)
			}
		}

		msg := fmt.Sprintf("The authenticity of host %s can't be established.\n\nKey fingerprint:\n%s %s\n\nContinue connecting and save this key?",
			hostname, key.Type(), fp)
		if _, err := p.prompt(promptRequest{Kind: promptConfirm, Title: "Unknown host key", Message: msg}); err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = fmt.Fprintln(f, knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key))
		return err
	}
}
