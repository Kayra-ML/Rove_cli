package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// SSH is the Transport over the system's ssh client, so ~/.ssh/config,
// the agent and known_hosts all apply. It never asks for a password
// (BatchMode): a key or the agent must let it in.
type SSH struct{ Target Target }

func (s SSH) args() []string {
	a := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
	}
	if s.Target.Port > 0 {
		a = append(a, "-p", fmt.Sprint(s.Target.Port))
	}
	if k := s.Target.KeyPath; k != "" {
		if strings.HasPrefix(k, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				k = filepath.Join(home, k[2:])
			}
		}
		a = append(a, "-i", k, "-o", "IdentitiesOnly=yes")
	}
	return a
}

func (s SSH) dest() string { return s.Target.Label() }

// Exec runs a POSIX sh script on the server (whatever its login shell),
// with stdin when given, and returns its output.
func (s SSH) Exec(ctx context.Context, script string, stdin io.Reader) (string, error) {
	args := append(s.args(), s.dest(), "sh -c "+shq(script))
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = stdin
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if len(msg) > 400 {
			msg = msg[len(msg)-400:]
		}
		if msg == "" {
			return out.String(), err
		}
		return out.String(), errors.New(msg)
	}
	return out.String(), nil
}

// Forward runs ssh -N -L for 127.0.0.1:local → the server's 127.0.0.1:remote.
func (s SSH) Forward(localPort, remotePort int) (Forward, error) {
	args := append(s.args(),
		"-N",
		"-o", "ExitOnForwardFailure=yes",
		"-L", fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", localPort, remotePort),
		s.dest())
	cmd := exec.Command("ssh", args...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	f := &procForward{cmd: cmd, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		f.once.Do(func() { close(f.done) })
	}()
	return f, nil
}

type procForward struct {
	cmd  *exec.Cmd
	done chan struct{}
	once sync.Once
}

func (f *procForward) Done() <-chan struct{} { return f.done }

func (f *procForward) Close() {
	if f.cmd.Process != nil {
		_ = f.cmd.Process.Kill()
	}
	<-f.done
}
