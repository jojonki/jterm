package main

import (
	"errors"
	"os"

	"github.com/zalando/go-keyring"
)

// パスワードは OS の資格情報ストアに保存する。
// Windows は資格情報マネージャー、macOS はキーチェーン。
const keyringService = "jterm"

func init() {
	// 自動テスト用: OS の資格情報ストアに触れず、メモリ上の偽物を使う
	if os.Getenv("JTERM_TEST_MOCK_KEYRING") == "1" {
		keyring.MockInit()
	}
}

func setSecret(sessionID, password string) error {
	return keyring.Set(keyringService, sessionID, password)
}

func getSecret(sessionID string) (string, error) {
	return keyring.Get(keyringService, sessionID)
}

func deleteSecret(sessionID string) error {
	err := keyring.Delete(keyringService, sessionID)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
