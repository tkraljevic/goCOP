package peers

// Zapis odbijenih i djelomično razumljenih poruka razmjene (G7,
// odbijeno.go): zbroj po danu, čišćenje starijeg od 30 dana, vrata, ishod
// razgovora, novosti iz knjige i kvarovi za pločicu.

import (
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

func zapisOdbijenog(t *testing.T, s *Service) []ZapisOdbijenog {
	t.Helper()
	z, err := s.Odbijeno(ctxRub, 7)
	if err != nil {
		t.Fatal(err)
	}
	return z
}

// Isti događaj u istom danu zbraja se u jedan redak; drugo računalo je
// drugi redak, a redak stariji od 30 dana briše se jednom na dan.
func TestZapisOdbijenogZbrajaICisti(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	staro := func(razlog string) {
		t.Helper()
		if _, err := s.db.Exec(`INSERT INTO razmjena_odbijeno (dan, vrsta, put, razlog, racunalo, broj, zadnje) VALUES (?, 'vrata', 'port', ?, '', 1, ?)`,
			time.Now().UTC().AddDate(0, 0, -40).Format(time.DateOnly), razlog, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	staro("prvo")
	for range 3 {
		s.zabiljeziOdbijeno(ctxRub, VrstaVrata, PutVrataPort, "zauzeto", "laptop-baranja")
	}
	staro("drugo")
	s.zabiljeziOdbijeno(ctxRub, VrstaVrata, PutVrataPort, "zauzeto", "ured-vukovar")
	z := zapisOdbijenog(t, s)
	if len(z) != 2 || z[0].Broj != 3 || z[0].Racunalo != "laptop-baranja" || z[1].Broj != 1 {
		t.Fatalf("zapis %+v", z)
	}
	var starih []string
	rows, err := s.db.Query(`SELECT razlog FROM razmjena_odbijeno WHERE dan < date('now', '-30 day') ORDER BY razlog`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			t.Fatal(err)
		}
		starih = append(starih, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(starih, ",") != "drugo" {
		t.Errorf("stari retci %v: prvi je obrisan prvim upisom dana, drugi čeka sutra", starih)
	}
}

// Zapis koji se ne da upisati ili očistiti ide samo u dnevnik; zapis koji
// se ne da pročitati stranica i pločica javljaju.
func TestZapisOdbijenogKadBazaNeRadi(t *testing.T) {
	dnevnik := dnevnikTesta(t)
	s := rubniCvor(t, "cop-osijek")
	if _, err := s.db.Exec(`INSERT INTO razmjena_odbijeno (dan, vrsta, put, razlog, racunalo, broj, zadnje) VALUES ('2026-01-02', 'vrata', 'port', 'zauzeto', '', 1, ?)`, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	kvarUpisa(t, s, "DELETE", "razmjena_odbijeno", "")
	s.zabiljeziOdbijeno(ctxRub, VrstaVrata, PutVrataPort, "zauzeto", "")
	if !strings.Contains(dnevnik.String(), "stari zapis odbijenog nije obrisan") {
		t.Errorf("dnevnik bez greške čišćenja:\n%s", dnevnik)
	}
	if _, err := s.db.Exec(`UPDATE razmjena_odbijeno SET broj = 'mnogo'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Odbijeno(ctxRub, 7); err == nil {
		t.Error("pokvaren redak pročitan bez greške")
	}
	pokvariTablicu(t, s, "razmjena_odbijeno")
	s.zabiljeziOdbijeno(ctxRub, VrstaVrata, PutVrataPort, "zauzeto", "")
	if !strings.Contains(dnevnik.String(), "zapis odbijenog nije spremljen") {
		t.Errorf("dnevnik bez greške upisa:\n%s", dnevnik)
	}
	if k := s.KvaroviRazmjene(ctxRub); len(k) != 1 || !strings.Contains(k[0], "ne da pročitati") {
		t.Errorf("kvarovi bez tablice: %v", k)
	}
}

// Vrata zapišu razlog i poznato računalo s tim ključem; nepoznat ključ i
// veza bez ključa ostaju bez imena. Odbijanje s ovog računala ide pod
// odbili-smo.
func TestOdbijenoNaVratimaIOdbiliSmo(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	poznat, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePeer(ctxRub, Peer{NodeID: "laptop-baranja", Name: "Laptop Baranja", PublicKey: razmjena.PublicKeyString(poznat)}); err != nil {
		t.Fatal(err)
	}
	drugi, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	s.naVratima(PutVrataTunel)(razmjena.VrataNijeClan, poznat)
	s.naVratima(PutVrataPort)(razmjena.VrataNijeClan, drugi)
	s.naVratima(PutVrataPort)(razmjena.VrataRukovanje, nil)
	s.odbiliSmo(RazlogBezTailscalea, poznat)
	got := map[string]bool{}
	for _, z := range zapisOdbijenog(t, s) {
		got[z.Vrsta+"|"+z.Put+"|"+z.Razlog+"|"+z.Racunalo] = true
	}
	for _, want := range []string{"vrata|tunel|nije-clan|laptop-baranja", "vrata|port|nije-clan|", "vrata|port|rukovanje|", "odbili-smo||bez-tailscalea|laptop-baranja"} {
		if !got[want] {
			t.Errorf("nema retka %s u %v", want, got)
		}
	}
}

// Odbijenica druge strane zapiše se po kodu, a nepoznat kod kao broj;
// oznake mogućnosti koje ovaj program ne zna idu pod djelomično razumljeno.
func TestOdbijenoIzIshodaRazgovora(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	p := &Peer{NodeID: "ured-vukovar"}
	for _, kod := range []razmjena.KodRazloga{razmjena.KodZauzet, razmjena.KodNijeClan, razmjena.KodUpisZauzet, razmjena.KodBezTailscalea, 0x0999} {
		s.zabiljeziIshod(ctxRub, p, syncOutcome{err: odbijenaRazmjena{razlog: "odbijeno", kod: kod}})
	}
	s.zabiljeziIshod(ctxRub, p, syncOutcome{err: errors.New("veza je pukla"), znanje: razmjena.Znanje{Mogucnosti: []string{razmjena.OznakaRazmjena, "stranice/2"}}})
	got := map[string]bool{}
	for _, z := range zapisOdbijenog(t, s) {
		got[z.Vrsta+"|"+z.Razlog] = z.Racunalo == "ured-vukovar"
	}
	for _, want := range []string{"odbili-su|zauzeto", "odbili-su|nije-clan", "odbili-su|upis-zauzet", "odbili-su|bez-tailscalea", "odbili-su|kod 0x0999", "djelomicno|oznaka stranice/2"} {
		if !got[want] {
			t.Errorf("nema retka %s za ured-vukovar u %v", want, got)
		}
	}
	if len(got) != 6 {
		t.Errorf("zapisano %v, a pukla veza nije odbijenica", got)
	}
}

// Zapis koji program ne razumije ide u dnevnik i pod djelomično razumljeno.
func TestNovostiUZapisuOdbijenog(t *testing.T) {
	dnevnik := dnevnikTesta(t)
	s := rubniCvor(t, "cop-osijek")
	s.PratiNovosti(s.rec)
	s.novost(ledger.Novost{Entity: "ustave", Nepoznat: true, Verzija: 3})
	s.novost(ledger.Novost{Entity: "letve", Shema: 4, Lokalna: 3, Verzija: 1})
	got := map[string]bool{}
	for _, z := range zapisOdbijenog(t, s) {
		got[z.Vrsta+"|"+z.Razlog] = true
	}
	if !got["djelomicno|nepoznat-zapis ustave"] || !got["djelomicno|novija-shema letve"] || strings.Count(dnevnik.String(), "nadogradite goCOP") != 2 {
		t.Errorf("zapis %v, dnevnik:\n%s", got, dnevnik)
	}
}

// Na pločicu ide poznato računalo koje u zadnja 24 sata zove, a ovdje nije
// član, jednom i s imenom; starije, nepoznato i drugi razlozi ne idu.
func TestKvaroviRazmjene(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	if err := s.SavePeer(ctxRub, Peer{NodeID: "laptop-baranja", Name: "Laptop Baranja", PublicKey: javniKljuc(t)}); err != nil {
		t.Fatal(err)
	}
	upisi := func(put, razlog, racunalo string, zadnje time.Time) {
		t.Helper()
		if _, err := s.db.Exec(`INSERT INTO razmjena_odbijeno (dan, vrsta, put, razlog, racunalo, broj, zadnje) VALUES (?, 'vrata', ?, ?, ?, 2, ?)`,
			zadnje.Format(time.DateOnly), put, razlog, racunalo, zadnje); err != nil {
			t.Fatal(err)
		}
	}
	sada := time.Now().UTC()
	upisi(PutVrataPort, "nije-clan", "laptop-baranja", sada)
	upisi(PutVrataTunel, "nije-clan", "laptop-baranja", sada.Add(-time.Hour))
	upisi(PutVrataPort, "nije-clan", "ured-vukovar", sada)
	upisi(PutVrataPort, "nije-clan", "", sada)
	upisi(PutVrataPort, "zauzeto", "windows-osijek", sada)
	upisi(PutVrataPort, "nije-clan", "ured-vinkovci", sada.Add(-25*time.Hour))
	k := s.KvaroviRazmjene(ctxRub)
	if len(k) != 2 || !strings.HasPrefix(k[0], "Laptop Baranja zove ovo računalo") || !strings.HasPrefix(k[1], "ured-vukovar zove") {
		t.Errorf("kvarovi %q", k)
	}
}
