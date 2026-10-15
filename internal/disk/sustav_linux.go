//go:build linux

package disk

import "syscall"

// velicinaBloka: Linux broji blokove (f_blocks, f_bavail) u jedinicama
// f_frsize. Na ext4, xfs i btrfs to je isto što i f_bsize, ali na FUSE
// sustavima (Unraid shfs, mergerfs, sshfs) ne mora biti. Stara jezgra koja
// f_frsize ne javlja daje 0, pa se tada uzme f_bsize.
func velicinaBloka(fs *syscall.Statfs_t) uint64 {
	if fs.Frsize > 0 {
		return uint64(fs.Frsize)
	}
	return uint64(fs.Bsize)
}
