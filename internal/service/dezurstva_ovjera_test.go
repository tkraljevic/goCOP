package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// ovjeren je dnevnik okoline kao da ga je voditelj COP-a ovjerio
func (o *okolinaDezurstava) ovjeren() *models.Journal {
	j := *o.dnevnik
	kraj, sad := dzSat(20, 0, 0), time.Now()
	j.EndedAt, j.ZakljucenoAt = &kraj, &sad
	return &j
}

// Ovjera dnevnika COP-a traži potvrđen plan dežurstava, a poslije nje plan
// je zaključan (Odluke (30. 10. 2026.), 5): osoba svoje dežurstvo više ne
// upisuje ni ne miče, a uprava centra ga ispravlja samo uz razlog, koji
// ostaje u napomeni i ne briše se izmjenom napomene.
func TestPlanZakljucanOvjerom(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	svoje := o.peroDezura(dzSat(7, 7, 0), dzSat(7, 19, 0))
	if err := o.svc.SpremiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik, svoje); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.PlanSpremanZaOvjeru(ctx, o.dnevnik); err == nil || !strings.Contains(err.Error(), "nepotvrđenih dežurstava (1)") {
		t.Errorf("ovjera s nepotvrđenim dežurstvom: %v", err)
	}
	upravino := o.peroDezura(dzSat(8, 7, 0), dzSat(8, 19, 0))
	if err := o.svc.SpremiDezurstvo(ctx, o.uprava, o.ovlUprav, o.opseg, o.dnevnik, upravino); err != nil {
		t.Fatal(err)
	}

	j := o.ovjeren()
	zakljucan := func(tko string, err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), "zaključen je ovjerom") {
			t.Errorf("%s: %v", tko, err)
		}
	}
	novo := o.peroDezura(dzSat(9, 7, 0), dzSat(9, 19, 0))
	zakljucan("osoba upisuje sebe", o.svc.SpremiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, j, novo))
	zakljucan("osoba miče svoje", o.svc.MakniDezurstvo(ctx, o.pperic, o.ovlPero, j, svoje.ID, "krivo"))
	zakljucan("uprava upisuje bez razloga", o.svc.SpremiDezurstvo(ctx, o.uprava, o.ovlUprav, o.opseg, j, novo))
	zakljucan("uprava potvrđuje bez razloga", o.svc.PotvrdiDezurstvo(ctx, o.uprava, o.ovlUprav, j, svoje.ID, " "))
	_, err := o.svc.IspraviOpisDezurstva(ctx, o.uprava, o.ovlUprav, j, upravino.ID, "Obilazak i pregled obrambenih objekata", "")
	zakljucan("uprava ispravlja opis bez razloga", err)
	zakljucan("uprava miče bez razloga", o.svc.MakniDezurstvo(ctx, o.uprava, o.ovlUprav, j, upravino.ID, ""))
	if dez, err := o.svc.Dezurstva(ctx, j.ID); err != nil || len(dez) != 2 {
		t.Errorf("zaključan plan ima %d dežurstava", len(dez))
	}

	// uprava uz razlog: razlog ostaje u napomeni i dežurstvo je potvrđeno
	trag := func(d *models.Dezurstvo, razlog string) {
		t.Helper()
		zapis, err := o.svc.repo.GetDezurstvo(ctx, d.ID)
		if err != nil || zapis == nil || !zapis.Potvrdeno() || len(models.TragoviIspravkaNakonOvjere(zapis.Napomena)) == 0 ||
			!strings.Contains(zapis.Napomena, "Uprava centra") || !strings.Contains(zapis.Napomena, "razlog: „"+razlog+"”") {
			t.Errorf("ispravak uz razlog %q: %+v", razlog, zapis)
		}
	}
	novo.RazlogIspravka = "sati iz putnog naloga"
	if err := o.svc.SpremiDezurstvo(ctx, o.uprava, o.ovlUprav, o.opseg, j, novo); err != nil {
		t.Fatal(err)
	}
	trag(novo, "sati iz putnog naloga")
	if err := o.svc.PotvrdiDezurstvo(ctx, o.uprava, o.ovlUprav, j, svoje.ID, "provjereno [naknadno]"); err != nil {
		t.Fatal(err)
	}
	trag(svoje, "provjereno (naknadno)")
	if _, err := o.svc.IspraviOpisDezurstva(ctx, o.uprava, o.ovlUprav, j, upravino.ID, "Obilazak i pregled obrambenih objekata", "bio na terenu"); err != nil {
		t.Fatal(err)
	}
	trag(upravino, "bio na terenu")

	// izmjena napomene ne briše razlog ispravka
	izmjena, err := o.svc.repo.GetDezurstvo(ctx, novo.ID)
	if err != nil || izmjena == nil {
		t.Fatalf("dežurstvo: %v", err)
	}
	izmjena.Napomena, izmjena.RazlogIspravka = "nova napomena", "dopisana napomena"
	if err := o.svc.SpremiDezurstvo(ctx, o.uprava, o.ovlUprav, o.opseg, j, izmjena); err != nil {
		t.Fatal(err)
	}
	if zapis, err := o.svc.repo.GetDezurstvo(ctx, novo.ID); err != nil || zapis == nil || len(models.TragoviIspravkaNakonOvjere(zapis.Napomena)) != 2 || !strings.Contains(zapis.Napomena, "nova napomena") {
		t.Errorf("izmjena napomene: %+v", zapis)
	}

	// micanje uz razlog: dežurstva nema, a razlog je u arhiviranoj verziji
	if err := o.svc.MakniDezurstvo(ctx, o.uprava, o.ovlUprav, j, novo.ID, "upisano dvaput"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := o.baza.QueryRow(`SELECT count(*) FROM record_versions WHERE entity_id = ? AND archived = 1 AND payload LIKE '%upisano dvaput%'`, novo.ID).Scan(&n); err != nil || n != 1 {
		t.Errorf("razlog micanja u knjizi: %d, %v", n, err)
	}
	if err := o.svc.PlanSpremanZaOvjeru(ctx, o.dnevnik); err != nil {
		t.Errorf("potvrđen plan nije spreman za ovjeru: %v", err)
	}
}

// Bilješku o ispravku nakon ovjere upisuje samo program: rukom upisana u
// napomenu ne sprema se.
func TestTragNakonOvjereNeUpisujeSeRukom(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	d := o.peroDezura(dzSat(7, 7, 0), dzSat(7, 19, 0))
	d.Napomena = models.TragIspravkaNakonOvjere("Uprava centra", time.Now(), "izmišljeno")
	if err := o.svc.SpremiDezurstvo(context.Background(), o.pperic, o.ovlPero, o.opseg, o.dnevnik, d); err == nil || !strings.Contains(err.Error(), "upisuje program") {
		t.Errorf("rukom upisana bilješka o ispravku nakon ovjere: %v", err)
	}
}

// Uprava centra tuđe dežurstvo upisuje samo osobi koja na dan dežurstva ima
// dužnost u sektoru centra: djelatnik iz drugog sektora ulazi u plan samo uz
// privremenu dužnost u ovom sektoru (Odluke (30. 10. 2026.), 7). Ispomoć
// koja je poslije istekla vrijedi za svoje dane. Dužnost uprave
// organizacije vrijedi svuda; gost ne dežura.
func TestPlanSamoSDuznoscuUSektoru(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	p, q := "P", "Q"
	od := time.Date(2020, 1, 1, 0, 0, 0, 0, models.Zagreb)
	istekla := dzSat(9, 0, 0)
	duznost := func(uloga models.Role, sektor *string, privremena bool, istek *time.Time) models.VerzijaZaduzenja {
		return models.VerzijaZaduzenja{Duty: models.Duty{ID: uuid.New(), Role: uloga, SectorID: sektor, IsActive: true, IsTemporary: privremena, ExpiresAt: istek}, Od: od}
	}
	povijest := map[string][]models.VerzijaZaduzenja{}
	osoba := func(z ...models.VerzijaZaduzenja) string {
		id := uuid.NewString()
		povijest[id] = z
		return id
	}
	izDrugog := osoba(duznost(models.RoleOperator, &q, false, nil))
	sIspomoci := osoba(duznost(models.RoleOperator, &q, false, nil), duznost(models.RoleOperator, &p, true, nil))
	ispomocIstekla := osoba(duznost(models.RoleOperator, &q, false, nil), duznost(models.RoleOperator, &p, true, &istekla))
	glavni := osoba(duznost(models.RoleNationalLeader, nil, false, nil))
	gost := osoba(duznost(models.RoleGuest, nil, false, nil))
	o.svc.SetPovijestZaduzenja(func(id uuid.UUID) ([]models.VerzijaZaduzenja, error) {
		z, ima := povijest[id.String()]
		if !ima {
			return nil, errors.New("osoba nije u imeniku")
		}
		return z, nil
	})
	upisi := func(userID string, dan int) error {
		d := &models.Dezurstvo{UserID: userID, UserName: "Osoba", Od: dzSat(dan, 7, 0), Do: dzSat(dan, 19, 0), Opis: "Dežurstvo u COP-u"}
		return o.svc.SpremiDezurstvo(ctx, o.uprava, o.ovlUprav, o.opseg, o.dnevnik, d)
	}
	for tko, slucaj := range map[string]struct {
		id  string
		dan int
	}{"iz drugog sektora": {izDrugog, 7}, "ispomoć istekla prije dana": {ispomocIstekla, 10}, "gost": {gost, 7}} {
		if err := upisi(slucaj.id, slucaj.dan); err == nil || !strings.Contains(err.Error(), "privremenu dužnost u ovom sektoru") {
			t.Errorf("%s: %v", tko, err)
		}
	}
	if err := upisi(uuid.NewString(), 7); err == nil {
		t.Error("osoba koje nema u imeniku ušla je u plan")
	}
	if err := upisi("nije-broj", 7); err == nil {
		t.Error("osoba s neispravnim brojem ušla je u plan")
	}

	// kontrola: ispomoć u sektoru, ispomoć na svoj dan, uprava organizacije
	for tko, slucaj := range map[string]struct {
		id  string
		dan int
	}{"s ispomoći": {sIspomoci, 7}, "ispomoć na svoj dan": {ispomocIstekla, 8}, "glavni rukovoditelj": {glavni, 7}} {
		if err := upisi(slucaj.id, slucaj.dan); err != nil {
			t.Errorf("%s ne ulazi u plan: %v", tko, err)
		}
	}
	// uprava upisuje sebe bez provjere dužnosti
	if err := upisi(o.uprava.ID.String(), 11); err != nil {
		t.Errorf("uprava ne upisuje sebe: %v", err)
	}
	// bez izvora dužnosti tuđe dežurstvo se ne upisuje
	o.svc.SetPovijestZaduzenja(nil)
	if err := upisi(sIspomoci, 12); err == nil {
		t.Error("bez izvora dužnosti tuđe dežurstvo je upisano")
	}
}

// Poslovođa izvođača ne ulazi u plan ni kad ga upisuje uprava centra
// (Odluke (30. 10. 2026.), 2), i ni kad uz to ima drugu dužnost u sektoru.
// Dužnost upisana samo na branjeno područje sektora (stariji zapis bez
// sektora) vrijedi kao dužnost u sektoru; područje drugog sektora ne.
func TestPlanBezIzvodjacaIDuznostPodrucja(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	p := "P"
	prvo, tude := 1, 7
	od := time.Date(2020, 1, 1, 0, 0, 0, 0, models.Zagreb)
	verzija := func(z models.Duty) models.VerzijaZaduzenja {
		z.ID, z.IsActive = uuid.New(), true
		return models.VerzijaZaduzenja{Duty: z, Od: od}
	}
	povijest := map[string][]models.VerzijaZaduzenja{}
	osoba := func(z ...models.Duty) string {
		id := uuid.NewString()
		for _, d := range z {
			povijest[id] = append(povijest[id], verzija(d))
		}
		return id
	}
	poslovodja := osoba(models.Duty{Role: models.RoleServiceLeaderForeman, SectorID: &p, AreaID: &prvo})
	poslovodjaIDezurni := osoba(models.Duty{Role: models.RoleServiceLeaderForeman, SectorID: &p, AreaID: &prvo}, models.Duty{Role: models.RoleOperator, SectorID: &p})
	podrucjeSektora := osoba(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &prvo})
	tudjePodrucje := osoba(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &tude})
	glavniIPoslovodja := osoba(models.Duty{Role: models.RoleNationalLeader}, models.Duty{Role: models.RoleServiceLeaderForeman, SectorID: &p, AreaID: &prvo})
	o.svc.SetPovijestZaduzenja(func(id uuid.UUID) ([]models.VerzijaZaduzenja, error) { return povijest[id.String()], nil })
	upisi := func(userID string, dan int) error {
		d := &models.Dezurstvo{UserID: userID, UserName: "Osoba", Od: dzSat(dan, 7, 0), Do: dzSat(dan, 19, 0), Opis: "Dežurstvo u COP-u"}
		return o.svc.SpremiDezurstvo(ctx, o.uprava, o.ovlUprav, o.opseg, o.dnevnik, d)
	}
	for tko, id := range map[string]string{"poslovođa": poslovodja, "poslovođa i dežurni": poslovodjaIDezurni} {
		if err := upisi(id, 7); err == nil || !strings.Contains(err.Error(), "poslovođa izvođača ne ulazi u plan") {
			t.Errorf("%s: %v", tko, err)
		}
	}
	if err := upisi(tudjePodrucje, 7); err == nil || !strings.Contains(err.Error(), "privremenu dužnost u ovom sektoru") {
		t.Errorf("dužnost na području drugog sektora: %v", err)
	}
	// kontrola: dužnost na području sektora i uprava organizacije
	for tko, id := range map[string]string{"dužnost na području sektora": podrucjeSektora, "glavni rukovoditelj": glavniIPoslovodja} {
		if err := upisi(id, 8); err != nil {
			t.Errorf("%s ne ulazi u plan: %v", tko, err)
		}
	}
	if !duznostUSektoru(models.Duty{Role: models.RoleOperator, SectorID: &p}, "P", nil) || duznostUSektoru(models.Duty{Role: models.RoleOperator}, "P", []int{1}) {
		t.Error("dužnost sa sektorom ili bez sektora i područja krivo prepoznata")
	}
}

// Ovjera ne prolazi ni kad je dežurstvo potvrđeno, a ne zna mu se mjesto
// rada (čeka provjeru). Plan se provjerava i pri spremanju ovjere, jer se
// mogao promijeniti dok se dnevnik potpisivao.
func TestOvjeraTraziPlanBezDezurstvaZaProvjeru(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	sad := time.Now().In(models.Zagreb)
	staro := &models.Dezurstvo{JournalID: o.dnevnik.ID, UserID: o.pperic.ID.String(), UserName: "Pero Perić",
		Od: dzSat(7, 7, 0), Do: dzSat(7, 19, 0), Opis: "Stari opis s drugog čvora", Potvrdio: "Uprava centra", PotvrdenoAt: &sad}
	if err := o.svc.repo.SaveDezurstvo(ctx, staro); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.PlanSpremanZaOvjeru(ctx, o.dnevnik); err == nil || !strings.Contains(err.Error(), "nepotvrđenih dežurstava (1)") {
		t.Errorf("ovjera s dežurstvom za provjeru: %v", err)
	}
	priprema := *o.ovjeren()
	priprema.ZakljucioID, priprema.Zakljucio = o.uprava.ID.String(), o.uprava.FullName
	pdf := []byte("%PDF-1.4\nprobni izvornik")
	if err := o.svc.SpremiOvjeruCOP(ctx, &priprema, pdf); err == nil || !strings.Contains(err.Error(), "nepotvrđenih") {
		t.Errorf("spremanje ovjere s nepotvrđenim planom: %v", err)
	}
	// kontrola: uprava ispravi opis i potvrdi, pa se ovjera sprema
	if _, err := o.svc.IspraviOpisDezurstva(ctx, o.uprava, o.ovlUprav, o.dnevnik, staro.ID, "Dežurstvo u COP-u", ""); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.PotvrdiDezurstvo(ctx, o.uprava, o.ovlUprav, o.dnevnik, staro.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.SpremiOvjeruCOP(ctx, &priprema, pdf); err != nil {
		t.Errorf("ovjera s potvrđenim planom: %v", err)
	}
}

// Zaključan plan ne ispravlja se tuđim očima: bilješka o ispravku nosi ime
// onoga tko ispravlja. Svojim očima uprava ispravlja kao i dosad.
func TestZakljucanPlanNeIspravljaSeTudjimOcima(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	d := o.peroDezura(dzSat(7, 7, 0), dzSat(7, 19, 0))
	if err := o.svc.SpremiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik, d); err != nil {
		t.Fatal(err)
	}
	j := o.ovjeren()
	if err := o.svc.PotvrdiDezurstvo(TudjimOcima(ctx), o.uprava, o.ovlUprav, j, d.ID, "provjereno"); err == nil || !strings.Contains(err.Error(), "tuđim očima") {
		t.Errorf("ispravak zaključanog plana tuđim očima: %v", err)
	}
	if zapis, err := o.svc.repo.GetDezurstvo(ctx, d.ID); err != nil || zapis == nil || zapis.Potvrdeno() {
		t.Errorf("dežurstvo je potvrđeno tuđim očima: %+v", zapis)
	}
	// kontrola: neovjeren plan tuđim očima i ovjeren svojim očima
	drugo := o.peroDezura(dzSat(8, 7, 0), dzSat(8, 19, 0))
	if err := o.svc.SpremiDezurstvo(TudjimOcima(ctx), o.uprava, o.ovlUprav, o.opseg, o.dnevnik, drugo); err != nil {
		t.Errorf("upis u neovjeren plan tuđim očima: %v", err)
	}
	if err := o.svc.PotvrdiDezurstvo(ctx, o.uprava, o.ovlUprav, j, d.ID, "provjereno"); err != nil {
		t.Errorf("uprava svojim očima ne ispravlja: %v", err)
	}
}
