package kroztudji

import (
	"testing"

	kripto "golang.org/x/crypto/utrka"
	sqlite "modernc.org/utrka"
)

func TestUtrkaUTudjemKodu(t *testing.T) {
	sqlite.Utrka()
	kripto.Utrka()
}
