package config

// UnloadForTest returns the package to its pre-Init state and restores the previous
// config when the returned function is called. Test-only.
//
// cfg is process-wide, so a test asserting on the unloaded case would otherwise pass
// or fail depending on whether an earlier test happened to load one.
func UnloadForTest() func() {
	cfgLock.Lock()
	previous := cfg
	cfg = nil
	cfgLock.Unlock()
	return func() {
		cfgLock.Lock()
		cfg = previous
		cfgLock.Unlock()
	}
}
