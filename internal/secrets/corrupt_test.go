package secrets

import "os"

func osWriteCorrupt(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(b) == 0 {
		return os.WriteFile(path, []byte("xxxx"), 0o600)
	}
	b[len(b)/2] ^= 0xff
	return os.WriteFile(path, b, 0o600)
}
