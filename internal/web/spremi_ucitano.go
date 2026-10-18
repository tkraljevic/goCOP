package web

import (
	"errors"
	"io"
	"os"
)

// spremiUcitano upisuje datoteku iz obrasca u privremenu datoteku na disku i
// zatvara je. Zatvaranje se gleda jer se upisano poslije čita s diska:
// nepotpun zapis paketa ili arhive javila bi tek provjera ZIP-a ili SQLite-a,
// a ugovor bi se upisao iz onoga što je stiglo na disk.
func spremiUcitano(out *os.File, r io.Reader) (int64, error) {
	n, err := io.Copy(out, r)
	return n, errors.Join(err, out.Close())
}
