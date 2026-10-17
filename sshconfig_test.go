package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAndResolve(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "extra"), []byte("Host ex\n  HostName ex.example.com\n"), 0o600)
	cfg := `# comment
Host web db
  HostName=10.0.0.1
  User  alice
  IdentityFile "~/.ssh/my key"

Host *.corp !bad.corp
  User corpuser

Match host foo
  User ignored

Include extra

Host *
  User fallback
  Port 2222
`
	p := filepath.Join(dir, "config")
	os.WriteFile(p, []byte(cfg), 0o600)
	blocks, err := parseSSHConfig(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	aliases := sshConfigAliases(blocks)
	if len(aliases) != 3 || aliases[0] != "web" || aliases[1] != "db" || aliases[2] != "ex" {
		t.Fatalf("aliases = %v", aliases)
	}
	o := resolveSSHConfig(blocks, "web")
	if o["hostname"] != "10.0.0.1" || o["user"] != "alice" || o["identityfile"] != "~/.ssh/my key" || o["port"] != "2222" {
		t.Fatalf("web = %v", o)
	}
	if o := resolveSSHConfig(blocks, "ex"); o["hostname"] != "ex.example.com" || o["user"] != "fallback" {
		t.Fatalf("ex = %v", o)
	}
	if o := resolveSSHConfig(blocks, "a.corp"); o["user"] != "corpuser" {
		t.Fatalf("a.corp = %v", o)
	}
	if o := resolveSSHConfig(blocks, "bad.corp"); o["user"] != "fallback" {
		t.Fatalf("bad.corp = %v", o)
	}
}
