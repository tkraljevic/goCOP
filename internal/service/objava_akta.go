package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gocop/internal/models"
)

// ErrPreventivnaObrana: u sektoru nema otvorenog dnevnika COP-a, pa je obrana
// preventivna; stupnjevi se proglašavaju i prekidaju samo u aktivnoj obrani
type ErrPreventivnaObrana struct{ Sektor string }

func (e ErrPreventivnaObrana) Error() string {
	return fmt.Sprintf("sektor %s je u preventivnoj obrani: nema otvorenog dnevnika COP-a. Stupnjevi obrane proglašavaju se samo u aktivnoj obrani; voditelj COP-a prvo otvara dnevnik (Dokumentacija › Dnevnici COP-a)", e.Sektor)
}

// AktivnaObrana vraća otvoren dnevnik COP-a sektora: postoji li, obrana je
// aktivna (proglašenja, dežurstva, obračun sati); nema li ga, preventivna.
// Prijepisi i zaključeni dnevnici se ne broje.
func (s *JournalService) AktivnaObrana(ctx context.Context, sektor string) *models.Journal {
	if s == nil || sektor == "" {
		return nil
	}
	dnevnici, err := s.repo.ListCOPJournals(ctx, sektor)
	if err != nil {
		return nil
	}
	for i := range dnevnici {
		if dnevnici[i].EndedAt == nil && !dnevnici[i].Reconstruction && !dnevnici[i].Ovjeren() {
			return &dnevnici[i]
		}
	}
	return nil
}

// ObjavaAkta upisuje ovjeren akt u dnevnike: obavijest u dnevnik COP-a,
// bilješku u dnevni list svakog vodočuvara branjenog područja i napomenu
// nadzora u otvorene dnevnike održavanja tog područja. Tako svi koji vode
// dnevnik doznaju za promjenu stupnja iz svog dnevnika.
type ObjavaAkta struct {
	journals  *JournalService
	vodocuvar *VodocuvarService
}

// NewObjavaAkta sastavlja objavu; dnevnik vodočuvara smije biti nil
func NewObjavaAkta(journals *JournalService, vodocuvar *VodocuvarService) *ObjavaAkta {
	return &ObjavaAkta{journals: journals, vodocuvar: vodocuvar}
}

// TekstObjave je rečenica kako se akt upisuje u dnevnike
func TekstObjave(a *models.Akt) string {
	var dionice []string
	for _, d := range a.Dionice {
		dionice = append(dionice, d.Code)
	}
	t := fmt.Sprintf("%s (%s), branjeno područje %d", a.Naslov(), a.Oznaka(), a.AreaID)
	if len(dionice) > 0 {
		t += ", dionice " + strings.Join(dionice, ", ")
	}
	t += ", vrijedi od " + a.Vrijedi.In(models.Zagreb).Format("02.01.2006. u 15:04")
	if a.VodostajCm != nil {
		t += fmt.Sprintf("; vodomjer %s %d cm", a.StationName, *a.VodostajCm)
		if a.Tendencija != "" {
			t += " " + strings.ToLower(models.TendencijaLabel(a.Tendencija))
		}
	} else if a.Prognoza != "" {
		t += "; po prognozi"
	}
	if a.Ovjerio != "" {
		t += ". Ovjerio " + a.Ovjerio
	}
	return t + "."
}

// Objavi upisuje akt u dnevnike; što ne uspije vraća kao upozorenja, jer je
// akt već ovjeren i odluka stoji bez obzira na upise
func (o *ObjavaAkta) Objavi(ctx context.Context, u *models.User, a *models.Akt, j *models.Journal) []string {
	if o == nil || a == nil {
		return nil
	}
	tekst := TekstObjave(a)
	dan := pocetakDana(a.Vrijedi.In(models.Zagreb))
	var upozorenja []string
	if o.journals != nil && j != nil {
		kad := a.Vrijedi
		e := &models.JournalEntry{Date: dan, HappenedAt: &kad, Kind: models.EntryKindNotice, Podrucje: &a.AreaID,
			ReportedBy: "goCOP, akt " + a.Oznaka(), UserID: a.OvjerioID, UserName: a.Ovjerio, Text: tekst}
		if err := o.journals.ZapisIzAkta(ctx, j, e); err != nil {
			upozorenja = append(upozorenja, "dnevnik COP-a: "+err.Error())
		}
		for _, d := range o.journals.OtvoreniDnevniciOdrzavanja(ctx, a.AreaID) {
			d := d
			if err := o.journals.NapomenaNadzoraIzAkta(ctx, &d, dan, a, tekst); err != nil {
				upozorenja = append(upozorenja, d.DisplayTitle()+": "+err.Error())
			}
		}
	}
	if o.vodocuvar != nil {
		upozorenja = append(upozorenja, o.vodocuvar.UpisiIzAkta(ctx, a, dan, tekst)...)
	}
	return upozorenja
}

// ZapisIzAkta upisuje obavijest o aktu u dnevnik COP-a; akt je ovjeren pa
// upis ne traži ovlast, ali dan mora biti unutar trajanja dnevnika
func (s *JournalService) ZapisIzAkta(ctx context.Context, j *models.Journal, e *models.JournalEntry) error {
	if j == nil || j.CentarSektor == "" {
		return fmt.Errorf("ovo nije dnevnik COP-a")
	}
	if j.StartedAt != nil && e.Date.Before(pocetakDana(j.StartedAt.In(models.Zagreb))) {
		// akt vrijedi od prije otvaranja dnevnika: upis ide na dan otvaranja
		e.Date = pocetakDana(j.StartedAt.In(models.Zagreb))
	}
	if j.EndedAt != nil && e.Date.After(*j.EndedAt) {
		return fmt.Errorf("dnevnik je zaključen %s", j.EndedAt.In(models.Zagreb).Format("2.1.2006."))
	}
	e.ID, e.Number, e.SheetID, e.Side = "", 0, "", ""
	e.JournalID = j.ID
	e.DueDate, e.Status, e.WorkItemID = nil, "", ""
	return s.repo.SaveEntry(ctx, e)
}

// OtvoreniDnevniciOdrzavanja vraća dnevnike usluga A.02 i A.03 područja koji
// još traju: u njih ide napomena nadzora o promjeni stupnja obrane
func (s *JournalService) OtvoreniDnevniciOdrzavanja(ctx context.Context, areaID int) []models.Journal {
	if s == nil || areaID == 0 {
		return nil
	}
	svi, err := s.repo.ListJournals(ctx, areaID)
	if err != nil {
		return nil
	}
	var out []models.Journal
	for _, j := range svi {
		if (j.Kind == models.JournalKindMaintenanceA02 || j.Kind == models.JournalKindMaintenanceA03) && j.EndedAt == nil && !j.Reconstruction && j.CentarSektor == "" {
			out = append(out, j)
		}
	}
	return out
}

// NapomenaNadzoraIzAkta upisuje napomenu nadzora o aktu na list dana u
// dnevniku održavanja; list za taj dan nastaje ako ga nema, bez posade
func (s *JournalService) NapomenaNadzoraIzAkta(ctx context.Context, j *models.Journal, dan time.Time, a *models.Akt, tekst string) error {
	listovi, err := s.repo.ListSheets(ctx, j.ID)
	if err != nil {
		return err
	}
	var sh *models.JournalSheet
	for i := range listovi {
		if pocetakDana(listovi[i].Date.In(models.Zagreb)).Equal(dan) && !listovi[i].IsConfirmed() {
			sh = &listovi[i]
			break
		}
	}
	if sh == nil {
		sh = &models.JournalSheet{JournalID: j.ID, Date: dan}
		if len(listovi) > 0 {
			sh.Staff, sh.Machines = listovi[0].Staff, listovi[0].Machines
		}
		if err := s.repo.SaveSheet(ctx, sh); err != nil {
			return err
		}
	}
	e := &models.JournalEntry{JournalID: j.ID, SheetID: sh.ID, Date: sh.Date, Kind: models.EntryKindNote, Side: models.EntrySideSupervisor,
		SectionCode: prvaDionica(a), Text: "Obrana od poplava: " + tekst, UserID: a.OvjerioID, UserName: a.Ovjerio}
	return s.repo.SaveEntry(ctx, e)
}

func prvaDionica(a *models.Akt) string {
	if len(a.Dionice) > 0 {
		return a.Dionice[0].Code
	}
	return ""
}

// UpisiIzAkta upisuje bilješku o aktu u dnevni list svakog vodočuvara
// branjenog područja na dan od kojeg akt vrijedi; list nastaje ako ga nema
func (s *VodocuvarService) UpisiIzAkta(ctx context.Context, a *models.Akt, dan time.Time, tekst string) []string {
	if s == nil || a == nil || Arhivirana(dan.Year()) {
		return nil
	}
	svi, err := s.users.ListUsers("", 0, "", "", "active")
	if err != nil {
		return []string{"dnevnici vodočuvara: " + err.Error()}
	}
	var upozorenja []string
	for i := range svi {
		v := &svi[i]
		d := terenskaDuznost(v)
		if d == nil || d.AreaID == nil || *d.AreaID != a.AreaID {
			continue
		}
		l, err := s.pripremiZa(ctx, v, dan)
		if err != nil {
			upozorenja = append(upozorenja, v.FullName+": "+err.Error())
			continue
		}
		if l.Broj == 0 {
			if l.Broj, err = s.repo.SljedeciBroj(ctx, v.ID.String(), dan.Year()); err != nil {
				upozorenja = append(upozorenja, v.FullName+": "+err.Error())
				continue
			}
		}
		l.Upisi = append(l.Upisi, models.UpisRukovoditelja{UserID: a.OvjerioID, Ime: a.Ovjerio, Funkcija: "akt " + a.Oznaka(), Kad: time.Now(), Tekst: "Obrana od poplava: " + tekst})
		if err := s.repo.Save(ctx, l); err != nil {
			upozorenja = append(upozorenja, v.FullName+": "+err.Error())
		}
	}
	return upozorenja
}
