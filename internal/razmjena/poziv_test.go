package razmjena

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// Okvir nosi do točno 64 KiB; veći se ne piše, a pročitana veća duljina
// odbija se prije čitanja sadržaja. Prekinut okvir je greška čitanja.
func TestOkvir(t *testing.T) {
	var b bytes.Buffer
	for _, n := range []int{0, 1, NajveciOkvir} {
		b.Reset()
		sadrzaj := bytes.Repeat([]byte{'x'}, n)
		if err := PisiOkvir(&b, sadrzaj); err != nil {
			t.Fatalf("%d B: %v", n, err)
		}
		if got, err := CitajOkvir(&b); err != nil || !bytes.Equal(got, sadrzaj) {
			t.Errorf("%d B: pročitano %d, %v", n, len(got), err)
		}
	}
	if err := PisiOkvir(&b, make([]byte, NajveciOkvir+1)); !errors.Is(err, ErrOkvirPrevelik) {
		t.Errorf("prevelik okvir se piše: %v", err)
	}
	var duljina [4]byte
	binary.BigEndian.PutUint32(duljina[:], NajveciOkvir+1)
	if _, err := CitajOkvir(bytes.NewReader(duljina[:])); !errors.Is(err, ErrOkvirPrevelik) {
		t.Errorf("prevelika duljina: %v", err)
	}
	binary.BigEndian.PutUint32(duljina[:], 10)
	if _, err := CitajOkvir(bytes.NewReader(append(duljina[:], "kratko"...))); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("prekinut okvir: %v", err)
	}
	if _, err := CitajOkvir(bytes.NewReader([]byte{0, 0})); err == nil {
		t.Error("prekinuta duljina")
	}
}

// provjeraZa je provjera poruke na računalu B, uz sastajalište R i
// pošiljatelja A koji je član
type provjeraZa struct {
	a, b, r ed25519.PrivateKey
	sad     time.Time
	pp      ProvjeraPoruka
}

func novaProvjera(t *testing.T) *provjeraZa {
	t.Helper()
	p := &provjeraZa{a: newKey(t), b: newKey(t), r: newKey(t), sad: time.Unix(1_790_990_000, 0)}
	javniA := p.a.Public().(ed25519.PublicKey)
	p.pp = ProvjeraPoruka{Protokol: "gocop", Moj: p.b.Public().(ed25519.PublicKey), Sastajaliste: p.r.Public().(ed25519.PublicKey),
		Vjeruje: func(k ed25519.PublicKey) bool { return k.Equal(javniA) }, Sat: func() time.Time { return p.sad }, Pricuva: NovaPricuvaPoruka()}
	return p
}

// tijelo je poziv od A za B preko R
func (p *provjeraZa) tijelo() TijeloPoruke {
	return TijeloPoruke{Sesija: NovaSesija(), Za: PublicKeyString(p.b.Public().(ed25519.PublicKey)),
		Sastajaliste: PublicKeyString(p.r.Public().(ed25519.PublicKey)), Izdano: p.sad.Unix(), Tajna: []byte("tajna")}
}

// Poziv od člana, za ovo računalo, preko njegova sastajališta i unutar
// 10 min prolazi jednom; ponovljeni ne prolazi. Sve ostalo se odbija:
// nečlan, krivi potpis, poruka druge vrste ili druge mreže, za drugo
// računalo, preko drugog sastajališta, prestara, krivog oblika.
func TestProvjeraPoruke(t *testing.T) {
	p := novaProvjera(t)
	poziv := Potpisi(p.a, "gocop", VrstaPoziv, p.tijelo())
	got, err := p.pp.Provjeri(poziv)
	if err != nil || got.Od != PublicKeyString(p.a.Public().(ed25519.PublicKey)) || got.Ver != 1 || string(got.Tajna) != "tajna" {
		t.Fatalf("ispravan poziv: %+v, %v", got, err)
	}
	if _, err := p.pp.Provjeri(poziv); !errors.Is(err, ErrPonovljenaPoruka) {
		t.Errorf("ponovljen poziv: %v", err)
	}
	sinkro := p.tijelo()
	sinkro.Pokusaj = 2
	if _, err := p.pp.Provjeri(Potpisi(p.a, "gocop", VrstaSinkro, sinkro)); err != nil {
		t.Errorf("sinkro 2. pokušaja: %v", err)
	}
	drugi := p.tijelo()
	for ime, c := range map[string]struct {
		poruka PotpisanaPoruka
		greska error
	}{
		"nečlan":              {Potpisi(newKey(t), "gocop", VrstaPoziv, p.tijelo()), ErrNepoznatPosiljatelj},
		"potpis za odziv":     {PotpisanaPoruka{Vrsta: VrstaPoziv, Tijelo: Potpisi(p.a, "gocop", VrstaOdziv, drugi).Tijelo, Potpis: Potpisi(p.a, "gocop", VrstaOdziv, drugi).Potpis}, ErrPotpisPoruke},
		"druga mreža":         {pregazi(Potpisi(p.a, "gocop-test", VrstaPoziv, p.tijelo()), VrstaPoziv), ErrPotpisPoruke},
		"za drugoga":          {Potpisi(p.a, "gocop", VrstaPoziv, s(p.tijelo(), func(t *TijeloPoruke) { t.Za = t.Sastajaliste })), ErrNijeZaMene},
		"drugo sastajalište":  {Potpisi(p.a, "gocop", VrstaPoziv, s(p.tijelo(), func(t *TijeloPoruke) { t.Sastajaliste = t.Za })), ErrDrugoSastajaliste},
		"prestar":             {Potpisi(p.a, "gocop", VrstaPoziv, s(p.tijelo(), func(t *TijeloPoruke) { t.Izdano -= 601 })), ErrVrijemePoruke},
		"iz budućnosti":       {Potpisi(p.a, "gocop", VrstaPoziv, s(p.tijelo(), func(t *TijeloPoruke) { t.Izdano += 601 })), ErrVrijemePoruke},
		"kriva sesija":        {Potpisi(p.a, "gocop", VrstaPoziv, s(p.tijelo(), func(t *TijeloPoruke) { t.Sesija = "abc" })), ErrOblikPoruke},
		"poziv s pokušajem":   {Potpisi(p.a, "gocop", VrstaPoziv, s(p.tijelo(), func(t *TijeloPoruke) { t.Pokusaj = 1 })), ErrOblikPoruke},
		"sinkro bez pokušaja": {Potpisi(p.a, "gocop", VrstaSinkro, p.tijelo()), ErrOblikPoruke},
		"četvrti pokušaj":     {Potpisi(p.a, "gocop", VrstaSinkro, s(p.tijelo(), func(t *TijeloPoruke) { t.Pokusaj = 4 })), ErrOblikPoruke},
		"nije JSON":           {PotpisanaPoruka{Vrsta: VrstaPoziv, Tijelo: []byte("{"), Potpis: []byte("x")}, ErrOblikPoruke},
		"krivi ključ od":      {PotpisanaPoruka{Vrsta: VrstaPoziv, Tijelo: []byte(`{"od":"nije ključ"}`)}, ErrNepoznatPosiljatelj},
	} {
		if _, err := p.pp.Provjeri(c.poruka); !errors.Is(err, c.greska) {
			t.Errorf("%s: %v, a treba %v", ime, err, c.greska)
		}
	}
	// rub prozora: 10 min prolazi
	if _, err := p.pp.Provjeri(Potpisi(p.a, "gocop", VrstaPoziv, s(p.tijelo(), func(t *TijeloPoruke) { t.Izdano -= 600 }))); err != nil {
		t.Errorf("točno 10 min: %v", err)
	}
	// Potpisi sam postavi vrijeme kad ga nema
	bez := p.tijelo()
	bez.Izdano = 0
	if z, err := Potpisi(p.a, "gocop", VrstaPoziv, bez).Zaglavlje(); err != nil || z.Izdano == 0 {
		t.Errorf("vrijeme nije postavljeno: %v", err)
	}
}

// s mijenja tijelo za slučaj testa
func s(t TijeloPoruke, f func(*TijeloPoruke)) TijeloPoruke {
	f(&t)
	return t
}

// pregazi daje poruci drugu vrstu bez novog potpisa
func pregazi(p PotpisanaPoruka, vrsta string) PotpisanaPoruka {
	p.Vrsta = vrsta
	return p
}

// Pričuva puna odbija nove dok joj ne isteknu stari; poruke kojima je
// prošlo vrijeme odlaze tek kad je puna. Poruka na rubu prozora ostaje u
// pričuvi dok je još valjana.
func TestPricuvaPoruka(t *testing.T) {
	c := NovaPricuvaPoruka()
	sad := time.Unix(1_790_990_000, 0)
	for i := range najvisePoruka {
		if err := c.Zapamti(VrstaPoziv, TijeloPoruke{Od: "a", Sesija: string(rune(i)) + "-" + strings.Repeat("x", i%7), Izdano: sad.Unix(), Pokusaj: i}, sad); err != nil {
			t.Fatalf("%d. poruka: %v", i, err)
		}
	}
	if err := c.Zapamti(VrstaPoziv, TijeloPoruke{Od: "a", Sesija: "nova", Izdano: sad.Unix()}, sad); !errors.Is(err, ErrPricuvaPuna) {
		t.Errorf("puna pričuva: %v", err)
	}
	if err := c.Zapamti(VrstaPoziv, TijeloPoruke{Od: "a", Sesija: "nova", Izdano: sad.Unix()}, sad.Add(10*time.Minute)); !errors.Is(err, ErrPricuvaPuna) {
		t.Errorf("stare još vrijede na rubu prozora: %v", err)
	}
	kasnije := sad.Add(odstupanjePoruke + 2*time.Minute)
	if err := c.Zapamti(VrstaPoziv, TijeloPoruke{Od: "a", Sesija: "nova", Izdano: kasnije.Unix()}, kasnije); err != nil {
		t.Errorf("nakon isteka: %v", err)
	}
	if n := len(c.vidjeno); n != 1 {
		t.Errorf("u pričuvi ostalo %d", n)
	}
}
