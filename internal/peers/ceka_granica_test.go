package peers

// Verzija koja čeka (potpis_provjera.go: errKljucNijeVezan,
// errNepotpisanaOdPotpisnika) ne ulazi u knjigu i ne javlja se kao
// odbijena. Granica knjige je najveći version_id po autoru i kanalu, a
// pošiljatelj šalje samo iznad nje. Kad s verzijom koja čeka stigne i
// novija verzija istog autora u istom kanalu koja prolazi, granica prijeđe
// preko verzije koja čeka, pa ona sama više ne bi stigla. Zato ide u trazi
// zapisa rupa i traži se popunom. Testovi tvrde da ta verzija stigne kad
// prestane čekati.

import (
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// zadnjaVerzija je zadnja verzija zapisa na čvoru
func zadnjaVerzija(t *testing.T, s *Service, entitet, zapis string) ledger.Version {
	t.Helper()
	v, err := s.rec.Latest(ctxRub, entitet, zapis)
	if err != nil {
		t.Fatalf("%s: %s/%s: %v", s.node.ID, entitet, zapis, err)
	}
	return *v
}

// clanoviBez su čvorovi iste mreže kao kod clanoviMreze (ne slušaju), samo
// što čvor bez ne zna članstvo čvora nepoznat: ni u tablici ni u knjizi.
// Tako izgleda računalo do kojeg članstvo novog člana još nije stiglo.
func clanoviBez(t *testing.T, bez, nepoznat int, ids ...string) []*Service {
	t.Helper()
	osnivac := rubnaMreza(t, "osnivac-mreze")
	var out []*Service
	var clanstva []razmjena.Membership
	for _, id := range ids {
		s := rubniCvor(t, id)
		clanstva = append(clanstva, primiIzravno(t, osnivac, s))
		out = append(out, s)
	}
	for i, a := range out {
		for j, b := range out {
			if i == j {
				continue
			}
			if i != bez || j != nepoznat {
				if err := a.saveMembership(ctxRub, clanstva[j]); err != nil {
					t.Fatal(err)
				}
			}
			upoznaj(t, a, b, adresaNaPetlji(b))
		}
	}
	return out
}

// 1) Nepotpisana verzija prava od autora koji već potpisuje, stigla od
// posrednika s potpis-verzije/1, čeka. U istoj poruci stiže novija verzija
// istog autora u istom kanalu (globalni kanal prava), potpisana, i prolazi.
// Ista verzija poslije mora moći stići: potpisana od autora ili nepotpisana
// od računala bez oznake potpisa.
//
// Put: autor (potpisuje) → stari posrednik (bez oznake, skida potpis) →
// novi posrednik (s oznakom; verziju drži bez potpisa) → laptop (strogo za
// prava). Druga verzija ide izravno od autora do novog posrednika.
//
// Kontrola (bez novije verzije): ista prva verzija bez druge stigne, pa
// ono što ruši glavni slučaj je samo granica.
func TestCekaGranicaNepotpisanaOdPotpisnika(t *testing.T) {
	t.Run("kontrola bez novije verzije", func(t *testing.T) { nepotpisanaOdPotpisnika(t, false) })
	t.Run("s novijom verzijom istog autora", func(t *testing.T) { nepotpisanaOdPotpisnika(t, true) })
}

func nepotpisanaOdPotpisnika(t *testing.T, sNovijom bool) {
	cv := clanoviMreze(t, false, "cop-osijek", "stari-posrednik", "posrednik", "laptop-baranja")
	a, o, p, c := cv[0], cv[1], cv[2], cv[3]
	for _, s := range cv {
		s.SetWantsAll(true)
	}
	o.mogucnosti = bezOznakePotpisa
	c.PostaviStrogePotpise("users")
	a.rec.PostaviPotpisnika(a.node.PotpisnikVerzija(Protocol))

	// laptop zna da autor potpisuje: u knjizi ima njegovu potpisanu verziju
	upisiOcitanja(t, a, "prijave/16/2026", "p0")
	razgovaraj(t, c, a)
	if u := redakKnjige(t, c, zadnjaVerzija(t, a, "probni", "p0").VersionID); u == nil || u.Potpis == "" {
		t.Fatalf("laptop nema potpisanu verziju autora: %+v", u)
	}

	// prva verzija prava: kroz starog posrednika stiže do novog bez potpisa
	if _, err := a.rec.Record(ctxRub, a.db, "users", "u1", map[string]string{"id": "u1"}); err != nil {
		t.Fatal(err)
	}
	prva := zadnjaVerzija(t, a, "users", "u1")
	razgovaraj(t, o, a)
	razgovaraj(t, p, o)
	if u := redakKnjige(t, p, prva.VersionID); u == nil || u.Potpis != "" {
		t.Fatalf("posrednik: prva verzija %+v", u)
	}

	// druga, novija verzija istog autora u istom kanalu, izravno i potpisana
	var druga ledger.Version
	if sNovijom {
		if _, err := a.rec.Record(ctxRub, a.db, "users", "u2", map[string]string{"id": "u2"}); err != nil {
			t.Fatal(err)
		}
		druga = zadnjaVerzija(t, a, "users", "u2")
		if druga.Channel != prva.Channel || druga.VersionID <= prva.VersionID {
			t.Fatalf("druga verzija nije iznad prve u istom kanalu: %+v / %+v", prva, druga)
		}
		razgovaraj(t, p, a)
	}

	// laptop od novog posrednika: prva čeka, druga prolazi
	cekaju := brojiDnevnik(t, "a upisat će se kad stigne članstvo autora ili potpisana inačica")
	razgovaraj(t, c, p)
	if n, _ := cekaju.broj(); n == 0 {
		t.Error("prva verzija nije zabilježena kao verzija koja čeka")
	}
	if redakKnjige(t, c, prva.VersionID) != nil {
		t.Fatal("nepotpisana verzija prava od autora koji potpisuje ušla je u knjigu (postava testa ne vrijedi)")
	}
	if sNovijom && redakKnjige(t, c, druga.VersionID) == nil {
		t.Fatal("druga verzija nije stigla (postava testa ne vrijedi)")
	}
	k := ledger.FrontierKey(a.node.ID, prva.Channel)

	// prva verzija mora moći stići: potpisana od autora, nepotpisana od
	// računala bez oznake potpisa, ili od novog posrednika
	razgovaraj(t, c, a)
	razgovaraj(t, c, o)
	razgovaraj(t, c, p)
	razgovaraj(t, c, a)
	if redakKnjige(t, c, prva.VersionID) == nil {
		t.Errorf("verzija koja je čekala nije stigla ni od autora (potpisana) ni od računala bez oznake potpisa: granica %s prešla je preko nje", k)
	}
}

// 2) Potpisana verzija ključem koji kod primatelja još nije vezan (članstvo
// novog člana još nije stiglo) čeka. U istom primanju stiže novija verzija
// istog autora u istom kanalu koja prolazi. Zatim stigne članstvo, pa
// verzija koja je čekala mora stići.
//
// Druga verzija prolazi samo ako je bez potpisa (primatelj autora ne zna
// kao potpisnika), pa ide kroz starog posrednika. Čekanje na članstvo
// razmjenom samom ne nastaje: računalo s oznakom potpisa koje šalje
// potpisanu verziju ima ključ vezan, dakle članstvo u knjizi, a ono ide u
// istu deltu (clanstvaIzPoruke). Zato prva verzija ovdje stiže COP paketom
// kanala, koji članstva ne nosi; članstvo i sve poslije ide pravom
// razmjenom.
//
// Kontrola (bez novije verzije): prva verzija bez druge stigne čim stigne
// članstvo, pa ono što ruši glavni slučaj je samo granica.
func TestCekaGranicaKljucNijeVezan(t *testing.T) {
	t.Run("kontrola bez novije verzije", func(t *testing.T) { kljucNijeVezan(t, false) })
	t.Run("s novijom verzijom istog autora", func(t *testing.T) { kljucNijeVezan(t, true) })
}

func kljucNijeVezan(t *testing.T, sNovijom bool) {
	// laptop (3) ne zna članstvo novog člana (0)
	cv := clanoviBez(t, 3, 0, "novi-clan", "stari-posrednik", "posrednik", "laptop-baranja")
	x, o, p, c := cv[0], cv[1], cv[2], cv[3]
	for _, s := range cv {
		s.SetWantsAll(true)
	}
	o.mogucnosti = bezOznakePotpisa
	x.rec.PostaviPotpisnika(x.node.PotpisnikVerzija(Protocol))
	kanal := "prijave/16/2026"

	// prva verzija: posrednik je dobije potpisanu, izravno od autora
	upisiOcitanja(t, x, kanal, "p1")
	prva := zadnjaVerzija(t, x, "probni", "p1")
	razgovaraj(t, p, x)
	if u := redakKnjige(t, p, prva.VersionID); u == nil || u.Potpis == "" {
		t.Fatalf("posrednik: prva verzija %+v", u)
	}
	// druga: stari posrednik je dobije od autora, a novi od njega, bez potpisa
	var druga ledger.Version
	if sNovijom {
		upisiOcitanja(t, x, kanal, "p2")
		druga = zadnjaVerzija(t, x, "probni", "p2")
		if druga.VersionID <= prva.VersionID {
			t.Fatal("druga verzija nije iznad prve")
		}
		razgovaraj(t, o, x)
		razgovaraj(t, p, o)
		if u := redakKnjige(t, p, druga.VersionID); u == nil || u.Potpis != "" {
			t.Fatalf("posrednik: druga verzija %+v", u)
		}
	}

	// laptop uveze paket kanala od posrednika: prva čeka članstvo, druga prolazi
	cekaju := brojiDnevnik(t, "a upisat će se kad stigne članstvo autora ili potpisana inačica")
	_, put := izvezi(t, p, CopObuhvat{Vrsta: "prijave", AreaID: 16, Od: 2026, Do: 2026}, []string{kanal}, false)
	if _, err := c.UveziCop(ctxRub, put); err != nil {
		t.Fatal(err)
	}
	if n, _ := cekaju.broj(); n == 0 {
		t.Error("prva verzija nije zabilježena kao verzija koja čeka")
	}
	if redakKnjige(t, c, prva.VersionID) != nil {
		t.Fatal("verzija s ključem koji nije vezan ušla je u knjigu (postava testa ne vrijedi)")
	}
	if sNovijom && redakKnjige(t, c, druga.VersionID) == nil {
		t.Fatal("druga verzija nije stigla (postava testa ne vrijedi)")
	}
	// rupa samo uz noviju verziju: bez nje ključ nema granicu, pa ga druga
	// strana šalje od početka i verzija stigne sama (zaCekanje)
	k := ledger.FrontierKey(x.node.ID, kanal)
	if trazi(t, c, k, prva.VersionID) != sNovijom {
		t.Errorf("uvoz paketa (novija %v): rupa za verziju koja čeka %+v", sNovijom, rupeCvora(t, c)[k])
	}

	// članstvo stigne razmjenom s posrednikom, a uz njega i verzija koja je
	// čekala (sNovijom: popunom); zatim razgovor s autorom
	razgovaraj(t, c, p)
	if redakKnjige(t, c, prva.VersionID) == nil {
		t.Error("razmjena s posrednikom nije donijela verziju koja je čekala")
	}
	if trazi(t, c, k, prva.VersionID) {
		t.Errorf("laptop i dalje traži verziju koja je stigla: %+v", rupeCvora(t, c)[k])
	}
	if m, err := c.ucitajClanstva(ctxRub); err != nil || len(m.potpisanaImena(x.node.ID)) == 0 {
		t.Fatalf("članstvo novog člana nije stiglo: %v", err)
	}
	razgovaraj(t, c, x)
	razgovaraj(t, c, p)
	if redakKnjige(t, c, prva.VersionID) == nil {
		g, err := c.rec.Frontier(ctxRub)
		if err != nil {
			t.Fatal(err)
		}
		t.Errorf("verzija koja je čekala članstvo nije stigla ni kad je članstvo stiglo: granica %s = %s, prva %s", k, g[k], prva.VersionID)
	}
}
