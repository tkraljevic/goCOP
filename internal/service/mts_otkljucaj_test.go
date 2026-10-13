package service

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Otključavanje inventure (Odluke (13. 10. 2026.)): zaključena inventura ne
// vidi promet koji je poslije upisan s danom prije nje. Globalni
// administrator je vraća u nacrt; usklađenja koja je zaključenje upisalo
// brišu se, a ponovno zaključenje razliku računa iznova.

var glavniAdmin = &models.UserPermissions{IsGlobalAdmin: true}

// uskladjenjaVrste vraća usklađenja inventure za jednu vrstu
func uskladjenjaVrste(t *testing.T, s *MtsService, popisID, vrstaID string) []models.Promet {
	t.Helper()
	sva, err := s.repo.UskladjenjaPopisa(t.Context(), popisID)
	if err != nil {
		t.Fatal(err)
	}
	var out []models.Promet
	for _, x := range sva {
		if x.VrstaID == vrstaID {
			out = append(out, x)
		}
	}
	return out
}

// postaviPrebrojano upisuje prebrojanu količinu stavke popisa
func postaviPrebrojano(t *testing.T, p *models.Popis, vrstaID, oblik string, k float64) {
	t.Helper()
	for i := range p.Stavke {
		if p.Stavke[i].VrstaID == vrstaID && p.Stavke[i].Oblik == oblik {
			p.Stavke[i].Utvrdjeno = k
			return
		}
	}
	t.Fatalf("popis nema stavku %s/%s", vrstaID, oblik)
}

// Slučaj s kojim je otključavanje nastalo: u inventuri je 0 vreća, a
// poslije se upiše premještaj 9 000 vreća s danom prije inventure. Stanje
// ih pokazuje, inventura ne. Otključavanje, upis 9 000 i ponovno
// zaključenje ne daju usklađenje za vreće, a usklađenje lopata upiše se
// iznova, jednom.
func TestMtsOtkljucanaInventuraRacunaRazlikuIznova(t *testing.T) {
	s, sk, ctx, u, uprava := pripremiMts(t)
	vrece, lopata := "vrece-50x80", "lopata"
	dan := pocetakDana(time.Now().In(models.Zagreb)).AddDate(0, 0, -1)
	bilje := &models.Skladiste{Sektor: "B", AreaID: 34, Naziv: "Skladište Bilje", Aktivno: true}
	if err := s.SpremiSkladiste(ctx, uprava, bilje); err != nil {
		t.Fatal(err)
	}
	for _, z := range []Zahvat{
		{Vrsta: models.PrometPocetno, SkladisteID: sk.ID, VrstaID: lopata, Kolicina: 46, Datum: dan.AddDate(0, 0, -5)},
		{Vrsta: models.PrometPocetno, SkladisteID: bilje.ID, VrstaID: vrece, Oblik: models.OblikPrazno, Kolicina: 9000, Datum: dan.AddDate(0, 0, -10)},
	} {
		if _, err := s.Provedi(ctx, u, uprava, z); err != nil {
			t.Fatal(err)
		}
	}
	p, err := s.PredlozakPopisa(ctx, sk.ID, dan)
	if err != nil {
		t.Fatal(err)
	}
	postaviPrebrojano(t, p, lopata, models.OblikOsnovni, 44)
	if err := s.SpremiPopis(ctx, u, uprava, p); err != nil {
		t.Fatal(err)
	}
	if err := s.ZakljuciPopis(ctx, u, uprava, p.ID); err != nil {
		t.Fatal(err)
	}
	staro := uskladjenjaVrste(t, s, p.ID, lopata)
	if len(staro) != 1 || staro[0].Kolicina != -2 {
		t.Fatalf("usklađenje lopata: %+v", staro)
	}

	// premještaj s danom prije inventure: stanje ga vidi, zaključena inventura ne
	if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPrijenos, SkladisteID: bilje.ID, NaSkladisteID: sk.ID,
		VrstaID: vrece, Oblik: models.OblikPrazno, Kolicina: 9000, Datum: dan.AddDate(0, 0, -1)}); err != nil {
		t.Fatal(err)
	}
	if k := kolicinaU(t, s, ctx, sk.ID, vrece); k != 9000 {
		t.Fatalf("stanje vreća poslije premještaja: %v", k)
	}
	tab, err := s.TablicaSkladista(ctx, sk.ID, dan)
	if err != nil {
		t.Fatal(err)
	}
	if k := tab.StanjeU(sk.ID, vrece); k != 0 {
		t.Fatalf("zaključena inventura na dan pokazuje %v vreća, a prebrojano je 0", k)
	}

	admin := &models.User{ID: uuid.New(), FullName: "Marko Marić"}
	if err := s.OtkljucajPopis(ctx, admin, glavniAdmin, p.ID); err != nil {
		t.Fatal(err)
	}
	nacrt, err := s.Popis(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if nacrt.Zakljucen() {
		t.Fatal("inventura je i dalje zaključena")
	}
	if !strings.HasPrefix(nacrt.Napomena, "Otključao: Marko Marić, ") || !strings.HasSuffix(nacrt.Napomena, "; 1 usklađenje je obrisano.") {
		t.Errorf("napomena: %q", nacrt.Napomena)
	}
	if ostala := uskladjenjaVrste(t, s, p.ID, lopata); len(ostala) != 0 {
		t.Errorf("usklađenja poslije otključavanja: %+v", ostala)
	}
	if k := kolicinaU(t, s, ctx, sk.ID, lopata); k != 46 {
		t.Errorf("stanje lopata bez usklađenja: %v", k)
	}

	// skladištar upiše prebrojanih 9 000 vreća i zaključi ponovno
	postaviPrebrojano(t, nacrt, vrece, models.OblikPrazno, 9000)
	if err := s.SpremiPopis(ctx, u, uprava, nacrt); err != nil {
		t.Fatal(err)
	}
	if err := s.ZakljuciPopis(ctx, u, uprava, p.ID); err != nil {
		t.Fatal(err)
	}
	if x := uskladjenjaVrste(t, s, p.ID, vrece); len(x) != 0 {
		t.Errorf("usklađenje za vreće, a prebrojano je jednako knjizi: %+v", x)
	}
	novo := uskladjenjaVrste(t, s, p.ID, lopata)
	if len(novo) != 1 || novo[0].Kolicina != -2 || novo[0].ID == staro[0].ID {
		t.Errorf("usklađenje lopata poslije ponovnog zaključenja: %+v", novo)
	}
	if k := kolicinaU(t, s, ctx, sk.ID, lopata); k != 44 {
		t.Errorf("stanje lopata: %v", k)
	}
	if k := kolicinaU(t, s, ctx, sk.ID, vrece); k != 9000 {
		t.Errorf("stanje vreća: %v", k)
	}
	zatvoren, _ := s.Popis(ctx, p.ID)
	if st := zatvoren.StavkaZa(vrece, models.OblikPrazno); !zatvoren.Zakljucen() || st.Knjizno != 9000 || st.Utvrdjeno != 9000 {
		t.Errorf("vreće na ponovno zaključenoj inventuri: %+v", st)
	}
}

func TestMtsOtkljucavanjeOdbija(t *testing.T) {
	s, sk, ctx, u, uprava := pripremiMts(t)
	admin := &models.User{ID: uuid.New(), FullName: "Marko Marić"}
	dan := pocetakDana(time.Now().In(models.Zagreb)).AddDate(0, 0, -10)
	if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPocetno, SkladisteID: sk.ID, VrstaID: "lopata",
		Kolicina: 46, Datum: dan.AddDate(0, 0, -5)}); err != nil {
		t.Fatal(err)
	}
	inventura := func(dan time.Time, zakljuci bool) *models.Popis {
		t.Helper()
		p, err := s.PredlozakPopisa(ctx, sk.ID, dan)
		if err != nil {
			t.Fatal(err)
		}
		postaviPrebrojano(t, p, "lopata", models.OblikOsnovni, 45)
		if err := s.SpremiPopis(ctx, u, uprava, p); err != nil {
			t.Fatal(err)
		}
		if zakljuci {
			if err := s.ZakljuciPopis(ctx, u, uprava, p.ID); err != nil {
				t.Fatal(err)
			}
		}
		return p
	}
	stara := inventura(dan, true)
	mora := func(sto string, err error, dio string) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), dio) {
			t.Errorf("%s: %v, a mora javiti %q", sto, err, dio)
		}
	}

	mora("uprava sektora", s.OtkljucajPopis(ctx, u, uprava, stara.ID), "samo globalni administrator")
	mora("bez prijave", s.OtkljucajPopis(ctx, nil, glavniAdmin, stara.ID), "samo globalni administrator")
	mora("bez ovlasti", s.OtkljucajPopis(ctx, admin, nil, stara.ID), "samo globalni administrator")
	mora("nepoznata", s.OtkljucajPopis(ctx, admin, glavniAdmin, "nema-je"), "nije pronađena")

	nacrt := inventura(dan.AddDate(0, 0, 5), false)
	mora("nacrt", s.OtkljucajPopis(ctx, admin, glavniAdmin, nacrt.ID), "nije zaključena")
	mora("starija od nacrta", s.OtkljucajPopis(ctx, admin, glavniAdmin, stara.ID), "novija inventura, na dan "+nacrt.Dan.Format("2. 1. 2006."))
	if err := s.ZakljuciPopis(ctx, u, uprava, nacrt.ID); err != nil {
		t.Fatal(err)
	}
	// noviji zaključeni smeta, a poruka kaže koji
	err := s.OtkljucajPopis(ctx, admin, glavniAdmin, stara.ID)
	mora("starija od zaključene", err, "na dan "+stara.Dan.Format("2. 1. 2006.")+" ne može se otključati")
	mora("starija od zaključene", err, "novija inventura, na dan "+nacrt.Dan.Format("2. 1. 2006.")+".")
	if p, _ := s.Popis(ctx, stara.ID); !p.Zakljucen() || len(uskladjenjaVrste(t, s, stara.ID, "lopata")) != 1 {
		t.Errorf("odbijeno otključavanje promijenilo je staru inventuru: %+v", p)
	}
	// zadnja se otključava
	if err := s.OtkljucajPopis(ctx, admin, glavniAdmin, nacrt.ID); err != nil {
		t.Fatalf("zadnja inventura: %v", err)
	}
	// inventura drugog skladišta ne smeta
	drugo := &models.Skladiste{Sektor: "B", AreaID: 34, Naziv: "Skladište Bilje", Aktivno: true}
	if err := s.SpremiSkladiste(ctx, uprava, drugo); err != nil {
		t.Fatal(err)
	}
	p, err := s.PredlozakPopisa(ctx, drugo.ID, dan.AddDate(0, 0, 8))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SpremiPopis(ctx, u, uprava, p); err != nil {
		t.Fatal(err)
	}
	if err := s.ZakljuciPopis(ctx, u, uprava, nacrt.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.OtkljucajPopis(ctx, admin, glavniAdmin, nacrt.ID); err != nil {
		t.Errorf("inventura drugog skladišta smeta: %v", err)
	}
}

// Kad se inventura ili njezina usklađenja ne daju pročitati, otključavanje
// javlja grešku i ne mijenja ništa
func TestMtsOtkljucavanjeKadCitanjePadne(t *testing.T) {
	s, sk, ctx, u, uprava, baza := pripremiMtsIBazu(t)
	admin := &models.User{ID: uuid.New(), FullName: "Marko Marić"}
	dan := pocetakDana(time.Now().In(models.Zagreb)).AddDate(0, 0, -3)
	p, err := s.PredlozakPopisa(ctx, sk.ID, dan)
	if err != nil {
		t.Fatal(err)
	}
	postaviPrebrojano(t, p, "lopata", models.OblikOsnovni, 3)
	if err := s.SpremiPopis(ctx, u, uprava, p); err != nil {
		t.Fatal(err)
	}
	if err := s.ZakljuciPopis(ctx, u, uprava, p.ID); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := baza.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	nepromijenjena := func(sto string) {
		t.Helper()
		if err := s.OtkljucajPopis(ctx, admin, glavniAdmin, p.ID); err == nil {
			t.Errorf("%s: otključavanje je prošlo", sto)
		}
		if cur, _ := s.repo.GetPopis(ctx, p.ID); cur != nil && !cur.Zakljucen() {
			t.Errorf("%s: inventura je otključana", sto)
		}
	}

	// druga inventura istog skladišta koja se ne da pročitati
	exec(`INSERT INTO mts_popisi (id, skladiste_id, sektor, dan, godina, stavke, izradeno_at, created_at, updated_at)
		VALUES ('pokvaren', ?, 'B', '2000-01-01', 2000, 'nije popis', '2026-01-01', '2026-01-01', '2026-01-01')`, sk.ID)
	nepromijenjena("nečitljiva druga inventura")
	exec(`DELETE FROM mts_popisi WHERE id = 'pokvaren'`)

	exec(`ALTER TABLE mts_promet RENAME TO mts_promet_drugo`)
	nepromijenjena("promet se ne da pročitati")
	exec(`ALTER TABLE mts_promet_drugo RENAME TO mts_promet`)

	exec(`ALTER TABLE mts_popisi RENAME TO mts_popisi_drugo`)
	nepromijenjena("inventure se ne daju pročitati")
}

func TestBiljeskaOtkljucavanja(t *testing.T) {
	kad := time.Date(2026, 10, 13, 9, 16, 0, 0, time.UTC) // 11:16 u Zagrebu
	for _, c := range []struct {
		n    int
		want string
	}{
		{0, "Otključao: Marko Marić, 13. 10. 2026. u 11:16; usklađenja nije bilo."},
		{1, "Otključao: Marko Marić, 13. 10. 2026. u 11:16; 1 usklađenje je obrisano."},
		{3, "Otključao: Marko Marić, 13. 10. 2026. u 11:16; 3 usklađenja su obrisana."},
		{5, "Otključao: Marko Marić, 13. 10. 2026. u 11:16; 5 usklađenja je obrisano."},
		{21, "Otključao: Marko Marić, 13. 10. 2026. u 11:16; 21 usklađenje je obrisano."},
	} {
		if got := biljeskaOtkljucavanja("Marko Marić", kad, c.n); got != c.want {
			t.Errorf("%d usklađenja:\n%q\na treba\n%q", c.n, got, c.want)
		}
	}
}

func TestDopisiRecenicu(t *testing.T) {
	r := "Otključao: Marko Marić."
	for _, c := range []struct{ napomena, want string }{
		{"", r},
		{"   ", r},
		{"brojala Petra", "brojala Petra. " + r},
		{"Brojala Petra.", "Brojala Petra. " + r},
		{"Fali li lopata?", "Fali li lopata? " + r},
		{"Pazi!", "Pazi! " + r},
		{"oštećeno 5 ćuskija ", "oštećeno 5 ćuskija. " + r},
		{"nedostaje kliješta i čekić", "nedostaje kliješta i čekić. " + r},
	} {
		if got := dopisiRecenicu(c.napomena, r); got != c.want {
			t.Errorf("%q: %q", c.napomena, got)
		}
	}
}

// SmijeOtkljucati: globalni administrator da, uprava sektora i nitko ne
func TestMtsSmijeOtkljucati(t *testing.T) {
	s := &MtsService{}
	for _, c := range []struct {
		perms *models.UserPermissions
		smije bool
	}{
		{nil, false},
		{&models.UserPermissions{}, false},
		{&models.UserPermissions{AdminSectors: map[string]bool{"B": true}}, false},
		{glavniAdmin, true},
	} {
		if s.SmijeOtkljucati(c.perms) != c.smije {
			t.Errorf("%+v: %v", c.perms, !c.smije)
		}
	}
}
