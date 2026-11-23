package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Nalozi u obrani u dnevniku usluge A.02 (Odluke (22. 11. 2026.)). Okolina
// je novaUpisiProba: sektor B s područjem 15 i sektor D s područjem 30.
// Dionice B.15.1 i B.15.2 su na području 15, D.30.1 na području 30. Osobe
// su izmišljene.

// obranaProba je izvor stanja obrane i dionica za probu: stanja dionica
// koja se mogu mijenjati i greška čitanja akata
type obranaProba struct {
	stanja map[string]models.DefensePhase
	greska error
}

func (o *obranaProba) stanjaSektora(_ context.Context, sektor string, _ time.Time) (map[string]models.StanjeObrane, error) {
	if o.greska != nil {
		return nil, o.greska
	}
	out := map[string]models.StanjeObrane{}
	for kod, stupanj := range o.stanja {
		if strings.HasPrefix(kod, sektor+".") {
			out[kod] = models.StanjeObrane{Aktivni: []models.AktivniStadij{{Stupanj: stupanj}}}
		}
	}
	return out, nil
}

func dioniceProbe(sektor string, podrucje int, _ string) ([]models.Section, error) {
	sve := []models.Section{
		{Code: "B.15.1", AreaID: 15, SectorID: "B", Description: "Prvi nasip"},
		{Code: "B.15.2", AreaID: 15, SectorID: "B"},
		{Code: "D.30.1", AreaID: 30, SectorID: "D"},
	}
	var out []models.Section
	for _, d := range sve {
		if (sektor == "" || d.SectorID == sektor) && (podrucje == 0 || d.AreaID == podrucje) {
			out = append(out, d)
		}
	}
	return out, nil
}

// uObrani je proba u kojoj je na dionici B.15.1 proglašena redovna obrana
func uObrani(t *testing.T) (*upisiProba, *obranaProba) {
	p := novaUpisiProba(t)
	o := &obranaProba{stanja: map[string]models.DefensePhase{"B.15.1": models.PhaseRegular}}
	p.s.SetObrana(o.stanjaSektora, dioniceProbe)
	return p, o
}

// osoba je izmišljena osoba s jednom dužnošću
func osoba(uloga models.Role, doseg models.ScopeType, sektor string, podrucje int) (*models.User, *models.UserPermissions) {
	perms := ovlastiOsobe(duznost(uloga, doseg, sektor, podrucje, ""))
	return &perms.User, perms
}

// otvoriList otvara list u dnevniku usluge B kao ovlaštenik
func (p *upisiProba) otvoriList(t *testing.T) *models.JournalSheet {
	t.Helper()
	u, perms := ovlastenik(15, models.RoleContractOfficerA2)
	sh, err := p.s.NewSheet(context.Background(), u, perms, p.oUB, p.uslugaB, p.pocetak, "")
	if err != nil {
		t.Fatal(err)
	}
	return sh
}

// Dok na području traje redovna obrana, nalog u dnevnik A.02 pišu
// rukovoditelj dionice i zamjenik, uprava područja i uprava sektora, s
// ulogom uz ime. Dežurni, rukovoditelj dionice drugog područja i izvođač
// ne pišu nalog, a nadzor ostaje ovlaštenik.
func TestNalogUObraniPisuRukovoditelji(t *testing.T) {
	p, _ := uObrani(t)
	ctx := context.Background()
	list := p.otvoriList(t)
	smiju := map[string]struct {
		uloga  models.Role
		doseg  models.ScopeType
		sektor string
		naziv  string
	}{
		"rukovoditelj dionice":  {models.RoleSectionLeader, models.ScopeSection, "B", "Rukovoditelj dionice"},
		"zamjenik na dionici":   {models.RoleSectionDeputy, models.ScopeSection, "B", "Zamjenik rukovoditelja dionice"},
		"rukovoditelj područja": {models.RoleAreaLeader, models.ScopeArea, "B", "Rukovoditelj branjenog područja"},
		"rukovoditelj sektora":  {models.RoleSectorLeader, models.ScopeSector, "B", "Rukovoditelj sektora"},
	}
	for tko, s := range smiju {
		podrucje := 15
		if s.doseg == models.ScopeSector {
			podrucje = 0
		}
		u, perms := osoba(s.uloga, s.doseg, s.sektor, podrucje)
		if !p.s.NalogUObrani(ctx, u, perms, p.oUB, p.uslugaB) {
			t.Errorf("%s ne piše nalog u obrani", tko)
			continue
		}
		e := &models.JournalEntry{Kind: models.EntryKindTask, Text: "dovesti bager na nasip", SectionCode: "B.15.1"}
		if _, err := p.s.AddEntry(ctx, u, perms, p.oUB, p.uslugaB, list, e); err != nil {
			t.Errorf("%s: %v", tko, err)
			continue
		}
		got := p.upis(t, e.ID)
		if got.UlogaDavatelja != s.naziv || got.Status != models.TaskOpen || !got.IsSupervisor() || got.SectionCode != "B.15.1" {
			t.Errorf("%s: nalog %+v", tko, got)
		}
		if got.DaoNalog() != "Pero Perić, "+strings.ToLower(s.naziv[:1])+s.naziv[1:] {
			t.Errorf("%s: tko je dao nalog %q", tko, got.DaoNalog())
		}
		// nadzor ostaje ovlaštenik
		if p.s.CanSupervise(u, perms, p.oUB, p.uslugaB) {
			t.Errorf("%s je nadzor", tko)
		}
		if err := p.s.ConfirmSheet(ctx, u, perms, p.oUB, p.uslugaB, list.ID); err == nil {
			t.Errorf("%s potvrđuje list za nadzor", tko)
		}
		if vrste := p.s.VrsteUpisa(ctx, u, perms, p.oUB, p.uslugaB); strings.Join(vrste, ",") != "RAD,NAPOMENA,NALOG" {
			t.Errorf("%s: vrste upisa %v", tko, vrste)
		}
	}

	nisu := map[string]func() (*models.User, *models.UserPermissions){
		"dežurni operater":            func() (*models.User, *models.UserPermissions) { return osoba(models.RoleOperator, models.ScopeSector, "B", 0) },
		"rukovoditelj dionice drugdje": func() (*models.User, *models.UserPermissions) { return osoba(models.RoleSectionLeader, models.ScopeSection, "D", 30) },
		"vodočuvar":                   func() (*models.User, *models.UserPermissions) { return osoba(models.RoleWaterGuard, models.ScopeSection, "B", 15) },
		"rukovoditelj obrane izvođača": func() (*models.User, *models.UserPermissions) {
			return osoba(models.RoleContractorAreaLeader, models.ScopeArea, "B", 15)
		},
	}
	for tko, f := range nisu {
		u, perms := f()
		e := &models.JournalEntry{Kind: models.EntryKindTask, Text: "dovesti bager"}
		if _, err := p.s.AddEntry(ctx, u, perms, p.oUB, p.uslugaB, list, e); err == nil {
			t.Errorf("%s piše nalog u obrani", tko)
		}
	}

	// ovlaštenikov nalog nosi njegovu ulogu, a vrste upisa su mu iste
	ou, op := ovlastenik(15, models.RoleContractOfficerA2)
	e := &models.JournalEntry{Kind: models.EntryKindTask, Text: "pokositi nasip"}
	if _, err := p.s.AddEntry(ctx, ou, op, p.oUB, p.uslugaB, list, e); err != nil || p.upis(t, e.ID).UlogaDavatelja != models.RoleContractOfficerA2.Label() {
		t.Errorf("nalog ovlaštenika: %v %+v", err, p.upis(t, e.ID))
	}
	if vrste := p.s.VrsteUpisa(ctx, ou, op, p.oUB, p.uslugaB); len(vrste) != len(models.EntryKinds) {
		t.Errorf("vrste upisa ovlaštenika: %v", vrste)
	}
	// rad uz nalog ne nosi ulogu davatelja ni kad je poslana
	u, perms := osoba(models.RoleSectionLeader, models.ScopeSection, "B", 15)
	rad := &models.JournalEntry{Kind: models.EntryKindNote, Text: "nasip pregledan", UlogaDavatelja: "Glavni rukovoditelj"}
	if _, err := p.s.AddEntry(ctx, u, perms, p.oUB, p.uslugaB, list, rad); err != nil || p.upis(t, rad.ID).UlogaDavatelja != "" {
		t.Errorf("napomena nosi ulogu davatelja: %v %+v", err, p.upis(t, rad.ID))
	}
}

// Dionica uz nalog mora biti dionica područja dnevnika; prazna prolazi
func TestNalogUObraniDionicaPodrucja(t *testing.T) {
	p, _ := uObrani(t)
	ctx := context.Background()
	list := p.otvoriList(t)
	u, perms := osoba(models.RoleSectionLeader, models.ScopeSection, "B", 15)
	for _, kod := range []string{"D.30.1", "B.99.1"} {
		e := &models.JournalEntry{Kind: models.EntryKindTask, Text: "dovesti bager", SectionCode: kod}
		if _, err := p.s.AddEntry(ctx, u, perms, p.oUB, p.uslugaB, list, e); err == nil || !strings.Contains(err.Error(), "dionica ovog branjenog područja") {
			t.Errorf("dionica %s: %v", kod, err)
		}
	}
	if _, err := p.s.AddEntry(ctx, u, perms, p.oUB, p.uslugaB, list, &models.JournalEntry{Kind: models.EntryKindTask, Text: "dovesti bager"}); err != nil {
		t.Errorf("nalog bez dionice: %v", err)
	}
	// greška registra dionica ne pušta nalog
	p.s.SetObrana((&obranaProba{stanja: map[string]models.DefensePhase{"B.15.1": models.PhaseRegular}}).stanjaSektora,
		func(string, int, string) ([]models.Section, error) { return nil, errors.New("registar nije dostupan") })
	e := &models.JournalEntry{Kind: models.EntryKindNote, Text: "nasip pregledan", SectionCode: "B.15.1"}
	if _, err := p.s.AddEntry(ctx, u, perms, p.oUB, p.uslugaB, list, e); err == nil {
		t.Error("upis s dionicom prošao bez registra dionica")
	}
}

// Pripremno stanje nije obrana; obrana na drugom području, dnevnik A.03,
// greška čitanja akata i servis bez izvora obrane ne daju nalog
func TestNalogUObraniSamoURedovnojIIzvanrednoj(t *testing.T) {
	p, o := uObrani(t)
	ctx := context.Background()
	u, perms := osoba(models.RoleSectionLeader, models.ScopeSection, "B", 15)
	for _, s := range []struct {
		ime     string
		stupanj models.DefensePhase
		smije   bool
	}{
		{"pripremno stanje", models.PhasePrep, false},
		{"redovna obrana", models.PhaseRegular, true},
		{"izvanredna obrana", models.PhaseEmergency, true},
		{"izvanredno stanje", models.PhaseState, true},
	} {
		o.stanja = map[string]models.DefensePhase{"B.15.2": s.stupanj}
		if got := p.s.NalogUObrani(ctx, u, perms, p.oUB, p.uslugaB); got != s.smije {
			t.Errorf("%s: nalog %v, želim %v", s.ime, got, s.smije)
		}
		if got := len(p.s.DioniceZaNalog(ctx, u, p.oUB, p.uslugaB)) == 2; got != s.smije {
			t.Errorf("%s: dionice uz nalog %v", s.ime, got)
		}
	}
	o.stanja = map[string]models.DefensePhase{"D.30.1": models.PhaseEmergency}
	if p.s.NalogUObrani(ctx, u, perms, p.oUB, p.uslugaB) {
		t.Error("obrana na drugom području daje nalog")
	}
	o.stanja = map[string]models.DefensePhase{"B.15.1": models.PhaseRegular}
	a03 := *p.uslugaB
	a03.Kind = models.JournalKindMaintenanceA03
	if p.s.NalogUObrani(ctx, u, perms, p.oUB, &a03) || p.s.DioniceZaNalog(ctx, u, p.oUB, &a03) != nil {
		t.Error("nalog u obrani u dnevniku A.03")
	}
	if p.s.NalogUObrani(ctx, u, perms, p.oB, p.copB) {
		t.Error("nalog u obrani u dnevniku COP-a")
	}
	izvodjac, _ := osoba(models.RoleServiceLeaderForeman, models.ScopeArea, "B", 15)
	if p.s.DioniceZaNalog(ctx, izvodjac, p.oUB, p.uslugaB) != nil {
		t.Error("izvođaču se nude dionice uz nalog")
	}
	o.greska = errors.New("akti nisu dostupni")
	if p.s.NalogUObrani(ctx, u, perms, p.oUB, p.uslugaB) {
		t.Error("greška čitanja akata daje nalog")
	}
	bez := novaUpisiProba(t)
	if bez.s.NalogUObrani(ctx, u, perms, bez.oUB, bez.uslugaB) {
		t.Error("servis bez izvora obrane daje nalog")
	}
	p.s.SetObrana(o.stanjaSektora, func(string, int, string) ([]models.Section, error) { return nil, errors.New("registar nije dostupan") })
	o.greska = nil
	if p.s.NalogUObrani(ctx, u, perms, p.oUB, p.uslugaB) {
		t.Error("greška registra dionica daje nalog")
	}
}

// Kad obrana prestane, nalog dan u obrani ostaje otvoren, a novi nalog opet
// piše samo nadzor. Tko je nalog dao, može ga otkazati ili označiti
// izvedenim i nakon obrane; tuđi nalog otkazuje samo nadzor, a ponovno ga
// otvara samo nadzor.
func TestNalogNakonObrane(t *testing.T) {
	p, o := uObrani(t)
	ctx := context.Background()
	list := p.otvoriList(t)
	dao, ovlDao := osoba(models.RoleSectionLeader, models.ScopeSection, "B", 15)
	prvi := &models.JournalEntry{Kind: models.EntryKindTask, Text: "dovesti bager"}
	drugi := &models.JournalEntry{Kind: models.EntryKindTask, Text: "postaviti vreće"}
	for _, e := range []*models.JournalEntry{prvi, drugi} {
		if _, err := p.s.AddEntry(ctx, dao, ovlDao, p.oUB, p.uslugaB, list, e); err != nil {
			t.Fatal(err)
		}
	}
	o.stanja = nil
	if _, err := p.s.AddEntry(ctx, dao, ovlDao, p.oUB, p.uslugaB, list, &models.JournalEntry{Kind: models.EntryKindTask, Text: "novi nalog"}); err == nil {
		t.Error("nakon obrane rukovoditelj dionice piše novi nalog")
	}
	if p.upis(t, prvi.ID).Status != models.TaskOpen {
		t.Error("nalog iz obrane nije ostao otvoren")
	}
	// drugi rukovoditelj dionice tuđi nalog ne otkazuje
	drugiRuk, ovlDrugi := osoba(models.RoleSectionDeputy, models.ScopeSection, "B", 15)
	if err := p.s.SetTaskStatus(ctx, drugiRuk, ovlDrugi, p.oUB, p.uslugaB, prvi.ID, models.TaskCancelled); err == nil || !strings.Contains(err.Error(), "samo nadzor") {
		t.Errorf("tuđi nalog otkazuje rukovoditelj dionice: %v", err)
	}
	if err := p.s.SetTaskStatus(ctx, drugiRuk, ovlDrugi, p.oUB, p.uslugaB, prvi.ID, models.TaskDone); err == nil {
		t.Error("tuđi nalog izvedenim označava rukovoditelj dionice")
	}
	// svoj nalog otkazuje i označava izvedenim, ali ga ne otvara ponovno
	if err := p.s.SetTaskStatus(ctx, dao, ovlDao, p.oUB, p.uslugaB, prvi.ID, models.TaskCancelled); err != nil || p.upis(t, prvi.ID).Status != models.TaskCancelled {
		t.Errorf("svoj nalog nije otkazan: %v", err)
	}
	if err := p.s.SetTaskStatus(ctx, dao, ovlDao, p.oUB, p.uslugaB, prvi.ID, models.TaskOpen); err == nil {
		t.Error("svoj nalog ponovno otvara tko ga je dao")
	}
	if err := p.s.SetTaskStatus(ctx, dao, ovlDao, p.oUB, p.uslugaB, drugi.ID, models.TaskDone); err != nil {
		t.Errorf("svoj nalog nije označen izvedenim: %v", err)
	}
	// nadzor ponovno otvara i otkazuje tuđi nalog
	ou, op := ovlastenik(15, models.RoleContractOfficerA2)
	if err := p.s.SetTaskStatus(ctx, ou, op, p.oUB, p.uslugaB, prvi.ID, models.TaskOpen); err != nil {
		t.Errorf("nadzor ne otvara nalog ponovno: %v", err)
	}
	if err := p.s.SetTaskStatus(ctx, ou, op, p.oUB, p.uslugaB, prvi.ID, models.TaskCancelled); err != nil {
		t.Errorf("nadzor ne otkazuje tuđi nalog: %v", err)
	}
}

// Rukovoditelj obrane izvođača i zamjenik vode dnevnik A.02 svoje firme na
// strani izvođača, kao voditelj usluga: otvaraju list, upisuju rad, ljude i
// strojeve, označavaju nalog izvedenim i potvrđuju list za izvođača. Nalog
// ne pišu ni u obrani, a dnevnik druge firme ne vide.
func TestObranaIzvodjacaVodiDnevnikA02(t *testing.T) {
	for _, uloga := range ulogeObraneIzvodjaca {
		t.Run(string(uloga), func(t *testing.T) {
			p, _ := uObrani(t)
			ctx := context.Background()
			p.uslugaB.Contractor = "Primjer d.o.o., Primjerica"
			if err := p.repo.SaveJournal(ctx, p.uslugaB); err != nil {
				t.Fatal(err)
			}
			perms := ovlastiOsobe(duznost(uloga, models.ScopeArea, "B", 15, ""))
			perms.User.OrgName = "PRIMJER DOO"
			u := &perms.User
			if !p.s.VidiDnevnikUsluge(ctx, perms, p.oUB, p.uslugaB) {
				t.Fatal("ne vidi dnevnik svoje firme")
			}
			tudji := *p.uslugaB
			tudji.ID, tudji.Contractor, tudji.CreatedBy = "drugi", "Druga firma d.d.", ""
			if p.s.VidiDnevnikUsluge(ctx, perms, p.oUB, &tudji) {
				t.Error("vidi dnevnik druge firme")
			}
			list, nalog := p.nalog(t, p.uslugaB, p.oUB)
			if _, err := p.s.NewSheet(ctx, u, perms, p.oUB, p.uslugaB, p.pocetak, "druga ekipa"); err != nil {
				t.Errorf("ne otvara list: %v", err)
			}
			rad := &models.JournalEntry{Kind: models.EntryKindWork, Text: "postavljene vreće na nasip"}
			if _, err := p.s.AddEntry(ctx, u, perms, p.oUB, p.uslugaB, list, rad); err != nil || p.upis(t, rad.ID).Side != models.EntrySideContractor {
				t.Errorf("rad izvođača: %v", err)
			}
			upd := *list
			upd.Staff, upd.Machines = "KV RADNIK:4", "BAGER do 100kW:1"
			if err := p.s.UpdateSheet(ctx, perms, p.oUB, p.uslugaB, &upd); err != nil {
				t.Errorf("ne upisuje ljude i strojeve: %v", err)
			}
			if err := p.s.SetTaskStatus(ctx, u, perms, p.oUB, p.uslugaB, nalog.ID, models.TaskDone); err != nil {
				t.Errorf("ne označava nalog izvedenim: %v", err)
			}
			if err := p.s.ConfirmSheet(ctx, u, perms, p.oUB, p.uslugaB, list.ID); err != nil {
				t.Errorf("ne potvrđuje list: %v", err)
			}
			sh, err := p.s.GetSheet(ctx, list.ID)
			if err != nil || sh.ContractorConfirmedBy != u.FullName || sh.SupervisorConfirmedAt != nil || sh.Staff != "KV RADNIK:4" {
				t.Errorf("list nakon izvođača: %+v %v", sh, err)
			}
			if _, err := p.s.AddEntry(ctx, u, perms, p.oUB, p.uslugaB, list, &models.JournalEntry{Kind: models.EntryKindTask, Text: "nalog"}); err == nil {
				t.Error("izvođač piše nalog u tuđe ime")
			}
		})
	}
}

// Globalni administrator nalog piše kao nadzor, bez uloge uz ime; bez
// ovlasti nema ni dužnosti davatelja
func TestUlogaDavateljaBezDuznosti(t *testing.T) {
	p, _ := uObrani(t)
	list := p.otvoriList(t)
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	e := &models.JournalEntry{Kind: models.EntryKindTask, Text: "pokositi nasip"}
	if _, err := p.s.AddEntry(context.Background(), &admin.User, admin, p.oUB, p.uslugaB, list, e); err != nil {
		t.Fatal(err)
	}
	if got := p.upis(t, e.ID); got.UlogaDavatelja != "" || !got.IsSupervisor() {
		t.Errorf("nalog globalnog administratora: %+v", got)
	}
	if duznostDavatelja(nil, p.oUB, p.uslugaB) != nil {
		t.Error("dužnost davatelja bez ovlasti")
	}
}
