//go:build !darwin

package secrets

// osKeyring is the system's key store; only the macOS keychain is used so
// far, elsewhere the key stays in its 0600 file.
func osKeyring(string) Keyring { return nil }
