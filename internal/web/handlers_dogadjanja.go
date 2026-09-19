package web

import (
	"context"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
	"gocop/internal/xlsxw"
)

// Događanja: isti zid kao na naslovnoj, ali s razdobljem, modulom, osobom
// i područjem, i s izvozom — kronologija obrane koja se prilaže izvješću.

type DogadjanjaHandler struct {
	zid    func() *service.ZidService
	users  *service.UserService
	tmpl   *template.Template
	opcije func(ctx context.Context) models.Opcije // nil = sve isključeno
}

// SetOpcije daje rukovatelju uvid u opće prekidače
func (h *DogadjanjaHandler) SetOpcije(f func(ctx context.Context) models.Opcije) { h.opcije = f }

// HandleObrisi briše zapis s oglasne ploče, uz uključen prekidač
func (h *DogadjanjaHandler) HandleObrisi(w http.ResponseWriter, r *http.Request) {
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	natrag := "/dogadjanja"
	if h.opcije == nil || !h.opcije(r.Context()).BrisanjeSOglasnePloce {
		redirectWith(w, r, natrag, "error", "Brisanje s oglasne ploče je isključeno (Administracija › Opcije)")
		return
	}
	z := h.zid()
	if z == nil {
		http.Error(w, "oglasna ploča nije spremna", http.StatusServiceUnavailable)
		return
	}
	if err := z.ObrisiDogadjaj(r.Context(), perms, r.FormValue("verzija")); err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	redirectWith(w, r, natrag, "success", "Zapis je obrisan s oglasne ploče i iz knjige verzija na ovom čvoru.")
}

func NewDogadjanjaHandler(zid func() *service.ZidService, users *service.UserService, tmpl *template.Template) *DogadjanjaHandler {
	return &DogadjanjaHandler{zid: zid, users: users, tmpl: tmpl}
}

// DogadjanjaPageData je stranica događanja
type DogadjanjaPageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions

	Dogadjaji []service.Dogadjaj
	Moduli    []struct{ ID, Naziv, Ikona string }
	Sektori   []models.Sector
	Podrucja  []models.Area
	Filtar    service.FiltarZida
	Od, Do    string
	Dalje     string // oznaka za listanje unatrag

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner

	SmijeBrisati bool // uprava organizacije uz uključen prekidač
}

// PoDanima grupira događaje po danu, najnoviji dan prvi
func (d DogadjanjaPageData) PoDanima() []DanDogadjaja {
	return grupirajPoDanima(d.Dogadjaji)
}

// DanDogadjaja je jedan dan na zidu
type DanDogadjaja struct {
	Dan       time.Time
	Dogadjaji []service.Dogadjaj
}

func grupirajPoDanima(sve []service.Dogadjaj) []DanDogadjaja {
	var out []DanDogadjaja
	for _, d := range sve {
		dan := d.Kad.In(models.Zagreb)
		dan = time.Date(dan.Year(), dan.Month(), dan.Day(), 0, 0, 0, 0, models.Zagreb)
		if n := len(out); n > 0 && out[n-1].Dan.Equal(dan) {
			out[n-1].Dogadjaji = append(out[n-1].Dogadjaji, d)
			continue
		}
		out = append(out, DanDogadjaja{Dan: dan, Dogadjaji: []service.Dogadjaj{d}})
	}
	return out
}

func (h *DogadjanjaHandler) filtar(r *http.Request, perms *models.UserPermissions) (service.FiltarZida, string, string) {
	q := r.URL.Query()
	f := service.FiltarZida{Modul: q.Get("modul"), Sektor: q.Get("sektor"), Osoba: strings.TrimSpace(q.Get("osoba")), Prije: q.Get("prije"), Limit: 100}
	f.AreaID, _ = strconv.Atoi(q.Get("podrucje"))
	od, do := q.Get("od"), q.Get("do")
	if t, err := time.ParseInLocation("2006-01-02", od, models.Zagreb); err == nil {
		f.Od = t
	}
	if t, err := time.ParseInLocation("2006-01-02", do, models.Zagreb); err == nil {
		f.Do = t.AddDate(0, 0, 1)
	}
	return f, od, do
}

// ShowDogadjanja prikazuje zid s filtrima
func (h *DogadjanjaHandler) ShowDogadjanja(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	svc := h.zid()
	if svc == nil {
		http.Error(w, "događanja nisu dostupna", http.StatusServiceUnavailable)
		return
	}
	f, od, do := h.filtar(r, perms)
	dogadjaji, dalje, err := svc.Zadnji(ctx, perms, f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data := DogadjanjaPageData{CurrentUser: u, Permissions: perms, Dogadjaji: dogadjaji, Moduli: service.ModuliZida, Filtar: f, Od: od, Do: do, Dalje: dalje,
		ActiveNav: "journals", ViewAsBanner: viewBanner(r)}
	data.Sektori, _ = h.users.ListSectors()
	data.Podrucja, _ = h.users.ListAreas(f.Sektor)
	data.SmijeBrisati = h.opcije != nil && perms != nil && perms.IsGlobalAdmin && h.opcije(r.Context()).BrisanjeSOglasnePloce
	if err := h.tmpl.ExecuteTemplate(w, "dogadjanja.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// IzvoziDogadjanja piše kronologiju po istim filtrima kao .xlsx
func (h *DogadjanjaHandler) IzvoziDogadjanja(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	f, od, do := h.filtar(r, perms)
	f.Limit, f.Prije = 2000, ""
	dogadjaji, _, err := h.zid().Zadnji(ctx, perms, f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	terms := models.Terms()
	z := ZaglavljeIzvoza{Organizacija: terms.OrgName, Sektor: f.Sektor, Datum: time.Now().In(models.Zagreb)}
	if terms.HasLogo() && terms.LogoMime == "image/png" {
		z.LogoPNG = terms.Logo
	}
	if sektori, err := h.users.ListSectors(); err == nil {
		for _, s := range sektori {
			if s.ID == f.Sektor {
				z.Odjel, z.Centar = s.VgoName, s.CenterCop
			}
		}
	}
	var opis []string
	if od != "" {
		opis = append(opis, "od "+od)
	}
	if do != "" {
		opis = append(opis, "do "+do)
	}
	if f.Modul != "" {
		opis = append(opis, service.ModulNaziv(f.Modul))
	}
	if f.Osoba != "" {
		opis = append(opis, f.Osoba)
	}
	podnaslov := strings.Join(opis, " · ")
	if podnaslov == "" {
		podnaslov = "sva događanja, najnovije prvo"
	}
	k := &xlsxw.Knjiga{LogoPNG: z.LogoPNG}
	B := xlsxw.T
	const stupaca = 7
	l := k.NoviList("Događanja")
	l.Vodoravno = true
	l.Sirine = []float64{16, 13, 30, 70, 22, 8, 10}
	zaglavljeLista(l, z, "KRONOLOGIJA DOGAĐANJA", podnaslov, stupaca)
	rr := l.Redak()
	l.Dodaj(B("Vrijeme", xlsxw.Zaglavlje), B("Modul", xlsxw.Zaglavlje), B("Događaj", xlsxw.Zaglavlje), B("Što", xlsxw.Zaglavlje), B("Tko", xlsxw.Zaglavlje), B("Sektor", xlsxw.Zaglavlje), B("BP", xlsxw.Zaglavlje))
	l.Visina(rr, 22)
	l.PonoviRetke(rr, rr)
	for _, d := range dogadjaji {
		bp := ""
		if d.AreaID > 0 {
			bp = strconv.Itoa(d.AreaID)
		}
		r := l.Redak()
		l.Dodaj(B(d.Kad.In(models.Zagreb).Format("02.01.2006. 15:04"), xlsxw.TablicaSredina), B(d.ModulNaziv(), xlsxw.Tablica), B(d.Naslov, xlsxw.Tablica),
			B(d.Tekst, xlsxw.TablicaTekst), B(d.Tko, xlsxw.Tablica), B(d.Sektor, xlsxw.TablicaSredina), B(bp, xlsxw.TablicaSredina))
		l.Visina(r, visinaTeksta(d.Tekst, 70, 18, 0))
	}
	napomenaLista(l, "Iz knjige verzija programa goCOP: svaka promjena zapisa, s vremenom i osobom koja ju je napravila; očitanja samo iznad praga obrane.", stupaca, 24)
	posaljiXLSX(w, "dogadjanja_"+time.Now().In(models.Zagreb).Format("2006-01-02")+".xlsx", k)
}
