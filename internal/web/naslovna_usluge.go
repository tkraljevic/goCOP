package web

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"gocop/internal/models"
)

// Naslovna ovlaštenika za praćenje ugovora A.02 i A.03 (i zamjenika) te
// voditelja usluga izvođača (Odluke (31. 10. 2026.)). Obje gledaju isti
// dnevnik radova područja: ovlaštenik ugovor, listove za potvrdu nadzora i
// naloge koje daje, voditelj usluga današnji list, naloge ovlaštenika i
// ljude na terenu. Zapisa ugovora i obračuna po ugovoru u programu još
// nema: ugovor je tekst na naslovnici dnevnika.

// Kartice ovlaštenika i voditelja usluga
const (
	kUgovor        = "ugovor"
	kNalozi        = "nalozi"
	kDnevnikRadova = "dnevnik-radova"
	kNaloziOvl     = "nalozi-ovlastenika"
	kLjudiNaTerenu = "ljudi-na-terenu"
)

// duznostUgovora je valjana dužnost ovlaštenika ili zamjenika za program
// usluga na području; nil kad je nema
func duznostUgovora(u *models.User, sad time.Time) *models.Duty {
	for _, d := range models.VazecaZaduzenja(u.Duties, sad) {
		if d.Role.ProgramUgovora() != "" && d.AreaID != nil && d.SectorID != nil {
			return &d
		}
	}
	return nil
}

// duznostUsluga je valjana dužnost voditelja usluga izvođača na
// području; nil kad je nema
func duznostUsluga(u *models.User, sad time.Time) *models.Duty {
	for _, d := range models.VazecaZaduzenja(u.Duties, sad) {
		if d.Role == models.RoleServiceLeaderForeman && d.AreaID != nil && d.SectorID != nil {
			return &d
		}
	}
	return nil
}

// duznostUsluge je dužnost po kojoj se slaže Naslovna dnevnika radova
func duznostUsluge(perms *models.UserPermissions, sad time.Time) *models.Duty {
	if ulogaNaslovne(perms, sad) == uUsluga {
		return duznostUsluga(&perms.User, sad)
	}
	return duznostUgovora(&perms.User, sad)
}

// karticaUsluga slaže spore kartice ovlaštenika i voditelja usluga
func (h *DashboardHandler) karticaUsluga(r *http.Request, perms *models.UserPermissions, id string, sad time.Time) KarticaNaslovne {
	d := duznostUsluge(perms, sad)
	if d == nil || h.izvori.DnevniciUsluga == nil {
		return nedostupna(id, "Dnevnik radova trenutačno nije dostupan.")
	}
	ctx := r.Context()
	j, err := h.dnevnikUsluge(ctx, perms, *d)
	if err != nil {
		return nedostupna(id, "Dnevnik radova trenutačno nije dostupan.")
	}
	d2 := dnevnikRadova{Dnevnik: j, Program: d.Role.ProgramUgovora(), Podrucje: *d.AreaID}
	if j != nil {
		d2.Program = j.Program()
		d2.Listovi, d2.Upisi = h.sadrzajDnevnika(ctx, j.ID)
	}
	switch id {
	case kUgovor:
		return karticaUgovora(d2, sad)
	case kZaRijesiti:
		return karticaZaRijesitiUgovora(d2, sad)
	case kNalozi, kNaloziOvl:
		return karticaNaloga(d2, id, sad)
	case kDnevnikRadova:
		return karticaDnevnikaRadova(d2, sad)
	}
	return karticaLjudiNaTerenu(d2, sad)
}

// dnevnikRadova je otvoren dnevnik radova osobe s listovima i upisima
type dnevnikRadova struct {
	Dnevnik  *models.Journal // nil dok dnevnik nije otvoren
	Program  string          // A.02 ili A.03
	Podrucje int
	Listovi  []models.JournalSheet
	Upisi    []models.JournalEntry
}

// dnevnikUsluge je otvoren dnevnik radova područja: ovlašteniku dnevnik
// njegova programa, voditelju usluga dnevnik njegove firme
func (h *DashboardHandler) dnevnikUsluge(ctx context.Context, perms *models.UserPermissions, d models.Duty) (*models.Journal, error) {
	dnevnici, err := h.izvori.DnevniciUsluga(ctx, *d.AreaID)
	if err != nil {
		return nil, err
	}
	for i := range dnevnici {
		j := &dnevnici[i]
		if (d.Role == models.RoleServiceLeaderForeman && j.VodiFirma(&perms.User)) || (d.Role.ProgramUgovora() != "" && j.Program() == d.Role.ProgramUgovora()) {
			return j, nil
		}
	}
	return nil, nil
}

// sadrzajDnevnika su listovi i upisi dnevnika; bez izvora prazno
func (h *DashboardHandler) sadrzajDnevnika(ctx context.Context, id string) ([]models.JournalSheet, []models.JournalEntry) {
	var listovi []models.JournalSheet
	var upisi []models.JournalEntry
	if h.izvori.ListoviDnevnika != nil {
		listovi, _ = h.izvori.ListoviDnevnika(ctx, id) // bez listova kartice kažu da list nije otvoren
	}
	if h.izvori.Zapisi != nil {
		upisi, _ = h.izvori.Zapisi(ctx, id) // bez upisa kartice su bez naloga
	}
	return listovi, upisi
}

// nijeOtvoren je prazna kartica dok dnevnik radova nije otvoren
func (d dnevnikRadova) nijeOtvoren(k KarticaNaslovne) KarticaNaslovne {
	k.Prazno = &PraznaKartica{Vrsta: PraznoCeka, Tekst: fmt.Sprintf("Dnevnik radova %s za BP %d još nije otvoren.", d.Program, d.Podrucje)}
	return k
}

// danasnjiList je list dnevnika za današnji dan; nil kad ga nema
func (d dnevnikRadova) danasnjiList(sad time.Time) *models.JournalSheet {
	danas := sad.In(models.Zagreb).Format("2006-01-02")
	for i := range d.Listovi {
		if d.Listovi[i].Date.In(models.Zagreb).Format("2006-01-02") == danas {
			return &d.Listovi[i]
		}
	}
	return nil
}

// adresaLista je adresa lista u dnevniku
func (d dnevnikRadova) adresaLista(list string) string {
	return "/dnevnici/" + d.Dnevnik.ID + "/listovi/" + list
}

// karticaUgovora: izvođač i voditelj usluga, ugovor i ljudi na terenu danas
func karticaUgovora(d dnevnikRadova, sad time.Time) KarticaNaslovne {
	k := KarticaNaslovne{ID: kUgovor, Naslov: fmt.Sprintf("Ugovor %s · BP %d", d.Program, d.Podrucje)}
	if d.Dnevnik == nil {
		return d.nijeOtvoren(k)
	}
	j := d.Dnevnik
	k.Poveznica, k.URL = "Dnevnik", "/dnevnici/"+j.ID
	if j.Contractor != "" {
		r := RedakKartice{Ikona: "building", Glavno: "Izvođač: " + j.Contractor}
		if j.ContractorLead != "" {
			r.Sporedno = "voditelj usluga " + j.ContractorLead
		}
		k.Redovi = append(k.Redovi, r)
	}
	if strings.TrimSpace(j.Contract) != "" {
		k.Redovi = append(k.Redovi, RedakKartice{Ikona: "file-text", Glavno: "Ugovor", Sporedno: skrati(j.Contract, 80)})
	}
	k.Redovi = append(k.Redovi, redakNaTerenu(d, sad))
	return k
}

// redakNaTerenu: ljudi i strojevi s današnjeg lista
func redakNaTerenu(d dnevnikRadova, sad time.Time) RedakKartice {
	r := RedakKartice{Ikona: "users", Glavno: "Na terenu danas", Sporedno: "Današnji list još nije otvoren."}
	if l := d.danasnjiList(sad); l != nil {
		r.Sporedno, r.URL = strings.Trim(models.CountsText(l.Staff)+" · "+models.CountsText(l.Machines), " ·"), d.adresaLista(l.ID)
		if r.Sporedno == "" {
			r.Sporedno = "Na listu još nisu upisani ljudi ni strojevi."
		}
	}
	return r
}

// karticaZaRijesitiUgovora: listovi koje je izvođač potvrdio, a nadzor još
// nije, i nalozi kojima je prošao rok
func karticaZaRijesitiUgovora(d dnevnikRadova, sad time.Time) KarticaNaslovne {
	k := glavaKartice(kZaRijesiti)
	if d.Dnevnik == nil {
		return d.nijeOtvoren(k)
	}
	var cekaju []models.JournalSheet
	for _, l := range d.Listovi {
		if l.ContractorConfirmedAt != nil && l.SupervisorConfirmedAt == nil {
			cekaju = append(cekaju, l)
		}
	}
	slices.SortFunc(cekaju, func(a, b models.JournalSheet) int { return a.Date.Compare(b.Date) })
	if n := len(cekaju); n > 0 {
		dan := cekaju[0].Date.In(models.Zagreb)
		k.Redovi = append(k.Redovi, RedakKartice{Ikona: "notebook-pen", Hitno: true, URL: d.adresaLista(cekaju[0].ID),
			Glavno:   fmt.Sprintf("%d %s", n, models.UzBrojHR(n, "dnevni list čeka potvrdu nadzora", "dnevna lista čekaju potvrdu nadzora", "dnevnih listova čeka potvrdu nadzora")),
			Sporedno: fmt.Sprintf("%s · najstariji od %d. %d.", d.Dnevnik.Contractor, dan.Day(), int(dan.Month()))})
	}
	for _, e := range d.Upisi {
		if e.IsOverdue(sad) && !e.Voided {
			k.Redovi = append(k.Redovi, RedakKartice{Ikona: "triangle-alert", Glavno: "Nalog preko roka: " + skrati(e.Text, 80), Hitno: true,
				Sporedno: "rok " + danNaslovne(*e.DueDate, sad), URL: d.adresaLista(e.SheetID)})
		}
	}
	k.Broj = len(k.Redovi)
	if k.Broj == 0 {
		k.Prazno = &PraznaKartica{Vrsta: PraznoGotovo, Tekst: "Nema ništa za riješiti."}
	}
	return k
}

// karticaNaloga: ovlašteniku nalozi izvedeni u zadnja 24 sata pa otvoreni,
// uz gumb „Novi nalog”; voditelju usluga samo otvoreni nalozi ovlaštenika
func karticaNaloga(d dnevnikRadova, id string, sad time.Time) KarticaNaslovne {
	k := KarticaNaslovne{ID: id, Naslov: "Nalozi", HitnoNaziv: "preko roka"}
	if id == kNaloziOvl {
		k.Naslov = "Nalozi ovlaštenika"
	}
	if d.Dnevnik == nil {
		return d.nijeOtvoren(k)
	}
	k.Poveznica, k.URL = "Dnevnik", "/dnevnici/"+d.Dnevnik.ID
	izvedeni, otvoreni := naloziDnevnika(d.Upisi, id == kNalozi, sad)
	for _, e := range izvedeni {
		k.Redovi = append(k.Redovi, RedakKartice{Ikona: "check", Glavno: skrati(e.Text, 100), Sporedno: "izveden " + kadNaslovne(e.UpdatedAt, sad), URL: d.adresaLista(e.SheetID)})
	}
	for _, e := range otvoreni {
		k.Redovi = append(k.Redovi, redakOtvorenogNaloga(e, d, sad))
	}
	if id == kNalozi {
		k.Gumbi = []GumbNaslovne{{Tekst: "Novi nalog", URL: adresaNovogUpisa(d, sad), Glavni: true}}
	}
	k.Broj = len(otvoreni)
	if len(k.Redovi) == 0 {
		k.Prazno = &PraznaKartica{Vrsta: PraznoGotovo, Tekst: "Nema otvorenih naloga."}
	}
	return k
}

// naloziDnevnika su nalozi izvedeni u zadnja 24 sata, najnoviji prvi, kad
// se traže, i otvoreni nalozi, najstariji prvi; stornirani ne ulaze
func naloziDnevnika(upisi []models.JournalEntry, sIzvedenima bool, sad time.Time) (izvedeni, otvoreni []models.JournalEntry) {
	for _, e := range upisi {
		switch {
		case !e.IsTask() || e.Voided:
		case e.Status == models.TaskOpen:
			otvoreni = append(otvoreni, e)
		case e.Status == models.TaskDone && sIzvedenima && sad.Sub(e.UpdatedAt) <= ZakljuceniNaKartici:
			izvedeni = append(izvedeni, e)
		}
	}
	slices.SortStableFunc(izvedeni, func(a, b models.JournalEntry) int { return b.UpdatedAt.Compare(a.UpdatedAt) })
	slices.SortStableFunc(otvoreni, func(a, b models.JournalEntry) int { return cmp.Or(a.Date.Compare(b.Date), a.Number-b.Number) })
	return izvedeni, otvoreni
}

// redakOtvorenogNaloga: nalog, tko ga je dao i rok; preko roka je hitan
func redakOtvorenogNaloga(e models.JournalEntry, d dnevnikRadova, sad time.Time) RedakKartice {
	dio := []string{e.UserName, "zadan " + danNaslovne(e.Date, sad)}
	if e.DueDate != nil {
		dio = append(dio, "rok "+danNaslovne(*e.DueDate, sad))
	}
	return RedakKartice{Ikona: "clipboard-list", Glavno: skrati(e.Text, 100), Sporedno: strings.TrimPrefix(strings.Join(dio, " · "), " · "),
		URL: d.adresaLista(e.SheetID), Hitno: e.IsOverdue(sad)}
}

// adresaNovogUpisa: današnji list, a dok ga nema, dnevnik u kojem se otvara
func adresaNovogUpisa(d dnevnikRadova, sad time.Time) string {
	if l := d.danasnjiList(sad); l != nil {
		return d.adresaLista(l.ID) + "#upisi"
	}
	return "/dnevnici/" + d.Dnevnik.ID
}

// karticaDnevnikaRadova: današnji list izvođača i gumbi „Otvori dnevnik” i
// „Ispiši”
func karticaDnevnikaRadova(d dnevnikRadova, sad time.Time) KarticaNaslovne {
	k := KarticaNaslovne{ID: kDnevnikRadova, Naslov: "Dnevnik radova · " + danUTjednu(sad)}
	if d.Dnevnik == nil {
		k.Prazno = &PraznaKartica{Vrsta: PraznoCeka, Tekst: fmt.Sprintf("Dnevnik radova vaše firme za BP %d još nije otvoren.", d.Podrucje)}
		return k
	}
	k.Tekst = "Ugovor " + d.Program + " · današnji list još nije otvoren"
	if l := d.danasnjiList(sad); l != nil {
		k.Tekst = "Ugovor " + d.Program + " · list još nije potvrđen"
		if l.ContractorConfirmedAt != nil {
			k.Tekst = "Ugovor " + d.Program + " · list je potvrđen"
		}
	}
	k.Gumbi = []GumbNaslovne{{Tekst: "Otvori dnevnik", URL: adresaNovogUpisa(d, sad), Glavni: true}, {Tekst: "Ispiši", URL: "/dnevnici/" + d.Dnevnik.ID + "/ispis", SamoSiroko: true}}
	return k
}

// karticaLjudiNaTerenu: ljudi i strojevi s današnjeg lista
func karticaLjudiNaTerenu(d dnevnikRadova, sad time.Time) KarticaNaslovne {
	k := KarticaNaslovne{ID: kLjudiNaTerenu, Naslov: "Na terenu danas"}
	l := d.danasnjiList(sad)
	if d.Dnevnik == nil || l == nil {
		k.Prazno = &PraznaKartica{Vrsta: PraznoCeka, Tekst: "Današnji list još nije otvoren."}
		return k
	}
	for _, s := range []struct{ ikona, naziv, broj string }{{"users", "Ljudi", l.Staff}, {"wrench", "Strojevi", l.Machines}} {
		if t := models.CountsText(s.broj); t != "" {
			k.Redovi = append(k.Redovi, RedakKartice{Ikona: s.ikona, Glavno: s.naziv, Sporedno: t, URL: d.adresaLista(l.ID)})
		}
	}
	if len(k.Redovi) == 0 {
		k.Prazno = &PraznaKartica{Vrsta: PraznoCeka, Tekst: "Na današnjem listu još nisu upisani ljudi ni strojevi."}
	}
	return k
}
