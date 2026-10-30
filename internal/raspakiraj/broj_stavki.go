package raspakiraj

import (
	"bytes"
	"fmt"
	"io"
)

// potpisZaglavlja počinje svako zaglavlje stavke u središnjem imeniku ZIP-a.
var potpisZaglavlja = []byte("PK\x01\x02")

// velicinaCitanja je koliko se ZIP-a čita odjednom pri brojanju stavki.
var velicinaCitanja = 1 << 20

// ProvjeriBrojStavki javlja grešku kad ZIP može imati više od najvise
// stavki. Zove se prije zip.NewReader, koji za svaku stavku središnjeg
// imenika drži zapis u memoriji, a broj stavki s kraja imenika ga ne
// ograničava. Broji se svako mjesto s potpisom zaglavlja, pa je to gornja
// granica broja stavki.
func ProvjeriBrojStavki(r io.ReaderAt, size int64, najvise int) error {
	sr := io.NewSectionReader(r, 0, size)
	buf := make([]byte, velicinaCitanja)
	ostatak, n := 0, 0
	for {
		k, err := io.ReadFull(sr, buf[ostatak:])
		dio := buf[:ostatak+k]
		if n += bytes.Count(dio, potpisZaglavlja); n > najvise {
			return fmt.Errorf("u arhivi je previše datoteka: smije ih imati najviše %d", najvise)
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil
		}
		if err != nil {
			return err
		}
		// zadnja tri bajta mogu biti početak potpisa koji se nastavlja dalje
		ostatak = copy(buf, dio[len(dio)-len(potpisZaglavlja)+1:])
	}
}
