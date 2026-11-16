package web

import (
	"context"
	"html/template"
	"net/http"
	"slices"
	"strconv"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Kartica Dežurstva u modulu Djelatnici (Odluke (16. 11. 2026.), Djelatnici
// 2): samo pokazuje tko dežura sada, dežurstva danas i jučer, planove po
// dnevnicima COP-a i put do vlastitih sati. Upis i potvrda ostaju u
// Dokumentima, uz dnevnik COP-a, i ista su prava čitanja plana.

// RedakDezurstva je jedno dežurstvo na kartici
type RedakDezurstva struct {
	Od, Do   time.Time
	OdTekst  string // „pon 26. 10. 06:00”
	DoTekst  string // „15:00”, a sutradan s danom
	Osoba    string
	Za       string // branjeno područje ili cijeli sektor
	Opis     string
	Teren    bool
	Potvrdio string
	Sad      bool
}

// PlanDezurstava je dnevnik COP-a s planom, kao poveznica na plan
type PlanDezurstava struct {
	Naslov, Razdoblje, Stanje, URL string
	Dezurstava                     int
}

// DezurstvaPageData je kartica Dežurstva
type DezurstvaPageData struct {
	CurrentUser    *models.User
	Permissions    *models.UserPermissions
	ActiveNav      string
	SuccessMessage string
	ErrorMessage   string
	ViewAsBanner

	Sektor     string
	Cip        CipFiltra
	Sada       []RedakDezurstva
	DanasJucer []RedakDezurstva
	Planovi    []PlanDezurstava
	Skriveno   bool   // sektor ima dnevnike COP-a, ali osoba plan ne smije vidjeti
	URLPlana   string // plan otvorenog dnevnika, za „Plan dežurstava”
}

// DezurstvaDjelatnika je rukovatelj kartice Dežurstva
type DezurstvaDjelatnika struct {
	journals *service.JournalService
	users    *service.UserService
	tmpl     *template.Template
	sada     func() time.Time
}

// NewDezurstvaDjelatnika spaja karticu s dnevnicima i imenikom
func NewDezurstvaDjelatnika(j *service.JournalService, u *service.UserService, t *template.Template) *DezurstvaDjelatnika {
	return &DezurstvaDjelatnika{journals: j, users: u, tmpl: t, sada: time.Now}
}

// Show prikazuje karticu za sektor iz adrese, a bez njega za sektor osobe
func (h *DezurstvaDjelatnika) Show(w http.ResponseWriter, r *http.Request) {
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	sektori, _ := h.users.ListSectors() // bez popisa sektora čip nudi samo sektor iz adrese
	sektor := sektorKartice(r.URL.Query().Get("sector"), u, sektori)
	data := DezurstvaPageData{CurrentUser: u, Permissions: perms, ActiveNav: "users", ViewAsBanner: viewBanner(r),
		SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error"), Sektor: sektor,
		Cip: cipSektoraDezurstava(sektor, sektori)}
	podrucja, _ := h.users.ListAreas("") // bez popisa područja piše se „BP n”
	h.popuni(r.Context(), &data, perms, podrucja)
	if err := h.tmpl.ExecuteTemplate(w, "djelatnici_dezurstva.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// popuni čita dnevnike COP-a sektora koje osoba smije čitati: otvoreni daje
// tko dežura i dežurstva danas i jučer, a svi ulaze u popis planova
func (h *DezurstvaDjelatnika) popuni(ctx context.Context, d *DezurstvaPageData, perms *models.UserPermissions, podrucja []models.Area) {
	dnevnici, _ := h.journals.ListCOPJournals(ctx, d.Sektor) // bez dnevnika kartica kaže da plana nema
	sad := h.sada()
	for i := range dnevnici {
		j := &dnevnici[i]
		if !h.journals.VidiPlanDezurstava(perms, models.OpsegDnevnika(*j, nil, podrucja), j) {
			d.Skriveno = true
			continue
		}
		plan, _ := h.journals.Dezurstva(ctx, j.ID) // nepročitan plan pokaže se prazan
		d.Planovi = append(d.Planovi, PlanDezurstava{Naslov: j.DisplayTitle(), Razdoblje: j.Razdoblje(), Stanje: j.StanjeCOP(),
			URL: "/dnevnici/" + j.ID + "/dezurstva", Dezurstava: len(plan)})
		if j.EndedAt != nil {
			continue
		}
		if d.URLPlana == "" {
			d.URLPlana = "/dnevnici/" + j.ID + "/dezurstva"
		}
		d.Sada, d.DanasJucer = rasporediDezurstva(plan, podrucja, sad, d.Sada, d.DanasJucer)
	}
}

// rasporediDezurstva dodaje dežurstva koja traju sada i ona koja su bila
// danas ili jučer, najnovije prvo
func rasporediDezurstva(plan []models.Dezurstvo, podrucja []models.Area, sad time.Time, sada, danasJucer []RedakDezurstva) ([]RedakDezurstva, []RedakDezurstva) {
	lok := sad.In(models.Zagreb)
	jucer := time.Date(lok.Year(), lok.Month(), lok.Day()-1, 0, 0, 0, 0, models.Zagreb)
	sutra := jucer.AddDate(0, 0, 2)
	for _, z := range plan {
		red := redakDezurstva(z, podrucja, sad)
		if red.Sad {
			sada = append(sada, red)
		}
		if z.Do.After(jucer) && z.Od.Before(sutra) {
			danasJucer = append(danasJucer, red)
		}
	}
	slices.SortFunc(danasJucer, func(a, b RedakDezurstva) int { return b.Od.Compare(a.Od) })
	return sada, danasJucer
}

func redakDezurstva(z models.Dezurstvo, podrucja []models.Area, sad time.Time) RedakDezurstva {
	za := "cijeli sektor"
	if z.Podrucje != nil {
		za = "BP " + strconv.Itoa(*z.Podrucje)
		for _, a := range podrucja {
			if a.ID == *z.Podrucje {
				za = a.Name
			}
		}
	}
	od, do := z.Od.In(models.Zagreb), z.Do.In(models.Zagreb)
	doTekst := do.Format("15:04")
	if do.YearDay() != od.YearDay() || do.Year() != od.Year() {
		doTekst = do.Format("2. 1. 15:04")
	}
	return RedakDezurstva{Od: z.Od, Do: z.Do, OdTekst: danKratko(od) + " " + od.Format("2. 1. 15:04"), DoTekst: doTekst, Osoba: z.UserName, Za: za, Opis: z.Opis, Teren: z.Mjesto == models.MjestoTeren,
		Potvrdio: z.Potvrdio, Sad: !sad.Before(z.Od) && sad.Before(z.Do)}
}

// sektorKartice je sektor iz adrese, inače sektor primarnog zaduženja osobe,
// inače prvi sektor
func sektorKartice(izAdrese string, u *models.User, sektori []models.Sector) string {
	if izAdrese != "" {
		return izAdrese
	}
	if u != nil {
		for _, z := range u.Duties {
			if z.SectorID != nil && *z.SectorID != "" {
				return *z.SectorID
			}
		}
	}
	for _, s := range sektori {
		if s.Level != 1 {
			return s.ID
		}
	}
	return ""
}

// cipSektoraDezurstava je čip izbora sektora kartice
func cipSektoraDezurstava(sektor string, sektori []models.Sector) CipFiltra {
	pojam := models.Terms().Get("sektor")
	c := CipFiltra{Natpis: pojam + ": " + sektor, Odabran: true}
	for _, s := range sektori {
		c.Izbor = append(c.Izbor, IzborFiltra{s.Name, "/users/dezurstva?sector=" + s.ID, s.ID == sektor})
	}
	return c
}
