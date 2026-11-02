package web

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// dozvoleUsluge su prava osobe s ulogom na BP 16 sektora B
func dozvoleUsluge(uloga models.Role) *models.UserPermissions {
	p := dozvoleNaslovne(false, uloga)
	p.User.Duties[0].SectorID, p.User.Duties[0].AreaID = nizNaslovne("B"), brojNaslovne(16)
	p.User.OrgName = "Vodogradnja Proba d.o.o."
	return p
}

// dnevnikProbe je otvoren dnevnik radova s današnjim listom i nalozima
func dnevnikProbe(sad time.Time) dnevnikRadova {
	jucer, prije := sad.Add(-3*time.Hour), sad.AddDate(0, 0, -2)
	rok := sad.AddDate(0, 0, -1)
	return dnevnikRadova{
		Dnevnik:  &models.Journal{ID: "dn", Kind: models.JournalKindMaintenanceA02, Contractor: "Vodogradnja Proba d.o.o.", ContractorLead: "Pero Proba", Contract: "Ugovor 12/2026"},
		Program:  "A.02",
		Podrucje: 16,
		Listovi: []models.JournalSheet{
			{ID: "l-danas", Date: sad, Staff: "KV RADNIK:6", Machines: "KAMION:2"},
			{ID: "l-jucer", Date: sad.AddDate(0, 0, -1), ContractorConfirmedAt: &jucer},
			{ID: "l-prije", Date: prije, ContractorConfirmedAt: &prije},
			{ID: "l-potvrden", Date: prije, ContractorConfirmedAt: &prije, SupervisorConfirmedAt: &prije},
		},
		Upisi: []models.JournalEntry{
			{SheetID: "l-prije", Kind: models.EntryKindTask, Status: models.TaskOpen, Text: "dovezi pijesak", Date: prije, DueDate: &rok, UserName: "Ana Proba"},
			{SheetID: "l-danas", Kind: models.EntryKindTask, Status: models.TaskOpen, Text: "pripremi bager", Date: sad, Number: 2},
			{SheetID: "l-jucer", Kind: models.EntryKindTask, Status: models.TaskDone, Text: "očisti rešetku", UpdatedAt: sad.Add(-time.Hour)},
			{SheetID: "l-prije", Kind: models.EntryKindTask, Status: models.TaskDone, Text: "staro", UpdatedAt: prije},
			{Kind: models.EntryKindTask, Status: models.TaskOpen, Text: "storniran", Voided: true},
			{Kind: models.EntryKindWork, Text: "rad"},
		},
	}
}

// Uloge: ovlaštenik i zamjenik po programu, voditelj usluga izvođača
func TestDuznostiUsluga(t *testing.T) {
	for uloga, zeli := range map[models.Role]string{models.RoleContractOfficerA2: uUgovor, models.RoleContractDeputyA3: uUgovor, models.RoleServiceLeaderForeman: uUsluga} {
		p := dozvoleUsluge(uloga)
		if u := ulogaNaslovne(p, sadNaslovne); u != zeli || duznostUsluge(p, sadNaslovne) == nil {
			t.Errorf("%s: %s", uloga, u)
		}
	}
	if duznostUgovora(&dozvoleNaslovne(false, models.RoleContractOfficerA2).User, sadNaslovne) != nil || duznostUsluga(&dozvoleNaslovne(false, models.RoleServiceLeaderForeman).User, sadNaslovne) != nil {
		t.Error("dužnost bez područja")
	}
}

// Kartica ugovora: izvođač, ugovor i ljudi na terenu s današnjeg lista
func TestKarticaUgovora(t *testing.T) {
	d := dnevnikProbe(sadNaslovne)
	k := karticaUgovora(d, sadNaslovne)
	if k.Naslov != "Ugovor A.02 · BP 16" || k.URL != "/dnevnici/dn" || len(k.Redovi) != 3 {
		t.Fatalf("kartica: %+v", k)
	}
	if r := k.Redovi[0]; r.Glavno != "Izvođač: Vodogradnja Proba d.o.o." || r.Sporedno != "voditelj usluga Pero Proba" {
		t.Errorf("izvođač: %+v", r)
	}
	if r := k.Redovi[2]; r.Sporedno != "KV RADNIK: 6 · KAMION: 2" || r.URL != "/dnevnici/dn/listovi/l-danas" {
		t.Errorf("na terenu: %+v", r)
	}
	d.Listovi[0].Staff, d.Listovi[0].Machines = "", ""
	if r := redakNaTerenu(d, sadNaslovne); r.Sporedno != "Na listu još nisu upisani ljudi ni strojevi." {
		t.Errorf("list bez ljudi: %+v", r)
	}
	if r := redakNaTerenu(dnevnikRadova{Dnevnik: d.Dnevnik}, sadNaslovne); r.Sporedno != "Današnji list još nije otvoren." {
		t.Errorf("bez lista: %+v", r)
	}
	bez := karticaUgovora(dnevnikRadova{Program: "A.03", Podrucje: 34}, sadNaslovne)
	if bez.Prazno == nil || bez.Prazno.Tekst != "Dnevnik radova A.03 za BP 34 još nije otvoren." {
		t.Errorf("bez dnevnika: %+v", bez)
	}
	if k := karticaUgovora(dnevnikRadova{Dnevnik: &models.Journal{ID: "dn"}, Program: "A.02", Podrucje: 16}, sadNaslovne); len(k.Redovi) != 1 {
		t.Errorf("dnevnik bez izvođača i ugovora: %+v", k.Redovi)
	}
}

// Za riješiti ovlaštenika: listovi koji čekaju nadzor i nalozi preko roka
func TestKarticaZaRijesitiUgovora(t *testing.T) {
	d := dnevnikProbe(sadNaslovne)
	k := karticaZaRijesitiUgovora(d, sadNaslovne)
	if k.Broj != 2 || k.Redovi[0].Glavno != "2 dnevna lista čekaju potvrdu nadzora" || k.Redovi[0].URL != "/dnevnici/dn/listovi/l-prije" ||
		!strings.HasPrefix(k.Redovi[1].Glavno, "Nalog preko roka: dovezi pijesak") || k.Redovi[1].Sporedno != "rok jučer" {
		t.Errorf("za riješiti: %+v", k.Redovi)
	}
	d.Listovi, d.Upisi = nil, nil
	if k := karticaZaRijesitiUgovora(d, sadNaslovne); k.Prazno == nil || k.Prazno.Vrsta != PraznoGotovo {
		t.Errorf("ništa: %+v", k)
	}
	if k := karticaZaRijesitiUgovora(dnevnikRadova{Program: "A.02"}, sadNaslovne); k.Prazno == nil || k.Prazno.Vrsta != PraznoCeka {
		t.Errorf("bez dnevnika: %+v", k)
	}
}

// Nalozi: ovlaštenik vidi izvedene u zadnja 24 sata pa otvorene i ima
// gumb; voditelj usluga samo otvorene, bez gumba
func TestKarticaNaloga(t *testing.T) {
	d := dnevnikProbe(sadNaslovne)
	k := karticaNaloga(d, kNalozi, sadNaslovne)
	var glavna []string
	for _, r := range k.Redovi {
		glavna = append(glavna, r.Glavno)
	}
	if strings.Join(glavna, ",") != "očisti rešetku,dovezi pijesak,pripremi bager" || k.Broj != 2 || k.Gumbi[0].URL != "/dnevnici/dn/listovi/l-danas#upisi" {
		t.Fatalf("nalozi ovlaštenika: %v %+v", glavna, k.Gumbi)
	}
	if r := k.Redovi[1]; !r.Hitno || r.Sporedno != "Ana Proba · zadan 31. 10. · rok jučer" {
		t.Errorf("nalog preko roka: %+v", r)
	}
	ovl := karticaNaloga(d, kNaloziOvl, sadNaslovne)
	if ovl.Naslov != "Nalozi ovlaštenika" || len(ovl.Redovi) != 2 || len(ovl.Gumbi) != 0 || ovl.Redovi[1].Sporedno != "zadan danas" {
		t.Errorf("nalozi voditelju usluga: %+v", ovl)
	}
	d.Listovi, d.Upisi = nil, nil
	if k := karticaNaloga(d, kNalozi, sadNaslovne); k.Prazno == nil || k.Gumbi[0].URL != "/dnevnici/dn" {
		t.Errorf("bez naloga: %+v", k)
	}
	if k := karticaNaloga(dnevnikRadova{Program: "A.02"}, kNaloziOvl, sadNaslovne); k.Prazno == nil {
		t.Errorf("bez dnevnika: %+v", k)
	}
}

// Dnevnik radova voditelja usluga i ljudi na terenu po današnjem listu
func TestKarticaDnevnikaRadova(t *testing.T) {
	d := dnevnikProbe(sadNaslovne)
	k := karticaDnevnikaRadova(d, sadNaslovne)
	if k.Tekst != "Ugovor A.02 · list još nije potvrđen" || len(k.Gumbi) != 2 || k.Gumbi[1].URL != "/dnevnici/dn/ispis" || !k.Gumbi[1].SamoSiroko {
		t.Errorf("nepotvrđen list: %+v", k)
	}
	d.Listovi[0].ContractorConfirmedAt = &sadNaslovne
	if k := karticaDnevnikaRadova(d, sadNaslovne); k.Tekst != "Ugovor A.02 · list je potvrđen" {
		t.Errorf("potvrđen list: %+v", k)
	}
	if l := karticaLjudiNaTerenu(d, sadNaslovne); len(l.Redovi) != 2 || l.Redovi[1].Glavno != "Strojevi" {
		t.Errorf("ljudi: %+v", l)
	}
	d.Listovi = d.Listovi[1:]
	if k := karticaDnevnikaRadova(d, sadNaslovne); k.Tekst != "Ugovor A.02 · današnji list još nije otvoren" || k.Gumbi[0].URL != "/dnevnici/dn" {
		t.Errorf("bez lista: %+v", k)
	}
	if l := karticaLjudiNaTerenu(d, sadNaslovne); l.Prazno == nil {
		t.Errorf("ljudi bez lista: %+v", l)
	}
	if l := karticaLjudiNaTerenu(dnevnikRadova{Dnevnik: d.Dnevnik, Listovi: []models.JournalSheet{{ID: "x", Date: sadNaslovne}}}, sadNaslovne); l.Prazno == nil {
		t.Errorf("list bez ljudi: %+v", l)
	}
	if k := karticaDnevnikaRadova(dnevnikRadova{Podrucje: 16}, sadNaslovne); k.Prazno == nil || !strings.Contains(k.Prazno.Tekst, "vaše firme") {
		t.Errorf("bez dnevnika: %+v", k)
	}
}

// Naslovna ovlaštenika i voditelja usluga kroz rukovatelja: dnevnik
// programa ili firme, bez izvora i s greškom
func TestNaslovnaUsluga(t *testing.T) {
	o := novaOkolinaNaslovne(t)
	ovl, vod := dozvoleUsluge(models.RoleContractOfficerA3), dozvoleUsluge(models.RoleServiceLeaderForeman)
	if w := o.zovi("/naslovna/kartica/ugovor", ovl).Body.String(); !strings.Contains(w, "Dnevnik radova trenutačno nije dostupan.") {
		t.Errorf("bez izvora: %s", w)
	}
	sad := time.Now()
	d := dnevnikProbe(sad)
	a3 := models.Journal{ID: "dn3", Kind: models.JournalKindMaintenanceA03, Contractor: "Druga firma"}
	o.h.SetIzvori(IzvoriNaslovne{
		DnevniciUsluga:  func(context.Context, int) ([]models.Journal, error) { return []models.Journal{*d.Dnevnik, a3}, nil },
		ListoviDnevnika: func(context.Context, string) ([]models.JournalSheet, error) { return d.Listovi, nil },
		Zapisi:          func(context.Context, string) ([]models.JournalEntry, error) { return d.Upisi, nil },
		StanjaSektora: func(context.Context, string) (map[string]models.StanjeObrane, []models.Section, error) {
			return nil, nil, nil
		},
		Letve: func(context.Context) ([]models.GaugeSummary, error) {
			return []models.GaugeSummary{{Name: "Branjin Vrh", StationID: "bv", AreaIDs: []int{16}, Latest: &models.Reading{LevelCm: cmNaslovne(88), MeasuredAt: sad}}}, nil
		},
	})
	if w := o.zovi("/naslovna/kartica/ugovor", ovl).Body.String(); !strings.Contains(w, "Ugovor A.03 · BP 16") || !strings.Contains(w, "Druga firma") {
		t.Errorf("ovlaštenik A.03: %s", w)
	}
	if w := o.zovi("/naslovna/kartica/za-rijesiti", ovl).Body.String(); !strings.Contains(w, "čekaju potvrdu nadzora") {
		t.Errorf("za riješiti ovlaštenika: %s", w)
	}
	if w := o.zovi("/naslovna/kartica/vodostaji", ovl).Body.String(); !strings.Contains(w, "Branjin Vrh") {
		t.Errorf("vodostaji ovlaštenika: %s", w)
	}
	if s := o.zovi("/", vod).Body.String(); !strings.Contains(s, "BP 16: preventivna obrana") || !strings.Contains(s, `class="naslovna teren"`) {
		t.Error("voditelj usluga bez trake ili dva stupca")
	}
	for id, zeli := range map[string]string{"dnevnik-radova": "Otvori dnevnik", "nalozi-ovlastenika": "pripremi bager", "ljudi-na-terenu": "KV RADNIK: 6"} {
		if w := o.zovi("/naslovna/kartica/"+id, vod).Body.String(); !strings.Contains(w, zeli) {
			t.Errorf("%s nema %q: %s", id, zeli, w)
		}
	}
	vod.User.OrgName = "Nepoznata firma"
	if w := o.zovi("/naslovna/kartica/dnevnik-radova", vod).Body.String(); !strings.Contains(w, "vaše firme") {
		t.Errorf("firma bez dnevnika: %s", w)
	}
	o.h.SetIzvori(IzvoriNaslovne{DnevniciUsluga: func(context.Context, int) ([]models.Journal, error) { return nil, errors.New("baza zauzeta") }})
	if w := o.zovi("/naslovna/kartica/nalozi", ovl).Body.String(); !strings.Contains(w, "Dnevnik radova trenutačno nije dostupan.") {
		t.Errorf("s greškom: %s", w)
	}
	if l, u := (&DashboardHandler{}).sadrzajDnevnika(context.Background(), "dn"); l != nil || u != nil {
		t.Error("sadržaj bez izvora")
	}
}
