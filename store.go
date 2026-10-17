package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Session は保存された接続先。
type Session struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Host        string    `json:"host"`
	Port        int       `json:"port"`
	User        string    `json:"user"`
	KeyFile     string    `json:"keyFile,omitempty"`
	HasPassword bool      `json:"hasPassword,omitempty"` // パスワード本体は secrets.go で OS に保存
	LastUsed    time.Time `json:"lastUsed,omitempty"`
}

type Store struct {
	path     string
	Sessions []*Session `json:"sessions"`
}

func configDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "jterm")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func loadStore() (*Store, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}
	st := &Store{path: filepath.Join(dir, "sessions.json")}
	data, err := os.ReadFile(st.path)
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, st); err != nil {
		return nil, err
	}
	// 旧形式(ID なし)のデータに ID を振る
	changed := false
	for _, s := range st.Sessions {
		if s.ID == "" {
			s.ID = newID()
			changed = true
		}
	}
	if changed {
		if err := st.save(); err != nil {
			return nil, err
		}
	}
	return st, nil
}

func (st *Store) save() error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := st.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, st.path)
}

func (st *Store) get(id string) *Session {
	for _, s := range st.Sessions {
		if s.ID == id {
			return s
		}
	}
	return nil
}

func (st *Store) remove(id string) {
	for i, s := range st.Sessions {
		if s.ID == id {
			st.Sessions = append(st.Sessions[:i], st.Sessions[i+1:]...)
			return
		}
	}
}
