//go:build darwin || linux

package disk

import (
	"os"
	"strconv"
	"syscall"
)

// Izmjeri čita statfs mape; disk prepoznaje po uređaju na kojem mapa leži
// (st_dev), pa dvije mape na istom disku daju istu oznaku. Skupovi podataka
// ZFS-a, podsvesci btrfs-a i svesci APFS-a dijele prostor, a imaju svaki svoj
// st_dev, pa se broje kao zasebni diskovi (INSTALACIJA.md, Docker).
func (Sustav) Izmjeri(put string) (Mjera, error) {
	var fs syscall.Statfs_t
	var st syscall.Stat_t
	err := syscall.Statfs(put, &fs)
	if err == nil {
		err = syscall.Stat(put, &st)
	}
	if err != nil {
		return Mjera{}, &os.PathError{Op: "statfs", Path: put, Err: err}
	}
	blok := velicinaBloka(&fs)
	return Mjera{Disk: strconv.FormatUint(uint64(st.Dev), 10), Ukupno: fs.Blocks * blok, Slobodno: fs.Bavail * blok}, nil
}
