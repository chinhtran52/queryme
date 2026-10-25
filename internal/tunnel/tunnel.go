// Package tunnel reaches a database through an SSH bastion, with the same keys and known_hosts as ssh(1).
package tunnel

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

const keepaliveInterval = 30 * time.Second

var keyFiles = []string{"id_ed25519", "id_ecdsa", "id_rsa"}

// Tunnel is an open SSH connection that database connections are dialed through.
// shortcut: no ~/.ssh/config aliases, ProxyJump, password auth, or passphrase keys outside the agent; add them if needed.
type Tunnel struct {
	client    *ssh.Client
	agentConn net.Conn
	agent     agent.ExtendedAgent
	host      string
	done      chan struct{}
}

// Open connects to spec, "[user@]host[:port]", trusting only host keys already in ~/.ssh/known_hosts.
func Open(ctx context.Context, spec string) (*Tunnel, error) {
	username, addr, err := parseSpec(spec)
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	knownHostsPath := filepath.Join(home, ".ssh", "known_hosts")
	hostKeys, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("ssh %s: read %s: %w", spec, knownHostsPath, err)
	}

	t := &Tunnel{host: spec, done: make(chan struct{})}
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			t.agentConn, t.agent = conn, agent.NewClient(conn)
		}
	}
	cfg := &ssh.ClientConfig{
		User:              username,
		Auth:              []ssh.AuthMethod{ssh.PublicKeysCallback(t.signers(filepath.Join(home, ".ssh")))},
		HostKeyCallback:   hostKeys,
		HostKeyAlgorithms: knownAlgorithms(hostKeys, addr),
	}

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		t.closeAgent()
		return nil, fmt.Errorf("ssh %s: %w", spec, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		t.closeAgent()
		return nil, explain(spec, addr, knownHostsPath, err)
	}
	_ = conn.SetDeadline(time.Time{})
	t.client = ssh.NewClient(sshConn, chans, reqs)
	go t.keepalive()
	return t, nil
}

func (t *Tunnel) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	return t.client.DialContext(ctx, network, addr)
}

func (t *Tunnel) Host() string { return t.host }

func (t *Tunnel) Close() error {
	select {
	case <-t.done:
		return nil
	default:
		close(t.done)
	}
	t.closeAgent()
	return t.client.Close()
}

// keepalive stops idle NAT and firewall timeouts from dropping the tunnel under an open session.
func (t *Tunnel) keepalive() {
	ticker := time.NewTicker(keepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-t.done:
			return
		case <-ticker.C:
			if _, _, err := t.client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				t.client.Close()
				return
			}
		}
	}
}

// signers offers the agent's keys first, then unencrypted default key files, all in one publickey method.
func (t *Tunnel) signers(sshDir string) func() ([]ssh.Signer, error) {
	return func() ([]ssh.Signer, error) {
		var signers []ssh.Signer
		if t.agent != nil {
			if s, err := t.agent.Signers(); err == nil {
				signers = append(signers, s...)
			}
		}
		for _, name := range keyFiles {
			data, err := os.ReadFile(filepath.Join(sshDir, name))
			if err != nil {
				continue
			}
			if s, err := ssh.ParsePrivateKey(data); err == nil {
				signers = append(signers, s)
			}
		}
		if len(signers) == 0 {
			return nil, errors.New("no SSH key: start ssh-agent with your key or add an unencrypted ~/.ssh/id_ed25519")
		}
		return signers, nil
	}
}

func (t *Tunnel) closeAgent() {
	if t.agentConn != nil {
		t.agentConn.Close()
	}
}

func parseSpec(spec string) (username, addr string, err error) {
	host := strings.TrimSpace(spec)
	if i := strings.LastIndex(host, "@"); i >= 0 {
		username, host = host[:i], host[i+1:]
	}
	if host == "" {
		return "", "", fmt.Errorf("ssh host %q: want [user@]host[:port]", spec)
	}
	if username == "" {
		u, err := user.Current()
		if err != nil {
			return "", "", err
		}
		username = u.Username
	}
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(strings.Trim(host, "[]"), "22")
	}
	return username, host, nil
}

// knownAlgorithms asks only for host key types known_hosts has for addr, so a server that also has
// a key type the user never accepted is not reported as a changed key.
func knownAlgorithms(hostKeys ssh.HostKeyCallback, addr string) []string {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	probe, _ := ssh.NewSignerFromKey(priv)
	var keyErr *knownhosts.KeyError
	if err := hostKeys(addr, &net.TCPAddr{}, probe.PublicKey()); !errors.As(err, &keyErr) {
		return nil
	}
	var algos []string
	for _, k := range keyErr.Want {
		switch typ := k.Key.Type(); typ {
		case ssh.KeyAlgoRSA:
			algos = append(algos, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA)
		default:
			algos = append(algos, typ)
		}
	}
	return algos
}

func explain(spec, addr, knownHostsPath string, err error) error {
	var keyErr *knownhosts.KeyError
	if errors.As(err, &keyErr) {
		host, port, _ := net.SplitHostPort(addr)
		if len(keyErr.Want) == 0 {
			return fmt.Errorf("ssh %s: host key is not in %s; run `ssh -p %s %s` once to check and accept it", spec, knownHostsPath, port, host)
		}
		return fmt.Errorf("ssh %s: HOST KEY CHANGED, does not match %s; refusing to connect", spec, knownHostsPath)
	}
	return fmt.Errorf("ssh %s: %w", spec, err)
}
