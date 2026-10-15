//go:build darwin

package disk

import "syscall"

// velicinaBloka: macOS broji blokove u jedinicama f_bsize (f_frsize nema)
func velicinaBloka(fs *syscall.Statfs_t) uint64 {
	return uint64(fs.Bsize)
}
