package magic

import (
	"fmt"
	"os"
	"syscall"
)

// State and runtime directories must have only one writer. Group read/traverse
// is useful for the exported snapshot, but group or world write grants an
// unrelated local process control over the reconciler's inputs.
func ensureOwnedDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || int64(stat.Uid) != int64(os.Geteuid()) || info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("directory %q must be service-owned and writable only by its owner", path)
	}
	return nil
}
