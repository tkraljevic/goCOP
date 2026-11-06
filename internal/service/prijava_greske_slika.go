package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	_ "image/jpeg" // slika zaslona s mobitela
	_ "image/png"  // slika zaslona s računala

	"gocop/internal/models"
)

// najviseTocakaSlike ograđuje sliku zaslona: i pet zaslona 4K stane, a slika od
// nekoliko KB koja se u pregledniku razvije u gigabajte ne prolazi.
const najviseTocakaSlike = 50_000_000

// vrstaSlikePrijave čita iz samih bajtova je li datoteka slika koju prijava smije
// nositi, a ime i vrsta koje javi preglednik ne vrijede. Provjerava zaglavlje,
// mjere i građu datoteke, bez čitanja svih točaka, pa i slika od 8 MB ne zauzima
// memoriju. Sadržaj unutar bloka slike time se ne može provjeriti, pa se slika
// pri prikazu ne smije izvršavati ni otkrivati po sadržaju (posluziTudjuDatoteku).
func vrstaSlikePrijave(b []byte) (string, error) {
	if len(b) > models.NajvecaSlikaPrijave {
		return "", ErrSlikaPrevelika
	}
	vrsta, w, h, err := mjereSlike(b)
	if err != nil {
		return "", err
	}
	if w <= 0 || h <= 0 || int64(w)*int64(h) > najviseTocakaSlike {
		return "", ErrSlikaPrevelika
	}
	if !cijelaSlika(vrsta, b) {
		return "", ErrSlikaNeispravna
	}
	return vrsta, nil
}

// cijelaSlika gleda građu datoteke: mora imati podatke slike i završiti gdje
// slika završava. Tako ne prolazi zaglavlje bez slike ni nešto dodano iza slike.
func cijelaSlika(vrsta string, b []byte) bool {
	switch vrsta {
	case models.SlikaPrijavePNG:
		return cijeliPNG(b)
	case models.SlikaPrijaveJPEG:
		return cijeliJPEG(b)
	}
	return cijeliWebP(b)
}

// cijeliPNG prolazi kroz blokove: mora biti blok s točkama, a završni blok
// IEND mora biti zadnje u datoteci
func cijeliPNG(b []byte) bool {
	imaTocke := false
	for i := int64(8); i+12 <= int64(len(b)); {
		kraj := i + 12 + int64(binary.BigEndian.Uint32(b[i:i+4]))
		if kraj > int64(len(b)) {
			return false
		}
		switch string(b[i+4 : i+8]) {
		case "IEND":
			return imaTocke && kraj == int64(len(b))
		case "IDAT":
			imaTocke = true
		}
		i = kraj
	}
	return false
}

// cijeliJPEG traži početak točaka i završnu oznaku na kraju datoteke. Neki uređaji
// iza oznake dodaju samo nule ili bajtove 0xFF, pa to ne smeta.
func cijeliJPEG(b []byte) bool {
	kraj := len(b)
	for kraj > 0 && (b[kraj-1] == 0x00 || b[kraj-1] == 0xff) {
		kraj--
	}
	return kraj >= 2 && b[kraj-2] == 0xff && b[kraj-1] == 0xd9 && bytes.Contains(b[:kraj-2], []byte{0xff, 0xda})
}

// cijeliWebP prolazi kroz blokove: moraju točno ispuniti datoteku, a jedan od njih
// mora nositi sliku. Blok neparne duljine ima bajt punjenja, koji zadnji blok
// smije i nemati.
func cijeliWebP(b []byte) bool {
	kraj := int64(binary.LittleEndian.Uint32(b[4:8])) + 8
	if n := int64(len(b)); n != kraj && (n != kraj+1 || kraj%2 == 0) {
		return false
	}
	imaSliku := false
	for i := int64(12); i < kraj; {
		if i+8 > kraj {
			return false
		}
		duljina := int64(binary.LittleEndian.Uint32(b[i+4 : i+8]))
		if i+8+duljina > kraj {
			return false
		}
		switch string(b[i : i+4]) {
		case "VP8 ", "VP8L", "ANMF":
			imaSliku = true
		}
		i += 8 + duljina + duljina%2
	}
	return imaSliku
}

// mjereSlike prepoznaje vrstu po potpisu i čita mjere. WebP se čita ovdje jer bi
// uvoz dekodera WebP-a promijenio što prihvaćaju sva druga čitanja slika u programu.
func mjereSlike(b []byte) (vrsta string, w, h int, err error) {
	if w, h, ok := mjereWebP(b); ok {
		return models.SlikaPrijaveWebP, w, h, nil
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return "", 0, 0, ErrSlikaNeispravna
	}
	switch format {
	case "png":
		return models.SlikaPrijavePNG, cfg.Width, cfg.Height, nil
	case "jpeg":
		return models.SlikaPrijaveJPEG, cfg.Width, cfg.Height, nil
	}
	return "", 0, 0, ErrSlikaNeispravna
}

// mjereWebP čita mjere iz prvog dijela datoteke WebP: VP8 (sa sažimanjem), VP8L
// (bez sažimanja) ili VP8X (proširen oblik). Mjere koje ne stanu u datoteku znače
// da nije WebP.
func mjereWebP(b []byte) (w, h int, ok bool) {
	if len(b) < 30 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return 0, 0, false
	}
	if int64(binary.LittleEndian.Uint32(b[4:8]))+8 > int64(len(b)) {
		return 0, 0, false
	}
	return mjereBlokaWebP(string(b[12:16]), b[20:])
}

// mjereBlokaWebP čita mjere iz prvog bloka; podaci imaju barem 10 bajtova
func mjereBlokaWebP(blok string, podaci []byte) (w, h int, ok bool) {
	switch {
	case blok == "VP8 " && podaci[3] == 0x9d && podaci[4] == 0x01 && podaci[5] == 0x2a:
		return int(binary.LittleEndian.Uint16(podaci[6:8]) & 0x3fff), int(binary.LittleEndian.Uint16(podaci[8:10]) & 0x3fff), true
	case blok == "VP8L" && podaci[0] == 0x2f:
		v := binary.LittleEndian.Uint32(podaci[1:5])
		return int(v&0x3fff) + 1, int((v>>14)&0x3fff) + 1, true
	case blok == "VP8X":
		return dvadesetCetiri(podaci[4:7]) + 1, dvadesetCetiri(podaci[7:10]) + 1, true
	}
	return 0, 0, false
}

func dvadesetCetiri(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 }

// SlikaPrijave daje sliku zaslona i njezinu vrstu; prazno kad je prijava nema
// ili slika još nije stigla na ovo računalo. Bajtovi se provjeravaju i pri
// čitanju: opis stiže s drugog računala i mogao bi pokazivati na drugi dokument.
func (s *PrijavaGreskeService) SlikaPrijave(ctx context.Context, p *models.PrijavaGreske) ([]byte, string) {
	b := s.repo.CitajSliku(ctx, p.SlikaOtisak)
	if len(b) == 0 {
		return nil, ""
	}
	vrsta, err := vrstaSlikePrijave(b)
	if err != nil || vrsta != p.SlikaVrsta {
		return nil, ""
	}
	return b, vrsta
}

// ImaSliku javlja je li slika prijave već na ovom računalu
func (s *PrijavaGreskeService) ImaSliku(ctx context.Context, p *models.PrijavaGreske) bool {
	return s.repo.ImaSliku(ctx, p.SlikaOtisak)
}
