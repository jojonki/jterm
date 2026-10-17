package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// SessionInfo はフロントエンドに渡すセッション情報。暗号化済みパスワードは渡さない。
type SessionInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	User        string `json:"user"`
	KeyFile     string `json:"keyFile"`
	HasPassword bool   `json:"hasPassword"`
	LastUsed    int64  `json:"lastUsed"` // UNIX ミリ秒。未使用なら 0

	// 保存時のみ使う入力用フィールド
	Password      string `json:"password"`      // 空なら変更しない
	ClearPassword bool   `json:"clearPassword"` // 保存済みパスワードを削除する
}

func toInfo(s *Session) SessionInfo {
	info := SessionInfo{
		ID: s.ID, Name: s.Name, Host: s.Host, Port: s.Port, User: s.User,
		KeyFile: s.KeyFile, HasPassword: s.HasPassword,
	}
	if !s.LastUsed.IsZero() {
		info.LastUsed = s.LastUsed.UnixMilli()
	}
	return info
}

// App はフロントエンドにバインドされる。
type App struct {
	ctx context.Context

	mu      sync.Mutex
	store   *Store
	conns   map[string]*conn
	prompts map[string]chan promptReply
}

// conn は1つのペインに対応する SSH 接続。
type conn struct {
	id     string
	ctx    context.Context
	cancel context.CancelFunc

	mu         sync.Mutex
	client     *ssh.Client
	sess       *ssh.Session
	stdin      io.WriteCloser
	cols, rows int
}

func NewApp() *App {
	return &App{
		conns:   map[string]*conn{},
		prompts: map[string]chan promptReply{},
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	st, err := loadStore()
	if err != nil {
		runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
			Type: runtime.ErrorDialog, Title: "jterm",
			Message: "Failed to load sessions: " + err.Error(),
		})
		st = &Store{}
		if dir, derr := configDir(); derr == nil {
			// 壊れたファイルを上書きしないよう別名で保存する
			st.path = filepath.Join(dir, "sessions.recovered.json")
		}
	}
	a.store = st
}

func (a *App) shutdown(context.Context) {
	a.DisconnectAll()
}

// DisconnectAll はすべての接続を閉じる。
func (a *App) DisconnectAll() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, c := range a.conns {
		c.close()
	}
}

// ---- セッション管理 ----

func (a *App) ListSessions() []SessionInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	list := make([]SessionInfo, 0, len(a.store.Sessions))
	for _, s := range a.store.Sessions {
		list = append(list, toInfo(s))
	}
	return list
}

// SaveSession は新規作成(ID が空)または更新を行う。
func (a *App) SaveSession(in SessionInfo) (SessionInfo, error) {
	in.Host = strings.TrimSpace(in.Host)
	in.User = strings.TrimSpace(in.User)
	in.Name = strings.TrimSpace(in.Name)
	in.KeyFile = strings.TrimSpace(in.KeyFile)
	if in.Host == "" {
		return SessionInfo{}, errors.New("Host is required")
	}
	if in.Port == 0 {
		in.Port = 22
	}
	if in.Port < 1 || in.Port > 65535 {
		return SessionInfo{}, errors.New("Port must be between 1 and 65535")
	}
	if in.Name == "" {
		in.Name = in.Host
		if in.User != "" {
			in.Name = in.User + "@" + in.Host
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.store.get(in.ID)
	if s == nil {
		s = &Session{ID: newID()}
		a.store.Sessions = append(a.store.Sessions, s)
	}
	s.Name, s.Host, s.Port, s.User, s.KeyFile = in.Name, in.Host, in.Port, in.User, in.KeyFile
	switch {
	case in.Password != "":
		if err := setSecret(s.ID, in.Password); err != nil {
			return SessionInfo{}, fmt.Errorf("failed to save password: %w", err)
		}
		s.HasPassword = true
	case in.ClearPassword:
		if err := deleteSecret(s.ID); err != nil {
			return SessionInfo{}, fmt.Errorf("failed to delete password: %w", err)
		}
		s.HasPassword = false
	}
	if err := a.store.save(); err != nil {
		return SessionInfo{}, err
	}
	a.notifySessions()
	return toInfo(s), nil
}

func (a *App) DeleteSession(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	deleteSecret(id)
	a.store.remove(id)
	if err := a.store.save(); err != nil {
		return err
	}
	a.notifySessions()
	return nil
}

// ForgetPassphrase はセッションの鍵ファイルに保存済みのパスフレーズを削除する。
func (a *App) ForgetPassphrase(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.store.get(id)
	if s == nil {
		return errors.New("session not found")
	}
	paths := defaultKeyPaths()
	if s.KeyFile != "" {
		paths = []string{expandHome(s.KeyFile)}
	}
	for _, path := range paths {
		if err := deleteSecret(passphraseSecretID(path)); err != nil {
			return err
		}
	}
	return nil
}

// ForgetPassword は保存済みのパスワードだけを削除する。
func (a *App) ForgetPassword(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := deleteSecret(id); err != nil {
		return err
	}
	if s := a.store.get(id); s != nil {
		s.HasPassword = false
	}
	if err := a.store.save(); err != nil {
		return err
	}
	a.notifySessions()
	return nil
}

// ListKeyFiles は ~/.ssh にある秘密鍵の一覧を返す。
func (a *App) ListKeyFiles() []string {
	return findKeyFiles()
}

func (a *App) PickKeyFile() (string, error) {
	opts := runtime.OpenDialogOptions{Title: "Select private key", ShowHiddenFiles: true}
	if home, err := os.UserHomeDir(); err == nil {
		dir := filepath.Join(home, ".ssh")
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			opts.DefaultDirectory = dir
		}
	}
	return runtime.OpenFileDialog(a.ctx, opts)
}

// notifySessions は a.mu を保持した状態で呼ぶ。
func (a *App) notifySessions() {
	runtime.EventsEmit(a.ctx, "sessions:changed")
}

// ---- 接続 ----

type statusEvent struct {
	Conn    string `json:"conn"`
	State   string `json:"state"` // connecting / connected / closed
	Message string `json:"message"`
}

func (a *App) emitStatus(id, state, msg string) {
	runtime.EventsEmit(a.ctx, "ssh:status", statusEvent{Conn: id, State: state, Message: msg})
}

func (a *App) emitText(id, text string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\n", "\r\n")
	runtime.EventsEmit(a.ctx, "data:"+id, base64.StdEncoding.EncodeToString([]byte(text)))
}

// Connect は connID で識別される接続を開始する。結果はイベントで通知する。
//   - data:<connID>  端末への出力(base64)
//   - ssh:status     状態の変化
//   - ssh:prompt     パスワードなどの問い合わせ
func (a *App) Connect(connID, sessionID string, cols, rows int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.store.get(sessionID)
	if s == nil {
		return errors.New("session not found")
	}
	if _, dup := a.conns[connID]; dup {
		return errors.New("duplicate connection ID")
	}
	ctx, cancel := context.WithCancel(a.ctx)
	c := &conn{id: connID, ctx: ctx, cancel: cancel, cols: cols, rows: rows}
	a.conns[connID] = c
	go a.run(c, *s)
	return nil
}

func (a *App) run(c *conn, s Session) {
	defer func() {
		a.mu.Lock()
		delete(a.conns, c.id)
		a.mu.Unlock()
		c.close()
	}()

	a.emitStatus(c.id, "connecting", "")
	res, err := dialSession(c.ctx, s, &connPrompter{a: a, c: c, title: s.Name})
	if err != nil {
		if c.ctx.Err() != nil {
			err = errCanceled
		}
		a.emitStatus(c.id, "closed", err.Error())
		return
	}

	a.mu.Lock()
	if st := a.store.get(s.ID); st != nil {
		st.LastUsed = time.Now()
		if res.passwordToRemember != "" {
			if err := setSecret(st.ID, res.passwordToRemember); err == nil {
				st.HasPassword = true
			} else {
				a.emitText(c.id, "\x1b[33mWarning: failed to save password: "+err.Error()+"\x1b[0m\n")
			}
		}
		a.store.save()
		a.notifySessions()
	}
	a.mu.Unlock()

	err = a.shell(c, res.client)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	a.emitStatus(c.id, "closed", msg)
}

func (a *App) shell(c *conn, client *ssh.Client) error {
	sess, err := client.NewSession()
	if err != nil {
		client.Close()
		return err
	}
	out := &eventWriter{a: a, id: c.id}
	sess.Stdout = out
	sess.Stderr = out
	stdin, err := sess.StdinPipe()
	if err != nil {
		client.Close()
		return err
	}

	c.mu.Lock()
	c.client, c.sess, c.stdin = client, sess, stdin
	cols, rows := c.cols, c.rows
	c.mu.Unlock()
	if c.ctx.Err() != nil {
		return errCanceled
	}
	if cols <= 0 || rows <= 0 {
		cols, rows = 80, 24
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sess.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		return err
	}
	if err := sess.Shell(); err != nil {
		return err
	}
	a.emitStatus(c.id, "connected", "")

	// キープアライブ。応答がなければ切断する。
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-c.ctx.Done():
				return
			case <-t.C:
				if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
					client.Close()
					return
				}
			}
		}
	}()

	err = sess.Wait()
	var exitErr *ssh.ExitError
	switch {
	case c.ctx.Err() != nil:
		return nil // ユーザーが閉じた
	case err == nil, errors.As(err, &exitErr):
		return nil // リモートシェルの終了
	}
	var missing *ssh.ExitMissingError
	if errors.As(err, &missing) {
		return errors.New("connection lost")
	}
	return err
}

func (c *conn) close() {
	c.cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil {
		c.client.Close()
	}
}

func (a *App) conn(id string) *conn {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.conns[id]
}

// Write は端末への入力をリモートへ送る。
func (a *App) Write(connID, data string) {
	c := a.conn(connID)
	if c == nil {
		return
	}
	c.mu.Lock()
	stdin := c.stdin
	c.mu.Unlock()
	if stdin != nil {
		stdin.Write([]byte(data))
	}
}

func (a *App) Resize(connID string, cols, rows int) {
	c := a.conn(connID)
	if c == nil || cols <= 0 || rows <= 0 {
		return
	}
	c.mu.Lock()
	c.cols, c.rows = cols, rows
	sess := c.sess
	c.mu.Unlock()
	if sess != nil {
		sess.WindowChange(rows, cols)
	}
}

func (a *App) Disconnect(connID string) {
	if c := a.conn(connID); c != nil {
		c.close()
	}
}

// AnswerPrompt はフロントエンドからの問い合わせへの回答を受け取る。
func (a *App) AnswerPrompt(id, value string, ok, remember bool) {
	a.mu.Lock()
	ch := a.prompts[id]
	delete(a.prompts, id)
	a.mu.Unlock()
	if ch != nil {
		ch <- promptReply{value: value, ok: ok, remember: remember}
	}
}

type eventWriter struct {
	a  *App
	id string
}

func (w *eventWriter) Write(p []byte) (int, error) {
	runtime.EventsEmit(w.a.ctx, "data:"+w.id, base64.StdEncoding.EncodeToString(p))
	return len(p), nil
}

type promptEvent struct {
	ID      string `json:"id"`
	Conn    string `json:"conn"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Message string `json:"message"`
}

type connPrompter struct {
	a     *App
	c     *conn
	title string
}

func (p *connPrompter) prompt(req promptRequest) (promptReply, error) {
	id := newID()
	ch := make(chan promptReply, 1)
	p.a.mu.Lock()
	p.a.prompts[id] = ch
	p.a.mu.Unlock()

	runtime.EventsEmit(p.a.ctx, "ssh:prompt", promptEvent{
		ID: id, Conn: p.c.id, Kind: string(req.Kind),
		Title: fmt.Sprintf("%s - %s", p.title, req.Title), Message: req.Message,
	})
	select {
	case r := <-ch:
		if !r.ok {
			return promptReply{}, errCanceled
		}
		return r, nil
	case <-p.c.ctx.Done():
		p.a.mu.Lock()
		delete(p.a.prompts, id)
		p.a.mu.Unlock()
		runtime.EventsEmit(p.a.ctx, "ssh:prompt-cancel", id)
		return promptReply{}, errCanceled
	}
}

func (p *connPrompter) info(message string) {
	p.a.emitText(p.c.id, "\x1b[90m"+message+"\x1b[0m\n")
}
