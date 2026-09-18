// Package sshtunnel manages SSH port-forward tunnels for remote Aether daemons.
package sshtunnel

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// RemotePort is the TCP port the aether daemon listens on when started via SSH.
const RemotePort = 34115

// Host represents a parsed SSH host spec: user@hostname:port
type Host struct {
	User     string
	Hostname string
	Port     int // SSH port, default 22
}

// ParseHost parses "user@host:port", "user@host", or "host".
func ParseHost(spec string) (*Host, error) {
	if spec == "" {
		return nil, fmt.Errorf("empty host spec")
	}
	h := &Host{Port: 22}

	// Split user@ prefix
	if idx := strings.Index(spec, "@"); idx >= 0 {
		h.User = spec[:idx]
		spec = spec[idx+1:]
	}

	// Split host:port
	if strings.Contains(spec, ":") {
		host, portStr, err := net.SplitHostPort(spec)
		if err != nil {
			return nil, fmt.Errorf("parse host:port %q: %w", spec, err)
		}
		h.Hostname = host
		p, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("invalid SSH port %q: %w", portStr, err)
		}
		h.Port = p
	} else {
		h.Hostname = spec
	}

	if h.Hostname == "" {
		return nil, fmt.Errorf("empty hostname in spec %q", spec)
	}
	return h, nil
}

// String serialises back to "user@hostname:port" form.
func (h *Host) String() string {
	s := h.Hostname
	if h.User != "" {
		s = h.User + "@" + s
	}
	if h.Port != 22 {
		s += ":" + strconv.Itoa(h.Port)
	}
	return s
}

// sshTarget returns "user@hostname" for ssh command.
func (h *Host) sshTarget() string {
	if h.User != "" {
		return h.User + "@" + h.Hostname
	}
	return h.Hostname
}

// Tunnel manages an SSH port-forward process.
type Tunnel struct {
	Host         *Host
	LocalPort    int
	RemoteSocket string // unused — we use TCP on remote
	cmd          *exec.Cmd
	done         chan struct{}
}

// NewTunnel creates a Tunnel for the given host spec.
func NewTunnel(spec string) (*Tunnel, error) {
	h, err := ParseHost(spec)
	if err != nil {
		return nil, err
	}
	return &Tunnel{
		Host: h,
		done: make(chan struct{}),
	}, nil
}

// Open starts the SSH tunnel:
//  1. Ensure the remote aether daemon is running on TCP port RemotePort.
//  2. Find a free local port.
//  3. Start ssh -N -L localPort:127.0.0.1:RemotePort user@host.
//  4. Wait up to 5 s for the tunnel to become connectable.
func (t *Tunnel) Open() error {
	// Ensure remote daemon is up
	if err := t.ensureRemoteDaemon(); err != nil {
		// Non-fatal: the daemon might already be running
		_ = err
	}

	// Find free local port
	port, err := findFreePort()
	if err != nil {
		return fmt.Errorf("ssh tunnel: no free port: %w", err)
	}
	t.LocalPort = port

	args := t.buildSSHArgs(port)
	cmd := exec.Command("ssh", args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	t.cmd = cmd

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ssh tunnel: start: %w", err)
	}

	// Monitor process exit in background
	go func() {
		_ = cmd.Wait()
		select {
		case <-t.done:
		default:
			close(t.done)
		}
	}()

	// Wait for tunnel to be ready (up to 5 s)
	if err := t.waitReady(5 * time.Second); err != nil {
		t.Close()
		return fmt.Errorf("ssh tunnel: not ready: %w", err)
	}
	return nil
}

// Close shuts down the SSH tunnel process.
func (t *Tunnel) Close() {
	select {
	case <-t.done:
	default:
		close(t.done)
	}
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
}

// LocalAddr returns "localhost:port" for IPC connection.
func (t *Tunnel) LocalAddr() string {
	return fmt.Sprintf("localhost:%d", t.LocalPort)
}

// FetchRemoteToken retrieves the token from the remote host.
func (t *Tunnel) FetchRemoteToken() (string, error) {
	tokenPaths := []string{
		"~/.local/share/aether/auth.token",
		"~/.local/share/aether/token",
	}
	for _, tp := range tokenPaths {
		out, err := t.runRemote("cat " + tp + " 2>/dev/null")
		if err == nil && strings.TrimSpace(out) != "" {
			return strings.TrimSpace(out), nil
		}
	}
	return "", fmt.Errorf("ssh tunnel: could not read remote token")
}

// ensureRemoteDaemon starts the aether daemon on the remote host if not running.
func (t *Tunnel) ensureRemoteDaemon() error {
	cmd := fmt.Sprintf(
		"aether daemon --status 2>/dev/null || nohup aether daemon --background --listen tcp://127.0.0.1:%d > /tmp/aether-daemon.log 2>&1 & sleep 1",
		RemotePort,
	)
	_, err := t.runRemote(cmd)
	return err
}

// runRemote executes a command on the remote host and returns stdout.
func (t *Tunnel) runRemote(cmd string) (string, error) {
	args := t.baseSSHArgs()
	args = append(args, t.Host.sshTarget(), cmd)
	out, err := exec.Command("ssh", args...).Output()
	return string(out), err
}

// buildSSHArgs returns args for the port-forward ssh process.
func (t *Tunnel) buildSSHArgs(localPort int) []string {
	fwd := fmt.Sprintf("%d:127.0.0.1:%d", localPort, RemotePort)
	args := t.baseSSHArgs()
	args = append(args,
		"-N",
		"-L", fwd,
		t.Host.sshTarget(),
	)
	return args
}

// baseSSHArgs returns args common to all SSH invocations.
func (t *Tunnel) baseSSHArgs() []string {
	args := []string{
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=10",
		"-o", "BatchMode=yes",
		"-p", strconv.Itoa(t.Host.Port),
	}

	// SSH config file
	home, _ := os.UserHomeDir()
	sshConfig := filepath.Join(home, ".ssh", "config")
	if _, err := os.Stat(sshConfig); err == nil {
		args = append(args, "-F", sshConfig)
	}

	// Identity file
	if key := os.Getenv("AETHER_SSH_KEY"); key != "" {
		args = append(args, "-i", key)
	} else {
		for _, name := range []string{"id_ed25519", "id_rsa", "id_ecdsa"} {
			keyPath := filepath.Join(home, ".ssh", name)
			if _, err := os.Stat(keyPath); err == nil {
				args = append(args, "-i", keyPath)
				break
			}
		}
	}

	return args
}

// waitReady polls the local port until it accepts connections or timeout.
func (t *Tunnel) waitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		// Check if SSH process died
		select {
		case <-t.done:
			return fmt.Errorf("ssh process exited unexpectedly")
		default:
		}

		conn, err := net.DialTimeout("tcp", t.LocalAddr(), 300*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("tunnel not ready after %s", timeout)
}

// findFreePort finds an available local TCP port.
func findFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port, nil
}