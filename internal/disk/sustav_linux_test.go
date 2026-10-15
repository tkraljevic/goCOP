//go:build linux

package disk

import (
	"syscall"
	"testing"
)

// Blokovi se broje u f_frsize, a f_bsize vrijedi samo kad f_frsize nema
func TestVelicinaBlokaNaLinuxu(t *testing.T) {
	for _, s := range []struct {
		ime     string
		fs      syscall.Statfs_t
		ocekuje uint64
	}{
		{"ext4: isto", syscall.Statfs_t{Bsize: 4096, Frsize: 4096}, 4096},
		{"FUSE: f_bsize veći od f_frsize", syscall.Statfs_t{Bsize: 131072, Frsize: 4096}, 4096},
		{"jezgra bez f_frsize", syscall.Statfs_t{Bsize: 4096}, 4096},
	} {
		if got := velicinaBloka(&s.fs); got != s.ocekuje {
			t.Errorf("%s: %d, očekivano %d", s.ime, got, s.ocekuje)
		}
	}
}
