package atomicfs

// SetFsyncDirForTest swaps the package-internal fsyncDir hook for the
// duration of a test and returns a restore func. Production callers must
// not use this — it is exported only because it lives behind the _test.go
// build tag (compiled only during `go test`).
func SetFsyncDirForTest(fn func(dir string) error) (restore func()) {
	orig := fsyncDir
	fsyncDir = fn
	return func() { fsyncDir = orig }
}
