package peers

// Šifrirana kopija ključa mreže (G9b, kljuc_kopija.go): sprema je samo
// računalo s ključem, s lozinkom od barem 12 znakova; drugo računalo iste
// mreže je učita samo s pravom lozinkom i neizmijenjenu, i tada prima u
// mrežu. Pločica računala s ključem traži kopiju i dva primatelja.

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const lozinkaKopije = "zelena-letva-na-dravi"

// brziScrypt: izvod ključa u testu ne treba trajati
func brziScrypt(t *testing.T) {
	t.Helper()
	staro := scryptN
	scryptN = 1 << 10
	t.Cleanup(func() { scryptN = staro })
}

// izmijeni vrati kopiju raw s promjenom f
func izmijeni(t *testing.T, raw []byte, f func(*kopijaKljuca)) []byte {
	t.Helper()
	var k kopijaKljuca
	if err := json.Unmarshal(raw, &k); err != nil {
		t.Fatal(err)
	}
	f(&k)
	b, err := json.Marshal(k)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestKopijaKljucaMreze(t *testing.T) {
	brziScrypt(t)
	a := rubnaMreza(t, "cop-osijek")
	b, x := rubniCvor(t, "server-ured"), rubniCvor(t, "laptop-ilok")
	primateljUMrezi(t, a, b)
	if _, _, err := b.KopijaKljucaMreze(ctxRub, lozinkaKopije); err == nil {
		t.Error("računalo bez ključa sprema kopiju")
	}
	if _, _, err := a.KopijaKljucaMreze(ctxRub, "kratka"); err == nil || !strings.Contains(err.Error(), "12 znakova") {
		t.Errorf("kratka lozinka: %v", err)
	}
	if a.KopijaKljucaKad(ctxRub) != nil {
		t.Error("kopija zapamćena prije spremanja")
	}
	raw, ime, err := a.KopijaKljucaMreze(ctxRub, lozinkaKopije)
	if err != nil || ime != "gocop-kljuc-mreze-hrvatske-vode.json" || a.KopijaKljucaKad(ctxRub) == nil || strings.Contains(string(raw), base64.StdEncoding.EncodeToString(a.network.Private().Seed())) {
		t.Fatalf("kopija %s %v", ime, err)
	}

	drugaMreza := rubnaMreza(t, "cop-split")
	tudja, _, err := drugaMreza.KopijaKljucaMreze(ctxRub, lozinkaKopije)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		ime     string
		raw     []byte
		lozinka string
		ima     string
	}{
		{"kriva lozinka", raw, "kriva-lozinka-kopije", ErrLozinkaKopije.Error()},
		{"nije kopija", []byte(`{"v":"nesto"}`), lozinkaKopije, "nije kopija"},
		{"druga mreža", tudja, lozinkaKopije, "druge mreže"},
		{"mijenjan naziv", izmijeni(t, raw, func(k *kopijaKljuca) { k.Mreza = "Lažna mreža" }), lozinkaKopije, ErrLozinkaKopije.Error()},
		{"skupa cijena", izmijeni(t, raw, func(k *kopijaKljuca) { k.N = 1 << 30 }), lozinkaKopije, ErrLozinkaKopije.Error()},
		{"cijena nije potencija", izmijeni(t, raw, func(k *kopijaKljuca) { k.N = 3 }), lozinkaKopije, ErrLozinkaKopije.Error()},
		{"kriva sol", izmijeni(t, raw, func(k *kopijaKljuca) { k.Sol = "nije base64!" }), lozinkaKopije, ErrLozinkaKopije.Error()},
		{"kratak nonce", izmijeni(t, raw, func(k *kopijaKljuca) { k.Nonce = "AAAA" }), lozinkaKopije, ErrLozinkaKopije.Error()},
		{"drugo sjeme", sifriraj(t, raw, make([]byte, 32)), lozinkaKopije, ErrLozinkaKopije.Error()},
		{"kratko sjeme", sifriraj(t, raw, make([]byte, 10)), lozinkaKopije, ErrLozinkaKopije.Error()},
	} {
		if err := b.UcitajKopijuKljuca(ctxRub, c.raw, c.lozinka); err == nil || !strings.Contains(err.Error(), c.ima) {
			t.Errorf("%s: %v", c.ime, err)
		}
	}
	if b.NetworkInfo().DrziKljuc {
		t.Fatal("ključ učitan iz krive kopije")
	}

	prije := verzijaZapisa(t, b, EntityOvlasti, b.node.ID)
	if err := b.UcitajKopijuKljuca(ctxRub, raw, lozinkaKopije); err != nil {
		t.Fatal(err)
	}
	if !b.NetworkInfo().DrziKljuc {
		t.Fatal("nakon učitavanja računalo ne drži ključ")
	}
	if b.KopijaKljucaKad(ctxRub) == nil || strings.Contains(strings.Join(b.upozorenjaKljucaMreze(ctxRub), "\n"), "Kopija ključa mreže još nije spremljena") {
		t.Error("računalo koje je učitalo kopiju javlja da kopije nema")
	}
	if n := verzijaZapisa(t, b, EntityOvlasti, b.node.ID); n != prije+1 {
		t.Errorf("vlastita ovlast nije obnovljena pri učitavanju: %d → %d verzija", prije, n)
	}
	if _, err := os.Stat(b.networkKeyPath()); err != nil {
		t.Errorf("ključ nije zapisan uz bazu: %v", err)
	}
	m, err := b.izdajClanstvo(ctxRub, x.node.ID, x.javni())
	if err != nil || m.Primatelj != nil || m.Verify(a.network.Public, x.javni(), m.IssuedAt) != nil {
		t.Errorf("računalo s učitanim ključem ne prima: %+v %v", m, err)
	}
	if err := b.UcitajKopijuKljuca(ctxRub, raw, lozinkaKopije); err == nil || !strings.Contains(err.Error(), "već drži") {
		t.Errorf("drugi put: %v", err)
	}
	if err := rubniCvor(t, "samotnjak").UcitajKopijuKljuca(ctxRub, raw, lozinkaKopije); err == nil || !strings.Contains(err.Error(), "nije ni u jednoj mreži") {
		t.Errorf("izvan mreže: %v", err)
	}
}

// sifriraj je kopija raw s pravom lozinkom, a drugim sadržajem
func sifriraj(t *testing.T, raw, sjeme []byte) []byte {
	t.Helper()
	return izmijeni(t, raw, func(k *kopijaKljuca) {
		sol, _ := base64.StdEncoding.DecodeString(k.Sol)     // sol je iz prave kopije, pa se da pročitati
		nonce, _ := base64.StdEncoding.DecodeString(k.Nonce) // nonce je iz prave kopije, pa se da pročitati
		aead, err := sifraKopije(lozinkaKopije, sol, k.N)
		if err != nil {
			t.Fatal(err)
		}
		k.Sifrirano = base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, sjeme, k.vezano()))
	})
}

// Ključ koji se ne da zapisati i kopija koja se ne da zapamtiti javljaju
// grešku, a računalo ostaje kakvo je bilo.
func TestKopijaKljucaKadDiskIBazaNeRade(t *testing.T) {
	brziScrypt(t)
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "server-ured")
	primiIzravno(t, a, b)
	raw, _, err := a.KopijaKljucaMreze(ctxRub, lozinkaKopije)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(b.networkKeyPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := b.UcitajKopijuKljuca(ctxRub, raw, lozinkaKopije); err == nil || !strings.Contains(err.Error(), "ne može zapisati") || b.NetworkInfo().DrziKljuc {
		t.Errorf("ključ bez mjesta na disku: %v", err)
	}
	if err := os.Remove(b.networkKeyPath()); err != nil {
		t.Fatal(err)
	}
	dnevnik := dnevnikTesta(t)
	kvarUpisa(t, b, "UPDATE", "network", "")
	if err := b.UcitajKopijuKljuca(ctxRub, raw, lozinkaKopije); err != nil || !strings.Contains(dnevnik.String(), "datum učitane kopije ključa nije zapamćen") {
		t.Errorf("učitavanje bez upisa datuma: %v\n%s", err, dnevnik)
	}
	scryptN = 3 // cijena koju scrypt ne prima
	if _, _, err := a.KopijaKljucaMreze(ctxRub, lozinkaKopije); err == nil {
		t.Error("kopija uz neispravnu cijenu izvoda")
	}
	scryptN = 1 << 10
	if rubniCvor(t, "samotnjak").KopijaKljucaKad(ctxRub) != nil {
		t.Error("kopija na računalu bez mreže")
	}
	kvarUpisa(t, a, "UPDATE", "network", "")
	if _, _, err := a.KopijaKljucaMreze(ctxRub, lozinkaKopije); err == nil || !strings.Contains(err.Error(), "nije zapamćena") {
		t.Errorf("kopija bez upisa: %v", err)
	}
}

func TestImeDatotekeKopije(t *testing.T) {
	for naziv, want := range map[string]string{"Hrvatske vode": "hrvatske-vode", "Probna mreža — Đakovo!": "probna-mreza-djakovo", "  ": ""} {
		if got := imeDatoteke(naziv); got != want {
			t.Errorf("%q: %q, želim %q", naziv, got, want)
		}
	}
}

// Računalo s ključem upozorava dok kopija nije spremljena i dok ovlast ne
// imaju barem dva računala; drugo računalo to ne vidi.
func TestUpozorenjaKljucaMreze(t *testing.T) {
	brziScrypt(t)
	a := rubnaMreza(t, "cop-osijek")
	p, q := rubniCvor(t, "server-ured"), rubniCvor(t, "server-split")
	pocetno, err := a.Status(ctxRub, false)
	if err != nil {
		t.Fatal(err)
	}
	u := strings.Join(pocetno.Alerts, "\n")
	if !strings.Contains(u, "Kopija ključa mreže još nije spremljena") || !strings.Contains(u, "Ovlast za primanje ima 0 računala") {
		t.Errorf("pločica svježe mreže:\n%s", u)
	}
	if _, _, err := a.KopijaKljucaMreze(ctxRub, lozinkaKopije); err != nil {
		t.Fatal(err)
	}
	primateljUMrezi(t, a, p)
	if u := a.upozorenjaKljucaMreze(ctxRub); len(u) != 1 || !strings.Contains(u[0], "ima 1 računalo") {
		t.Errorf("jedan primatelj: %q", u)
	}
	primateljUMrezi(t, a, q)
	if u := a.upozorenjaKljucaMreze(ctxRub); u != nil {
		t.Errorf("dva primatelja i kopija: %q", u)
	}
	if p.upozorenjaKljucaMreze(ctxRub) != nil {
		t.Error("računalo bez ključa vidi upozorenje ključa")
	}
	st, err := a.Status(ctxRub, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(st.Alerts, "\n"), "Kopija ključa mreže") {
		t.Errorf("pločica nakon kopije: %q", st.Alerts)
	}
	// primatelj u pričuvi više ne prima, pa se ne broji
	uMrezuSRokom(t, a, q, -2*dan)
	if b := a.brojPrimatelja(ctxRub); b != 1 {
		t.Errorf("primatelj u pričuvi se broji: %d", b)
	}
	pokvariTablicu(t, a, "memberships")
	if a.brojPrimatelja(ctxRub) != 0 {
		t.Error("primatelji bez tablice članstava")
	}
}

// Vlastita ovlast se ne izdaje na računalu bez mreže ni bez ključa mreže;
// neuspjela obnova ide u dnevnik.
func TestIzdajVlastituOvlastRubovi(t *testing.T) {
	dnevnik := dnevnikTesta(t)
	if err := rubniCvor(t, "samotnjak").izdajVlastituOvlast(ctxRub); err == nil {
		t.Error("vlastita ovlast izvan mreže")
	}
	a := rubnaMreza(t, "cop-osijek")
	p := rubniCvor(t, "server-ured")
	primateljUMrezi(t, a, p)
	p.obnoviVlastituOvlast(ctxRub, true)
	if !strings.Contains(dnevnik.String(), "vlastita ovlast za primanje nije obnovljena") {
		t.Errorf("dnevnik bez greške:\n%s", dnevnik)
	}
}
