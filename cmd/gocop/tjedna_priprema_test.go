package main

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/poslovi"
	"gocop/internal/prognoza"
	"gocop/internal/web"
)

// Svi promašaji u ovim provjerama su izmišljeni.

// modelOd spremi u bazu prognoza lanac od jednog računa donja ← gornja,
// namješten u trenutku kad.
func modelOd(t *testing.T, pb *sql.DB, kad time.Time) {
	t.Helper()
	if err := prognoza.Spremi(pb, []prognoza.Pojas{{Letva: "donja", Velicina: "vodostaj", Od: -1000, Do: 1000, Rasap: 3,
		Ulazi: []prognoza.Ulaz{{Letva: "gornja", Velicina: "vodostaj", PomakH: 6, Sirina: 1, Nagib: 1}}}}, kad.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
}

// mjera je izmjereni promašaj letve donja na svim dosezima tjedne pripreme.
func mjera(cm float64) prognoza.MjereProvjere {
	po := map[int]prognoza.MjeraProvjere{}
	for _, d := range prognoza.DoseziTjedne {
		po[d] = prognoza.MjeraProvjere{Promasaj: cm, Slucaja: 200}
	}
	return prognoza.MjereProvjere{{Letva: "donja", Velicina: "vodostaj"}: po}
}

// lazanTjedni je okolina tjedne pripreme: namještanje spremi novi lanac
// (namješten u trenutku termina), a mjerenje daje promašaj starog i novog
// lanca kako ga test postavi. Broji namještanja.
type lazanTjedni struct {
	stari, novi     float64
	namjestanja     int
	sada            time.Time
	obranaTraje     bool
	izdano          int
	greskaIzdavanja error
}

func tjednaZaTest(t *testing.T) (*tjednaPriprema, *lazanTjedni, *sql.DB) {
	t.Helper()
	pb, err := prognoza.Otvori(filepath.Join(t.TempDir(), "prognoze.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pb.Close() })
	staraNamjesten := time.Date(2026, 11, 15, 2, 0, 0, 0, models.Zagreb)
	modelOd(t, pb, staraNamjesten)
	l := &lazanTjedni{stari: 10, novi: 10}
	staroN, staroP, staroM, staroZ := namjestiLanac, provjeriUnatrag, izmjeriLanac, zaboraviDnevneModele
	t.Cleanup(func() {
		namjestiLanac, provjeriUnatrag, izmjeriLanac, zaboraviDnevneModele = staroN, staroP, staroM, staroZ
	})
	zaboraviDnevneModele = func() {}
	namjestiLanac = func(_, baza *sql.DB, _ prognoza.OpcijeNamjestanja) (int, error) {
		l.namjestanja++
		modelOd(t, baza, prognoza.TerminTjedne(l.sada, models.Zagreb))
		return 1, nil
	}
	provjeriUnatrag = func(_, _ *sql.DB, _ prognoza.OpcijeProvjere) (int, error) { return 0, nil }
	izmjeriLanac = func(_, baza *sql.DB, od, do time.Time) (prognoza.MjereProvjere, error) {
		if !do.After(od) || do.After(l.sada) {
			t.Errorf("razdoblje mjerenja %s – %s", od, do)
		}
		if prognoza.NamjestenOd(baza).Equal(prognoza.TerminTjedne(l.sada, models.Zagreb)) {
			return mjera(l.novi), nil
		}
		return mjera(l.stari), nil
	}
	tp := &tjednaPriprema{pb: pb, izdaje: func() bool { return true },
		obrana: func(context.Context) (string, bool) { return "B-3/2026", l.obranaTraje },
		sat:    func() time.Time { return l.sada },
		izdaj: func(context.Context, *poslovi.Posao) (string, error) {
			l.izdano++
			return "izdano", l.greskaIzdavanja
		}}
	return tp, l, pb
}

func subota(d int, h, m int) time.Time { return time.Date(2026, 11, d, h, m, 0, 0, models.Zagreb) }

func pripreme(t *testing.T, pb *sql.DB) []prognoza.TjednaPriprema {
	t.Helper()
	p, err := prognoza.TjednePripreme(pb, 10)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Priprema ide u subotu u 2:00 i samo jednom; u petak, poslije 6:00 i na
// računalu koje ne izdaje ne ide.
func TestTjednaJednomUSubotu(t *testing.T) {
	tp, l, pb := tjednaZaTest(t)
	ctx := context.Background()
	for _, kad := range []time.Time{subota(27, 23, 59), subota(28, 1, 59)} {
		l.sada = kad
		tp.provjeri(ctx)
	}
	if l.namjestanja != 0 {
		t.Fatalf("prije subote u 2:00 namješteno %d puta", l.namjestanja)
	}
	for _, kad := range []time.Time{subota(28, 2, 0), subota(28, 2, 1), subota(28, 5, 0)} {
		l.sada = kad
		tp.provjeri(ctx)
	}
	if l.namjestanja != 1 || len(pripreme(t, pb)) != 1 {
		t.Fatalf("u subotu namješteno %d puta, zapisa %d", l.namjestanja, len(pripreme(t, pb)))
	}
	l.sada = subota(28, 2, 0).AddDate(0, 0, 7).Add(4 * time.Hour) // iduća subota, ali iza 6:00
	tp.provjeri(ctx)
	tp.izdaje = func() bool { return false }
	l.sada = subota(28, 2, 0).AddDate(0, 0, 14)
	tp.provjeri(ctx)
	if l.namjestanja != 1 {
		t.Errorf("namješteno %d puta, želim 1", l.namjestanja)
	}
}

// Dok traje obrana, priprema se preskače i zapiše zašto; lanac ostaje.
func TestTjednaPreskocenaUObrani(t *testing.T) {
	tp, l, pb := tjednaZaTest(t)
	l.obranaTraje, l.sada = true, subota(28, 2, 0)
	prije := prognoza.NamjestenOd(pb)
	tp.provjeri(context.Background())
	p := pripreme(t, pb)
	if len(p) != 1 || p[0].Ishod != prognoza.TjednaPreskocena || p[0].Razlog != "traje obrana od poplava (B-3/2026)" {
		t.Fatalf("zapis %+v", p)
	}
	if l.namjestanja != 0 || !prognoza.NamjestenOd(pb).Equal(prije) {
		t.Error("u obrani se lanac namještao")
	}
}

// Lošiji novi lanac ne primijeni se i stari ostaje netaknut; lanac koji nije
// lošiji od 2 % primijeni se.
func TestTjednaPrimjenaSamoKadNijeLosiji(t *testing.T) {
	tp, l, pb := tjednaZaTest(t)
	prije := prognoza.NamjestenOd(pb)
	l.sada, l.stari, l.novi = subota(28, 2, 0), 10, 10.5
	tp.provjeri(context.Background())
	p := pripreme(t, pb)
	if p[0].Ishod != prognoza.TjednaNijePrimijenjena || len(p[0].Losije) != 0 || !prognoza.NamjestenOd(pb).Equal(prije) {
		t.Fatalf("lošiji za 5 %%: %+v, lanac od %s", p[0], prognoza.NamjestenOd(pb))
	}
	if !p[0].LanacOd.Equal(prije) || p[0].Prije != 10 || p[0].Poslije != 10.5 {
		t.Errorf("zapis %+v", p[0])
	}
	l.sada, l.novi = subota(28, 2, 0).AddDate(0, 0, 7), 10.15
	tp.provjeri(context.Background())
	p = pripreme(t, pb)
	if p[0].Ishod != prognoza.TjednaPrimijenjena || !prognoza.NamjestenOd(pb).Equal(prognoza.TerminTjedne(l.sada, models.Zagreb)) {
		t.Fatalf("lošiji za 1,5 %%: %+v, lanac od %s", p[0], prognoza.NamjestenOd(pb))
	}
}

// Letva na kojoj je novi lanac lošiji ide na popis.
func TestTjednaPopisLosijih(t *testing.T) {
	tp, l, pb := tjednaZaTest(t)
	l.sada, l.stari, l.novi = subota(28, 2, 0), 10, 13
	tp.provjeri(context.Background())
	p := pripreme(t, pb)
	if len(p[0].Losije) != 1 || p[0].Losije[0].Letva != "donja" || p[0].Losije[0].Razlika != 3 {
		t.Errorf("lošije: %+v", p[0].Losije)
	}
}

// Tri pripreme zaredom koje nisu prošle probu traže pregled; preskočena
// tjedna niz ne prekida.
func TestTjednaTriPutaZaredom(t *testing.T) {
	tp, l, pb := tjednaZaTest(t)
	l.stari, l.novi = 10, 12
	for i, obrana := range []bool{false, false, true, false} {
		l.sada, l.obranaTraje = subota(28, 2, 0).AddDate(0, 0, 7*i), obrana
		tp.provjeri(context.Background())
		u := prognoza.UpozorenjeTjedne(pripreme(t, pb))
		if zelim := i == 3; zelim != (u == prognoza.UpozorenjeZaredom) {
			t.Errorf("%d. tjedan: upozorenje %q", i+1, u)
		}
	}
}

// Primijenjeni lanac koji je 20 % lošiji nego kad je primijenjen slabi; novi
// lanac koji to ne popravi ostavlja upozorenje.
func TestTjednaStariLanacSlabi(t *testing.T) {
	tp, l, pb := tjednaZaTest(t)
	l.sada, l.stari, l.novi = subota(28, 2, 0), 12, 11.9
	tp.provjeri(context.Background()) // primijenjen s promašajem 11,9
	if u := prognoza.UpozorenjeTjedne(pripreme(t, pb)); u != "" {
		t.Fatalf("prvi tjedan: %q", u)
	}
	l.sada, l.stari, l.novi = subota(28, 2, 0).AddDate(0, 0, 7), 14.6, 14.6
	tp.provjeri(context.Background())
	p := pripreme(t, pb)
	if !p[0].Slabi || p[0].Osnovica != 11.9 || prognoza.UpozorenjeTjedne(p) != prognoza.UpozorenjeSlabi {
		t.Fatalf("drugi tjedan: %+v", p[0])
	}
	// lanac koji vrati promašaj blizu osnovice skida upozorenje
	l.sada, l.stari, l.novi = subota(28, 2, 0).AddDate(0, 0, 14), 14.6, 12
	tp.provjeri(context.Background())
	if u := prognoza.UpozorenjeTjedne(pripreme(t, pb)); u != "" {
		t.Errorf("treći tjedan: %q", u)
	}
}

// Lanac primijenjen rukom (drugi otisak modela) dobije osnovicu iz prvog
// mjerenja, pa ne slabi odmah.
func TestTjednaOsnovicaRucnogLanca(t *testing.T) {
	tp, l, pb := tjednaZaTest(t)
	if err := prognoza.UpisiOsnovicu(pb, prognoza.Osnovica{Otisak: "drugi", Promasaj: 5}); err != nil {
		t.Fatal(err)
	}
	l.sada, l.stari, l.novi = subota(28, 2, 0), 12, 13
	tp.provjeri(context.Background())
	if p := pripreme(t, pb); p[0].Slabi || p[0].Osnovica != 12 {
		t.Errorf("ručni lanac: %+v", p[0])
	}
}

// Oznake akata po kojima traje obrana, poredane; bez obrane prazno, a kad
// se stanje ne da pročitati, obrana traje bez oznake.
func TestObranaUAktima(t *testing.T) {
	stanja := func(context.Context, string, time.Time) (map[string]models.StanjeObrane, error) {
		return map[string]models.StanjeObrane{
			"B.1": {Aktivni: []models.AktivniStadij{{AktID: "a2"}, {AktID: "a1"}}},
			"B.2": {Aktivni: []models.AktivniStadij{{AktID: "a1"}}},
		}, nil
	}
	akt := func(_ context.Context, id string) (*models.Akt, error) {
		return &models.Akt{ID: id, Sektor: "B", Broj: map[string]int{"a1": 3, "a2": 4}[id]}, nil
	}
	o := obranaUAktima(func(context.Context) bool { return true }, stanja, akt)
	oznaka, traje := o(context.Background())
	if !traje || strings.Count(oznaka, ",") != 1 {
		t.Errorf("obrana %v %q", traje, oznaka)
	}
	if _, traje := obranaUAktima(func(context.Context) bool { return false }, stanja, akt)(context.Background()); traje {
		t.Error("bez obrane javljeno da traje")
	}
	pukla := func(context.Context, string, time.Time) (map[string]models.StanjeObrane, error) {
		return nil, errors.New("akti se ne čitaju")
	}
	if oznaka, traje := obranaUAktima(func(context.Context) bool { return true }, pukla, akt)(context.Background()); !traje || oznaka != "" {
		t.Errorf("stanje se ne čita: %v %q", traje, oznaka)
	}
	bezAkta := func(context.Context, string, time.Time) (map[string]models.StanjeObrane, error) {
		return map[string]models.StanjeObrane{"B.3": {Aktivni: []models.AktivniStadij{{}, {AktID: "nema"}}}}, nil
	}
	nemaAkta := func(context.Context, string) (*models.Akt, error) { return nil, errors.New("nema akta") }
	if oznaka, traje := obranaUAktima(func(context.Context) bool { return true }, bezAkta, nemaAkta)(context.Background()); !traje || oznaka != "" {
		t.Errorf("epizoda bez akta: %v %q", traje, oznaka)
	}
}

// Greške pripreme: namještanje, mjerenje i osnovica zapišu se kao
// neuspjela priprema, a lanac ostaje; neuspjelo izdavanje novim modelom
// samo se javi.
func TestTjednaGreske(t *testing.T) {
	tp, l, pb := tjednaZaTest(t)
	prije := prognoza.NamjestenOd(pb)
	staro := namjestiLanac
	namjestiLanac = func(*sql.DB, *sql.DB, prognoza.OpcijeNamjestanja) (int, error) {
		return 0, errors.New("arhiva je zauzeta")
	}
	l.sada = subota(28, 2, 0)
	tp.provjeri(context.Background())
	namjestiLanac = staro
	if p := pripreme(t, pb); p[0].Ishod != prognoza.TjednaGreska || p[0].Razlog != "arhiva je zauzeta" {
		t.Errorf("namještanje: %+v", p[0])
	}
	staroM := izmjeriLanac
	izmjeriLanac = func(*sql.DB, *sql.DB, time.Time, time.Time) (prognoza.MjereProvjere, error) {
		return nil, errors.New("arhiva se ne čita")
	}
	if p := tp.pripremi(context.Background(), subota(28, 2, 0).AddDate(0, 0, 7)); p.Ishod != prognoza.TjednaGreska {
		t.Errorf("mjerenje: %+v", p)
	}
	izmjeriLanac = staroM
	l.sada = subota(28, 2, 0).AddDate(0, 0, 7)
	if _, err := pb.Exec(`DROP TABLE tjedna_osnovica`); err != nil {
		t.Fatal(err)
	}
	if p := tp.pripremi(context.Background(), subota(28, 2, 0).AddDate(0, 0, 7)); p.Ishod != prognoza.TjednaGreska {
		t.Errorf("osnovica: %+v", p)
	}
	if _, err := pb.Exec(`CREATE TABLE tjedna_osnovica (id INTEGER PRIMARY KEY, otisak TEXT, promasaj REAL)`); err != nil {
		t.Fatal(err)
	}
	l.greskaIzdavanja = errors.New("krug traje")
	dnevnik := zapisDnevnika(t)
	l.sada = subota(28, 2, 0).AddDate(0, 0, 14)
	tp.provjeri(context.Background())
	if p := pripreme(t, pb); p[0].Ishod != prognoza.TjednaPrimijenjena || !strings.Contains(dnevnik.String(), "krug traje") {
		t.Errorf("izdavanje: %+v, dnevnik %q", p[0], dnevnik.String())
	}
	if prognoza.NamjestenOd(pb).Equal(prije) {
		t.Error("primijenjeni lanac nije u bazi")
	}
	// zapis koji se ne da pročitati ni upisati samo se javi
	if _, err := pb.Exec(`DROP TABLE tjedna_priprema`); err != nil {
		t.Fatal(err)
	}
	tp.provjeri(context.Background())
	if !strings.Contains(dnevnik.String(), "tjedna priprema: ") {
		t.Errorf("dnevnik %q", dnevnik.String())
	}
}

// Mjerenje lanca ide u privremenoj kopiji modela, a mjere su one koje je
// provjera unatrag izmjerila.
func TestIzmjeriULanac(t *testing.T) {
	_, _, pb := tjednaZaTest(t)
	provjeriUnatrag = func(_, baza *sql.DB, o prognoza.OpcijeProvjere) (int, error) {
		if baza == pb || !o.Zapisi || o.Korak != korakMjerenja {
			t.Errorf("mjerenje u bazi prognoza ili s krivim opcijama: %+v", o)
		}
		o.Mjera[prognoza.Izvor{Letva: "donja", Velicina: "vodostaj"}] = map[int]prognoza.MjeraProvjere{24: {Promasaj: 7, Slucaja: 100}}
		return 1, nil
	}
	m, err := izmjeriULanac(nil, pb, subota(1, 0, 0), subota(20, 0, 0))
	if err != nil || m[prognoza.Izvor{Letva: "donja", Velicina: "vodostaj"}][24].Promasaj != 7 {
		t.Errorf("mjere %v %v", m, err)
	}
	_ = pb.Close() // baza se namjerno zatvara, da se vidi greška
	if _, err := izmjeriULanac(nil, pb, subota(1, 0, 0), subota(20, 0, 0)); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}

// lazniPosluzitelj pamti što mu je tjedna priprema dala.
type lazniPosluzitelj struct{ stanje func() web.StanjeTjedne }

func (l *lazniPosluzitelj) SetTjednaPriprema(f func() web.StanjeTjedne) { l.stanje = f }

// Tjedna priprema čvora: izdaje kad čvor izdaje, izdaje novim modelom
// (ili čeka idući krug), a stranicama daje stanje s nazivima letvi.
func TestTjednaCvora(t *testing.T) {
	_, _, pb := tjednaZaTest(t)
	registar, _, _ := cvorZaTest(t, "kucni-server")
	if _, err := registar.Exec(`INSERT INTO stations (id, code, name, created_at, updated_at)
		VALUES ('s1', 'donja', 'Donja', datetime('now'), datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	ctx, otkazi := context.WithCancel(context.Background())
	otkazi()
	srv := &lazniPosluzitelj{}
	tp := tjednaCvora(ctx, srv, func() peers.Uloge { return peers.Uloge{Izdaje: true} }, pb, nil, registar, nil,
		func() bool { return true }, nil, nil)
	if !tp.izdaje() {
		t.Error("čvor koji izdaje")
	}
	if _, traje := tp.obrana(ctx); traje {
		t.Error("bez podataka o obrani javljeno da traje")
	}
	if poruka, err := tp.izdaj(ctx, &poslovi.Posao{}); err != nil || !strings.Contains(poruka, "idućeg satnog kruga") {
		t.Errorf("izdavanje dok krug traje: %q %v", poruka, err)
	}
	st := srv.stanje()
	if !st.Izdaje || st.Nazivi["donja"] != "Donja" || st.Sljedeca.Weekday() != time.Saturday {
		t.Errorf("stanje %+v", st)
	}
	_ = registar.Close() // baza se namjerno zatvara, da se vidi greška
	_ = pb.Close() // baza se namjerno zatvara, da se vidi greška
	if st := srv.stanje(); len(st.Nazivi) != 0 || len(st.Pripreme) != 0 {
		t.Errorf("zatvorene baze: %+v", st)
	}
}

// Krug tjedne pripreme staje kad se program gasi.
func TestTjednaVrtiStaje(t *testing.T) {
	ctx, otkazi := context.WithCancel(context.Background())
	otkazi()
	(&tjednaPriprema{sat: time.Now, izdaje: func() bool { return false }}).vrti(ctx)
}

// Rečenice za dnevnik i razlog preskočene pripreme.
func TestOpisTjedne(t *testing.T) {
	for _, s := range []struct {
		p     prognoza.TjednaPriprema
		zelim string
	}{
		{prognoza.TjednaPriprema{Ishod: prognoza.TjednaPreskocena, Razlog: razlogObrane("")}, "preskočena: traje obrana od poplava"},
		{prognoza.TjednaPriprema{Ishod: prognoza.TjednaNijePrimijenjena, Prije: 10, Poslije: 11}, "nije primijenjena (10.0 → 11.0 cm)"},
		{prognoza.TjednaPriprema{Ishod: prognoza.TjednaPrimijenjena, Prije: 14.6, Poslije: 14.6, Slabi: true}, "primijenjena (14.6 → 14.6 cm), stari lanac slabi"},
	} {
		if got := opisTjedne(s.p); got != s.zelim {
			t.Errorf("%q, želim %q", got, s.zelim)
		}
	}
}
