//go:build windows

package memory

// lock is a no-op where the platform has no flock: Windows is paused (see
// docs/STATUS.md), and one process at a time is assumed there.
func lock(string) (func(), error) { return func() {}, nil }
