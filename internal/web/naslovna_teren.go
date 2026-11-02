package web

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Naslovna vodočuvara (Odluke (31. 10. i 1. 11. 2026.)): traka faze
// vlastitog područja, gumbi „Nova prijava” i „Nazovi”, dnevni list, zadaci
// rukovoditelja, moje letve i vrijeme. Na računalu iste kartice kao na
// mobitelu, u dva stupca na sredini.

// Kartice vodočuvara
const (
	kDnevniList = "dnevni-list"
	kZadaci     = "zadaci"
	kMojeLetve  = "moje-letve"
)

// terenskaDuznost je valjana dužnost vodočuvara osobe; nil kad je nema
func terenskaDuznost(u *models.User, sad time.Time) *models.Duty {
	for _, d := range models.VazecaZaduzenja(u.Duties, sad) {
		if d.Role == models.RoleWaterGuard && d.AreaID != nil && d.SectorID != nil {
			return &d
		}
	}
	return nil
}

// naslovnaVodocuvara puni traku, gumbe i osobe za poziv vodočuvara
func (h *DashboardHandler) naslovnaVodocuvara(r *http.Request, data *DashboardViewData, sad time.Time) {
	d := terenskaDuznost(data.CurrentUser, sad)
	if d == nil {
		return
	}
	data.Teren = true
	data.Akcije = []GumbNaslovne{{Tekst: "Nova prijava", URL: "/prijave/nova", Glavni: true}}
	ctx := r.Context()
	if h.izvori.StanjaSektora != nil {
		stanja, dionice, err := h.izvori.StanjaSektora(ctx, *d.SectorID)
		if err == nil {
			data.Traka = trakaPodrucja(*d.AreaID, *d.SectorID, stanja, dionice)
		}
	}
	data.Nazovi = h.osobeZaPoziv(*d, data.Stanja, sad)
}

// trakaPodrucja: „BP 16: preventivna obrana · Sektor B: pripremno stanje na
// BP 34 od 06:15”. Traka ima boju faze vlastitog područja; o ostatku
// sektora piše samo najviši stupanj (Odluke (31. 10. 2026.)).
func trakaPodrucja(area int, sektor string, stanja map[string]models.StanjeObrane, dionice []models.Section) *TrakaNaslovne {
	vlastito, drugo, drugoBP := fazaPodrucja(area, stanja, dionice), models.AktivniStadij{}, 0
	for _, d := range dionice {
		if v := stanja[d.Code].Vrh(); d.AreaID != area && v.Stupanj.Severity() > drugo.Stupanj.Severity() {
			drugo, drugoBP = v, d.AreaID
		}
	}
	t := &TrakaNaslovne{Tekst: fmt.Sprintf("BP %d: preventivna obrana", area)}
	if vlastito.Stupanj.InForce() {
		t.Tekst = fmt.Sprintf("BP %d: %s od %s", area, strings.ToLower(vlastito.Stupanj.Label()), odKadFaze(vlastito.Od))
		t.Razred = vlastito.Stupanj.PillClass()
	}
	if drugo.Stupanj.Severity() > vlastito.Stupanj.Severity() {
		t.Tekst += fmt.Sprintf(" · Sektor %s: %s na BP %d od %s", sektor, strings.ToLower(drugo.Stupanj.Label()), drugoBP, odKadFaze(drugo.Od))
	}
	return t
}

// odKadFaze: „06:15” za danas, inače „30. 10.”
func odKadFaze(t time.Time) string {
	lt, sad := t.In(models.Zagreb), time.Now().In(models.Zagreb)
	if lt.Format("2006-01-02") == sad.Format("2006-01-02") {
		return lt.Format("15:04")
	}
	return fmt.Sprintf("%d. %d.", lt.Day(), int(lt.Month()))
}

// funkcijePoziva su uloge koje vodočuvar zove, redom, s funkcijom kako
// piše u izboru
var funkcijePoziva = []struct {
	uloga    models.Role
	funkcija string
	podrucje bool // uloga na području vodočuvara (inače na sektoru)
}{
	{models.RoleAreaLeader, "rukovoditelj BP %d", true},
	{models.RoleAreaDeputy, "zamjenik rukovoditelja BP %d", true},
	{models.RoleSectionLeader, "rukovoditelj dionice %s", true},
	{models.RoleSectionDeputy, "zamjenik rukovoditelja dionice %s", true},
	{models.RoleCopLeader, "voditelj COP-a, Sektor %s", false},
}

// osobeZaPoziv: dežurni samo dok dežurstvo stvarno traje, a uvijek
// rukovoditelji područja i dionica vodočuvara i voditelj COP-a (Odluke
// (1. 11. 2026.))
func (h *DashboardHandler) osobeZaPoziv(d models.Duty, stanja []service.StanjeObrane, sad time.Time) []OsobaZaPoziv {
	if h.izvori.Osobe == nil {
		return nil
	}
	vidjen := map[string]bool{}
	out := h.dezurniUIzboru(*d.SectorID, stanja, sad, vidjen)
	return append(out, h.rukovoditeljiUIzboru(d, sad, vidjen)...)
}

// dezurniUIzboru je dežurni sektora dok dežurstvo traje
func (h *DashboardHandler) dezurniUIzboru(sektor string, stanja []service.StanjeObrane, sad time.Time, vidjen map[string]bool) []OsobaZaPoziv {
	for _, st := range stanja {
		if st.Sektor == sektor && st.Dnevnik != nil && st.Dnevnik.NetkoDezura() && DezurstvoTraje(st.Stadij, sad) {
			vidjen[st.Dnevnik.DezurniID] = true
			return h.dezurni(sektor, st.Dnevnik.DezurniID)
		}
	}
	return nil
}

// rukovoditeljiUIzboru su rukovoditelji područja i dionica vodočuvara i
// voditelj COP-a, svaka osoba jednom
func (h *DashboardHandler) rukovoditeljiUIzboru(d models.Duty, sad time.Time, vidjen map[string]bool) []OsobaZaPoziv {
	var out []OsobaZaPoziv
	for _, f := range funkcijePoziva {
		area := 0
		if f.podrucje {
			area = *d.AreaID
		}
		osobe, _ := h.izvori.Osobe(*d.SectorID, area, f.uloga) // bez imenika izbor ostane kraći
		for _, o := range osobe {
			if p, ima := osobaZaPoziv(o, f.uloga, f.funkcija, d, sad); ima && !vidjen[o.ID.String()] {
				vidjen[o.ID.String()] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// dezurni je dežurni operater sektora po oznaci iz dnevnika COP-a
func (h *DashboardHandler) dezurni(sektor, id string) []OsobaZaPoziv {
	osobe, _ := h.izvori.Osobe(sektor, 0, models.RoleOperator) // bez imenika dežurnog nema u izboru
	for _, o := range osobe {
		if o.ID.String() == id {
			return []OsobaZaPoziv{{Ime: o.FullName, Funkcija: "dežurni u COP-u, Sektor " + sektor, Broj: brojZaPoziv(o)}}
		}
	}
	return nil
}

// osobaZaPoziv je osoba s ulogom kao stavka izbora; rukovoditelj dionice
// ulazi samo ako vodi dionicu vodočuvara
func osobaZaPoziv(o models.User, uloga models.Role, funkcija string, vod models.Duty, sad time.Time) (OsobaZaPoziv, bool) {
	for _, d := range models.VazecaZaduzenja(o.Duties, sad) {
		if d.Role != uloga {
			continue
		}
		switch uloga {
		case models.RoleSectionLeader, models.RoleSectionDeputy:
			if zajednicka := zajednickaDionica(d, vod); zajednicka != "" {
				return OsobaZaPoziv{Ime: o.FullName, Funkcija: fmt.Sprintf(funkcija, zajednicka), Broj: brojZaPoziv(o)}, true
			}
		case models.RoleCopLeader:
			return OsobaZaPoziv{Ime: o.FullName, Funkcija: fmt.Sprintf(funkcija, *vod.SectorID), Broj: brojZaPoziv(o)}, true
		default:
			return OsobaZaPoziv{Ime: o.FullName, Funkcija: fmt.Sprintf(funkcija, *vod.AreaID), Broj: brojZaPoziv(o)}, true
		}
	}
	return OsobaZaPoziv{}, false
}

// zajednickaDionica je prva dionica rukovoditelja koju čuva i vodočuvar;
// vodočuvar bez popisa dionica čuva cijelo područje
func zajednickaDionica(ruk, vod models.Duty) string {
	njegove := vod.Dionice()
	for _, x := range ruk.Dionice() {
		if len(njegove) == 0 || slices.Contains(njegove, x) {
			return x
		}
	}
	return ""
}

// brojZaPoziv: mobitel, a bez njega telefon u uredu
func brojZaPoziv(u models.User) string {
	for _, b := range []string{u.MobilePhone, u.Phone} {
		if b = strings.TrimSpace(b); b != "" && !strings.EqualFold(b, "N/A") {
			return b
		}
	}
	return ""
}

// DezurstvoTraje: dežura se od prvog stupnja, samo drukčije. Pripremno
// stanje: radnim danom od 7 do 15 h; redovna obrana od 7 do 19 h;
// izvanredna obrana i izvanredno stanje cijeli dan. U preventivnoj obrani
// dežurnog nema (Odluke (1. 11. 2026.)).
func DezurstvoTraje(stadij models.DefensePhase, t time.Time) bool {
	sat := t.In(models.Zagreb).Hour()
	switch stadij {
	case models.PhasePrep:
		dan := t.In(models.Zagreb).Weekday()
		return dan != time.Saturday && dan != time.Sunday && sat >= 7 && sat < 15
	case models.PhaseRegular:
		return sat >= 7 && sat < 19
	case models.PhaseEmergency, models.PhaseState:
		return true
	}
	return false
}

// karticaDnevnogLista: je li današnji list otvoren i predan, i gumb za njega
func karticaDnevnogLista(l *models.VodocuvarskiList, sad time.Time) KarticaNaslovne {
	k := KarticaNaslovne{ID: kDnevniList, Naslov: "Dnevni list · " + danUTjednu(sad)}
	k.Gumbi = []GumbNaslovne{{Tekst: "Otvori list", URL: "/vodocuvar/dan", Glavni: true}}
	switch {
	case l == nil:
		k.Tekst = "Danas još nije otvoren."
	case l.Predan():
		k.Tekst = "Predan u " + l.PredanoAt.In(models.Zagreb).Format("15:04") + "."
	default:
		k.Tekst = "Otvoren u " + l.CreatedAt.In(models.Zagreb).Format("15:04") + " · još nije predan"
	}
	if l != nil && l.ID != "" {
		k.Gumbi = append(k.Gumbi, GumbNaslovne{Tekst: "Ispiši list", URL: "/vodocuvar/" + l.ID + "/list", SamoSiroko: true})
	}
	return k
}

// danUTjednu: „ned 2. 11.”
func danUTjednu(t time.Time) string {
	lt := t.In(models.Zagreb)
	return fmt.Sprintf("%s %d. %d.", daniUTjednu[lt.Weekday()], lt.Day(), int(lt.Month()))
}

// karticaZadataka: otvoreni zadaci rukovoditelja, najstariji prvi. Zadatak
// se označi obavljenim na listu, uz opis što je obavljeno, pa redak vodi
// na današnji list.
func karticaZadataka(zadaci []models.Zadatak, sad time.Time) KarticaNaslovne {
	k := KarticaNaslovne{ID: kZadaci, Naslov: "Zadaci rukovoditelja", URL: "/vodocuvar/dan"}
	var otvoreni []models.Zadatak
	for _, z := range zadaci {
		if z.Otvoren() {
			otvoreni = append(otvoreni, z)
		}
	}
	slices.SortFunc(otvoreni, func(a, b models.Zadatak) int { return a.ZadanoAt.Compare(b.ZadanoAt) })
	for _, z := range otvoreni {
		k.Redovi = append(k.Redovi, RedakKartice{Ikona: "clipboard-list", Glavno: z.Tekst, Sporedno: z.Zadao + " · " + kadNaslovne(z.ZadanoAt, sad), URL: "/vodocuvar/dan"})
	}
	k.Broj = len(k.Redovi)
	if k.Broj == 0 {
		k.Prazno = &PraznaKartica{Vrsta: PraznoGotovo, Tekst: "Nema otvorenih zadataka."}
	}
	return k
}

// mojeLetve: letve koje vodočuvar redovito očitava, s današnjim
// vodostajem ili gumbom „Upiši”
func (h *DashboardHandler) mojeLetve(ctx context.Context, perms *models.UserPermissions, sad time.Time) KarticaNaslovne {
	if h.izvori.Teren == nil {
		return nedostupna(kMojeLetve, "Letve trenutačno nisu dostupne.")
	}
	fo, err := h.izvori.Teren(ctx, perms, &perms.User)
	if err != nil || fo == nil {
		return nedostupna(kMojeLetve, "Letve trenutačno nisu dostupne.")
	}
	return karticaMojihLetava(fo.Mine, sad)
}

// karticaMojihLetava: očitana letva s vodostajem i vremenom, neočitana s
// gumbom „Upiši” koji se vraća na Naslovnu
func karticaMojihLetava(letve []models.GaugeSummary, sad time.Time) KarticaNaslovne {
	k := glavaKartice(kMojeLetve)
	for _, g := range letve {
		r := RedakKartice{Ikona: "ruler", Glavno: g.Name, URL: g.URL}
		if g.DoneToday && g.Latest != nil && g.Latest.LevelCm != nil {
			r.Sporedno = "upisano " + kadNaslovne(g.Latest.MeasuredAt, sad)
			r.Vrijednost = fmt.Sprintf("%d cm", *g.Latest.LevelCm)
		} else {
			r.Sporedno, r.Gumb = "danas još nije očitana", &GumbNaslovne{Tekst: "Upiši", URL: g.NewURL + "&back=/"}
		}
		k.Redovi = append(k.Redovi, r)
	}
	if len(k.Redovi) == 0 {
		k.Prazno = &PraznaKartica{Vrsta: PraznoCeka, Tekst: "Još nema letava koje redovito očitavaš. Sve letve područja su na stranici Teren."}
	}
	return k
}
