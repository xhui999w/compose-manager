//go:build !linux

package compose

// Production targets are Linux. Windows junctions are rejected by RemovalDirectory
// and the per-directory EvalSymlinks check in the deletion preview.
func checkRemovalMounts(path string) error { return nil }
