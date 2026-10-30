package service

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// danMts je stalni dan u prošlosti: promet i popis se ne smiju upisati
// unaprijed, a test ne smije ovisiti o današnjem danu
func danMts(godina int, mjesec time.Month, dan int) time.Time {
	return time.Date(godina, mjesec, dan, 10, 0, 0, 0, models.Zagreb)
}

func TestMtsPravoGledanja(t *testing.T) {
	s, _, _, _, _ := pripremiMts(t)
	if s.SmijeVidjeti(nil) {
		t.Error("bez prijave se sredstva ne vide")
	}
	if !s.SmijeVidjeti(&models.UserPermissions{AllowedSectors: map[string]bool{"F": true}}) {
		t.Error("sredstva vidi cijela obrana, i drugi sektor")
	}
}

func TestMtsSpremiVrstuOdbija(t *testing.T) {
	s, _, ctx, _, _ := pripremiMts(t)
	slucajevi := []struct {
		naziv  string
		v      models.VrstaSredstva
		poruka string
	}{
		{"bez naziva", models.VrstaSredstva{Naziv: "  ", Jedinica: "kom", Grupa: models.GrupaAlat}, "mora imati naziv"},
		{"bez jedinice", models.VrstaSredstva{Naziv: "Kramp", Jedinica: " ", Grupa: models.GrupaAlat}, "jedinicu mjere"},
		{"nepoznata skupina", models.VrstaSredstva{Naziv: "Kramp", Jedinica: "kom", Grupa: "IGRACKE"}, "nepoznata skupina"},
		{"naziv bez slova", models.VrstaSredstva{Naziv: "—", Jedinica: "kom", Grupa: models.GrupaAlat}, "upišite je ručno"},
		{"oznaka zauzeta", models.VrstaSredstva{Naziv: "Lopata", Jedinica: "kom", Grupa: models.GrupaAlat}, "već postoji"},
	}
	for _, sl := range slucajevi {
		v := sl.v
		if err := s.SpremiVrstu(ctx, &v); err == nil || !strings.Contains(err.Error(), sl.poruka) {
			t.Errorf("%s: očekivano %q, dobiveno %v", sl.naziv, sl.poruka, err)
		}
	}
}

func TestMtsUgasenaVrstaSamoUPunomKatalogu(t *testing.T) {
	s, _, ctx, _, _ := pripremiMts(t)
	v := &models.VrstaSredstva{Grupa: models.GrupaAlat, Naziv: "Kliješta stara", Jedinica: "kom", Aktivna: false, Redoslijed: 98}
	if err := s.SpremiVrstu(ctx, v); err != nil {
		t.Fatal(err)
	}
	ima := func(popis []models.VrstaSredstva) bool {
		for _, x := range popis {
			if x.ID == v.ID {
				return true
			}
		}
		return false
	}
	aktivne, err := s.Vrste(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sve, err := s.SveVrste(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ima(aktivne) {
		t.Error("ugašena vrsta je u katalogu aktivnih")
	}
	if !ima(sve) || len(sve) != len(aktivne)+1 {
		t.Errorf("puni katalog: %d vrsta, aktivnih %d", len(sve), len(aktivne))
	}
	g, err := s.Vrsta(ctx, v.ID)
	if err != nil || g == nil || g.Naziv != "Kliješta stara" || g.Aktivna {
		t.Errorf("dohvat ugašene vrste: %+v, %v", g, err)
	}
	if g, err := s.Vrsta(ctx, "nema-takve"); err != nil || g != nil {
		t.Errorf("nepostojeća vrsta: %+v, %v", g, err)
	}
}

func TestMtsSpremiSkladisteOdbija(t *testing.T) {
	s, _, ctx, _, uprava := pripremiMts(t)
	if err := s.SpremiSkladiste(ctx, uprava, &models.Skladiste{Sektor: "B", AreaID: 34, Naziv: "  "}); err == nil || !strings.Contains(err.Error(), "naziv") {
		t.Errorf("bez naziva: %v", err)
	}
	if err := s.SpremiSkladiste(ctx, uprava, &models.Skladiste{AreaID: 34, Naziv: "Skladište"}); err == nil || !strings.Contains(err.Error(), "sektoru") {
		t.Errorf("bez sektora: %v", err)
	}
	if err := s.SpremiSkladiste(ctx, uprava, &models.Skladiste{Sektor: "F", AreaID: 40, Naziv: "Tuđe"}); err == nil || !strings.Contains(err.Error(), "uprava") {
		t.Errorf("tuđi sektor: %v", err)
	}
}

func TestMtsBrojSkladistaBrojiAktivna(t *testing.T) {
	s, _, ctx, _, uprava := pripremiMts(t)
	n, err := s.BrojSkladista(ctx)
	if err != nil || n != 1 {
		t.Fatalf("broj skladišta: %d, %v", n, err)
	}
	ugaseno := &models.Skladiste{Sektor: "B", AreaID: 34, Naziv: "Staro skladište", Aktivno: false}
	if err := s.SpremiSkladiste(ctx, uprava, ugaseno); err != nil {
		t.Fatal(err)
	}
	drugo := &models.Skladiste{Sektor: "B", AreaID: 34, Naziv: "Skladište Bilje", Aktivno: true}
	if err := s.SpremiSkladiste(ctx, uprava, drugo); err != nil {
		t.Fatal(err)
	}
	if n, err := s.BrojSkladista(ctx); err != nil || n != 2 {
		t.Errorf("broj aktivnih skladišta: %d, očekivano 2", n)
	}
}

// Kad katalog vrsti makne oblike, promet u starom obliku ne smije nestati
// iz stanja
func TestMtsStanjeCuvaOblikKojegKatalogViseNema(t *testing.T) {
	s, sk, ctx, u, uprava := pripremiMts(t)
	vrece := "vrece-50x80"
	if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPocetno, SkladisteID: sk.ID, VrstaID: vrece,
		Oblik: models.OblikPrazno, Kolicina: 700, Datum: danMts(2025, 3, 1)}); err != nil {
		t.Fatal(err)
	}
	v, err := s.Vrsta(ctx, vrece)
	if err != nil || v == nil {
		t.Fatal(err)
	}
	v.Oblici = nil
	if err := s.SpremiVrstu(ctx, v); err != nil {
		t.Fatal(err)
	}
	st, err := s.StanjeSkladista(ctx, sk.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, sv := range st {
		if sv.Vrsta.ID != vrece {
			continue
		}
		if sv.Ukupno != 700 || sv.PoOblicima[models.OblikPrazno] != 700 || sv.PoOblicima[models.OblikOsnovni] != 0 {
			t.Errorf("stanje vreća bez oblika u katalogu: ukupno %v, po oblicima %v", sv.Ukupno, sv.PoOblicima)
		}
		return
	}
	t.Fatal("vreće nisu u stanju")
}

func TestMtsGdjeImaPreskaceNuleIRedaPoNazivu(t *testing.T) {
	s, osijek, ctx, u, uprava := pripremiMts(t)
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	bilje := &models.Skladiste{Sektor: "B", AreaID: 34, Naziv: "Bilje", Aktivno: true}
	darda := &models.Skladiste{Sektor: "B", AreaID: 34, Naziv: "Darda", Aktivno: true}
	for _, x := range []*models.Skladiste{bilje, darda} {
		if err := s.SpremiSkladiste(ctx, uprava, x); err != nil {
			t.Fatal(err)
		}
	}
	dan := danMts(2025, 4, 1)
	for _, z := range []Zahvat{
		{Vrsta: models.PrometPocetno, SkladisteID: darda.ID, VrstaID: "lopata", Kolicina: 10, Datum: dan},
		{Vrsta: models.PrometPocetno, SkladisteID: bilje.ID, VrstaID: "lopata", Kolicina: 10, Datum: dan},
		{Vrsta: models.PrometPocetno, SkladisteID: osijek.ID, VrstaID: "lopata", Kolicina: 4, Datum: dan},
		// Osijek sve prenese u Bilje: ostaje mu nula, pa ga nema u popisu
		{Vrsta: models.PrometPrijenos, SkladisteID: osijek.ID, NaSkladisteID: bilje.ID, VrstaID: "lopata", Kolicina: 4, Datum: dan},
		{Vrsta: models.PrometPrijenos, SkladisteID: bilje.ID, NaSkladisteID: darda.ID, VrstaID: "lopata", Kolicina: 4, Datum: dan},
	} {
		if _, err := s.Provedi(ctx, u, admin, z); err != nil {
			t.Fatal(err)
		}
	}
	gdje, err := s.GdjeIma(ctx, "lopata")
	if err != nil {
		t.Fatal(err)
	}
	if len(gdje) != 2 {
		t.Fatalf("gdje ima: %+v", gdje)
	}
	if gdje[0].Skladiste.Naziv != "Darda" || gdje[0].Kolicina != 14 || gdje[1].Skladiste.Naziv != "Bilje" || gdje[1].Kolicina != 10 {
		t.Errorf("redoslijed: %s %v, %s %v", gdje[0].Skladiste.Naziv, gdje[0].Kolicina, gdje[1].Skladiste.Naziv, gdje[1].Kolicina)
	}

	// jednake količine: redom naziva
	if _, err := s.Provedi(ctx, u, admin, Zahvat{Vrsta: models.PrometPrijenos, SkladisteID: darda.ID, NaSkladisteID: bilje.ID,
		VrstaID: "lopata", Kolicina: 2, Datum: dan}); err != nil {
		t.Fatal(err)
	}
	gdje, _ = s.GdjeIma(ctx, "lopata") // probni podaci su ispravni; kvar bi pokazala provjera niže
	if len(gdje) != 2 || gdje[0].Skladiste.Naziv != "Bilje" || gdje[1].Skladiste.Naziv != "Darda" || gdje[0].Kolicina != 12 {
		t.Errorf("jednake količine: %+v", gdje)
	}
	if gdje, err := s.GdjeIma(ctx, "kramp-nema"); err != nil || len(gdje) != 0 {
		t.Errorf("vrsta koje nigdje nema: %+v", gdje)
	}
}

func TestKolicinaTekst(t *testing.T) {
	for v, ocekivano := range map[float64]string{0: "0", 12: "12", 2.5: "2,5", 0.125: "0,125", 1.0004: "1", -3.75: "-3,75"} {
		if got := kolicinaTekst(v); got != ocekivano {
			t.Errorf("kolicinaTekst(%v) = %q, očekivano %q", v, got, ocekivano)
		}
	}
}

func TestMtsPredlozakPopisa(t *testing.T) {
	s, sk, ctx, u, uprava := pripremiMts(t)
	if _, err := s.PredlozakPopisa(ctx, "nema-skladista", danMts(2025, 6, 1)); err == nil || !strings.Contains(err.Error(), "nepoznato") {
		t.Errorf("nepoznato skladište: %v", err)
	}
	dan := danMts(2025, 6, 30)
	if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPocetno, SkladisteID: sk.ID, VrstaID: "lopata", Kolicina: 30, Datum: danMts(2025, 6, 1)}); err != nil {
		t.Fatal(err)
	}
	// promet poslije dana popisa ne ulazi u knjižno stanje predloška
	if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPrimka, SkladisteID: sk.ID, VrstaID: "lopata", Kolicina: 5, Datum: danMts(2025, 7, 2)}); err != nil {
		t.Fatal(err)
	}
	p, err := s.PredlozakPopisa(ctx, sk.ID, dan)
	if err != nil {
		t.Fatal(err)
	}
	if p.Godina != 2025 || p.Sektor != "B" || p.SkladisteNaziv != sk.Naziv || !p.Dan.Equal(pocetakDana(dan)) {
		t.Errorf("zaglavlje predloška: %+v", p)
	}
	var lopata *models.PopisnaStavka
	for i := range p.Stavke {
		if p.Stavke[i].VrstaID == "lopata" {
			lopata = &p.Stavke[i]
		}
	}
	if lopata == nil || lopata.Knjizno != 30 || lopata.VrstaNaziv == "" || lopata.Jedinica == "" {
		t.Fatalf("stavka lopata: %+v", lopata)
	}
	lopata.Utvrdjeno = 28
	if err := s.SpremiPopis(ctx, u, uprava, p); err != nil {
		t.Fatal(err)
	}

	// drugi put isti dan vraća spremljeni popis, s nazivima vrsta
	drugi, err := s.PredlozakPopisa(ctx, sk.ID, dan)
	if err != nil {
		t.Fatal(err)
	}
	if drugi.ID != p.ID {
		t.Fatalf("predložak nije vratio spremljeni popis: %q, a spremljen %q", drugi.ID, p.ID)
	}
	for _, st := range drugi.Stavke {
		if st.VrstaID == "lopata" && (st.Utvrdjeno != 28 || st.VrstaNaziv == "" || st.Jedinica == "") {
			t.Errorf("spremljena stavka: %+v", st)
		}
	}
}

func TestMtsSpremiPopisOdbija(t *testing.T) {
	s, sk, ctx, u, uprava := pripremiMts(t)
	dan := danMts(2025, 2, 1)
	if err := s.SpremiPopis(ctx, nil, uprava, &models.Popis{SkladisteID: sk.ID, Dan: dan}); err == nil || !strings.Contains(err.Error(), "prijavu") {
		t.Errorf("bez prijave: %v", err)
	}
	if err := s.SpremiPopis(ctx, u, uprava, &models.Popis{SkladisteID: "nema", Dan: dan}); err == nil || !strings.Contains(err.Error(), "nepoznato skladište") {
		t.Errorf("nepoznato skladište: %v", err)
	}
	if err := s.SpremiPopis(ctx, u, uprava, &models.Popis{ID: uuid.NewString(), SkladisteID: sk.ID, Dan: dan}); err == nil || !strings.Contains(err.Error(), "nije pronađen") {
		t.Errorf("nepostojeći popis: %v", err)
	}
	vodocuvar := &models.UserPermissions{AllowedAreas: map[int]bool{34: true}}
	if err := s.SpremiPopis(ctx, u, vodocuvar, &models.Popis{SkladisteID: sk.ID, Dan: dan}); err == nil || !strings.Contains(err.Error(), "uprava") {
		t.Errorf("bez prava: %v", err)
	}
	if err := s.SpremiPopis(ctx, u, uprava, &models.Popis{SkladisteID: sk.ID}); err == nil || !strings.Contains(err.Error(), "mora imati dan") {
		t.Errorf("bez dana: %v", err)
	}
	// SpremiPopis uspoređuje s pravim satom; mjesec unaprijed je uvijek budućnost
	unaprijed := pocetakDana(time.Now().In(models.Zagreb)).AddDate(0, 1, 0)
	if err := s.SpremiPopis(ctx, u, uprava, &models.Popis{SkladisteID: sk.ID, Dan: unaprijed}); err == nil || !strings.Contains(err.Error(), "ne unaprijed") {
		t.Errorf("popis unaprijed: %v", err)
	}
}

func TestMtsPopisiPoSektoruSkladistuIGodini(t *testing.T) {
	s, sk, ctx, u, uprava := pripremiMts(t)
	for _, dan := range []time.Time{danMts(2024, 12, 31), danMts(2025, 12, 31)} {
		p, err := s.PredlozakPopisa(ctx, sk.ID, dan)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SpremiPopis(ctx, u, uprava, p); err != nil {
			t.Fatal(err)
		}
	}
	svi, err := s.Popisi(ctx, "B", "", 0)
	if err != nil || len(svi) != 2 {
		t.Fatalf("popisi sektora: %d, %v", len(svi), err)
	}
	if svi[0].Godina != 2025 || svi[1].Godina != 2024 {
		t.Errorf("najnoviji popis mora biti prvi: %d, %d", svi[0].Godina, svi[1].Godina)
	}
	if g, err := s.Popisi(ctx, "", sk.ID, 2024); err != nil || len(g) != 1 || g[0].Godina != 2024 {
		t.Errorf("popisi skladišta za 2024.: %+v", g)
	}
	if f, err := s.Popisi(ctx, "F", "", 0); err != nil || len(f) != 0 {
		t.Errorf("sektor F nema popisa: %+v", f)
	}
	if p, err := s.Popis(ctx, uuid.NewString()); err != nil || p != nil {
		t.Errorf("nepostojeći popis: %+v, %v", p, err)
	}
	if err := s.ZakljuciPopis(ctx, u, uprava, uuid.NewString()); err == nil || !strings.Contains(err.Error(), "nije pronađen") {
		t.Errorf("zaključenje nepostojećeg: %v", err)
	}
}

func TestMtsPotrebeOdbijanjaIPreskakanje(t *testing.T) {
	s, sk, ctx, u, uprava := pripremiMts(t)
	if err := s.SpremiPotrebe(ctx, nil, uprava, sk.ID, 2026, nil); err == nil || !strings.Contains(err.Error(), "prijavu") {
		t.Errorf("bez prijave: %v", err)
	}
	if err := s.SpremiPotrebe(ctx, u, uprava, "nema", 2026, nil); err == nil || !strings.Contains(err.Error(), "nepoznato") {
		t.Errorf("nepoznato skladište: %v", err)
	}
	for _, g := range []int{1999, 2201} {
		if err := s.SpremiPotrebe(ctx, u, uprava, sk.ID, g, nil); err == nil || !strings.Contains(err.Error(), "godina") {
			t.Errorf("godina %d: %v", g, err)
		}
	}
	if err := s.SpremiPotrebe(ctx, u, uprava, sk.ID, 2026, []models.Potreba{{VrstaID: "lopata", Kolicina: -1}}); err == nil || !strings.Contains(err.Error(), "negativna") {
		t.Errorf("negativna potreba: %v", err)
	}

	if err := s.SpremiPotrebe(ctx, u, uprava, sk.ID, 2026, []models.Potreba{
		{VrstaID: "lopata", Kolicina: 20, Napomena: " za nasip "},
		{VrstaID: "vrece-50x80", Kolicina: 0}, // nula bez napomene se ne upisuje
	}); err != nil {
		t.Fatal(err)
	}
	pp, err := s.PotrebeSkladista(ctx, sk.ID, 2026)
	if err != nil {
		t.Fatal(err)
	}
	if len(pp) != 1 || pp["lopata"].Kolicina != 20 || pp["lopata"].Napomena != "za nasip" || pp["lopata"].UserName != u.FullName {
		t.Fatalf("potrebe: %+v", pp)
	}
	prije := pp["lopata"].UpdatedAt

	// ista potreba drugi put ne piše novu verziju; druga skladištarka mijenja
	drugi := &models.User{ID: uuid.New(), FullName: "Ivana Probić"}
	if err := s.SpremiPotrebe(ctx, drugi, uprava, sk.ID, 2026, []models.Potreba{{VrstaID: "lopata", Kolicina: 20, Napomena: "za nasip"}}); err != nil {
		t.Fatal(err)
	}
	if pp, err := s.PotrebeSkladista(ctx, sk.ID, 2026); err != nil || pp["lopata"].UserName != u.FullName || !pp["lopata"].UpdatedAt.Equal(prije) {
		t.Errorf("nepromijenjena potreba je prepisana: %+v", pp["lopata"])
	}
	if err := s.SpremiPotrebe(ctx, drugi, uprava, sk.ID, 2026, []models.Potreba{{VrstaID: "lopata", Kolicina: 25, Napomena: "za nasip"}}); err != nil {
		t.Fatal(err)
	}
	if pp, err := s.PotrebeSkladista(ctx, sk.ID, 2026); err != nil || pp["lopata"].Kolicina != 25 || pp["lopata"].UserName != "Ivana Probić" {
		t.Errorf("izmijenjena potreba: %+v", pp["lopata"])
	}

	sektor, err := s.PotrebeSektora(ctx, "B", 2026)
	if err != nil || len(sektor) != 1 || sektor[0].VrstaID != "lopata" {
		t.Errorf("potrebe sektora: %+v, %v", sektor, err)
	}
	if f, err := s.PotrebeSektora(ctx, "F", 2026); err != nil || len(f) != 0 {
		t.Errorf("sektor F nema potreba: %+v", f)
	}
}

// Tablica za Glavni centar: skladište s inventurom daje prebrojano,
// skladište bez nje knjižno stanje; zbroj ide preko svih skladišta
func TestMtsTablicaSektora(t *testing.T) {
	s, osijek, ctx, u, uprava := pripremiMts(t)
	bilje := &models.Skladiste{Sektor: "B", AreaID: 34, Naziv: "Bilje", Aktivno: true}
	darda := &models.Skladiste{Sektor: "B", AreaID: 34, Naziv: "Darda", Aktivno: true}
	for _, x := range []*models.Skladiste{bilje, darda} {
		if err := s.SpremiSkladiste(ctx, uprava, x); err != nil {
			t.Fatal(err)
		}
	}
	dan := danMts(2025, 12, 31)
	for _, z := range []Zahvat{
		{Vrsta: models.PrometPocetno, SkladisteID: osijek.ID, VrstaID: "lopata", Kolicina: 40, Datum: danMts(2025, 5, 1)},
		{Vrsta: models.PrometPocetno, SkladisteID: bilje.ID, VrstaID: "lopata", Kolicina: 12, Datum: danMts(2025, 5, 1)},
		{Vrsta: models.PrometPocetno, SkladisteID: darda.ID, VrstaID: "lopata", Kolicina: 7, Datum: danMts(2025, 5, 1)},
		// poslije dana tablice: ne broji se
		{Vrsta: models.PrometPrimka, SkladisteID: darda.ID, VrstaID: "lopata", Kolicina: 100, Datum: danMts(2026, 1, 5)},
	} {
		if _, err := s.Provedi(ctx, u, uprava, z); err != nil {
			t.Fatal(err)
		}
	}
	// Osijek ima zaključenu inventuru (38), Bilje nacrt (11), Darda ništa
	inventura := func(sk *models.Skladiste, utvrdjeno float64, zakljuci bool) {
		t.Helper()
		p, err := s.PredlozakPopisa(ctx, sk.ID, dan)
		if err != nil {
			t.Fatal(err)
		}
		postaviPrebrojano(t, p, "lopata", models.OblikOsnovni, utvrdjeno)
		if err := s.SpremiPopis(ctx, u, uprava, p); err != nil {
			t.Fatal(err)
		}
		if zakljuci {
			if err := s.ZakljuciPopis(ctx, u, uprava, p.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	inventura(osijek, 38, true)
	inventura(bilje, 11, false)
	// potrebe idu u godinu nabave koja pripada 31.12. — sljedeću
	if err := s.SpremiPotrebe(ctx, u, uprava, darda.ID, 2026, []models.Potreba{{VrstaID: "lopata", Kolicina: 9}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SpremiPotrebe(ctx, u, uprava, osijek.ID, 2026, []models.Potreba{{VrstaID: "lopata", Kolicina: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SpremiPotrebe(ctx, u, uprava, bilje.ID, 2025, []models.Potreba{{VrstaID: "lopata", Kolicina: 50}}); err != nil {
		t.Fatal(err)
	}

	tab, err := s.Tablica(ctx, "B", dan)
	if err != nil {
		t.Fatal(err)
	}
	if tab.Godina != 2026 || len(tab.Skladista) != 3 || len(tab.Vrste) == 0 {
		t.Fatalf("tablica: godina %d, skladišta %d, vrsta %d", tab.Godina, len(tab.Skladista), len(tab.Vrste))
	}
	ocekivano := map[string]struct {
		stanje, potrebe float64
		izvor           string
	}{
		osijek.ID: {38, 1, "inventura zaključena"},
		bilje.ID:  {11, 0, "inventura u nacrtu"},
		darda.ID:  {7, 9, "knjižno stanje"},
	}
	for id, o := range ocekivano {
		if got := tab.StanjeU(id, "lopata"); got != o.stanje {
			t.Errorf("stanje u %s: %v, očekivano %v", id, got, o.stanje)
		}
		if got := tab.PotrebeU(id, "lopata"); got != o.potrebe {
			t.Errorf("potrebe u %s: %v, očekivano %v", id, got, o.potrebe)
		}
		if got := tab.Izvor(id); got != o.izvor {
			t.Errorf("izvor %s: %q, očekivano %q", id, got, o.izvor)
		}
	}
	if got := tab.UkupnoStanje("lopata"); got != 56 {
		t.Errorf("ukupno stanje: %v, očekivano 56", got)
	}
	if got := tab.UkupnoPotrebe("lopata"); got != 10 {
		t.Errorf("ukupno potrebe: %v, očekivano 10", got)
	}
	if tab.UkupnoStanje("kramp-nema") != 0 || tab.StanjeU("nema", "lopata") != 0 || tab.PotrebeU("nema", "lopata") != 0 {
		t.Error("nepoznata vrsta ili skladište moraju dati nulu")
	}

	// tablica jednog skladišta
	jedno, err := s.TablicaSkladista(ctx, darda.ID, dan)
	if err != nil {
		t.Fatal(err)
	}
	if len(jedno.Skladista) != 1 || jedno.Sektor != "B" || jedno.UkupnoStanje("lopata") != 7 {
		t.Errorf("tablica Darde: %d skladišta, sektor %s, stanje %v", len(jedno.Skladista), jedno.Sektor, jedno.UkupnoStanje("lopata"))
	}
	if _, err := s.TablicaSkladista(ctx, "nema", dan); err == nil || !strings.Contains(err.Error(), "nepoznato") {
		t.Errorf("tablica nepoznatog skladišta: %v", err)
	}
	// sektor bez skladišta daje praznu tablicu
	prazna, err := s.Tablica(ctx, "F", dan)
	if err != nil || len(prazna.Skladista) != 0 || prazna.UkupnoStanje("lopata") != 0 {
		t.Errorf("prazna tablica: %+v, %v", prazna, err)
	}
}

func TestMtsObrisiVrstu(t *testing.T) {
	s, sk, ctx, u, uprava := pripremiMts(t)
	if err := s.ObrisiVrstu(ctx, "nema-takve"); err == nil || !strings.Contains(err.Error(), "nije pronađena") {
		t.Errorf("nepostojeća vrsta: %v", err)
	}

	nekoristena := &models.VrstaSredstva{Grupa: models.GrupaAlat, Naziv: "Probni čekić", Jedinica: "kom", Aktivna: true, Redoslijed: 97}
	if err := s.SpremiVrstu(ctx, nekoristena); err != nil {
		t.Fatal(err)
	}
	if err := s.ObrisiVrstu(ctx, nekoristena.ID); err != nil {
		t.Fatalf("brisanje nekorištene: %v", err)
	}
	if v, err := s.Vrsta(ctx, nekoristena.ID); err != nil || v != nil {
		t.Error("nekorištena vrsta nije obrisana")
	}

	// vrsta s prometom se ne briše
	if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPocetno, SkladisteID: sk.ID, VrstaID: "lopata", Kolicina: 3, Datum: danMts(2025, 1, 10)}); err != nil {
		t.Fatal(err)
	}
	err := s.ObrisiVrstu(ctx, "lopata")
	if err == nil || !strings.Contains(err.Error(), "redaka prometa: 1, popisa: 0") || !strings.Contains(err.Error(), "ugasite") {
		t.Errorf("vrsta s prometom: %v", err)
	}

	// vrsta samo na popisu, s količinom, isto se ne briše
	naPopisu := &models.VrstaSredstva{Grupa: models.GrupaAlat, Naziv: "Probne grablje", Jedinica: "kom", Aktivna: true, Redoslijed: 96}
	if err := s.SpremiVrstu(ctx, naPopisu); err != nil {
		t.Fatal(err)
	}
	p, err := s.PredlozakPopisa(ctx, sk.ID, danMts(2025, 1, 20))
	if err != nil {
		t.Fatal(err)
	}
	postaviPrebrojano(t, p, naPopisu.ID, models.OblikOsnovni, 2)
	if err := s.SpremiPopis(ctx, u, uprava, p); err != nil {
		t.Fatal(err)
	}
	if err := s.ObrisiVrstu(ctx, naPopisu.ID); err == nil || !strings.Contains(err.Error(), "popisa: 1") {
		t.Errorf("vrsta na popisu: %v", err)
	}
	if v, err := s.Vrsta(ctx, "lopata"); err != nil || v == nil {
		t.Error("korištena vrsta je obrisana")
	}
}

func TestMtsPrijenosTraziDrugoSkladiste(t *testing.T) {
	s, sk, ctx, u, uprava := pripremiMts(t)
	dan := danMts(2025, 8, 1)
	if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPocetno, SkladisteID: sk.ID, VrstaID: "lopata", Kolicina: 10, Datum: dan}); err != nil {
		t.Fatal(err)
	}
	for naziv, cilj := range map[string]string{"isto skladište": sk.ID, "nepoznato skladište": "nema", "bez odredišta": ""} {
		if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPrijenos, SkladisteID: sk.ID, NaSkladisteID: cilj,
			VrstaID: "lopata", Kolicina: 2, Datum: dan}); err == nil || !strings.Contains(err.Error(), "drugo skladište") {
			t.Errorf("%s: %v", naziv, err)
		}
	}
	drugo := &models.Skladiste{Sektor: "B", AreaID: 34, Naziv: "Bilje", Aktivno: true}
	if err := s.SpremiSkladiste(ctx, uprava, drugo); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPrijenos, SkladisteID: sk.ID, NaSkladisteID: drugo.ID,
		VrstaID: "lopata", Kolicina: 12.5, Datum: dan}); err == nil || !strings.Contains(err.Error(), "u skladištu stoji 10 kom, a skida se 12,5") {
		t.Errorf("prijenos više nego što ima: %v", err)
	}
	if k := kolicinaU(t, s, ctx, sk.ID, "lopata"); k != 10 {
		t.Errorf("odbijeni prijenos promijenio je stanje: %v", k)
	}
}
