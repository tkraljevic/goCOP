//go:build !darwin && !linux && !windows

package disk

import (
	"errors"
	"os"
)

// Izmjeri na ostalim sustavima ne zna mjeriti: nadzor to jednom zapiše u
// dnevnik i ne upozorava
func (Sustav) Izmjeri(put string) (Mjera, error) {
	return Mjera{}, &os.PathError{Op: "statfs", Path: put, Err: errors.ErrUnsupported}
}
