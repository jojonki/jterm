//go:build !windows

// testsshd は jterm の動作確認用の SSH サーバー。
// 起動したユーザーのシェルをそのまま提供するので、信頼できるネットワーク以外では使わないこと。
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/crypto/ssh"
)

var (
	listen   = flag.String("listen", "127.0.0.1:2222", "listen address")
	user     = flag.String("user", "test", "login user name")
	password = flag.String("password", "test", "login password")
	hostKey  = flag.String("hostkey", filepath.Join(os.TempDir(), "jterm-testsshd-hostkey"), "host key file (generated if missing)")
)

func main() {
	flag.Parse()

	signer, err := loadOrCreateHostKey(*hostKey)
	if err != nil {
		log.Fatal(err)
	}
	authorized := loadAuthorizedKeys()

	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			if c.User() == *user && string(pw) == *password {
				log.Printf("%s: password auth OK", c.RemoteAddr())
				return nil, nil
			}
			log.Printf("%s: password auth failed (user=%q)", c.RemoteAddr(), c.User())
			return nil, errors.New("authentication failed")
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if c.User() == *user && authorized[string(key.Marshal())] {
				log.Printf("%s: public key auth OK (%s)", c.RemoteAddr(), ssh.FingerprintSHA256(key))
				return nil, nil
			}
			return nil, errors.New("authentication failed")
		},
		KeyboardInteractiveCallback: func(c ssh.ConnMetadata, client ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			ans, err := client("", "keyboard-interactive test", []string{"Password: "}, []bool{false})
			if err == nil && len(ans) == 1 && c.User() == *user && ans[0] == *password {
				log.Printf("%s: keyboard-interactive auth OK", c.RemoteAddr())
				return nil, nil
			}
			return nil, errors.New("authentication failed")
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Test SSH server is running\n")
	fmt.Printf("  Address    : %s\n", ln.Addr())
	fmt.Printf("  User       : %s\n", *user)
	fmt.Printf("  Password   : %s\n", *password)
	fmt.Printf("  Public keys: accepting %d key(s) from ~/.ssh/*.pub and ~/.ssh/authorized_keys\n", len(authorized))
	fmt.Printf("  Host key   : %s\n", ssh.FingerprintSHA256(signer.PublicKey()))
	fmt.Printf("Press Ctrl+C to stop\n\n")

	for {
		nc, err := ln.Accept()
		if err != nil {
			log.Fatal(err)
		}
		go handleConn(nc, cfg)
	}
}

func loadOrCreateHostKey(path string) (ssh.Signer, error) {
	if data, err := os.ReadFile(path); err == nil {
		return ssh.ParsePrivateKey(data)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(priv, "jterm testsshd")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(priv)
}

func loadAuthorizedKeys() map[string]bool {
	keys := map[string]bool{}
	home, err := os.UserHomeDir()
	if err != nil {
		return keys
	}
	files, _ := filepath.Glob(filepath.Join(home, ".ssh", "*.pub"))
	files = append(files, filepath.Join(home, ".ssh", "authorized_keys"))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for len(data) > 0 {
			key, _, _, rest, err := ssh.ParseAuthorizedKey(data)
			if err != nil {
				break
			}
			keys[string(key.Marshal())] = true
			data = rest
		}
	}
	return keys
}

func handleConn(nc net.Conn, cfg *ssh.ServerConfig) {
	sc, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		log.Printf("%s: handshake failed: %v", nc.RemoteAddr(), err)
		return
	}
	log.Printf("%s: connected (%s)", sc.RemoteAddr(), sc.ClientVersion())
	defer log.Printf("%s: disconnected", sc.RemoteAddr())
	go ssh.DiscardRequests(reqs)
	for nch := range chans {
		if nch.ChannelType() != "session" {
			nch.Reject(ssh.UnknownChannelType, "only session channels are supported")
			continue
		}
		ch, chReqs, err := nch.Accept()
		if err != nil {
			continue
		}
		go handleSession(ch, chReqs)
	}
}

type ptyReq struct {
	Term          string
	Cols, Rows    uint32
	Width, Height uint32
	Modes         string
}

func handleSession(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()
	var (
		mu   sync.Mutex
		tty  *os.File
		size *pty.Winsize
		term = "xterm-256color"
		env  []string
	)
	for req := range reqs {
		switch req.Type {
		case "pty-req":
			var p ptyReq
			if err := ssh.Unmarshal(req.Payload, &p); err != nil {
				req.Reply(false, nil)
				continue
			}
			term = p.Term
			size = &pty.Winsize{Cols: uint16(p.Cols), Rows: uint16(p.Rows)}
			log.Printf("pty-req: %s %dx%d", p.Term, p.Cols, p.Rows)
			req.Reply(true, nil)
		case "window-change":
			if len(req.Payload) >= 8 {
				cols := binary.BigEndian.Uint32(req.Payload)
				rows := binary.BigEndian.Uint32(req.Payload[4:])
				log.Printf("window-change: %dx%d", cols, rows)
				mu.Lock()
				size = &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}
				if tty != nil {
					pty.Setsize(tty, size)
				}
				mu.Unlock()
			}
		case "env":
			var kv struct{ Name, Value string }
			if ssh.Unmarshal(req.Payload, &kv) == nil {
				env = append(env, kv.Name+"="+kv.Value)
			}
			req.Reply(true, nil)
		case "shell", "exec":
			shell := os.Getenv("SHELL")
			if shell == "" {
				shell = "/bin/sh"
			}
			var cmd *exec.Cmd
			if req.Type == "exec" {
				var c struct{ Command string }
				ssh.Unmarshal(req.Payload, &c)
				cmd = exec.Command(shell, "-c", c.Command)
			} else {
				cmd = exec.Command(shell, "-l")
			}
			home, _ := os.UserHomeDir()
			cmd.Dir = home
			cmd.Env = append(append(os.Environ(), "TERM="+term), env...)
			req.Reply(true, nil)
			status := run(cmd, ch, &mu, &tty, size)
			ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
			return
		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}

func run(cmd *exec.Cmd, ch ssh.Channel, mu *sync.Mutex, tty **os.File, size *pty.Winsize) uint32 {
	if size == nil {
		// PTY なし(exec など)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = ch, ch, ch.Stderr()
		return exitStatus(cmd.Run())
	}
	f, err := pty.StartWithSize(cmd, size)
	if err != nil {
		fmt.Fprintf(ch, "failed to start shell: %v\r\n", err)
		return 1
	}
	mu.Lock()
	*tty = f
	mu.Unlock()
	go io.Copy(f, ch)
	io.Copy(ch, f)
	return exitStatus(cmd.Wait())
}

func exitStatus(err error) uint32 {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok {
			return uint32(ws.ExitStatus())
		}
		return 1
	}
	if err != nil {
		return 1
	}
	return 0
}
