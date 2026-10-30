package repository

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/sadrzaj"
)

// dnevniciProbe su dnevnici za provjeru popisa: dnevnik održavanja s dva
// lista i četiri upisa, dnevnik obrane s rupama u brojevima te dva dnevnika
// COP-a (sektor B uz područje 34, sektor A bez područja)
type dnevniciProbe struct {
	repo                           *JournalRepository
	odrzavanje, obrana, copB, copA *models.Journal
	list1, list2                   *models.JournalSheet
	rad, nalog                     *models.JournalEntry
}

func noviDnevniciProbe(t *testing.T) *dnevniciProbe {
	t.Helper()
	baza, rec := bazaSPodrucjem(t)
	sp, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "sadrzaj.db"))
	nuzno(t, err)
	prijasnje := Spremiste()
	SetSpremiste(sp)
	t.Cleanup(func() {
		SetSpremiste(prijasnje)
		_ = sp.Zatvori()
	})
	for _, q := range []string{
		`UPDATE sectors SET center_cop = 'COP Osijek' WHERE id = 'B'`,
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('A', 'A', 'VGO', 'COP Zagreb')`,
		`INSERT INTO watercourses (code, official_name, name, kind) VALUES ('rijeka-drava', 'rijeka Drava', 'Drava', 'rijeka')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	mr := NewMaintenanceRepository(baza, rec)
	mw := &models.MaintainedWater{AreaID: 34, WatercourseCode: "rijeka-drava", Name: "Drava"}
	nuzno(t, mr.UpsertWater(ctx, mw))
	wi := &models.WorkItem{AreaID: 34, Number: "225", Description: "košnja trave", Unit: "ha", Active: true}
	nuzno(t, mr.SaveItem(ctx, wi))

	p := &dnevniciProbe{repo: NewJournalRepository(baza, rec)}
	dan := func(m, d int) time.Time { return time.Date(2026, time.Month(m), d, 0, 0, 0, 0, models.Zagreb) }
	pocetakB, pocetakA := dan(3, 1), dan(2, 1)
	podrucje := 34
	p.odrzavanje = &models.Journal{AreaID: 34, Kind: models.JournalKindMaintenanceA02, Title: "Održavanje 2026", Year: 2026}
	p.obrana = &models.Journal{AreaID: 34, Kind: models.JournalKindDefense, Title: "Obrana 2025", Year: 2025}
	p.copB = &models.Journal{CentarSektor: "B", CentarPodrucje: &podrucje, Kind: models.JournalKindDefense, Title: "Dežurstvo COP Osijek", Year: 2026, StartedAt: &pocetakB}
	p.copA = &models.Journal{CentarSektor: "A", Kind: models.JournalKindDefense, Title: "Dežurstvo COP Zagreb", Year: 2026, StartedAt: &pocetakA}
	for _, j := range []*models.Journal{p.odrzavanje, p.obrana, p.copB, p.copA} {
		nuzno(t, p.repo.SaveJournal(ctx, j))
	}

	p.list1 = &models.JournalSheet{JournalID: p.odrzavanje.ID, Date: dan(3, 10), Conditions: "sunčano"}
	p.list2 = &models.JournalSheet{JournalID: p.odrzavanje.ID, Date: dan(3, 11)}
	nuzno(t, p.repo.SaveSheet(ctx, p.list1))
	nuzno(t, p.repo.SaveSheet(ctx, p.list2))
	rok, sati := dan(3, 15), 6.0
	p.rad = &models.JournalEntry{JournalID: p.odrzavanje.ID, SheetID: p.list1.ID, Date: dan(3, 10), Kind: models.EntryKindWork,
		MaintainedWaterID: mw.ID, WorkItemID: wi.ID, Text: "pokošeno 2 ha", Hours: &sati}
	p.nalog = &models.JournalEntry{JournalID: p.odrzavanje.ID, SheetID: p.list1.ID, Date: dan(3, 10), Kind: models.EntryKindTask,
		Text: "ukloniti nanos", DueDate: &rok, Status: models.TaskOpen}
	for _, e := range []*models.JournalEntry{
		p.rad, p.nalog,
		{JournalID: p.odrzavanje.ID, SheetID: p.list2.ID, Date: dan(3, 11), Kind: models.EntryKindTask, Text: "storniran", Status: models.TaskOpen,
			Voided: true, VoidReason: "pogrešan upis"},
		{JournalID: p.odrzavanje.ID, SheetID: p.list2.ID, Date: dan(3, 11), Kind: models.EntryKindTask, Text: "izveden", Status: models.TaskDone},
		{JournalID: p.copB.ID, Date: dan(3, 10), Kind: models.EntryKindReport, Text: "dojava s terena"},
		{JournalID: p.copB.ID, Date: dan(3, 10), Kind: models.EntryKindNote, Text: "napomena"},
		{JournalID: p.copB.ID, Date: dan(3, 11), Kind: models.EntryKindDuty, Text: "primopredaja"},
	} {
		nuzno(t, p.repo.SaveEntry(ctx, e))
	}
	return p
}

func idDnevnika(l []models.Journal) []string {
	var out []string
	for _, j := range l {
		out = append(out, j.Title)
	}
	return out
}

func tekstoviUpisa(l []models.JournalEntry) []string {
	var out []string
	for _, e := range l {
		out = append(out, e.Text)
	}
	return out
}

// Popis dnevnika po području ili svih: najnovija godina prva, pa vrsta i
// naslov; uz svaki broj listova, zadnji list, otvoreni nalozi i naziv
func TestPopisDnevnikaSBrojevima(t *testing.T) {
	p := noviDnevniciProbe(t)
	ctx := context.Background()
	l, err := p.repo.ListJournals(ctx, 34)
	if err != nil {
		t.Fatal(err)
	}
	if got := idDnevnika(l); !reflect.DeepEqual(got, []string{"Održavanje 2026", "Obrana 2025"}) {
		t.Errorf("po području %v", got)
	}
	o := l[0]
	if o.SheetCount != 2 || o.LastSheetOn != "2026-03-11" || o.OpenTasks != 1 || o.AreaName != "Drava i Dunav" || o.CentarNaziv != "" {
		t.Errorf("dnevnik održavanja %+v", o)
	}
	l, _ = p.repo.ListJournals(ctx, 0)
	if got := idDnevnika(l); !reflect.DeepEqual(got, []string{"Dežurstvo COP Osijek", "Dežurstvo COP Zagreb", "Održavanje 2026", "Obrana 2025"}) {
		t.Errorf("svi %v", got)
	}
	if _, err := NewJournalRepository(zatvorenaBaza(t), nil).ListJournals(ctx, 0); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}

// Dnevnici COP-a: dani dežurstva umjesto listova, naziv centra uz
// područje; popis po sektoru i centri s brojem dnevnika
func TestDnevniciCOPa(t *testing.T) {
	p := noviDnevniciProbe(t)
	ctx := context.Background()
	l, err := p.repo.ListCOPJournals(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := idDnevnika(l); !reflect.DeepEqual(got, []string{"Dežurstvo COP Osijek", "Dežurstvo COP Zagreb"}) {
		t.Fatalf("dnevnici COP-a %v", got)
	}
	if b := l[0]; b.SheetCount != 2 || b.LastSheetOn != "2026-03-11" || b.CentarNaziv != "COP Osijek — Drava i Dunav" || b.AreaID != 0 {
		t.Errorf("COP Osijek %+v", b)
	}
	if a := l[1]; a.SheetCount != 0 || a.CentarNaziv != "COP Zagreb" || a.CentarPodrucje != nil {
		t.Errorf("COP Zagreb %+v", a)
	}
	if l, _ := p.repo.ListCOPJournals(ctx, "A"); !reflect.DeepEqual(idDnevnika(l), []string{"Dežurstvo COP Zagreb"}) {
		t.Errorf("sektor A %v", idDnevnika(l))
	}
	centri, err := p.repo.CentriSDnevnicima(ctx)
	if want := []models.Centar{{Sektor: "B", Naziv: "COP Osijek", Dnevnika: 1}, {Sektor: "A", Naziv: "COP Zagreb", Dnevnika: 1}}; err != nil || !reflect.DeepEqual(centri, want) {
		t.Errorf("centri %+v, %v", centri, err)
	}
	vrste, err := p.repo.BrojPoVrstama(ctx)
	if want := map[string]int{models.JournalKindDefense: 3, models.JournalKindMaintenanceA02: 1}; err != nil || !reflect.DeepEqual(vrste, want) {
		t.Errorf("po vrstama %v, %v", vrste, err)
	}
	zatvoreno := NewJournalRepository(zatvorenaBaza(t), nil)
	if _, err := zatvoreno.ListCOPJournals(ctx, ""); err == nil {
		t.Error("dnevnici COP-a: zatvorena baza bez greške")
	}
	if _, err := zatvoreno.CentriSDnevnicima(ctx); err == nil {
		t.Error("centri: zatvorena baza bez greške")
	}
	if _, err := zatvoreno.BrojPoVrstama(ctx); err == nil {
		t.Error("po vrstama: zatvorena baza bez greške")
	}
}

// Listovi: najnoviji prvi, s brojem upisa i rednim brojem u dnevniku
func TestListoviDnevnika(t *testing.T) {
	p := noviDnevniciProbe(t)
	ctx := context.Background()
	l, err := p.repo.ListSheets(ctx, p.odrzavanje.ID)
	if err != nil || len(l) != 2 {
		t.Fatalf("listovi %+v, %v", l, err)
	}
	if l[0].ID != p.list2.ID || l[0].Number != 2 || l[0].EntryCount != 2 || l[1].Number != 1 || l[1].Conditions != "sunčano" || l[1].EntryCount != 2 {
		t.Errorf("listovi %+v", l)
	}
	if !l[1].Date.Equal(time.Date(2026, 3, 10, 0, 0, 0, 0, models.Zagreb)) {
		t.Errorf("datum lista %v", l[1].Date)
	}
	if s, err := p.repo.GetSheet(ctx, "nema-ga"); err != nil || s != nil {
		t.Errorf("nepostojeći list %+v, %v", s, err)
	}
	if _, err := NewJournalRepository(zatvorenaBaza(t), nil).ListSheets(ctx, p.odrzavanje.ID); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}

// Upisi lista, dnevnika i otvoreni nalozi (bez storniranih i izvedenih);
// upis nosi naziv lokacije iz registra i stavku radova
func TestUpisiDnevnika(t *testing.T) {
	p := noviDnevniciProbe(t)
	ctx := context.Background()
	l, err := p.repo.EntriesForSheet(ctx, p.list1.ID)
	if err != nil || !reflect.DeepEqual(tekstoviUpisa(l), []string{"pokošeno 2 ha", "ukloniti nanos"}) {
		t.Errorf("upisi lista %v, %v", tekstoviUpisa(l), err)
	}
	l, err = p.repo.OpenTasks(ctx, p.odrzavanje.ID)
	if err != nil || !reflect.DeepEqual(tekstoviUpisa(l), []string{"ukloniti nanos"}) {
		t.Errorf("otvoreni nalozi %v, %v", tekstoviUpisa(l), err)
	}
	if l[0].DueDate == nil || !l[0].DueDate.Equal(time.Date(2026, 3, 15, 0, 0, 0, 0, models.Zagreb)) {
		t.Errorf("rok naloga %v", l[0].DueDate)
	}
	l, err = p.repo.EntriesForJournal(ctx, p.odrzavanje.ID)
	if err != nil || !reflect.DeepEqual(tekstoviUpisa(l), []string{"pokošeno 2 ha", "ukloniti nanos", "storniran", "izveden"}) {
		t.Errorf("upisi dnevnika %v, %v", tekstoviUpisa(l), err)
	}
	if !l[2].Voided || l[2].VoidReason != "pogrešan upis" || l[3].Number != 4 {
		t.Errorf("storniran i četvrti upis %+v %+v", l[2], l[3])
	}

	e, err := p.repo.GetEntry(ctx, p.rad.ID)
	if err != nil || e == nil || e.LocationName != "rijeka Drava" || e.WorkItemText != "košnja trave" || e.WorkItemNo != "225" || e.Hours == nil || *e.Hours != 6 || e.Number != 1 {
		t.Errorf("upis rada %+v, %v", e, err)
	}
	if e, err := p.repo.GetEntry(ctx, "nema-ga"); err != nil || e != nil {
		t.Errorf("nepostojeći upis %+v, %v", e, err)
	}
	if _, err := NewJournalRepository(zatvorenaBaza(t), nil).EntriesForJournal(ctx, p.odrzavanje.ID); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}

// Rupe u brojevima: brojevi koji nedostaju od 1 do zadnjeg upisa;
// dvostruki broj nije rupa
func TestRupeUBrojevimaUpisa(t *testing.T) {
	p := noviDnevniciProbe(t)
	ctx := context.Background()
	if g, err := p.repo.NumberGaps(ctx, p.odrzavanje.ID); err != nil || g != nil {
		t.Errorf("dnevnik bez rupa %v, %v", g, err)
	}
	for _, n := range []int{2, 2, 5, 7} {
		nuzno(t, p.repo.SaveEntry(ctx, &models.JournalEntry{JournalID: p.obrana.ID, Number: n, Date: time.Date(2025, 4, 1, 0, 0, 0, 0, models.Zagreb),
			Kind: models.EntryKindNote, Text: "napomena"}))
	}
	if g, err := p.repo.NumberGaps(ctx, p.obrana.ID); err != nil || !reflect.DeepEqual(g, []int{1, 3, 4, 6}) {
		t.Errorf("rupe %v, %v; htio [1 3 4 6]", g, err)
	}
	if _, err := NewJournalRepository(zatvorenaBaza(t), nil).NumberGaps(ctx, p.obrana.ID); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}

// Ovjera dnevnika COP-a traži dnevnik COP-a, potpisnika i PDF; ovjeren
// dnevnik se ne ovjerava ponovno, a izvornik se čita kako je upisan
func TestOvjeraDnevnikaCOPaIIzvornik(t *testing.T) {
	p := noviDnevniciProbe(t)
	ctx := context.Background()
	if b, err := p.repo.JournalIzvornik(ctx, p.copB.ID); err != nil || b != nil {
		t.Fatalf("izvornik prije ovjere %q, %v", b, err)
	}
	kad := time.Date(2026, 3, 31, 20, 0, 0, 0, models.Zagreb)
	pdf := []byte("%PDF-1.7\nzapisnik dezurstva")
	ovjeren := func(j models.Journal) *models.Journal {
		j.ZakljucioID, j.Zakljucio, j.ZakljucenoAt = "r-1", "Ana Anić", &kad
		return &j
	}
	for _, s := range []struct {
		ime  string
		j    *models.Journal
		pdf  []byte
		htio string
	}{
		{"bez dnevnika", nil, pdf, "dnevnik COP-a"},
		{"dnevnik održavanja", ovjeren(*p.odrzavanje), pdf, "dnevnik COP-a"},
		{"bez potpisnika", p.copB, pdf, "bez potpisnika"},
		{"nije PDF", ovjeren(*p.copB), []byte("<html>ispis</html>"), "nije PDF"},
	} {
		if err := p.repo.SaveOvjeraCOP(ctx, s.j, s.pdf); err == nil || !strings.Contains(err.Error(), s.htio) {
			t.Errorf("%s: %v", s.ime, err)
		}
	}
	if g, _ := p.repo.GetJournal(ctx, p.copB.ID); g.ZakljucenoAt != nil {
		t.Fatal("odbijena ovjera je upisana")
	}

	nuzno(t, p.repo.SaveOvjeraCOP(ctx, ovjeren(*p.copB), pdf))
	g, _ := p.repo.GetJournal(ctx, p.copB.ID)
	if g.ZakljucenoAt == nil || !g.ZakljucenoAt.Equal(kad) || g.Zakljucio != "Ana Anić" {
		t.Errorf("ovjera %+v", g)
	}
	if b, err := p.repo.JournalIzvornik(ctx, p.copB.ID); err != nil || string(b) != string(pdf) {
		t.Errorf("izvornik %q, %v", b, err)
	}
	if err := p.repo.SaveOvjeraCOP(ctx, ovjeren(*p.copB), []byte("%PDF-1.7\ndrugi")); err == nil || !strings.Contains(err.Error(), "već ovjeren") {
		t.Errorf("druga ovjera: %v", err)
	}
	if b, _ := p.repo.JournalIzvornik(ctx, p.copB.ID); string(b) != string(pdf) {
		t.Errorf("druga ovjera promijenila izvornik: %q", b)
	}
	if _, err := NewJournalRepository(zatvorenaBaza(t), nil).JournalIzvornik(ctx, p.copB.ID); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}
