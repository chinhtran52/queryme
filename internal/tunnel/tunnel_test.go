package tunnel_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/pem"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/chinhtran52/queryme/internal/db/postgres"
	"github.com/chinhtran52/queryme/internal/tunnel"
)

// sshServer is a minimal bastion that accepts one client key and forwards direct-tcpip channels.
type sshServer struct {
	addr    string
	hostKey ssh.Signer
}

func newSigner(t *testing.T) (ssh.Signer, ed25519.PrivateKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signer, priv
}

// startServer offers hostKeys, or a fresh ed25519 key when none are given; hostKey is the first.
func startServer(t *testing.T, clientKey ssh.PublicKey, hostKeys ...ssh.Signer) *sshServer {
	t.Helper()
	if len(hostKeys) == 0 {
		key, _ := newSigner(t)
		hostKeys = append(hostKeys, key)
	}
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if string(key.Marshal()) == string(clientKey.Marshal()) {
			return nil, nil
		}
		return nil, io.EOF
	}}
	for _, k := range hostKeys {
		cfg.AddHostKey(k)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serve(conn, cfg)
		}
	}()
	return &sshServer{addr: ln.Addr().String(), hostKey: hostKeys[0]}
}

func serve(conn net.Conn, cfg *ssh.ServerConfig) {
	_, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for newChan := range chans {
		var target struct {
			Host       string
			Port       uint32
			OriginHost string
			OriginPort uint32
		}
		if newChan.ChannelType() != "direct-tcpip" || ssh.Unmarshal(newChan.ExtraData(), &target) != nil {
			_ = newChan.Reject(ssh.UnknownChannelType, "unsupported")
			continue
		}
		upstream, err := net.Dial("tcp", net.JoinHostPort(target.Host, strconv.Itoa(int(target.Port))))
		if err != nil {
			_ = newChan.Reject(ssh.ConnectionFailed, err.Error())
			continue
		}
		ch, chReqs, err := newChan.Accept()
		if err != nil {
			upstream.Close()
			continue
		}
		go ssh.DiscardRequests(chReqs)
		go func() { _, _ = io.Copy(ch, upstream); ch.Close() }()
		go func() { _, _ = io.Copy(upstream, ch); upstream.Close() }()
	}
}

// home writes the client key and known_hosts into a fresh $HOME, with no ssh-agent.
func home(t *testing.T, clientKey ed25519.PrivateKey, knownHostsLines ...string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SSH_AUTH_SOCK", "")
	sshDir := filepath.Join(dir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(clientKey, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "id_ed25519"), pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "known_hosts"), []byte(strings.Join(knownHostsLines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func knownHost(addr string, key ssh.PublicKey) string {
	return knownhosts.Line([]string{knownhosts.Normalize(addr)}, key)
}

func echoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(conn, conn); conn.Close() }()
		}
	}()
	return ln.Addr().String()
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestDialsThroughTheTunnel(t *testing.T) {
	clientSigner, clientKey := newSigner(t)
	srv := startServer(t, clientSigner.PublicKey())
	home(t, clientKey, knownHost(srv.addr, srv.hostKey.PublicKey()))

	tun, err := tunnel.Open(ctx(t), "me@"+srv.addr)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := tun.Dial(ctx(t), "tcp", echoServer(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("echo = %q, %v", buf, err)
	}
	conn.Close()

	if err := tun.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := tun.Dial(ctx(t), "tcp", echoServer(t)); err == nil {
		t.Fatal("dial after Close should fail")
	}
	if err := tun.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestRefusesUnknownAndChangedHostKeys(t *testing.T) {
	clientSigner, clientKey := newSigner(t)
	srv := startServer(t, clientSigner.PublicKey())
	otherKey, _ := newSigner(t)

	cases := map[string]struct {
		knownHosts []string
		want       string
	}{
		"unknown": {nil, "is not in"},
		"changed": {[]string{knownHost(srv.addr, otherKey.PublicKey())}, "HOST KEY CHANGED"},
	}
	for name, tc := range cases {
		home(t, clientKey, tc.knownHosts...)
		if _, err := tunnel.Open(ctx(t), "me@"+srv.addr); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

func TestOnlyAsksForHostKeyTypesAlreadyKnown(t *testing.T) {
	clientSigner, clientKey := newSigner(t)
	ed, _ := newSigner(t)
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ec, _ := ssh.NewSignerFromKey(ecKey)
	srv := startServer(t, clientSigner.PublicKey(), ed, ec)

	// Whichever type the client prefers, a server also offering the other, unaccepted type must still connect.
	for _, known := range []ssh.Signer{ed, ec} {
		home(t, clientKey, knownHost(srv.addr, known.PublicKey()))
		tun, err := tunnel.Open(ctx(t), "me@"+srv.addr)
		if err != nil {
			t.Fatalf("known %s: %v", known.PublicKey().Type(), err)
		}
		tun.Close()
	}
}

func TestRejectedKeyFails(t *testing.T) {
	allowed, _ := newSigner(t)
	srv := startServer(t, allowed.PublicKey())
	_, otherKey := newSigner(t)
	home(t, otherKey, knownHost(srv.addr, srv.hostKey.PublicKey()))
	if _, err := tunnel.Open(ctx(t), "me@"+srv.addr); err == nil || !strings.Contains(err.Error(), "unable to authenticate") {
		t.Fatalf("err = %v", err)
	}
}

func TestPostgresThroughTheTunnel(t *testing.T) {
	url := os.Getenv("QUERYME_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("QUERYME_TEST_DATABASE_URL not set")
	}
	clientSigner, clientKey := newSigner(t)
	srv := startServer(t, clientSigner.PublicKey())
	home(t, clientKey, knownHost(srv.addr, srv.hostKey.PublicKey()))

	d, err := postgres.Open(ctx(t), url, "", "me@"+srv.addr, false)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if !strings.Contains(d.Target(), "via me@"+srv.addr) {
		t.Fatalf("target = %q", d.Target())
	}
	res, err := d.Query(ctx(t), "SELECT 42", 1)
	if err != nil || *res.Rows[0][0] != "42" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}
