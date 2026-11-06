package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/sadrzaj"
)

func slikaPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func slikaJPEG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// slikaWebP sastavlja početak datoteke WebP s traženom vrstom bloka
func slikaWebP(blok string, podaci []byte) []byte {
	tijelo := append([]byte("WEBP"+blok), make([]byte, 4)...)
	binary.LittleEndian.PutUint32(tijelo[8:12], uint32(len(podaci)))
	tijelo = append(tijelo, podaci...)
	out := append([]byte("RIFF"), make([]byte, 4)...)
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(tijelo)))
	return append(out, tijelo...)
}

func webpVP8(w, h uint16) []byte {
	p := make([]byte, 16)
	copy(p[3:6], []byte{0x9d, 0x01, 0x2a})
	binary.LittleEndian.PutUint16(p[6:8], w)
	binary.LittleEndian.PutUint16(p[8:10], h)
	return slikaWebP("VP8 ", p)
}

func webpVP8L(w, h uint32) []byte {
	p := make([]byte, 16)
	p[0] = 0x2f
	binary.LittleEndian.PutUint32(p[1:5], (w-1)|(h-1)<<14)
	return slikaWebP("VP8L", p)
}

func webpVP8X(w, h int) []byte {
	p := make([]byte, 16)
	p[4], p[5], p[6] = byte(w-1), byte((w-1)>>8), byte((w-1)>>16)
	p[7], p[8], p[9] = byte(h-1), byte((h-1)>>8), byte((h-1)>>16)
	return dodajBlokWebP(slikaWebP("VP8X", p), "VP8 ", make([]byte, 16))
}

// dodajBlokWebP dopisuje blok na kraj datoteke WebP i ispravlja njezinu duljinu
func dodajBlokWebP(b []byte, blok string, podaci []byte) []byte {
	b = append(append([]byte(nil), b...), blok...)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(podaci)))
	b = append(b, podaci...)
	binary.LittleEndian.PutUint32(b[4:8], uint32(len(b)-8))
	return b
}

// Vrijedi ono što je u bajtovima, a ne ime ili vrsta koje javi preglednik
func TestVrstaSlikePrijave(t *testing.T) {
	kratakVP8 := webpVP8(1, 1)
	kratakVP8 = kratakVP8[:len(kratakVP8)-3] // obećana duljina ne stane u datoteku
	losVP8 := webpVP8(1, 1)
	losVP8[23] = 0
	losVP8L := webpVP8L(1, 1)
	losVP8L[20] = 0
	for _, s := range []struct {
		ime   string
		b     []byte
		vrsta string
		err   error
	}{
		{"PNG", slikaPNG(t, 4, 4), models.SlikaPrijavePNG, nil},
		{"JPEG", slikaJPEG(t), models.SlikaPrijaveJPEG, nil},
		{"WebP VP8", webpVP8(1440, 900), models.SlikaPrijaveWebP, nil},
		{"WebP VP8L", webpVP8L(1440, 900), models.SlikaPrijaveWebP, nil},
		{"WebP VP8X", webpVP8X(1440, 900), models.SlikaPrijaveWebP, nil},
		{"HTML", []byte("<html><script>alert(1)</script></html>"), "", ErrSlikaNeispravna},
		{"SVG", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), "", ErrSlikaNeispravna},
		{"PDF", []byte("%PDF-1.7 ......"), "", ErrSlikaNeispravna},
		{"GIF", []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00"), "", ErrSlikaNeispravna},
		{"samo potpis PNG-a", []byte("\x89PNG\r\n\x1a\n"), "", ErrSlikaNeispravna},
		{"RIFF koji nije WebP", append([]byte("RIFF\x00\x00\x00\x00WAVEfmt "), make([]byte, 40)...), "", ErrSlikaNeispravna},
		{"odrezan WebP", kratakVP8, "", ErrSlikaNeispravna},
		{"VP8 bez oznake", losVP8, "", ErrSlikaNeispravna},
		{"VP8L bez oznake", losVP8L, "", ErrSlikaNeispravna},
		{"nepoznat blok WebP-a", slikaWebP("ABCD", make([]byte, 16)), "", ErrSlikaNeispravna},
		{"previše bajtova", make([]byte, models.NajvecaSlikaPrijave+1), "", ErrSlikaPrevelika},
		{"previše točaka", webpVP8X(10000, 10000), "", ErrSlikaPrevelika},
		{"VP8 bez mjera", webpVP8(0, 0), "", ErrSlikaPrevelika},
	} {
		vrsta, err := vrstaSlikePrijave(s.b)
		if vrsta != s.vrsta || !errors.Is(err, s.err) {
			t.Errorf("%s: %q %v", s.ime, vrsta, err)
		}
	}
}

// Zaglavlje slike nije dovoljno: mora postojati slika i iza nje ne smije biti ničega
func TestGradjaSlikePrijave(t *testing.T) {
	skripta := []byte("<script>alert(1)</script>")
	sPunjenjem := func(b []byte) []byte { return append(append([]byte(nil), b...), 0, 0, 0xff) }
	pngSlika, jpegSlika := slikaPNG(t, 4, 4), slikaJPEG(t)
	vp8xBezSlike := make([]byte, 16)
	vp8xBezSlike[4], vp8xBezSlike[7] = 9, 9
	duljinaDatoteke := func(b []byte) []byte { // duljina datoteke se slaže, a blokovi ne
		binary.LittleEndian.PutUint32(b[4:8], uint32(len(b)-8))
		return b
	}
	for _, s := range []struct {
		ime   string
		b     []byte
		vrsta string
		err   error
	}{
		{"JPEG s nulama na kraju", sPunjenjem(jpegSlika), models.SlikaPrijaveJPEG, nil},
		{"WebP neparnog bloka bez punjenja", slikaWebP("VP8 ", webpPodaci(17)), models.SlikaPrijaveWebP, nil},
		{"WebP neparnog bloka s punjenjem", append(slikaWebP("VP8 ", webpPodaci(17)), 0), models.SlikaPrijaveWebP, nil},
		{"WebP s dodatnim blokom", dodajBlokWebP(webpVP8L(4, 4), "EXIF", make([]byte, 7)), models.SlikaPrijaveWebP, nil},
		{"PNG sa skriptom iza slike", append(append([]byte(nil), pngSlika...), skripta...), "", ErrSlikaNeispravna},
		{"PNG bez točaka", append(append([]byte(nil), pngSlika[:33]...), pngSlika[len(pngSlika)-12:]...), "", ErrSlikaNeispravna},
		{"PNG bez završnog bloka", pngSlika[:len(pngSlika)-12], "", ErrSlikaNeispravna},
		{"PNG s blokom duljim od datoteke", pngSlika[:len(pngSlika)-14], "", ErrSlikaNeispravna},
		{"JPEG sa skriptom iza slike", append(append([]byte(nil), jpegSlika...), skripta...), "", ErrSlikaNeispravna},
		{"JPEG bez točaka", append(jpegDoTocaka(t, jpegSlika), 0xff, 0xd9), "", ErrSlikaNeispravna},
		{"WebP sa skriptom iza slike", append(webpVP8L(4, 4), skripta...), "", ErrSlikaNeispravna},
		{"WebP u kojem iza slike slijedi skripta", duljinaDatoteke(append(webpVP8L(4, 4), skripta...)), "", ErrSlikaNeispravna},
		{"WebP s blokom duljim od datoteke", func() []byte { b := webpVP8L(4, 4); b[16] = 0xff; return b }(), "", ErrSlikaNeispravna},
		{"WebP VP8X bez slike", slikaWebP("VP8X", vp8xBezSlike), "", ErrSlikaNeispravna},
		{"WebP s odsječenim zaglavljem bloka", duljinaDatoteke(append(webpVP8L(4, 4), 1, 2, 3)), "", ErrSlikaNeispravna},
	} {
		vrsta, err := vrstaSlikePrijave(s.b)
		if vrsta != s.vrsta || !errors.Is(err, s.err) {
			t.Errorf("%s: %q %v", s.ime, vrsta, err)
		}
	}
}

// webpPodaci su podaci bloka VP8 s ispravnom oznakom i mjerama 4 × 4
func webpPodaci(n int) []byte {
	p := make([]byte, n)
	copy(p[3:6], []byte{0x9d, 0x01, 0x2a})
	p[6], p[8] = 4, 4
	return p
}

// jpegDoTocaka reže JPEG ispred početka točaka; zaglavlje i mjere ostaju. Dodaje
// oznaku JFIF, kakvu imaju slike iz drugih programa: uz nju standardno čitanje
// mjera stane na mjerama i ne vidi da točaka nema.
func jpegDoTocaka(t *testing.T, b []byte) []byte {
	t.Helper()
	i := bytes.Index(b, []byte{0xff, 0xda})
	if i < 0 {
		t.Fatal("JPEG bez točaka")
	}
	out := append([]byte{}, b[:2]...)
	out = append(out, 0xff, 0xe0, 0, 16, 'J', 'F', 'I', 'F', 0, 1, 1, 0, 0, 1, 0, 1, 0, 0)
	return append(out, b[2:i]...)
}

// PNG koji u zaglavlju traži previše točaka odbija se bez čitanja točaka
func TestSlikaPrevelikihMjera(t *testing.T) {
	b := slikaPNG(t, 2, 2)
	binary.BigEndian.PutUint32(b[16:20], 20000)
	binary.BigEndian.PutUint32(b[20:24], 20000)
	binary.BigEndian.PutUint32(b[29:33], crc32.ChecksumIEEE(b[12:29])) // zaglavlje ima svoj zbroj
	if _, err := vrstaSlikePrijave(b); !errors.Is(err, ErrSlikaPrevelika) {
		t.Errorf("%v", err)
	}
}

func saSpremistem(t *testing.T) *sadrzaj.Spremiste {
	t.Helper()
	prije := repository.Spremiste()
	t.Cleanup(func() { repository.SetSpremiste(prije) })
	sp, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "sadrzaj.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sp.Zatvori(); err != nil {
			t.Error(err)
		}
	})
	repository.SetSpremiste(sp)
	return sp
}

func TestPosaljiSaSlikom(t *testing.T) {
	sp := saSpremistem(t)
	s, u := servisPrijavaGreske(t)
	ctx := context.Background()
	slika := slikaPNG(t, 10, 10)
	p, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "Graf se ne crta.", Stranica: "/karta", Slika: slika})
	if err != nil {
		t.Fatal(err)
	}
	if p.SlikaOtisak != sadrzaj.Otisak(slika) || p.SlikaBajtova != len(slika) || p.SlikaVrsta != models.SlikaPrijavePNG {
		t.Fatalf("opis: %+v", p)
	}
	if !sp.Ima(ctx, p.SlikaOtisak) || !s.ImaSliku(ctx, p) {
		t.Fatal("slika nije u spremištu")
	}
	if b, vrsta := s.SlikaPrijave(ctx, p); !bytes.Equal(b, slika) || vrsta != models.SlikaPrijavePNG {
		t.Fatalf("čitanje: %d %q", len(b), vrsta)
	}
	// ponovljeno slanje s istom slikom je ista prijava
	isti, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "Graf se ne crta.", Stranica: "/karta", Slika: slika})
	if err != nil || isti.ID != p.ID {
		t.Errorf("ponovljeno: %v %v", isti, err)
	}
	// isti tekst s drugom ili bez slike je nova prijava, da se slika ne izgubi
	druga, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "Graf se ne crta.", Stranica: "/karta", Slika: slikaJPEG(t)})
	if err != nil || druga.ID == p.ID || druga.SlikaVrsta != models.SlikaPrijaveJPEG {
		t.Errorf("druga slika: %+v %v", druga, err)
	}
	bez, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "Graf se ne crta.", Stranica: "/karta"})
	if err != nil || bez.SlikaOtisak != "" || bez.ID == p.ID {
		t.Errorf("bez slike: %+v %v", bez, err)
	}
}

func TestPosaljiOdbijaLosuSliku(t *testing.T) {
	saSpremistem(t)
	s, u := servisPrijavaGreske(t)
	ctx := context.Background()
	if _, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "x", Slika: []byte("<svg></svg>")}); !errors.Is(err, ErrSlikaNeispravna) {
		t.Errorf("SVG: %v", err)
	}
	if _, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "x", Slika: make([]byte, models.NajvecaSlikaPrijave+1)}); !errors.Is(err, ErrSlikaPrevelika) {
		t.Errorf("velika: %v", err)
	}
	if moje, err := s.MojePrijave(ctx, u.ID.String()); err != nil || len(moje) != 0 {
		t.Errorf("spremljeno je: %d %v", len(moje), err)
	}
}

// Slika je teža od teksta: nakon granice prolazi samo prijava bez slike, a
// nakon sata opet i sa slikom
func TestGranicaSlikaUSatu(t *testing.T) {
	saSpremistem(t)
	s, u := servisPrijavaGreske(t)
	ctx := context.Background()
	for i := range najviseSlikaUSatu {
		if _, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "slika " + strings.Repeat("x", i+1), Slika: slikaPNG(t, 2+i, 2)}); err != nil {
			t.Fatalf("%d: %v", i, err)
		}
	}
	slika := slikaPNG(t, 1, 1)
	if _, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "još jedna", Slika: slika}); !errors.Is(err, ErrPreviseSlika) {
		t.Errorf("preko granice: %v", err)
	}
	if _, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "još jedna"}); err != nil {
		t.Errorf("bez slike: %v", err)
	}
	s.sat = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if _, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "još jedna", Slika: slika}); err != nil {
		t.Errorf("nakon sata: %v", err)
	}
}

// Opis s drugog računala mogao bi pokazivati na tuđi dokument: što nije slika
// opisane vrste, ne daje se, i kad bajtovi postoje
func TestSlikaPrijaveProvjeravaBajtove(t *testing.T) {
	sp := saSpremistem(t)
	s, _ := servisPrijavaGreske(t)
	ctx := context.Background()
	otisak, err := sp.Upisi(ctx, "application/pdf", []byte("%PDF-1.7 tajni akt"), "ovdje")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := s.SlikaPrijave(ctx, &models.PrijavaGreske{SlikaOtisak: otisak, SlikaVrsta: models.SlikaPrijavePNG}); b != nil {
		t.Error("dan je dokument koji nije slika")
	}
	jpg := slikaJPEG(t)
	otisak, err = sp.Upisi(ctx, models.SlikaPrijaveJPEG, jpg, "ovdje")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := s.SlikaPrijave(ctx, &models.PrijavaGreske{SlikaOtisak: otisak, SlikaVrsta: models.SlikaPrijavePNG}); b != nil {
		t.Error("vrsta se ne slaže s opisom")
	}
	if b, _ := s.SlikaPrijave(ctx, &models.PrijavaGreske{SlikaOtisak: strings.Repeat("0", 64), SlikaVrsta: models.SlikaPrijavePNG}); b != nil {
		t.Error("slike nema")
	}
}

// pngDuljine je ispravan PNG točno zadane duljine: razliku nosi dodatni blok teksta
func pngDuljine(t *testing.T, n int) []byte {
	t.Helper()
	b := slikaPNG(t, 2, 2)
	dodatak := n - len(b) - 12
	if dodatak < 0 {
		t.Fatalf("premalo: %d", n)
	}
	blok := binary.BigEndian.AppendUint32(nil, uint32(dodatak))
	blok = append(blok, "tEXt"...)
	blok = append(blok, make([]byte, dodatak)...)
	blok = binary.BigEndian.AppendUint32(blok, crc32.ChecksumIEEE(blok[4:]))
	return append(append(append([]byte(nil), b[:33]...), blok...), b[33:]...)
}

// Najveća dopuštena slika prolazi, a bajt više ne; i poznati JPEG s dodatkom iza
// slike ostaje odbijen
func TestGranicaSlikeOdDvaMB(t *testing.T) {
	if vrsta, err := vrstaSlikePrijave(pngDuljine(t, 2<<20)); err != nil || vrsta != models.SlikaPrijavePNG {
		t.Errorf("točno 2 MB: %q %v", vrsta, err)
	}
	if _, err := vrstaSlikePrijave(pngDuljine(t, 2<<20+1)); !errors.Is(err, ErrSlikaPrevelika) {
		t.Errorf("bajt preko: %v", err)
	}
	if _, err := vrstaSlikePrijave(append(slikaJPEG(t), "dodatak iza slike"...)); !errors.Is(err, ErrSlikaNeispravna) {
		t.Errorf("JPEG s dodatkom: %v", err)
	}
}

// Slika kojoj je rok prošao ne daje se ni dok bajtovi postoje jer ih drži druga
// prijava s istom slikom, a ta prijava svoju sliku i dalje ima
func TestSlikaNakonRokaSeNeDaje(t *testing.T) {
	sp := saSpremistem(t)
	s, u := servisPrijavaGreske(t)
	admin := &models.User{ID: u.ID, FullName: "Ivo Ivić"}
	ctx := context.Background()
	slika := slikaPNG(t, 10, 10)
	prva, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "Graf se ne crta.", Slika: slika})
	if err != nil {
		t.Fatal(err)
	}
	druga, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "I karta je prazna.", Slika: slika})
	if err != nil {
		t.Fatal(err)
	}
	rijesena, err := s.Odgovori(ctx, admin, prva.ID, models.StanjePrijaveRijeseno, "Popravljeno.")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := s.SlikaPrijave(ctx, rijesena); len(b) == 0 || !s.ImaSliku(ctx, rijesena) {
		t.Fatal("slika mora ostati dok rok traje")
	}

	s.sat = func() time.Time { return time.Now().Add(models.RokSlikePrijave + time.Minute) }
	if b, _ := s.SlikaPrijave(ctx, rijesena); b != nil || s.ImaSliku(ctx, rijesena) {
		t.Error("slika je dana nakon roka")
	}
	s.ocistiSlike(ctx)
	if b, _ := s.SlikaPrijave(ctx, druga); !bytes.Equal(b, slika) || !sp.Ima(ctx, druga.SlikaOtisak) {
		t.Error("druga prijava je ostala bez slike")
	}
	veza := sadrzaj.Veza{Entitet: repository.EntityPrijaveGresaka, EntitetID: prva.ID, Uloga: "slika"}
	if sp.ImaVezu(ctx, prva.SlikaOtisak, veza) {
		t.Error("prijava kojoj je rok prošao još drži sliku")
	}
}

// Računalo koje se upali odmah briše ono čemu je rok prošao dok je bilo ugašeno, a
// dok radi, gleda i dalje. Zaustavljanje pričeka da posao stane.
func TestCiscenjeSlikaPriPokretanju(t *testing.T) {
	sp := saSpremistem(t)
	s, u := servisPrijavaGreske(t)
	admin := &models.User{ID: u.ID, FullName: "Ivo Ivić"}
	ctx := context.Background()
	rijesi := func(opis string, w int) string {
		t.Helper()
		p, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: opis, Slika: slikaPNG(t, w, 2)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Odgovori(ctx, admin, p.ID, models.StanjePrijaveRijeseno, ""); err != nil {
			t.Fatal(err)
		}
		return p.SlikaOtisak
	}
	prva := rijesi("Graf se ne crta.", 2)
	razmak := razmakCiscenjaSlika
	razmakCiscenjaSlika = 5 * time.Millisecond
	defer func() { razmakCiscenjaSlika = razmak }()
	s.sat = func() time.Time { return time.Now().Add(models.RokSlikePrijave + time.Minute) }

	stani := s.PokreniCiscenjeSlika(ctx)
	defer stani()
	cekaj(t, func() bool { return !sp.Ima(ctx, prva) })
	druga := rijesi("I karta je prazna.", 3)
	cekaj(t, func() bool { return !sp.Ima(ctx, druga) })
}

// Kvar baze pri brisanju samo ostaje u dnevniku, a posao se ne ruši
func TestCiscenjeSlikaKvarBaze(t *testing.T) {
	saSpremistem(t)
	s, _, baza := servisPrijavaGreskeSBazom(t)
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	s.ocistiSlike(context.Background())
}

// Kvar baze pri brojanju slika ide pozivatelju kao greška
func TestBrojSlikaKvarBaze(t *testing.T) {
	s, u, baza := servisPrijavaGreskeSBazom(t)
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.provjeriBrojSlika(context.Background(), u.ID.String()); err == nil {
		t.Error("kvar baze")
	}
}
