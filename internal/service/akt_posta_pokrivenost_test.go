package service_test

import (
	"context"
	"encoding/base64"
	"errors"
	"mime"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Pošta iz programa: postavke poslužitelja, sandučić (mape, pisma,
// privitci, premještanje i brisanje), slanje pisma, slanje ovjerenog akta
// „na znanje”, prijedlozi adresa i zadani potpis. Poslužitelj je probni
// EWS na 127.0.0.1. Osobe su izmišljene: dežurna operaterka Ana (sektor B)
// i vodočuvar Ivo.

type okolinaSanducica struct {
	*okolinaOvlasti
	srv    *posta.ProbniPosluzitelj
	ana    *models.User
	anaOvl *models.UserPermissions
}

// novaOkolinaSanducica: probni EWS s Aninim računom voda.int\ana, postavke
// spremljene u programu i Anina lozinka sandučića spremljena i provjerena
func novaOkolinaSanducica(t *testing.T) *okolinaSanducica {
	t.Helper()
	ctx := context.Background()
	o := novaOkolinaOvlasti(t)
	srv, err := posta.PokreniProbniEWS(`voda.int\ana`, "Lozinka-Ane-1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Zatvori)
	pp := srv.Postavke()
	pp.Domena = "voda.int"
	o.akti.SetPosta(pp)
	if err := o.akti.SpremiPostu(ctx, o.admin, pp); err != nil {
		t.Fatal(err)
	}
	ana := o.osoba(t, "ana", service.AddDutyRequest{Role: models.RoleOperator, SectorID: sektorP("B")})
	if _, err := o.akti.SpremiRacunPoste(ctx, ana, "ana@voda.hr", "Lozinka-Ane-1"); err != nil {
		t.Fatal(err)
	}
	return &okolinaSanducica{okolinaOvlasti: o, srv: srv, ana: ana, anaOvl: o.ovlasti(t, ana.ID)}
}

// pismaSanducica stavlja u Anin inbox dva pisma, jedno s privitkom
func (o *okolinaSanducica) pismaSanducica() {
	kad := time.Date(2026, 10, 12, 9, 30, 0, 0, time.UTC)
	o.srv.Pisma = []posta.Pismo{
		{ID: "pismo-1", MessageID: "<p1@voda.hr>", Predmet: "Stanje nasipa", Od: "Ivo Ivić", OdAdresa: "ivo@voda.hr", Kad: kad,
			Tekst: "Nasip je pregledan.", Privitci: []posta.PrivitakPisma{{ID: "privitak-1", Ime: "nasip.pdf", Vrsta: "application/pdf", Velicina: 9}}},
		{ID: "pismo-2", MessageID: "<p2@voda.hr>", Predmet: "Dežurstvo", Od: "Petra Perić", OdAdresa: "petra@voda.hr", Kad: kad.Add(time.Hour),
			Tekst: "Raspored za studeni.", Procitano: true},
	}
	o.srv.Datoteke = map[string][]byte{"privitak-1": []byte("%PDF-1.7\n")}
	o.srv.Adresar = []posta.Kontakt{{Ime: "Marko Markić", Email: "marko.markic@voda.hr"}, {Ime: "Ana Anić", Email: "ana@voda.hr"}}
}

func mapaSanducica(t *testing.T, mape []posta.Mapa, id string) posta.Mapa {
	t.Helper()
	for _, m := range mape {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("nema mape %s u %+v", id, mape)
	return posta.Mapa{}
}

func TestPostavkePosteUProgramuIIzDatoteke(t *testing.T) {
	ctx := context.Background()
	o := novaOkolinaOvlasti(t)
	if o.akti.PostaSpremljena(ctx) || o.akti.PostaPodesena(ctx) || o.akti.PostaPosluzitelj(ctx) != "" || o.akti.PostaDomena(ctx) != "" {
		t.Fatal("bez postavki pošta nije podešena")
	}

	// zadane postavke iz datoteke vrijede dok u programu nema spremljenih
	o.akti.SetPosta(posta.Postavke{Nacin: posta.NacinEWS, Posluzitelj: "owa.datoteka.test", Domena: "DATOTEKA"})
	if o.akti.PostaSpremljena(ctx) || !o.akti.PostaPodesena(ctx) || o.akti.PostaPosluzitelj(ctx) != "owa.datoteka.test" ||
		o.akti.PostaDomena(ctx) != "DATOTEKA" || !o.akti.PostaSpremaPoslano(ctx) {
		t.Error("zadane postavke iz datoteke nisu primijenjene")
	}

	if err := o.akti.SpremiPostu(ctx, o.admin, posta.Postavke{Nacin: posta.NacinSMTP, Posluzitelj: " smtp.primjer.test ", Domena: " VODA ", Port: 70000}); err == nil ||
		!strings.Contains(err.Error(), "port") {
		t.Errorf("port izvan raspona: %v", err)
	}
	if err := o.akti.SpremiPostu(ctx, o.admin, posta.Postavke{Nacin: posta.NacinSMTP, Posluzitelj: " smtp.primjer.test ", Domena: " VODA ", Port: 587}); err != nil {
		t.Fatal(err)
	}
	if !o.akti.PostaSpremljena(ctx) || o.akti.PostaPosluzitelj(ctx) != "smtp.primjer.test" || o.akti.PostaDomena(ctx) != "VODA" {
		t.Errorf("spremljene postavke: %q, %q", o.akti.PostaPosluzitelj(ctx), o.akti.PostaDomena(ctx))
	}
	if o.akti.PostaSpremaPoslano(ctx) {
		t.Error("SMTP ne sprema poslano u sandučić")
	}
	if p := o.akti.Posta(ctx); p.Nacin != posta.NacinSMTP || p.Sigurnost != posta.STARTTLS || p.Port != 587 {
		t.Errorf("spremljeni način: %+v", p)
	}
}

func TestSanducicBezLozinkeIBezPoste(t *testing.T) {
	ctx := context.Background()
	o := novaOkolinaSanducica(t)
	ivo := o.osoba(t, "ivo", service.AddDutyRequest{Role: models.RoleWaterGuard, AreaID: podrucjeP(16)})

	radnje := map[string]func(u *models.User) error{
		"mape":            func(u *models.User) error { _, err := o.akti.MapeSanducica(ctx, u); return err },
		"premještanje":    func(u *models.User) error { return o.akti.PremjestiPisma(ctx, u, []string{"x"}, "deleteditems") },
		"trajno brisanje": func(u *models.User) error { return o.akti.ObrisiTrajno(ctx, u, []string{"x"}) },
		"pročitano više":  func(u *models.User) error { return o.akti.OznaciProcitanoVise(ctx, u, []posta.Stavka{{ID: "x"}}, true) },
		"pročitano":       func(u *models.User) error { return o.akti.OznaciProcitano(ctx, u, "x", "ck1", true) },
		"brisanje pisma":  func(u *models.User) error { return o.akti.ObrisiPismo(ctx, u, "x") },
		"otvaranje pisma": func(u *models.User) error { _, err := o.akti.Pismo(ctx, u, "x"); return err },
		"privitak":        func(u *models.User) error { _, _, err := o.akti.Privitak(ctx, u, "x"); return err },
		"adresar":         func(u *models.User) error { _, err := o.akti.Imenik(ctx, u, "ana"); return err },
		"slanje pisma": func(u *models.User) error {
			return o.akti.PosaljiPismo(ctx, u, service.NovoPismo{Za: "a@b.hr", Predmet: "P"})
		},
		"otključan račun": func(u *models.User) error { _, _, err := o.akti.RacunPosteOtkljucan(ctx, u); return err },
		"stranica pošte":  func(u *models.User) error { _, _, err := o.akti.Sanducic(ctx, u, "inbox", "", 1, 20); return err },
	}
	for naziv, radnja := range radnje {
		if err := radnja(ivo); !errors.Is(err, service.ErrNemaLozinkePoste) {
			t.Errorf("%s bez spremljene lozinke: %v", naziv, err)
		}
	}
	// bez prijavljene osobe sandučić se ne otvara
	if _, err := o.akti.MapeSanducica(ctx, nil); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("mape bez osobe: %v", err)
	}

	// obrisana lozinka više ne otvara sandučić
	if k, _ := o.akti.RacunPoste(ctx, o.ana.ID.String()); k == "" {
		t.Fatal("Anina lozinka nije spremljena")
	}
	if err := o.akti.ObrisiRacunPoste(ctx, o.ana); err != nil {
		t.Fatal(err)
	}
	if k, kad := o.akti.RacunPoste(ctx, o.ana.ID.String()); k != "" || !kad.IsZero() {
		t.Errorf("nakon brisanja: %q, %v", k, kad)
	}
	if _, err := o.akti.MapeSanducica(ctx, o.ana); !errors.Is(err, service.ErrNemaLozinkePoste) {
		t.Errorf("mape nakon brisanja lozinke: %v", err)
	}
	if err := o.akti.ObrisiRacunPoste(ctx, o.ana); err != nil {
		t.Errorf("drugo brisanje: %v", err)
	}
}

func TestSanducicPismaIMape(t *testing.T) {
	ctx := context.Background()
	o := novaOkolinaSanducica(t)
	o.pismaSanducica()

	mape, err := o.akti.MapeSanducica(ctx, o.ana)
	if err != nil {
		t.Fatal(err)
	}
	if in := mapaSanducica(t, mape, "inbox"); in.Ukupno != 2 || in.Neprocitano != 1 {
		t.Errorf("inbox: %+v", in)
	}
	for _, m := range mape {
		if m.ID == "archive" {
			t.Error("sandučić bez arhive ne smije nuditi mapu Arhiva")
		}
	}

	p, err := o.akti.Pismo(ctx, o.ana, "pismo-1")
	if err != nil {
		t.Fatal(err)
	}
	if p.Predmet != "Stanje nasipa" || p.OdAdresa != "ivo@voda.hr" || len(p.Privitci) != 1 || p.Privitci[0].Ime != "nasip.pdf" {
		t.Errorf("otvoreno pismo: %+v", p)
	}
	if _, err := o.akti.Pismo(ctx, o.ana, "nema-ga"); err == nil {
		t.Error("nepostojeće pismo mora javiti grešku")
	}
	pr, podaci, err := o.akti.Privitak(ctx, o.ana, "privitak-1")
	if err != nil || pr == nil || pr.Ime != "nasip.pdf" || string(podaci) != "%PDF-1.7\n" {
		t.Errorf("privitak: %+v, %q, %v", pr, podaci, err)
	}

	// pročitano i nepročitano, jedno i više odjednom
	if err := o.akti.OznaciProcitano(ctx, o.ana, "pismo-1", "ck1", true); err != nil {
		t.Fatal(err)
	}
	mape, _ = o.akti.MapeSanducica(ctx, o.ana) // probni podaci su ispravni; kvar bi pokazala provjera niže
	if in := mapaSanducica(t, mape, "inbox"); in.Neprocitano != 0 {
		t.Errorf("nakon označavanja pročitanim: %+v", in)
	}
	if err := o.akti.OznaciProcitano(ctx, o.ana, "pismo-1", "zastarjeli-kljuc", false); err == nil {
		t.Error("zastarjela oznaka izmjene mora javiti sukob")
	}
	if err := o.akti.OznaciProcitanoVise(ctx, o.ana, []posta.Stavka{{ID: "pismo-1", ChangeKey: "ck1"}, {ID: "pismo-2", ChangeKey: "ck1"}}, false); err != nil {
		t.Fatal(err)
	}
	mape, _ = o.akti.MapeSanducica(ctx, o.ana) // probni podaci su ispravni; kvar bi pokazala provjera niže
	if in := mapaSanducica(t, mape, "inbox"); in.Neprocitano != 2 {
		t.Errorf("nakon označavanja nepročitanima: %+v", in)
	}

	// premještanje, brisanje u Obrisano i trajno brisanje
	if err := o.akti.PremjestiPisma(ctx, o.ana, []string{"pismo-1"}, "archive"); err == nil || !strings.Contains(err.Error(), "nema mape Arhiva") {
		t.Errorf("premještanje u nepostojeću arhivu: %v", err)
	}
	if err := o.akti.ObrisiPismo(ctx, o.ana, "pismo-2"); err != nil {
		t.Fatal(err)
	}
	if err := o.akti.PremjestiPisma(ctx, o.ana, []string{"pismo-1"}, "deleteditems"); err != nil {
		t.Fatal(err)
	}
	obrisano, n, err := o.akti.Sanducic(ctx, o.ana, "deleteditems", "", 0, 20)
	if err != nil || n != 2 || len(obrisano) != 2 {
		t.Fatalf("Obrisano: %d pisama (%d), %v", len(obrisano), n, err)
	}
	if ulazna, n, err := o.akti.Sanducic(ctx, o.ana, "inbox", "", 1, 20); err != nil || n != 0 || len(ulazna) != 0 {
		t.Errorf("inbox nakon brisanja: %+v", ulazna)
	}
	if err := o.akti.ObrisiTrajno(ctx, o.ana, []string{"pismo-1"}); err != nil {
		t.Fatal(err)
	}
	obrisano, _, _ = o.akti.Sanducic(ctx, o.ana, "deleteditems", "", 1, 20) // probni podaci su ispravni; kvar bi pokazala provjera niže
	if len(obrisano) != 1 || obrisano[0].ID != "pismo-2" {
		t.Errorf("Obrisano nakon trajnog brisanja: %+v", obrisano)
	}

	// adresar tvrtke
	k, err := o.akti.Imenik(ctx, o.ana, "marko")
	if err != nil || len(k) != 1 || k[0].Email != "marko.markic@voda.hr" {
		t.Errorf("adresar: %+v, %v", k, err)
	}
}

func TestPosaljiPismoIzSanducica(t *testing.T) {
	ctx := context.Background()
	o := novaOkolinaSanducica(t)
	o.pismaSanducica()

	bezAdrese := *o.ana
	bezAdrese.Email = ""
	if err := o.akti.PosaljiPismo(ctx, &bezAdrese, service.NovoPismo{Za: "ivo@voda.hr", Predmet: "P"}); err == nil || !strings.Contains(err.Error(), "nema adrese") {
		t.Errorf("bez adrese pošiljatelja: %v", err)
	}
	if err := o.akti.PosaljiPismo(ctx, o.ana, service.NovoPismo{Za: "nije adresa", Predmet: "P"}); err == nil || !strings.Contains(err.Error(), "ispravnu adresu") {
		t.Errorf("bez ispravnog primatelja: %v", err)
	}
	if err := o.akti.PosaljiPismo(ctx, o.ana, service.NovoPismo{Za: "ivo@voda.hr", Predmet: "   "}); err == nil || !strings.Contains(err.Error(), "predmet") {
		t.Errorf("bez predmeta: %v", err)
	}
	if err := o.akti.PosaljiPismo(ctx, o.ana, service.NovoPismo{Za: "ivo@voda.hr", Predmet: "P", Proslijedi: []string{"nema-privitka"}}); err == nil || !strings.Contains(err.Error(), "prosljeđivanje") {
		t.Errorf("nepostojeći privitak za prosljeđivanje: %v", err)
	}
	if n := len(o.srv.Poruke()); n != 0 {
		t.Fatalf("odbijena pisma su poslana: %d", n)
	}

	png := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nprobna"))
	err := o.akti.PosaljiPismo(ctx, o.ana, service.NovoPismo{
		Za: "ivo@voda.hr; petra@voda.hr", Kopija: "marko.markic@voda.hr", Predmet: " Izvještaj s nasipa ",
		HTML:       `<p>Nasip je pregledan.</p><img src="data:image/png;base64,` + png + `">`,
		Proslijedi: []string{"privitak-1"},
	})
	if err != nil {
		t.Fatalf("slanje: %v", err)
	}
	poruke := o.srv.Poruke()
	if len(poruke) != 3 {
		t.Fatalf("primljeno %d poruka, očekivano 3 (dva primatelja i kopija)", len(poruke))
	}
	za := map[string]bool{}
	for _, p := range poruke {
		za[p.Za] = true
		if p.Od != "ana@voda.hr" {
			t.Errorf("pošiljatelj: %s", p.Od)
		}
	}
	if !za["ivo@voda.hr"] || !za["petra@voda.hr"] || !za["marko.markic@voda.hr"] {
		t.Errorf("primatelji: %v", za)
	}
	mime := poruke[0].Podaci
	for _, ocekivano := range []string{"Izvještaj s nasipa", "cid:slika1@gocop", "slika1.png", "nasip.pdf"} {
		if !strings.Contains(mimeTekst(t, mime), ocekivano) {
			t.Errorf("pismo nema %q", ocekivano)
		}
	}
	if strings.Contains(mime, "data:image/png") {
		t.Error("slika je ostala kao data: adresa, a ne ugrađena")
	}

	// poslužitelj odbije primatelja: greška se javlja
	o.srv.Odbij = []string{"odbijen@voda.hr"}
	if err := o.akti.PosaljiPismo(ctx, o.ana, service.NovoPismo{Za: "odbijen@voda.hr", Predmet: "P", Tekst: "T"}); err == nil {
		t.Error("odbijen primatelj mora javiti grešku")
	}
	// promijenjena lozinka sandučića
	o.srv.Lozinka = "Nova-Lozinka-2"
	if err := o.akti.PosaljiPismo(ctx, o.ana, service.NovoPismo{Za: "ivo@voda.hr", Predmet: "P", Tekst: "T"}); err == nil || !strings.Contains(err.Error(), "odbio vašu lozinku") {
		t.Errorf("odbijena lozinka: %v", err)
	}
}

// mimeTekst vraća pismo s dekodiranim zaglavljima, da se predmet s
// dijakritikom može usporediti
func mimeTekst(t *testing.T, poruka string) string {
	t.Helper()
	var b strings.Builder
	var dekoder mime.WordDecoder
	for _, red := range strings.Split(poruka, "\n") {
		if d, err := dekoder.DecodeHeader(strings.TrimSpace(red)); err == nil {
			b.WriteString(d)
		} else {
			b.WriteString(red)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func TestPosaljiPismoBezPoste(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	u := o.osoba(t, "ana", service.AddDutyRequest{})
	if err := o.akti.PosaljiPismo(context.Background(), u, service.NovoPismo{Za: "ivo@voda.hr", Predmet: "P"}); err == nil || !strings.Contains(err.Error(), "nije uključena") {
		t.Errorf("bez poslužitelja: %v", err)
	}
	if _, err := o.akti.PosaljiNaZnanje(context.Background(), o.admin, u, "nema", nil, nil, false, service.PorukaAkta{}); err == nil || !strings.Contains(err.Error(), "ne postoji") {
		t.Errorf("nepostojeći akt: %v", err)
	}
}

// aktNaZnanje upisuje akt sektora B s primateljima na znanje, kakav stiže
// razmjenom: jedan s dvije adrese, jedan bez adrese i isti primatelj dvaput
func (o *okolinaSanducica) aktNaZnanje(t *testing.T, status string) *models.Akt {
	t.Helper()
	kad := time.Date(2026, 10, 5, 8, 0, 0, 0, models.Zagreb)
	a := &models.Akt{Sektor: "B", AreaID: 16, Broj: 7, Godina: 2026, Radnja: models.AktUspostava, Stupanj: models.PhasePrep,
		Vrijedi: kad, Dionice: []models.AktDionica{{Code: "B.16.1"}}, Status: status,
		IzradioID: o.ana.ID.String(), Izradio: o.ana.FullName, IzradenoAt: kad.Add(-time.Hour),
		Primatelji: []models.AktPrimatelj{
			{Naziv: "Općina Probna", Email: "opcina@probna.test; pisarnica@probna.test", Skupina: "Lokalna samouprava"},
			{Naziv: "DVD Probni", Email: "dvd@probni.test", Skupina: "Vatrogasci"},
			{Naziv: "Mjesni odbor bez adrese"},
			{Naziv: "DVD Probni (dvaput)", Email: "DVD@probni.test"},
		}}
	if status == models.AktOvjeren {
		a.OvjerioID, a.Ovjerio, a.OvjerenoAt = o.sef.ID.String(), o.sef.FullName, &kad
	}
	if err := repository.NewAktiRepository(o.db, o.rec).SaveAkt(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAdresatiAktaISlanjeNaZnanje(t *testing.T) {
	ctx := context.Background()
	o := novaOkolinaSanducica(t)
	a := o.aktNaZnanje(t, models.AktOvjeren)

	adresati, bez, slanja := o.akti.AdresatiAkta(ctx, a)
	if len(adresati) != 3 || len(bez) != 1 || len(slanja) != 0 {
		t.Fatalf("adresati %+v, bez adrese %+v, slanja %d", adresati, bez, len(slanja))
	}
	if adresati[0].Adresa != "opcina@probna.test" || adresati[1].Adresa != "pisarnica@probna.test" || adresati[1].Naziv != "Općina Probna" ||
		adresati[2].Adresa != "dvd@probni.test" || adresati[2].Skupina != "Vatrogasci" || bez[0].Naziv != "Mjesni odbor bez adrese" {
		t.Errorf("adresati: %+v", adresati)
	}
	for _, x := range adresati {
		if x.Poslano() {
			t.Errorf("%s: poslano prije slanja", x.Adresa)
		}
	}

	poruka := service.PorukaAkta{Predmet: "Akt 7/2026", Tekst: "U privitku je akt.", ImeDatoteke: "akt-7-2026.pdf", PDF: []byte("%PDF-1.7 akt\n")}
	vodocuvar := o.osoba(t, "ivo", service.AddDutyRequest{Role: models.RoleWaterGuard, AreaID: podrucjeP(16)})
	if _, err := o.akti.PosaljiNaZnanje(ctx, o.ovlasti(t, vodocuvar.ID), vodocuvar, a.ID, []string{"opcina@probna.test"}, nil, false, poruka); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("vodočuvar šalje akt: %v", err)
	}
	nacrt := o.aktNaZnanje(t, models.AktNacrt)
	if _, err := o.akti.PosaljiNaZnanje(ctx, o.anaOvl, o.ana, nacrt.ID, []string{"opcina@probna.test"}, nil, false, poruka); err == nil || !strings.Contains(err.Error(), "tek kad je ovjeren") {
		t.Errorf("nacrt: %v", err)
	}
	bezAdrese := *o.ana
	bezAdrese.Email = ""
	if _, err := o.akti.PosaljiNaZnanje(ctx, o.anaOvl, &bezAdrese, a.ID, []string{"opcina@probna.test"}, nil, false, poruka); err == nil || !strings.Contains(err.Error(), "nema adrese") {
		t.Errorf("pošiljatelj bez adrese: %v", err)
	}
	if _, err := o.akti.PosaljiNaZnanje(ctx, o.anaOvl, o.ana, a.ID, []string{"opcina@probna.test"}, nil, false, service.PorukaAkta{Predmet: "P"}); err == nil || !strings.Contains(err.Error(), "PDF akta nije dostupan") {
		t.Errorf("bez PDF-a: %v", err)
	}
	if _, err := o.akti.PosaljiNaZnanje(ctx, o.anaOvl, o.ana, a.ID, []string{"tudja@adresa.test"}, nil, false, poruka); err == nil || !strings.Contains(err.Error(), "barem jednu adresu") {
		t.Errorf("adresa koja nije na popisu: %v", err)
	}
	if n := len(o.srv.Poruke()); n != 0 {
		t.Fatalf("odbijena slanja su poslana: %d", n)
	}

	// pisarnica odbijena; dodatna adresa i kopija pošiljateljici prolaze
	o.srv.Odbij = []string{"pisarnica@probna.test"}
	ishod, err := o.akti.PosaljiNaZnanje(ctx, o.anaOvl, o.ana, a.ID,
		[]string{"opcina@probna.test", "pisarnica@probna.test", "tudja@adresa.test"},
		[]string{"dodatno@probna.test", "opcina@probna.test"}, true, poruka)
	if err != nil {
		t.Fatal(err)
	}
	if ishod.Poslano != 2 || !ishod.Kopija || len(ishod.Greske) != 1 || !strings.HasPrefix(ishod.Greske[0], "pisarnica@probna.test: ") {
		t.Errorf("ishod: %+v", ishod)
	}
	primljeno := map[string]bool{}
	for _, p := range o.srv.Poruke() {
		primljeno[p.Za] = true
		if p.Od != "ana@voda.hr" || !strings.Contains(p.Podaci, "akt-7-2026.pdf") {
			t.Errorf("poruka %s: od %s, bez privitka", p.Za, p.Od)
		}
	}
	if len(primljeno) != 3 || !primljeno["opcina@probna.test"] || !primljeno["dodatno@probna.test"] || !primljeno["ana@voda.hr"] {
		t.Errorf("primljeno: %v", primljeno)
	}

	adresati, _, slanja = o.akti.AdresatiAkta(ctx, a)
	if len(slanja) != 3 {
		t.Fatalf("zapisi slanja: %+v", slanja)
	}
	stanje := map[string]service.Adresat{}
	for _, x := range adresati {
		stanje[x.Adresa] = x
	}
	if !stanje["opcina@probna.test"].Poslano() || stanje["pisarnica@probna.test"].Poslano() || stanje["pisarnica@probna.test"].Zadnje == nil ||
		stanje["pisarnica@probna.test"].Zadnje.Greska == "" || stanje["dvd@probni.test"].Zadnje != nil {
		t.Errorf("stanje slanja po adresi: %+v", stanje)
	}
	for _, z := range slanja {
		if z.Adresa == "dodatno@probna.test" && (z.Naziv != "dodatno upisano" || !z.Uspjelo || z.Poslao != o.ana.FullName) {
			t.Errorf("zapis dodatne adrese: %+v", z)
		}
	}

	// promijenjena lozinka sandučića: ništa se ne šalje ni ne bilježi
	o.srv.Lozinka = "Nova-Lozinka-2"
	if _, err := o.akti.PosaljiNaZnanje(ctx, o.anaOvl, o.ana, a.ID, []string{"dvd@probni.test"}, nil, false, poruka); err == nil || !strings.Contains(err.Error(), "odbio vašu lozinku") {
		t.Errorf("odbijena lozinka: %v", err)
	}
	if _, _, slanja := o.akti.AdresatiAkta(ctx, a); len(slanja) != 3 {
		t.Errorf("odbijena prijava zabilježila je slanje: %d", len(slanja))
	}
}

func TestAdreseZaPismo(t *testing.T) {
	ctx := context.Background()
	o := novaOkolinaSanducica(t)
	o.pismaSanducica()
	if _, err := o.db.Exec(`UPDATE users SET full_name = 'Ana Anić', last_login_at = datetime('now') WHERE id = ?`, o.ana.ID.String()); err != nil {
		t.Fatal(err)
	}

	if a := o.akti.AdreseZaPismo(ctx, o.ana, " a "); a != nil {
		t.Errorf("prekratak upit: %+v", a)
	}
	// izvori po adresi; ista adresa smije doći samo jednom
	izvori := func(adrese []service.PredlozenaAdresa) map[string]string {
		t.Helper()
		izvor := map[string]string{}
		for _, a := range adrese {
			if _, bilo := izvor[a.Email]; bilo {
				t.Errorf("adresa %s ponovljena: %+v", a.Email, adrese)
			}
			izvor[a.Email] = a.Izvor
		}
		return izvor
	}
	adrese := o.akti.AdreseZaPismo(ctx, o.ana, "AN")
	izvor := izvori(adrese)
	// Ana je i u adresaru tvrtke, ali se predlaže jednom, kao djelatnica
	if izvor["ana@voda.hr"] != "djelatnik" {
		t.Errorf("prijedlozi: %+v", adrese)
	}
	if _, ima := izvor["marko.markic@voda.hr"]; ima {
		t.Errorf("adresa koja ne sadrži upit: %+v", adrese)
	}

	// adresar tvrtke pita se samo za prijavljenu osobu sa sandučićem
	marko := o.akti.AdreseZaPismo(ctx, o.ana, "markić")
	if izvori(marko)["marko.markic@voda.hr"] != "adresar" {
		t.Errorf("adresar: %+v", marko)
	}
	if bez := o.akti.AdreseZaPismo(ctx, nil, "markić"); len(bez) != 0 {
		t.Errorf("bez osobe adresar se ne pita: %+v", bez)
	}
}

func TestZadaniPotpis(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	if _, err := o.db.Exec(`UPDATE sectors SET phone = '031 200 300' WHERE id = 'B'`); err != nil {
		t.Fatal(err)
	}
	ana := o.osoba(t, "ana", service.AddDutyRequest{Role: models.RoleOperator, SectorID: sektorP("B"), Title: "Dežurna <operaterka>"})
	ana.FullName, ana.Title, ana.Phone, ana.MobilePhone = "Ana Anić", "dipl. ing.", "031 100 100", "098 111 2222"

	h := o.akti.ZadaniPotpis(ana)
	org := models.Terms().OrgName
	if org == "" {
		org = "Hrvatske vode"
	}
	for _, ocekivano := range []string{
		"Srdačan pozdrav", strings.ToUpper(org), "VGO B", "<b>Dežurna &lt;operaterka&gt;</b>", "<b>Ana Anić</b>, dipl. ing.",
		"Tel: <span", "031 100 100", "031 200 300", "Gsm: <span", "098 111 2222", `href="mailto:ana@voda.hr"`,
	} {
		if !strings.Contains(h, ocekivano) {
			t.Errorf("potpis nema %q:\n%s", ocekivano, h)
		}
	}

	// isti broj telefona kao centar ne piše se dvaput; bez dužnosti
	// jedinica je organizacija osobe
	ivo := o.osoba(t, "ivo", service.AddDutyRequest{})
	ivo.FullName, ivo.Phone, ivo.OrgName = "Ivo Ivić", "", "Probna d.o.o."
	h = o.akti.ZadaniPotpis(ivo)
	if !strings.Contains(h, "PROBNA D.O.O.") || strings.Contains(h, "Tel:") || strings.Contains(h, "Gsm:") || strings.Contains(h, "VGO B") {
		t.Errorf("potpis bez dužnosti:\n%s", h)
	}
	ana.Phone = "031/200-300"
	if h := o.akti.ZadaniPotpis(ana); strings.Count(h, "200") != 1 {
		t.Errorf("isti broj centra ponovljen:\n%s", h)
	}
}
