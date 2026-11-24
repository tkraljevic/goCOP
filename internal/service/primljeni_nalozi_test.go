package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Primljeni nalog i pomoć drugog područja (Odluke (22. 11. 2026.)).
// Okolina je novaUpisiProba s obranom na dionici B.15.1 (uObrani). Firma
// Primjer d.o.o. vodi dnevnik A.02 na području 15. Osobe su izmišljene.

// davateljiProbe su djelatnici sektora B: rukovoditelj dionice, dežurni
// (nije davatelj), izvođač (nije davatelj) i isključen račun
type davateljiProbe struct {
	dionicar, dezurni, izvodjac, iskljucen models.User
	greska                                 error
}

func noviDavatelji() *davateljiProbe {
	d := &davateljiProbe{}
	d.dionicar = ovlastiOsobe(duznost(models.RoleSectionLeader, models.ScopeSection, "B", 15, "B.15.1")).User
	d.dionicar.FullName = "Ivo Ivić"
	d.dezurni = ovlastiOsobe(duznost(models.RoleOperator, models.ScopeSector, "B", 0, "")).User
	d.izvodjac = ovlastiOsobe(duznost(models.RoleContractorAreaLeader, models.ScopeArea, "B", 15, "")).User
	d.iskljucen = ovlastiOsobe(duznost(models.RoleAreaLeader, models.ScopeArea, "B", 15, "")).User
	d.iskljucen.IsActive = false
	return d
}

func (d *davateljiProbe) osobe(sektor string) ([]models.User, error) {
	if d.greska != nil {
		return nil, d.greska
	}
	if sektor != "B" {
		return nil, nil
	}
	return []models.User{d.dionicar, d.dezurni, d.izvodjac, d.iskljucen}, nil
}

// primljeniProba je proba s obranom, davateljima i izvođačem firme
// Primjer d.o.o. na području 15, s otvorenim listom
type primljeniProba struct {
	*upisiProba
	obrana    *obranaProba
	davatelji *davateljiProbe
	izvodjac  *models.User
	ovlIzv    *models.UserPermissions
	list      *models.JournalSheet
}

func novaPrimljeniProba(t *testing.T) *primljeniProba {
	t.Helper()
	p, o := uObrani(t)
	d := noviDavatelji()
	p.s.SetOsobeSektora(d.osobe)
	p.uslugaB.Contractor = "Primjer d.o.o."
	if err := p.repo.SaveJournal(context.Background(), p.uslugaB); err != nil {
		t.Fatal(err)
	}
	perms := ovlastiOsobe(duznost(models.RoleServiceLeaderForeman, models.ScopeArea, "B", 15, ""))
	perms.User.FullName, perms.User.OrgName = "Marko Markić", "Primjer d.o.o."
	return &primljeniProba{upisiProba: p, obrana: o, davatelji: d, izvodjac: &perms.User, ovlIzv: perms, list: p.otvoriList(t)}
}

// primi upisuje primljeni nalog kao izvođač
func (p *primljeniProba) primi(e *models.JournalEntry) error {
	e.Kind = models.EntryKindTask
	if e.Text == "" {
		e.Text = "postaviti vreće na nasip"
	}
	_, err := p.s.AddEntry(context.Background(), p.izvodjac, p.ovlIzv, p.oUB, p.uslugaB, p.list, e)
	return err
}

// Izvođač u obrani u svoj dnevnik upisuje primljeni nalog s dionicom bilo
// kojeg područja sektora na kojem traje obrana, vremenom i davateljem s
// popisa ili upisanim imenom
func TestPrimljeniNalogUObrani(t *testing.T) {
	p := novaPrimljeniProba(t)
	ctx := context.Background()
	if vrste := p.s.VrsteUpisa(ctx, p.izvodjac, p.ovlIzv, p.oUB, p.uslugaB); strings.Join(vrste, ",") != "RAD,NAPOMENA,NALOG" {
		t.Errorf("vrste upisa izvođača u obrani: %v", vrste)
	}
	davatelji := p.s.DavateljiUObrani("B")
	if len(davatelji) != 1 || davatelji[0].Ime != "Ivo Ivić" || davatelji[0].Uloga != "Rukovoditelj dionice" {
		t.Fatalf("popis davatelja: %+v", davatelji)
	}
	kad := time.Date(2026, 11, 22, 14, 30, 0, 0, models.Zagreb)
	e := &models.JournalEntry{SectionCode: "B.15.1", HappenedAt: &kad, DavateljID: davatelji[0].ID, Davatelj: "krivo ime", Odgovor: models.OdgovorPotvrden}
	if err := p.primi(e); err != nil {
		t.Fatal(err)
	}
	got := p.upis(t, e.ID)
	if !got.Primljen() || got.Side != models.EntrySideContractor || got.Status != models.TaskOpen || got.Davatelj != "Ivo Ivić" ||
		got.UlogaDavatelja != "Rukovoditelj dionice" || got.Odgovor != "" || !got.HappenedAt.Equal(kad) || !got.CekaOdgovor() {
		t.Errorf("primljeni nalog: %+v", got)
	}
	if got.KindLabel() != "Primljeni nalog" || got.DaoNalog() != "Ivo Ivić, rukovoditelj dionice" {
		t.Errorf("prikaz: %q, %q", got.KindLabel(), got.DaoNalog())
	}
	// ime slobodnim tekstom; bez vremena nalog je dan sada
	slobodno := &models.JournalEntry{SectionCode: "B.15.2", Davatelj: "  Ana Anić  "}
	if err := p.primi(slobodno); err != nil {
		t.Fatal(err)
	}
	if got := p.upis(t, slobodno.ID); got.Davatelj != "Ana Anić" || got.DavateljID != "" || got.HappenedAt == nil || got.CekaOdgovor() || got.DaoNalog() != "Ana Anić" {
		t.Errorf("nalog s upisanim imenom: %+v", got)
	}
	// rad uz nalog ne nosi polja primljenog naloga
	rad := &models.JournalEntry{Kind: models.EntryKindWork, Text: "postavljeno 200 vreća", SectionCode: "B.15.1", Davatelj: "Ana Anić"}
	if _, err := p.s.AddEntry(ctx, p.izvodjac, p.ovlIzv, p.oUB, p.uslugaB, p.list, rad); err != nil || p.upis(t, rad.ID).Davatelj != "" {
		t.Errorf("rad uz nalog: %v %+v", err, p.upis(t, rad.ID))
	}
	// Hrvatske vode u tuđi dnevnik ne upisuju polja primljenog naloga
	ou, op := ovlastenik(15, models.RoleContractOfficerA2)
	nalog := &models.JournalEntry{Kind: models.EntryKindTask, Text: "pokositi", DavateljID: uuid.NewString(), Davatelj: "Ana Anić"}
	if _, err := p.s.AddEntry(ctx, ou, op, p.oUB, p.uslugaB, p.list, nalog); err != nil || p.upis(t, nalog.ID).Davatelj != "" || p.upis(t, nalog.ID).Primljen() {
		t.Errorf("nalog ovlaštenika: %v %+v", err, p.upis(t, nalog.ID))
	}
}

// Primljeni nalog traži dionicu na kojoj traje obrana i davatelja; davatelj
// s popisa mora imati dužnost u obrani sektora. Izvan obrane izvođač nalog
// ne upisuje.
func TestPrimljeniNalogProvjere(t *testing.T) {
	p := novaPrimljeniProba(t)
	for _, s := range []struct {
		ime    string
		e      models.JournalEntry
		poruka string
	}{
		{"bez dionice", models.JournalEntry{Davatelj: "Ana Anić"}, "izaberite dionicu"},
		{"dionica bez obrane", models.JournalEntry{SectionCode: "D.30.1", Davatelj: "Ana Anić"}, "na kojoj traje obrana"},
		{"bez davatelja", models.JournalEntry{SectionCode: "B.15.1"}, "tko je dao nalog"},
		{"dežurni nije davatelj", models.JournalEntry{SectionCode: "B.15.1", DavateljID: p.davatelji.dezurni.ID.String()}, "nema dužnost u obrani"},
		{"izvođač nije davatelj", models.JournalEntry{SectionCode: "B.15.1", DavateljID: p.davatelji.izvodjac.ID.String()}, "nema dužnost u obrani"},
		{"isključen račun", models.JournalEntry{SectionCode: "B.15.1", DavateljID: p.davatelji.iskljucen.ID.String()}, "nema dužnost u obrani"},
	} {
		e := s.e
		if err := p.primi(&e); err == nil || !strings.Contains(err.Error(), s.poruka) {
			t.Errorf("%s: %v", s.ime, err)
		}
	}
	// greška popisa: davatelj s popisa ne prolazi, upisano ime prolazi
	p.davatelji.greska = errors.New("imenik nije dostupan")
	if err := p.primi(&models.JournalEntry{SectionCode: "B.15.1", DavateljID: p.davatelji.dionicar.ID.String()}); err == nil {
		t.Error("davatelj s popisa prošao uz grešku imenika")
	}
	if err := p.primi(&models.JournalEntry{SectionCode: "B.15.1", Davatelj: "Ana Anić"}); err != nil {
		t.Errorf("upisano ime uz grešku imenika: %v", err)
	}
	// izvan obrane nema primljenog naloga ni dionica
	p.obrana.stanja = nil
	if err := p.primi(&models.JournalEntry{SectionCode: "B.15.1", Davatelj: "Ana Anić"}); err == nil {
		t.Error("izvan obrane izvođač upisuje nalog")
	}
	if p.s.DioniceZaNalog(context.Background(), p.izvodjac, p.oUB, p.uslugaB) != nil {
		t.Error("izvan obrane izvođaču se nude dionice")
	}
	if p.s.PrimaNalogUObrani(context.Background(), p.izvodjac, p.oUB, p.uslugaD) {
		t.Error("izvođač prima nalog u dnevnik drugog područja")
	}
	bez := novaUpisiProba(t)
	if bez.s.DavateljiUObrani("B") != nil {
		t.Error("popis davatelja bez izvora")
	}
}

// Izvođač s područja 15 na poziv radi na području 16 sektora B, gdje traje
// obrana, a na njegovu području ne: nalog upisuje u svoj dnevnik, s
// dionicom drugog područja
func TestPrimljeniNalogPomocDrugogPodrucja(t *testing.T) {
	p := novaPrimljeniProba(t)
	p.s.SetObrana(p.obrana.stanjaSektora, func(sektor string, podrucje int, _ string) ([]models.Section, error) {
		var out []models.Section
		for _, d := range []models.Section{{Code: "B.15.1", AreaID: 15, SectorID: "B"}, {Code: "B.16.3", AreaID: 16, SectorID: "B"}} {
			if (sektor == "" || d.SectorID == sektor) && (podrucje == 0 || d.AreaID == podrucje) {
				out = append(out, d)
			}
		}
		return out, nil
	})
	p.obrana.stanja = map[string]models.DefensePhase{"B.16.3": models.PhaseEmergency}
	dionice := p.s.DioniceZaNalog(context.Background(), p.izvodjac, p.oUB, p.uslugaB)
	if len(dionice) != 1 || dionice[0].Code != "B.16.3" {
		t.Fatalf("dionice za pomoć: %+v", dionice)
	}
	e := &models.JournalEntry{SectionCode: "B.16.3", DavateljID: p.davatelji.dionicar.ID.String()}
	if err := p.primi(e); err != nil {
		t.Fatalf("pomoć drugom području: %v", err)
	}
	if p.upis(t, e.ID).JournalID != p.uslugaB.ID {
		t.Error("nalog nije u dnevniku izvođača")
	}
	// rukovoditelj dionice područja 15 bez obrane na svom području ne piše nalog
	dionicar := ovlastiOsobe(duznost(models.RoleSectionLeader, models.ScopeSection, "B", 15, ""))
	if p.s.NalogUObrani(context.Background(), &dionicar.User, dionicar, p.oUB, p.uslugaB) {
		t.Error("nalog u obrani bez obrane na području dnevnika")
	}
}

// Davatelj potvrđuje primljeni nalog ili javlja da nije tako rečeno, jednom;
// drugi ne odgovaraju, a davatelj ništa drugo ne mijenja
func TestOdgovorNaPrimljeniNalog(t *testing.T) {
	p := novaPrimljeniProba(t)
	ctx := context.Background()
	davatelj := &p.davatelji.dionicar
	prvi := &models.JournalEntry{SectionCode: "B.15.1", DavateljID: davatelj.ID.String()}
	drugi := &models.JournalEntry{SectionCode: "B.15.1", DavateljID: davatelj.ID.String()}
	slobodni := &models.JournalEntry{SectionCode: "B.15.1", Davatelj: "Ana Anić"}
	for _, e := range []*models.JournalEntry{prvi, drugi, slobodni} {
		if err := p.primi(e); err != nil {
			t.Fatal(err)
		}
	}
	drugiCovjek := &models.User{ID: uuid.New(), FullName: "Pero Perić"}
	for ime, s := range map[string]struct {
		u        *models.User
		id, odg  string
		napomena string
		poruka   string
	}{
		"bez prijave":          {nil, prvi.ID, models.OdgovorPotvrden, "", "prijavu"},
		"netko drugi":          {drugiCovjek, prvi.ID, models.OdgovorPotvrden, "", "samo onaj tko ga je dao"},
		"slobodno ime":         {davatelj, slobodni.ID, models.OdgovorPotvrden, "", "samo onaj tko ga je dao"},
		"nepoznat odgovor":     {davatelj, prvi.ID, "MOŽDA", "", "nepoznat odgovor"},
		"nije tako bez svoga":  {davatelj, prvi.ID, models.OdgovorNijeTako, " ", "što ste zapravo rekli"},
		"upis drugog dnevnika": {davatelj, "nema", models.OdgovorPotvrden, "", "nije pronađen"},
	} {
		if err := p.s.OdgovoriNaNalog(ctx, s.u, p.uslugaB, s.id, s.odg, s.napomena); err == nil || !strings.Contains(err.Error(), s.poruka) {
			t.Errorf("%s: %v", ime, err)
		}
	}
	if err := p.s.OdgovoriNaNalog(ctx, davatelj, p.uslugaB, prvi.ID, models.OdgovorPotvrden, ""); err != nil {
		t.Fatal(err)
	}
	if got := p.upis(t, prvi.ID); got.Odgovor != models.OdgovorPotvrden || got.OdgovorAt == nil || got.CekaOdgovor() || got.Status != models.TaskOpen || got.Text != "postaviti vreće na nasip" {
		t.Errorf("potvrđen nalog: %+v", got)
	}
	if err := p.s.OdgovoriNaNalog(ctx, davatelj, p.uslugaB, prvi.ID, models.OdgovorNijeTako, "drugo"); err == nil || !strings.Contains(err.Error(), "već odgovorili") {
		t.Errorf("drugi odgovor: %v", err)
	}
	if err := p.s.OdgovoriNaNalog(ctx, davatelj, p.uslugaB, drugi.ID, models.OdgovorNijeTako, "  rekao sam B.15.2  "); err != nil {
		t.Fatal(err)
	}
	if got := p.upis(t, drugi.ID); got.Odgovor != models.OdgovorNijeTako || got.OdgovorNapomena != "rekao sam B.15.2" || got.OdgovorLabel() == "" {
		t.Errorf("nalog koji nije tako rečen: %+v", got)
	}
	// storniran nalog i ovjeren dnevnik ne primaju odgovor
	treci := &models.JournalEntry{SectionCode: "B.15.1", DavateljID: davatelj.ID.String()}
	if err := p.primi(treci); err != nil {
		t.Fatal(err)
	}
	if err := p.s.VoidEntry(ctx, p.izvodjac, p.ovlIzv, p.oUB, p.uslugaB, treci.ID, "krivo upisano"); err != nil {
		t.Fatal(err)
	}
	if err := p.s.OdgovoriNaNalog(ctx, davatelj, p.uslugaB, treci.ID, models.OdgovorPotvrden, ""); err == nil {
		t.Error("odgovor na storniran nalog")
	}
	ovjeren := *p.uslugaB
	sad := time.Now()
	ovjeren.ZakljucenoAt = &sad
	if err := p.s.OdgovoriNaNalog(ctx, davatelj, &ovjeren, slobodni.ID, models.OdgovorPotvrden, ""); err == nil || !strings.Contains(err.Error(), "ovjereni dnevnik") {
		t.Errorf("odgovor u ovjerenom dnevniku: %v", err)
	}
}

// Davatelj na Naslovnoj vidi naloge koje je dao: primljene koji su otvoreni
// ili čekaju njegov odgovor i one koje je sam upisao dok su otvoreni. Na
// stranici dionice vide se nalozi svih firmi na dionici, a izvođač ih ne
// vidi.
func TestNaloziDavateljaINaDionici(t *testing.T) {
	p := novaPrimljeniProba(t)
	ctx := context.Background()
	davatelj := &p.davatelji.dionicar
	primljeni := &models.JournalEntry{SectionCode: "B.15.1", DavateljID: davatelj.ID.String(), Text: "postaviti vreće"}
	if err := p.primi(primljeni); err != nil {
		t.Fatal(err)
	}
	perms := models.NewUserPermissions(*davatelj)
	svoj := &models.JournalEntry{Kind: models.EntryKindTask, Text: "dovesti bager", SectionCode: "B.15.1"}
	if _, err := p.s.AddEntry(ctx, davatelj, perms, p.oUB, p.uslugaB, p.list, svoj); err != nil {
		t.Fatal(err)
	}
	// izvedeni nalozi na B.15.2: današnji i onaj s lista od 1. 10.
	_, op := ovlastenik(15, models.RoleContractOfficerA2)
	danas, err := p.s.NewSheet(ctx, &op.User, op, p.oUB, p.uslugaB, time.Now(), "")
	if err != nil {
		t.Fatal(err)
	}
	gotovo := &models.JournalEntry{Kind: models.EntryKindTask, Text: "gotov nalog", SectionCode: "B.15.2"}
	stari := &models.JournalEntry{Kind: models.EntryKindTask, Text: "stari nalog", SectionCode: "B.15.2"}
	for _, n := range []struct {
		e    *models.JournalEntry
		list *models.JournalSheet
	}{{gotovo, danas}, {stari, p.list}} {
		if _, err := p.s.AddEntry(ctx, davatelj, perms, p.oUB, p.uslugaB, n.list, n.e); err != nil {
			t.Fatal(err)
		}
		if err := p.s.SetTaskStatus(ctx, davatelj, perms, p.oUB, p.uslugaB, n.e.ID, models.TaskDone); err != nil {
			t.Fatal(err)
		}
	}
	// na dionici su otvoreni nalozi, najnoviji prvi
	if n, err := p.s.NaloziNaDionici(ctx, op, "B.15.1"); err != nil || len(n) != 2 || n[0].ID != svoj.ID || n[1].ID != primljeni.ID {
		t.Errorf("otvoreni nalozi na dionici: %+v, %v", n, err)
	}
	nalozi, err := p.s.NaloziDavatelja(ctx, davatelj)
	if err != nil || len(nalozi) != 2 || nalozi[0].Firma != "Primjer d.o.o." || nalozi[0].Podrucje != 15 {
		t.Fatalf("nalozi davatelja: %+v, %v", nalozi, err)
	}
	// izveden primljeni nalog ostaje dok davatelj ne odgovori
	if err := p.s.SetTaskStatus(ctx, p.izvodjac, p.ovlIzv, p.oUB, p.uslugaB, primljeni.ID, models.TaskDone); err != nil {
		t.Fatal(err)
	}
	if nalozi, err = p.s.NaloziDavatelja(ctx, davatelj); err != nil || len(nalozi) != 2 {
		t.Errorf("izveden primljeni nalog bez odgovora nestao: %+v, %v", nalozi, err)
	}
	if err := p.s.OdgovoriNaNalog(ctx, davatelj, p.uslugaB, primljeni.ID, models.OdgovorPotvrden, ""); err != nil {
		t.Fatal(err)
	}
	if nalozi, err = p.s.NaloziDavatelja(ctx, davatelj); err != nil || len(nalozi) != 1 || nalozi[0].ID != svoj.ID {
		t.Errorf("nakon odgovora: %+v, %v", nalozi, err)
	}
	if nalozi, err := p.s.NaloziDavatelja(ctx, nil); nalozi != nil || err != nil {
		t.Error("nalozi bez osobe")
	}

	// primljeni nalog je izveden, potvrđen i s lista od 1. 10., pa ga više nema
	naDionici, err := p.s.NaloziNaDionici(ctx, op, "B.15.1")
	if err != nil || len(naDionici) != 1 || naDionici[0].ID != svoj.ID {
		t.Errorf("nalozi na dionici: %+v, %v", naDionici, err)
	}
	if n, err := p.s.NaloziNaDionici(ctx, op, "B.15.2"); err != nil || len(n) != 1 || n[0].ID != gotovo.ID {
		t.Errorf("izvedeni nalozi: samo današnji, a ne stariji od %d dana: %+v, %v", DanaNaDionici, n, err)
	}
	if n, err := p.s.NaloziNaDionici(ctx, p.ovlIzv, "B.15.1"); n != nil || err != nil {
		t.Error("izvođač vidi naloge na dionici")
	}
}

// Dužnost daje nalog u sektoru samo kad je u tom sektoru; dužnost bez
// sektora ne daje
func TestDajeNalogUSektoru(t *testing.T) {
	if !dajeNalogUSektoru(duznost(models.RoleSectorLeader, models.ScopeSector, "B", 0, ""), "B") {
		t.Error("rukovoditelj sektora ne daje nalog u svom sektoru")
	}
	if dajeNalogUSektoru(duznost(models.RoleSectorLeader, models.ScopeSector, "D", 0, ""), "B") {
		t.Error("rukovoditelj drugog sektora daje nalog")
	}
	if dajeNalogUSektoru(duznost(models.RoleNationalLeader, models.ScopeAll, "", 0, ""), "B") {
		t.Error("dužnost bez sektora daje nalog u sektoru")
	}
}

// Greška čitanja baze vraća se, a ne prazan popis
func TestNaloziIzvanDnevnikaGreskaBaze(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "zatvorena.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewJournalService(repository.NewJournalRepository(baza, ledger.New(baza, "test")), nil, nil)
	ctx := context.Background()
	u := &models.User{ID: uuid.New()}
	perms := models.NewUserPermissions(*u)
	if _, err := s.NaloziDavatelja(ctx, u); err != nil {
		t.Fatal(err)
	}
	baza.Close()
	if _, err := s.NaloziDavatelja(ctx, u); err == nil {
		t.Error("nalozi davatelja uz zatvorenu bazu")
	}
	if _, err := s.NaloziNaDionici(ctx, perms, "B.15.1"); err == nil {
		t.Error("nalozi na dionici uz zatvorenu bazu")
	}
	if _, err := s.uzDnevnike(ctx, []models.JournalEntry{{JournalID: "j1"}}); err == nil {
		t.Error("dnevnik naloga uz zatvorenu bazu")
	}
}
