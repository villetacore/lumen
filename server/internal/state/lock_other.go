//go:build !unix

package state

// lockDir is a no-op where flock is unavailable: the server profile this
// lock protects runs on Linux, and desktop builds do not open a state store.
func lockDir(string) (func(), error) { return func() {}, nil }
