//go:build windows

package disk

import (
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// Izmjeri pita Windows koliko je mjesta (GetDiskFreeSpaceExW); disk
// prepoznaje po svesku na kojem mapa leži (GetVolumePathNameW, npr. C:\)
func (Sustav) Izmjeri(put string) (Mjera, error) {
	p, err := windows.UTF16PtrFromString(put)
	if err != nil {
		return Mjera{}, &os.PathError{Op: "GetDiskFreeSpaceEx", Path: put, Err: err}
	}
	var slobodno, ukupno, sveSlobodno uint64
	if err := windows.GetDiskFreeSpaceEx(p, &slobodno, &ukupno, &sveSlobodno); err != nil {
		return Mjera{}, &os.PathError{Op: "GetDiskFreeSpaceEx", Path: put, Err: err}
	}
	svezak := make([]uint16, windows.MAX_PATH+1)
	if err := windows.GetVolumePathName(p, &svezak[0], uint32(len(svezak))); err != nil {
		return Mjera{}, &os.PathError{Op: "GetVolumePathName", Path: put, Err: err}
	}
	return Mjera{Disk: strings.ToLower(windows.UTF16ToString(svezak)), Ukupno: ukupno, Slobodno: slobodno}, nil
}
