package repository

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Sredstva: katalog, skladišta, promet, stanje i popisi, kako ih čita
// ostatak programa. Osobe i skladišta su izmišljeni.

func danMts(g int, m time.Month, d int) time.Time {
	return time.Date(g, m, d, 0, 0, 0, 0, models.Zagreb)
}

func TestOsigurajKatalogPuniPrazanIPrijeKrajaNePrepisuje(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewMtsRepository(n.db, n.rec)

	nuzno(t, repo.OsigurajKatalog(ctxRaz))
	vrste, err := repo.ListVrste(ctxRaz, true)
	nuzno(t, err)
	if len(vrste) != len(models.KatalogSredstava()) {
		t.Fatalf("katalog: %d vrsta, propisano %d", len(vrste), len(models.KatalogSredstava()))
	}
	// redom skupina pa rednog broja
	for i := 1; i < len(vrste); i++ {
		if vrste[i-1].Grupa == vrste[i].Grupa && vrste[i-1].Redoslijed > vrste[i].Redoslijed {
			t.Fatalf("poredak: %s (%d) prije %s (%d)", vrste[i-1].ID, vrste[i-1].Redoslijed, vrste[i].ID, vrste[i].Redoslijed)
		}
	}
	if vrste[0].Grupa != models.GrupaOprema {
		t.Errorf("prva skupina: %s", vrste[0].Grupa)
	}

	// izmjena na čvoru se drugim pokretanjem ne prepisuje
	v, err := repo.GetVrsta(ctxRaz, "vrece-50x80")
	nuzno(t, err)
	if v == nil || len(v.Oblici) != 2 || !v.Aktivna {
		t.Fatalf("vreće iz kataloga: %+v", v)
	}
	v.Naziv, v.Aktivna = "Vreće (naše)", false
	nuzno(t, repo.SaveVrsta(ctxRaz, v))
	nuzno(t, repo.OsigurajKatalog(ctxRaz))
	v, _ = repo.GetVrsta(ctxRaz, "vrece-50x80") // probni podaci su ispravni; kvar bi pokazala provjera niže
	if v.Naziv != "Vreće (naše)" || v.Aktivna {
		t.Errorf("drugo pokretanje prepisalo je vrstu: %+v", v)
	}

	// ugašena vrsta ne ide u popis aktivnih
	aktivne, err := repo.ListVrste(ctxRaz, false)
	nuzno(t, err)
	if len(aktivne) != len(vrste)-1 {
		t.Errorf("aktivnih %d, svih %d", len(aktivne), len(vrste))
	}
	for _, a := range aktivne {
		if a.ID == "vrece-50x80" {
			t.Error("ugašena vrsta je među aktivnima")
		}
	}

	if v, err := repo.GetVrsta(ctxRaz, "nepostojeca"); err != nil || v != nil {
		t.Errorf("nepostojeća vrsta: %v, %v", v, err)
	}
}

// Vrsta koja je oblike dobila poslije prvog sijanja dobiva ih, a njezin
// promet bez oblika postaje „prazno“; tuđi promet ostaje kakav je.
func TestOsigurajKatalogDopunjujeOblike(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewMtsRepository(n.db, n.rec)
	nuzno(t, repo.SaveVrsta(ctxRaz, &models.VrstaSredstva{ID: "box-barijera-1x1x1", Grupa: models.GrupaMaterijal, Naziv: "Box barijere 1x1x1", Jedinica: "m'", Aktivna: true}))
	nuzno(t, repo.SaveVrsta(ctxRaz, &models.VrstaSredstva{ID: "lopata", Grupa: models.GrupaAlat, Naziv: "Lopata", Jedinica: "kom", Aktivna: true}))
	sk := &models.Skladiste{Sektor: "B", AreaID: 16, Naziv: "Skladište Ribnjak", Aktivno: true}
	nuzno(t, repo.SaveSkladiste(ctxRaz, sk))
	nuzno(t, repo.SavePromet(ctxRaz, []models.Promet{
		{Datum: danMts(2026, 3, 1), VrstaID: "box-barijera-1x1x1", Kolicina: 20, Vrsta: models.PrometPocetno, Sektor: "B", SkladisteID: sk.ID},
		{Datum: danMts(2026, 3, 1), VrstaID: "lopata", Kolicina: 5, Vrsta: models.PrometPocetno, Sektor: "B", SkladisteID: sk.ID},
	}))

	nuzno(t, repo.OsigurajKatalog(ctxRaz))

	v, _ := repo.GetVrsta(ctxRaz, "box-barijera-1x1x1") // probni podaci su ispravni; kvar bi pokazala provjera niže
	if len(v.Oblici) != 2 || v.Oblici[0] != models.OblikPrazno {
		t.Fatalf("oblici barijere: %v", v.Oblici)
	}
	box, err := repo.ListPromet(ctxRaz, FiltarPrometa{VrstaID: "box-barijera-1x1x1"})
	nuzno(t, err)
	if len(box) != 1 || box[0].Oblik != models.OblikPrazno || box[0].Kolicina != 20 {
		t.Errorf("promet barijera: %+v", box)
	}
	lop, _ := repo.ListPromet(ctxRaz, FiltarPrometa{VrstaID: "lopata"}) // probni podaci su ispravni; kvar bi pokazala provjera niže
	if len(lop) != 1 || lop[0].Oblik != models.OblikOsnovni {
		t.Errorf("promet lopata: %+v", lop)
	}
	// u katalogu nisu dodane ostale vrste
	if sve, err := repo.ListVrste(ctxRaz, true); err != nil || len(sve) != 2 {
		t.Errorf("katalog s postojećim vrstama: %d", len(sve))
	}
}

// Redak na terenu koji ima samo dionicu dobiva područje te dionice.
func TestOsigurajKatalogDopisujePodrucjeTerena(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewMtsRepository(n.db, n.rec)
	nuzno(t, repo.SaveVrsta(ctxRaz, &models.VrstaSredstva{ID: "lopata", Grupa: models.GrupaAlat, Naziv: "Lopata", Jedinica: "kom", Aktivna: true}))
	nuzno(t, repo.SavePromet(ctxRaz, []models.Promet{
		{Datum: danMts(2026, 4, 2), VrstaID: "lopata", Kolicina: 3, Vrsta: models.PrometIzdano, Sektor: "B", SectionCode: osnovaDionica},
		{Datum: danMts(2026, 4, 2), VrstaID: "lopata", Kolicina: 1, Vrsta: models.PrometIzdano, Sektor: "B", Mjesto: "Kod mosta"},
	}))

	nuzno(t, repo.OsigurajKatalog(ctxRaz))

	teren, err := repo.StanjeNaTerenu(ctxRaz, "", "B")
	nuzno(t, err)
	if len(teren) != 2 {
		t.Fatalf("stanje na terenu: %+v", teren)
	}
	var dionica, mjesto *models.Stanje
	for i := range teren {
		if teren[i].SectionCode == osnovaDionica {
			dionica = &teren[i]
		} else {
			mjesto = &teren[i]
		}
	}
	if dionica == nil || dionica.AreaID != 16 || dionica.AreaName != "Područje 16" || dionica.Kolicina != 3 {
		t.Errorf("dionica na terenu: %+v", dionica)
	}
	if mjesto == nil || mjesto.AreaID != 0 || mjesto.Mjesto != "Kod mosta" || mjesto.Kolicina != 1 {
		t.Errorf("mjesto bez dionice: %+v", mjesto)
	}
}

// Potrebe iz starih inventura prelaze u zasebnu evidenciju jednom; stanje
// na 31. prosinca nosi potrebe iduće godine.
func TestOsigurajKatalogPrenosiPotrebeIzInventura(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewMtsRepository(n.db, n.rec)
	nuzno(t, repo.SaveVrsta(ctxRaz, &models.VrstaSredstva{ID: "lopata", Grupa: models.GrupaAlat, Naziv: "Lopata", Jedinica: "kom", Aktivna: true}))
	sk := &models.Skladiste{Sektor: "B", AreaID: 16, Naziv: "Skladište Ribnjak", Aktivno: true}
	nuzno(t, repo.SaveSkladiste(ctxRaz, sk))
	nuzno(t, repo.SavePopis(ctxRaz, &models.Popis{SkladisteID: sk.ID, Sektor: "B", Dan: danMts(2025, 12, 31), Godina: 2025,
		Izradio: "Petra Perić", IzradenoAt: danMts(2025, 12, 31),
		Stavke: []models.PopisnaStavka{
			{VrstaID: "lopata", Potrebno: 4, Knjizno: 10, Utvrdjeno: 10},
			{VrstaID: "lopata", Oblik: models.OblikNeispravno, Potrebno: 2},
			{VrstaID: "krampa", Utvrdjeno: 3},
		}}))
	// inventura bez potreba ne daje ništa
	nuzno(t, repo.SavePopis(ctxRaz, &models.Popis{SkladisteID: sk.ID, Sektor: "B", Dan: danMts(2026, 6, 30), Godina: 2026,
		Izradio: "Petra Perić", IzradenoAt: danMts(2026, 6, 30), Stavke: []models.PopisnaStavka{{VrstaID: "lopata", Utvrdjeno: 9}}}))

	nuzno(t, repo.OsigurajKatalog(ctxRaz))
	potrebe, err := repo.Potrebe(ctxRaz, "B", sk.ID, 2026)
	nuzno(t, err)
	if len(potrebe) != 1 {
		t.Fatalf("potrebe: %+v", potrebe)
	}
	p := potrebe[0]
	if p.VrstaID != "lopata" || p.Kolicina != 6 || p.Godina != 2026 || p.UserName != "Petra Perić" ||
		p.Napomena != "iz inventure na dan 31.12.2025." || p.ID != models.PotrebaID(sk.ID, 2026, "lopata") {
		t.Errorf("prenesena potreba: %+v", p)
	}

	// drugi put se ne prenosi ništa, ni kad je potreba u međuvremenu ispravljena
	p.Kolicina = 1
	nuzno(t, repo.SavePotrebe(ctxRaz, []models.Potreba{p}))
	nuzno(t, repo.OsigurajKatalog(ctxRaz))
	potrebe, _ = repo.Potrebe(ctxRaz, "", "", 0) // probni podaci su ispravni; kvar bi pokazala provjera niže
	if len(potrebe) != 1 || potrebe[0].Kolicina != 1 {
		t.Errorf("ponovni prijenos: %+v", potrebe)
	}
	if drugi, err := repo.Potrebe(ctxRaz, "A", "", 0); err != nil || len(drugi) != 0 {
		t.Errorf("potrebe drugog sektora: %+v", drugi)
	}
}

func TestListSkladistaIBroj(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewMtsRepository(n.db, n.rec)
	if _, err := n.db.Exec(`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (17, 'B', 'Područje 17', 'VGI')`); err != nil {
		t.Fatal(err)
	}
	for _, s := range []*models.Skladiste{
		{Sektor: "B", AreaID: 16, Naziv: "zapadno", Aktivno: true},
		{Sektor: "B", AreaID: 16, Naziv: "Žitnjak centralno", Centralno: true, Aktivno: true},
		{Sektor: "B", AreaID: 16, Naziv: "Istočno", Aktivno: true},
		{Sektor: "B", AreaID: 16, Naziv: "bara", Aktivno: true},
		{Sektor: "B", AreaID: 17, Naziv: "Ugašeno", Aktivno: false},
		{Sektor: "B", AreaID: 17, Naziv: "Darda", Aktivno: true},
	} {
		nuzno(t, repo.SaveSkladiste(ctxRaz, s))
	}

	sva, err := repo.ListSkladista(ctxRaz, "", 0, false)
	nuzno(t, err)
	imena := func(ss []models.Skladiste) []string {
		var out []string
		for _, s := range ss {
			out = append(out, s.Naziv)
		}
		return out
	}
	// područje, centralno prvo (iako je abecedno zadnje), pa naziv bez
	// obzira na velika slova („bara” prije „Istočno”)
	want := []string{"Žitnjak centralno", "bara", "Istočno", "zapadno", "Darda"}
	if got := imena(sva); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("redoslijed skladišta: %v, očekivano %v", got, want)
	}
	if sva[0].AreaName != "Područje 16" || !sva[0].Centralno {
		t.Errorf("centralno skladište: %+v", sva[0])
	}
	if s, err := repo.ListSkladista(ctxRaz, "B", 17, true); err != nil || len(s) != 2 {
		t.Errorf("područje 17 s ugašenima: %v", imena(s))
	}
	if s, err := repo.ListSkladista(ctxRaz, "B", 17, false); err != nil || len(s) != 1 || s[0].Naziv != "Darda" {
		t.Errorf("područje 17 bez ugašenih: %v", imena(s))
	}
	if s, err := repo.ListSkladista(ctxRaz, "A", 0, true); err != nil || len(s) != 0 {
		t.Errorf("drugi sektor: %v", imena(s))
	}
	if b, err := repo.BrojSkladista(ctxRaz); err != nil || b != 5 {
		t.Errorf("broj aktivnih skladišta: %d, %v", b, err)
	}
	if s, err := repo.GetSkladiste(ctxRaz, "nema"); err != nil || s != nil {
		t.Errorf("nepostojeće skladište: %v, %v", s, err)
	}
}

func TestListPrometIStanjePoFiltrima(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewMtsRepository(n.db, n.rec)
	nuzno(t, repo.SaveVrsta(ctxRaz, &models.VrstaSredstva{ID: "lopata", Grupa: models.GrupaAlat, Naziv: "Lopata", Jedinica: "kom", Aktivna: true}))
	a := &models.Skladiste{Sektor: "B", AreaID: 16, Naziv: "Ribnjak", Aktivno: true}
	b := &models.Skladiste{Sektor: "A", AreaID: 16, Naziv: "Darda", Aktivno: true}
	nuzno(t, repo.SaveSkladiste(ctxRaz, a))
	nuzno(t, repo.SaveSkladiste(ctxRaz, b))
	nuzno(t, repo.SavePromet(ctxRaz, []models.Promet{
		{Datum: danMts(2026, 1, 10), VrstaID: "lopata", Kolicina: 10, Vrsta: models.PrometPocetno, Sektor: "B", SkladisteID: a.ID},
		{Datum: danMts(2026, 2, 10), VrstaID: "lopata", Kolicina: -4, Vrsta: models.PrometIzdano, Sektor: "B", SkladisteID: a.ID, VezaID: "potez-1", JournalID: osnovaDnevnik},
		{Datum: danMts(2026, 2, 10), VrstaID: "lopata", Kolicina: 4, Vrsta: models.PrometIzdano, Sektor: "B", Mjesto: "Nasip", VezaID: "potez-1", JournalID: osnovaDnevnik},
		{Datum: danMts(2026, 3, 10), VrstaID: "vrece-50x80", Oblik: models.OblikPrazno, Kolicina: 100, Vrsta: models.PrometPrimka, Sektor: "A", SkladisteID: b.ID},
	}))

	sve, err := repo.ListPromet(ctxRaz, FiltarPrometa{})
	nuzno(t, err)
	if len(sve) != 4 || !sve[0].Datum.Equal(danMts(2026, 3, 10)) || !sve[3].Datum.Equal(danMts(2026, 1, 10)) {
		t.Fatalf("knjiga prometa, najnoviji prvi: %+v", sve)
	}
	if sve[3].VrstaNaziv != "Lopata" || sve[3].Jedinica != "kom" || sve[3].SkladisteNaziv != "Ribnjak" {
		t.Errorf("izvedena polja: %+v", sve[3])
	}
	od, do := danMts(2026, 2, 1), danMts(2026, 2, 28)
	for ime, slucaj := range map[string]struct {
		f    FiltarPrometa
		broj int
	}{
		"skladište": {FiltarPrometa{SkladisteID: a.ID}, 2},
		"sektor":    {FiltarPrometa{Sektor: "B"}, 3},
		"vrsta":     {FiltarPrometa{VrstaID: "vrece-50x80"}, 1},
		"obrana":    {FiltarPrometa{JournalID: osnovaDnevnik}, 2},
		"potez":     {FiltarPrometa{VezaID: "potez-1"}, 2},
		"razdoblje": {FiltarPrometa{Od: &od, Do: &do}, 2},
		"od":        {FiltarPrometa{Od: &od}, 3},
		"granica":   {FiltarPrometa{Limit: 1}, 1},
	} {
		redci, err := repo.ListPromet(ctxRaz, slucaj.f)
		nuzno(t, err)
		if len(redci) != slucaj.broj {
			t.Errorf("%s: %d redaka, očekivano %d", ime, len(redci), slucaj.broj)
		}
	}

	// stanje broji samo skladišta; teren je odvojen
	st, err := repo.Stanje(ctxRaz, "", "", nil)
	nuzno(t, err)
	if len(st) != 2 {
		t.Fatalf("stanje svih skladišta: %+v", st)
	}
	st, _ = repo.Stanje(ctxRaz, a.ID, "B", nil) // probni podaci su ispravni; kvar bi pokazala provjera niže
	if len(st) != 1 || st[0].Kolicina != 6 {
		t.Errorf("stanje Ribnjaka: %+v", st)
	}
	sijecanj := danMts(2026, 1, 31)
	st, _ = repo.Stanje(ctxRaz, "", "B", &sijecanj) // probni podaci su ispravni; kvar bi pokazala provjera niže
	if len(st) != 1 || st[0].Kolicina != 10 {
		t.Errorf("stanje na 31.1.: %+v", st)
	}
	teren, err := repo.StanjeNaTerenu(ctxRaz, osnovaDnevnik, "")
	nuzno(t, err)
	if len(teren) != 1 || teren[0].Kolicina != 4 || teren[0].Mjesto != "Nasip" {
		t.Errorf("teren obrane: %+v", teren)
	}
	if teren, err := repo.StanjeNaTerenu(ctxRaz, "druga-obrana", ""); err != nil || len(teren) != 0 {
		t.Errorf("teren druge obrane: %+v", teren)
	}
}

func TestPopisiPoDanuIFiltrima(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewMtsRepository(n.db, n.rec)
	a := &models.Skladiste{Sektor: "B", AreaID: 16, Naziv: "Ribnjak", Aktivno: true}
	b := &models.Skladiste{Sektor: "A", AreaID: 16, Naziv: "Darda", Aktivno: true}
	nuzno(t, repo.SaveSkladiste(ctxRaz, a))
	nuzno(t, repo.SaveSkladiste(ctxRaz, b))
	for _, p := range []*models.Popis{
		{SkladisteID: a.ID, Sektor: "B", Dan: danMts(2025, 12, 31), Godina: 2025, IzradenoAt: danMts(2025, 12, 31)},
		{SkladisteID: a.ID, Sektor: "B", Dan: danMts(2026, 6, 30), Godina: 2026, IzradenoAt: danMts(2026, 6, 30)},
		{SkladisteID: b.ID, Sektor: "A", Dan: danMts(2026, 6, 30), Godina: 2026, IzradenoAt: danMts(2026, 6, 30)},
	} {
		nuzno(t, repo.SavePopis(ctxRaz, p))
	}

	p, err := repo.PopisZaDan(ctxRaz, a.ID, danMts(2026, 6, 30))
	nuzno(t, err)
	if p == nil || p.Godina != 2026 || p.SkladisteNaziv != "Ribnjak" || p.AreaID != 16 || !p.Dan.Equal(danMts(2026, 6, 30)) {
		t.Fatalf("popis na dan: %+v", p)
	}
	if p, err := repo.PopisZaDan(ctxRaz, a.ID, danMts(2026, 6, 29)); err != nil || p != nil {
		t.Errorf("popis na dan bez popisa: %v, %v", p, err)
	}
	if p, err := repo.GetPopis(ctxRaz, "nema"); err != nil || p != nil {
		t.Errorf("nepostojeći popis: %v, %v", p, err)
	}

	sve, err := repo.ListPopisi(ctxRaz, "", "", 0)
	nuzno(t, err)
	if len(sve) != 3 || !sve[0].Dan.Equal(danMts(2026, 6, 30)) || !sve[2].Dan.Equal(danMts(2025, 12, 31)) {
		t.Fatalf("svi popisi, najnoviji dan prvi: %+v", sve)
	}
	if s, err := repo.ListPopisi(ctxRaz, "B", "", 0); err != nil || len(s) != 2 {
		t.Errorf("popisi sektora B: %d", len(s))
	}
	if s, err := repo.ListPopisi(ctxRaz, "", a.ID, 2025); err != nil || len(s) != 1 || s[0].Godina != 2025 {
		t.Errorf("popis Ribnjaka za 2025.: %+v", s)
	}
}

func TestUpotrebaIArhiviranjeVrste(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewMtsRepository(n.db, n.rec)
	v := &models.VrstaSredstva{ID: "lopata", Grupa: models.GrupaAlat, Naziv: "Lopata", Jedinica: "kom", Aktivna: true}
	nuzno(t, repo.SaveVrsta(ctxRaz, v))
	sk := &models.Skladiste{Sektor: "B", AreaID: 16, Naziv: "Ribnjak", Aktivno: true}
	nuzno(t, repo.SaveSkladiste(ctxRaz, sk))

	if pr, po, err := repo.UpotrebaVrste(ctxRaz, "lopata"); err != nil || pr != 0 || po != 0 {
		t.Fatalf("neupotrijebljena vrsta: %d, %d, %v", pr, po, err)
	}
	nuzno(t, repo.SavePromet(ctxRaz, []models.Promet{
		{Datum: danMts(2026, 1, 10), VrstaID: "lopata", Kolicina: 2, Vrsta: models.PrometPocetno, Sektor: "B", SkladisteID: sk.ID},
		{Datum: danMts(2026, 1, 11), VrstaID: "lopata", Kolicina: 1, Vrsta: models.PrometPrimka, Sektor: "B", SkladisteID: sk.ID},
	}))
	for _, p := range []*models.Popis{
		// prazna stavka se ne broji
		{SkladisteID: sk.ID, Sektor: "B", Dan: danMts(2026, 1, 1), Godina: 2026, Stavke: []models.PopisnaStavka{{VrstaID: "lopata"}}},
		{SkladisteID: sk.ID, Sektor: "B", Dan: danMts(2026, 2, 1), Godina: 2026, Stavke: []models.PopisnaStavka{{VrstaID: "lopata", Knjizno: 3}}},
		{SkladisteID: sk.ID, Sektor: "B", Dan: danMts(2026, 3, 1), Godina: 2026, Stavke: []models.PopisnaStavka{{VrstaID: "lopata", Potrebno: 1}, {VrstaID: "lopata", Oblik: models.OblikNeispravno, Utvrdjeno: 1}}},
		// slična oznaka nije ista vrsta
		{SkladisteID: sk.ID, Sektor: "B", Dan: danMts(2026, 4, 1), Godina: 2026, Stavke: []models.PopisnaStavka{{VrstaID: "lopata-mala", Utvrdjeno: 5}}},
	} {
		nuzno(t, repo.SavePopis(ctxRaz, p))
	}
	pr, po, err := repo.UpotrebaVrste(ctxRaz, "lopata")
	nuzno(t, err)
	if pr != 2 || po != 2 {
		t.Errorf("upotreba lopate: prometa %d, popisa %d", pr, po)
	}

	nuzno(t, repo.ArhivirajVrstu(ctxRaz, v))
	if g, err := repo.GetVrsta(ctxRaz, "lopata"); err != nil || g != nil {
		t.Error("vrsta je ostala u katalogu")
	}
	zadnja, err := n.rec.Latest(ctxRaz, EntityMtsVrste, "lopata")
	nuzno(t, err)
	if zadnja == nil || !zadnja.Archived {
		t.Errorf("u knjizi vrsta nije arhivirana: %+v", zadnja)
	}
}

func TestArhivirajPopisSamoNacrt(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewMtsRepository(n.db, n.rec)
	sk := &models.Skladiste{Sektor: "B", AreaID: 16, Naziv: "Ribnjak", Aktivno: true}
	nuzno(t, repo.SaveSkladiste(ctxRaz, sk))
	zakljuceno := time.Date(2026, 10, 1, 9, 0, 0, 0, models.Zagreb)
	zakljucen := &models.Popis{SkladisteID: sk.ID, Sektor: "B", Dan: danMts(2026, 9, 30), Godina: 2026, ZakljucenoAt: &zakljuceno}
	nacrt := &models.Popis{SkladisteID: sk.ID, Sektor: "B", Dan: danMts(2026, 10, 15), Godina: 2026}
	nuzno(t, repo.SavePopis(ctxRaz, zakljucen))
	nuzno(t, repo.SavePopis(ctxRaz, nacrt))

	if err := repo.ArhivirajPopis(ctxRaz, zakljucen); err == nil {
		t.Error("zaključeni popis je maknut")
	}
	if p, err := repo.GetPopis(ctxRaz, zakljucen.ID); err != nil || p == nil || !p.Zakljucen() {
		t.Errorf("zaključeni popis: %+v", p)
	}

	nuzno(t, repo.ArhivirajPopis(ctxRaz, nacrt))
	if p, err := repo.GetPopis(ctxRaz, nacrt.ID); err != nil || p != nil {
		t.Error("nacrt popisa je ostao")
	}
	zadnja, err := n.rec.Latest(ctxRaz, EntityMtsPopisi, nacrt.ID)
	nuzno(t, err)
	if zadnja == nil || !zadnja.Archived {
		t.Errorf("u knjizi nacrt nije arhiviran: %+v", zadnja)
	}
}

func TestMtsZatvorenaBaza(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewMtsRepository(n.db, n.rec)
	n.db.Close()

	if _, err := repo.ListVrste(ctxRaz, true); err == nil {
		t.Error("ListVrste bez baze")
	}
	if _, err := repo.GetVrsta(ctxRaz, "x"); err == nil {
		t.Error("GetVrsta bez baze")
	}
	if err := repo.SaveVrsta(ctxRaz, &models.VrstaSredstva{ID: "x"}); err == nil {
		t.Error("SaveVrsta bez baze")
	}
	if err := repo.OsigurajKatalog(ctxRaz); err == nil {
		t.Error("OsigurajKatalog bez baze")
	}
	if _, err := repo.ListSkladista(ctxRaz, "", 0, true); err == nil {
		t.Error("ListSkladista bez baze")
	}
	if _, err := repo.GetSkladiste(ctxRaz, "x"); err == nil {
		t.Error("GetSkladiste bez baze")
	}
	if err := repo.SaveSkladiste(ctxRaz, &models.Skladiste{}); err == nil {
		t.Error("SaveSkladiste bez baze")
	}
	if err := repo.SavePromet(ctxRaz, []models.Promet{{}}); err == nil {
		t.Error("SavePromet bez baze")
	}
	if _, err := repo.ListPromet(ctxRaz, FiltarPrometa{}); err == nil {
		t.Error("ListPromet bez baze")
	}
	if _, err := repo.Stanje(ctxRaz, "", "", nil); err == nil {
		t.Error("Stanje bez baze")
	}
	if _, err := repo.StanjeNaTerenu(ctxRaz, "", ""); err == nil {
		t.Error("StanjeNaTerenu bez baze")
	}
	if err := repo.SavePopis(ctxRaz, &models.Popis{}); err == nil {
		t.Error("SavePopis bez baze")
	}
	if _, err := repo.ListPopisi(ctxRaz, "", "", 0); err == nil {
		t.Error("ListPopisi bez baze")
	}
	if _, err := repo.BrojSkladista(ctxRaz); err == nil {
		t.Error("BrojSkladista bez baze")
	}
	if _, _, err := repo.UpotrebaVrste(ctxRaz, "x"); err == nil {
		t.Error("UpotrebaVrste bez baze")
	}
	if err := repo.ArhivirajVrstu(ctxRaz, &models.VrstaSredstva{ID: "x"}); err == nil {
		t.Error("ArhivirajVrstu bez baze")
	}
	if err := repo.ArhivirajPromet(ctxRaz, nil); err == nil {
		t.Error("ArhivirajPromet bez baze")
	}
	if err := repo.ArhivirajPopis(ctxRaz, &models.Popis{ID: "x"}); err == nil {
		t.Error("ArhivirajPopis bez baze")
	}
	if _, err := repo.Potrebe(ctxRaz, "", "", 0); err == nil {
		t.Error("Potrebe bez baze")
	}
	if err := repo.SavePotrebe(ctxRaz, []models.Potreba{{}}); err == nil {
		t.Error("SavePotrebe bez baze")
	}
	// prazan upis ne dira bazu
	if err := repo.SavePotrebe(ctxRaz, nil); err != nil {
		t.Errorf("prazne potrebe: %v", err)
	}
	if err := repo.SavePromet(ctxRaz, nil); err != nil {
		t.Errorf("prazan promet: %v", err)
	}
}
