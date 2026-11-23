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

// Rukovoditelj obrane izvođača za branjeno područje i zamjenik (Odluke
// (22. 11. 2026.)). Okolina je ona iz ocitanja_prava_test.go: sektor P s
// područjima 1 i 2, sektor Q s područjem 3. Osobe su izmišljene.

var ulogeObraneIzvodjaca = []models.Role{models.RoleContractorAreaLeader, models.RoleContractorAreaDeputy}

// Pitanje 3: sve što ima voditelj usluga, bez dnevnika COP-a, plana
// dežurstava, obračuna sati, IORS-a, listova vodočuvara i prijava s
// terena; ništa ne proglašavaju i ne upravljaju.
func TestObranaIzvodjacaVidiKaoVoditeljUsluga(t *testing.T) {
	cop := &models.Journal{CentarSektor: "P"}
	opsegCOP := models.Opseg{Sektor: "P", Podrucja: []int{1, 2}}
	js := &JournalService{}
	for _, uloga := range ulogeObraneIzvodjaca {
		t.Run(string(uloga), func(t *testing.T) {
			perms := ovlastiOsobe(duznost(uloga, models.ScopeArea, "P", 1, ""))
			u := &perms.User
			// strana izvođača: kao voditelj usluga
			if !IsContractor(u) || !JeIzvodjacZaCOP(u) || !js.IzvodjacZaDnevnike(u) {
				t.Error("nije na strani izvođača")
			}
			// dnevnici usluga svog područja da, tuđeg ne
			if !js.CanWrite(perms, models.OpsegPodrucja(models.Area{ID: 1, SectorID: "P"})) || js.CanWrite(perms, models.OpsegPodrucja(models.Area{ID: 2, SectorID: "P"})) {
				t.Error("piše u dnevnike usluga izvan svog područja ili ne piše u svoje")
			}
			// dnevnik COP-a, plan dežurstava, obračun i IORS ne
			if js.CitaDnevnikeCOP(perms) || js.PiseUDnevnik(perms, opsegCOP, cop) || js.VidiPlanDezurstava(perms, opsegCOP, cop) ||
				js.VidiObracun(perms, cop) || js.VidiIORS(u, perms, cop, u.ID.String()) {
				t.Error("vidi dnevnik COP-a, plan dežurstava, obračun ili IORS")
			}
			if duznostUSektoru(u.Duties[0], "P", []int{1}) {
				t.Error("ulazi u plan dežurstava centra")
			}
			// listovi vodočuvara i prijave s terena ne
			if u.VidiVodocuvarskiDnevnik() || uloga.OvlastKnjige() != models.KnjigaNe {
				t.Error("vidi knjigu vodočuvara")
			}
			// ne proglašava, ne upravlja
			if SmijeSastaviti(perms, "P") || SastavljaNacrte(perms) || perms.SmijeGeneriratiPrognozu() {
				t.Error("sastavlja akte ili pokreće prognozu")
			}
			if perms.IsGlobalAdmin || len(perms.AdminSectors) > 0 || len(perms.AdminAreas) > 0 || uloga.RazinaUprave() != 0 {
				t.Errorf("upravlja: %+v %+v", perms.AdminSectors, perms.AdminAreas)
			}
			// rješenja i obavijesti svog područja; ovjeren akt vidi cijela organizacija
			if !VidiAkt(perms, &models.Akt{Sektor: "P", AreaID: 1}) || VidiAkt(perms, &models.Akt{Sektor: "P", AreaID: 2}) ||
				!VidiAkt(perms, &models.Akt{Sektor: "P", AreaID: 2, Status: models.AktOvjeren}) {
				t.Error("akti: vidi tuđe područje ili ne vidi svoje")
			}
		})
	}
}

// Prijašnja dužnost rukovoditelja obrane izvođača i dalje zatvara dnevnike
// usluga drugih firmi, kao i poslovođi, a dežurstvo u vrijeme te dužnosti
// ne ulazi u obračun sati
func TestObranaIzvodjacaUPovijesti(t *testing.T) {
	bivsi := &models.User{ID: uuid.New(), FullName: "Pero Perić"}
	imao := func(_ uuid.UUID, r models.Role) (bool, error) { return r == models.RoleContractorAreaDeputy, nil }
	if !izvodjacZaDnevnike(bivsi, imao) {
		t.Error("bivši zamjenik rukovoditelja obrane izvođača otvara tuđe dnevnike usluga")
	}
	nikad := func(uuid.UUID, models.Role) (bool, error) { return false, nil }
	if izvodjacZaDnevnike(bivsi, nikad) {
		t.Error("osoba koja nikad nije bila izvođač gleda se kao izvođač")
	}
	greska := func(uuid.UUID, models.Role) (bool, error) { return false, errors.New("baza") }
	if !izvodjacZaDnevnike(bivsi, greska) {
		t.Error("kad se povijest ne da pročitati, osoba bez dužnosti u ustroju gleda se kao izvođač")
	}

	od := time.Now().Add(-48 * time.Hour)
	d := duznost(models.RoleContractorAreaLeader, models.ScopeArea, "P", 1, "")
	d.CreatedAt = od.Add(-time.Hour)
	js := &JournalService{povijestZaduzenja: func(uuid.UUID) ([]models.VerzijaZaduzenja, error) {
		return []models.VerzijaZaduzenja{{Duty: d, Od: d.CreatedAt}}, nil
	}}
	if !js.izvodjacUDezurstvu(models.Dezurstvo{UserID: bivsi.ID.String(), Od: od}, map[string][]models.VerzijaZaduzenja{}) {
		t.Error("dežurstvo rukovoditelja obrane izvođača ulazi u obračun sati")
	}
}

// Pitanje 6: vodostaj upisuju na letvama branjenog područja svoje dužnosti,
// kao rukovoditelj dionice na svojim letvama, a ne po cijelom sektoru.
// Letva bez sektora prima očitanje od svakoga tko igdje upisuje. Na tuđu
// letvu bez navedenog izvora upis je odbijen.
func TestObranaIzvodjacaUpisujeNaLetvamaPodrucja(t *testing.T) {
	o := novaOkolinaPracenja(t)
	ctx := context.Background()
	letve := []*models.Station{o.primjerovo, o.probno, o.dvije, o.drugdje, o.mohacs, o.barcs, o.uzvodna, o.letenye}
	upisuje := func(perms *models.UserPermissions) string {
		var smije []string
		for _, st := range letve {
			if o.rs.CanRecordStation(perms, st) {
				smije = append(smije, st.Name)
			}
		}
		return strings.Join(smije, ",")
	}
	for i, uloga := range ulogeObraneIzvodjaca {
		t.Run(string(uloga), func(t *testing.T) {
			d := duznost(uloga, models.ScopeArea, "P", 1, "")
			d.Title = uloga.Label()
			u, perms := o.pperic(t, "izv"+string(rune('a'+i)), &d)
			if got, want := upisuje(perms), "Primjerovo,Granica,Uzvodna,Letenye probna"; got != want {
				t.Errorf("upisuje na %q, a treba %q", got, want)
			}
			// svoja letva: upis prolazi i nosi ime osobe
			rd := &models.Reading{StationID: o.primjerovo.ID.String(), MeasuredAt: time.Now().Add(-time.Hour), LevelCm: cmP(140)}
			if err := o.rs.Create(ctx, perms, rd); err != nil {
				t.Fatalf("upis na letvu svog područja: %v", err)
			}
			if rd.UserID != u.ID.String() || rd.Observer != u.FullName {
				t.Errorf("upis ne nosi osobu: %q, %q", rd.UserID, rd.Observer)
			}
			// tuđe područje i tuđi sektor: bez izvora odbijeno
			for _, st := range []*models.Station{o.probno, o.drugdje} {
				tudji := &models.Reading{StationID: st.ID.String(), MeasuredAt: time.Now().Add(-time.Hour), LevelCm: cmP(90)}
				if err := o.rs.Create(ctx, perms, tudji); !errors.Is(err, ErrTrebaIzvor) {
					t.Errorf("%s: upis bez izvora mora biti odbijen: %v", st.Name, err)
				}
			}
		})
	}

	// voditelj usluga i dalje upisuje po cijelom sektoru (Odluke (8. 10. 2026.), pitanje 8)
	vu := ovlastiOsobe(duznost(models.RoleServiceLeaderForeman, models.ScopeArea, "P", 1, ""))
	if got := upisuje(vu); !strings.Contains(got, "Probno") || !strings.Contains(got, "Mohács probni") {
		t.Errorf("voditelj usluga više ne upisuje po sektoru: %q", got)
	}
}

// Svoje očitanje ispravljaju po istom pravilu kao ostali: dok imaju
// dužnost i još 72 sata od upisa. Tuđe ne ispravljaju i ne brišu ništa;
// to smije samo tko odgovara za letvu.
func TestObranaIzvodjacaIspravljaSvojeOcitanje(t *testing.T) {
	o := novaOkolinaPracenja(t)
	sad := time.Now()
	for _, uloga := range ulogeObraneIzvodjaca {
		perms := ovlastiOsobe(duznost(uloga, models.ScopeArea, "P", 1, ""))
		ja := perms.User.ID.String()
		svoje := &models.Reading{UserID: ja, CreatedAt: sad.Add(-100 * time.Hour)}
		tudje := &models.Reading{UserID: uuid.NewString(), CreatedAt: sad.Add(-time.Hour)}
		p := o.rs.PravaNaLetvu(perms, o.primjerovo)
		if !p.MozeIspraviti(svoje, sad) || p.MozeIspraviti(tudje, sad) || p.MozeObrisati() {
			t.Errorf("%s s dužnošću: svoje %v, tuđe %v, briše %v", uloga, p.MozeIspraviti(svoje, sad), p.MozeIspraviti(tudje, sad), p.MozeObrisati())
		}
		// bez dužnosti: svoje još 72 sata od upisa
		bez := o.rs.PravaNaLetvu(models.NewUserPermissions(models.User{ID: perms.User.ID}), o.primjerovo)
		nedavno := &models.Reading{UserID: ja, CreatedAt: sad.Add(-71 * time.Hour)}
		if !bez.MozeIspraviti(nedavno, sad) || bez.MozeIspraviti(svoje, sad) {
			t.Errorf("%s bez dužnosti: unutar roka %v, nakon roka %v", uloga, bez.MozeIspraviti(nedavno, sad), bez.MozeIspraviti(svoje, sad))
		}
	}
}

// Firma se ne provjerava kad se račun ne da pročitati ili ga nema: dužnost
// se tada ne daje
func TestFirmaRacunaBezRacuna(t *testing.T) {
	s, podrucje := &UserService{}, 1
	citanje := errors.New("baza zaključana")
	if err := s.firmaRacuna(func() (*models.User, error) { return nil, citanje }, models.RoleContractorAreaLeader, &podrucje); !errors.Is(err, citanje) {
		t.Errorf("račun se ne da pročitati: %v", err)
	}
	if err := s.firmaRacuna(func() (*models.User, error) { return nil, nil }, models.RoleContractorAreaDeputy, &podrucje); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("računa nema: %v", err)
	}
	if err := s.firmaRacuna(nil, models.RoleServiceLeaderForeman, &podrucje); err != nil {
		t.Errorf("voditelj usluga ne treba firmu iz registra: %v", err)
	}
}
