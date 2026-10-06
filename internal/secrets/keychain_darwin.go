//go:build darwin

package secrets

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// keychain keeps the master key in the login keychain, so the key and the
// secrets it opens are no longer side by side in one folder. Items go
// through /usr/bin/security; the key is written on its stdin (security -i),
// never on a command line another process could read.
type keychain struct{ dataDir string }

func osKeyring(dataDir string) Keyring { return keychain{dataDir: dataDir} }

const securityBin = "/usr/bin/security"

// account names the data folder too: a second Rove Code home (a dev copy,
// ROVECODE_HOME) has its own key.
func (k keychain) account(user string) string { return user + "@" + k.dataDir }

func (k keychain) Get(service, user string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, securityBin, "find-generic-password", "-s", service, "-a", k.account(user), "-w")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 44 {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("keychain: %v %s", err, strings.TrimSpace(errb.String()))
	}
	return hex.DecodeString(strings.TrimSpace(out.String()))
}

func (k keychain) Set(service, user string, secret []byte) error {
	if strings.ContainsAny(k.dataDir, "\"\n\\") {
		return errors.New("keychain: data folder name cannot be quoted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	line := fmt.Sprintf("add-generic-password -s %s -a \"%s\" -l \"Rove Code\" -D \"Rove Code secrets key\" -w %s\n",
		service, k.account(user), hex.EncodeToString(secret))
	cmd := exec.CommandContext(ctx, securityBin, "-i")
	cmd.Stdin = strings.NewReader(line)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("keychain: %v %s", err, strings.TrimSpace(errb.String()))
	}
	// security -i reports a failed command on stderr, not in its exit code
	got, err := k.Get(service, user)
	if err != nil || !bytes.Equal(got, secret) {
		return fmt.Errorf("keychain: key not stored %s", strings.TrimSpace(errb.String()))
	}
	return nil
}
