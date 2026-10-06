//go:build windows

package daemon

// On Windows the named pipe itself refuses a second listener, which is
// enough to stop a duplicate daemon.
func lockDataDir(string) (func(), error) { return func() {}, nil }
