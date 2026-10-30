package web

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/uvoz/izvori"
)

// Kad se mapa sliva ne da pročitati, ne zna se u kojem slivu letva stoji, pa
// se uvoz odbija na svim vratima: pregled posebnog izvora, potvrda, HydroView
// i pregled niza. Kad se mapa opet da pročitati, ista potvrda prolazi.
func TestUvozOdbijenKadSeStabloNeDaProcitati(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("administrator sustava čita mapu i bez prava")
	}
	o, _, _ := okolinaUpisa(t)
	pegel := izvori.Datoteka{Ime: "primjerovo.json", Sadrzaj: []byte(pegelPrimjerova)}
	id := o.cekaPegel(t, o.pero, "probni", pegel)
	sliv := filepath.Join(o.koren, "primjerica")
	if err := os.Chmod(sliv, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(sliv, 0o755) // da TempDir može počistiti i kad test padne prije vraćanja prava
	})

	poruka := "stablo s podacima ne da se pročitati"
	hv := url.Values{"letva": {"primjerovo"}, "sliv": {"drugica"}}
	for opis, html := range map[string]string{
		"pregled izvora": htmlUTekst(odgovor(o.h.PregledIzvora, viseDijelni(t, "/administracija/uvoz-izvora/pregled",
			map[string]string{"format": "his2000", "letva": "primjerovo", "sliv": "drugica"},
			datotekaObrasca{"datoteke", "vodostaj.csv", dnevniPrimjerovaHIS}), o.pero, o.peroOvl).Body.String()),
		"potvrda izvora": htmlUTekst(odgovor(o.h.UpisiIzvor, obicniObrazac("/administracija/uvoz-izvora/upisi", "id="+id),
			o.pero, o.peroOvl).Body.String()),
		"HydroView": htmlUTekst(odgovor(o.h.PreuzmiHidroView, obicniObrazac("/administracija/uvoz-izvora/hidroview",
			hv.Encode()), o.pero, o.peroOvl).Body.String()),
		"pregled niza": htmlUTekst(odgovor(o.h.PregledUvoza, viseDijelni(t, "/administracija/uvoz-niza/pregled", nil,
			datotekaObrasca{"datoteka", "primjerovo_probni_vodostaj_dnevni.csv", dnevniPrimjerova}), o.pero, o.peroOvl).Body.String()),
	} {
		if !strings.Contains(html, poruka) || rePosaoNaStranici.MatchString(html) || reIdObrasca.MatchString(html) {
			t.Errorf("%s nije odbijen: %s", opis, porukeStranice(html))
		}
	}
	if err := os.Chmod(sliv, 0o755); err != nil {
		t.Fatal(err)
	}
	if d := o.datoteke(t); len(d) != 0 {
		t.Fatalf("odbijen uvoz zapisao je %v", d)
	}

	w := odgovor(o.h.UpisiIzvor, obicniObrazac("/administracija/uvoz-izvora/upisi", "id="+id), o.pero, o.peroOvl)
	if p := posaoSaStranice(t, o.h.poslovi, w.Body.String(), o.pero); p.Greska() != "" {
		t.Fatalf("potvrda nakon popravka: %q", p.Greska())
	}
	if d := o.datoteke(t); len(d) != 1 || !strings.HasPrefix(d[0], filepath.Join(o.koren, "primjerica", "primjerovo")) {
		t.Errorf("zapisano %v", d)
	}
}

// Potvrda ponovno gleda stablo: letva koja je bila nova pri pregledu, a
// prije potvrde se pojavila u stablu, upisuje se u svoj sliv, a ne u onaj iz
// obrasca. Letva koja je i dalje nova ide u sliv iz obrasca.
func TestPotvrdaUzimaSlivIzStabla(t *testing.T) {
	o, _, _ := okolinaUpisa(t)
	pegel := izvori.Datoteka{Ime: "novovo.json", Sadrzaj: []byte(pegelPrimjerova)}
	ceka := func(letva string) string {
		id := novIdUvoza()
		uvoziIzvora.spremi(id, cekaIzvor{format: "pegelonline", zadatak: izvori.Zadatak{Koren: o.koren, Sliv: "novica",
			Letva: letva, Izvor: "probni"}, datoteke: []izvori.Datoteka{pegel}, korisnik: o.pero.ID.String()})
		t.Cleanup(func() { uvoziIzvora.makni(id) })
		return id
	}
	uStablu, nova := ceka("novovo"), ceka("trecovo")
	if err := os.MkdirAll(filepath.Join(o.koren, "primjerica", "novovo"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{uStablu, nova} {
		w := odgovor(o.h.UpisiIzvor, obicniObrazac("/administracija/uvoz-izvora/upisi", "id="+id), o.pero, o.peroOvl)
		if p := posaoSaStranice(t, o.h.poslovi, w.Body.String(), o.pero); p.Greska() != "" {
			t.Fatalf("potvrda: %q", p.Greska())
		}
	}
	for _, mapa := range []string{filepath.Join("primjerica", "novovo"), filepath.Join("novica", "trecovo")} {
		puts, err := filepath.Glob(filepath.Join(o.koren, mapa, "*_probni_vodostaj_satni_*.csv"))
		if err != nil || len(puts) != 1 {
			t.Errorf("u %s: %v, %v (sve: %v)", mapa, puts, err, o.datoteke(t))
		}
	}
	if _, err := os.Stat(filepath.Join(o.koren, "novica", "novovo")); err == nil {
		t.Error("letva iz stabla upisana je u sliv iz obrasca")
	}
}
