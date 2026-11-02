package web

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Naslovna skladištara sredstava za obranu (Odluke (31. 10. 2026.)): traka
// stanja obrane područja, Za riješiti (godišnji popis), skladište, sredstva
// na terenu i zadnji promet. Zahtjeva za sredstva, povrata s rokom i
// najmanje količine u programu još nema, pa ih nema ni na kartici.

// Kartice skladištara
const (
	kSkladiste      = "skladiste"
	kSredstvaTeren  = "sredstva-na-terenu"
	kPrometSredstva = "promet-sredstava"
)

// duznostSkladista je valjana dužnost skladištara osobe; područje je
// prazno kad vodi skladišta cijelog sektora
func duznostSkladista(u *models.User, sad time.Time) *models.Duty {
	for _, d := range models.VazecaZaduzenja(u.Duties, sad) {
		if d.Role == models.RoleWarehouseKeeper && d.SectorID != nil && *d.SectorID != "" {
			return &d
		}
	}
	return nil
}

// podrucjeSkladista je područje dužnosti skladištara; 0 za cijeli sektor
func podrucjeSkladista(d models.Duty) int {
	if d.AreaID == nil {
		return 0
	}
	return *d.AreaID
}

// PopisOdMjeseca je mjesec od kojeg Za riješiti podsjeća na godišnji popis
// sa stanjem na 31. 12.
const PopisOdMjeseca = time.December

// karticaSkladistara slaže spore kartice skladištara
func (h *DashboardHandler) karticaSkladistara(r *http.Request, perms *models.UserPermissions, id string, sad time.Time) KarticaNaslovne {
	d := duznostSkladista(&perms.User, sad)
	if d == nil || h.izvori.Skladista == nil {
		return nedostupna(id, "Sredstva trenutačno nisu dostupna.")
	}
	ctx := r.Context()
	sektor, area := *d.SectorID, podrucjeSkladista(*d)
	if id == kSredstvaTeren {
		return h.sredstvaNaTerenu(ctx, sektor, area)
	}
	skladista, err := h.izvori.Skladista(ctx, sektor, area)
	if err != nil {
		return nedostupna(id, "Sredstva trenutačno nisu dostupna.")
	}
	switch id {
	case kZaRijesiti:
		return h.popisiZaRijesiti(ctx, skladista, sad)
	case kPrometSredstva:
		return h.prometSkladista(ctx, skladista, sektor, sad)
	}
	return h.skladiste(ctx, skladista)
}

// popisiZaRijesiti: u prosincu skladišta čiji godišnji popis još nije
// zaključen
func (h *DashboardHandler) popisiZaRijesiti(ctx context.Context, skladista []models.Skladiste, sad time.Time) KarticaNaslovne {
	k := glavaKartice(kZaRijesiti)
	godina := sad.In(models.Zagreb).Year()
	if sad.In(models.Zagreb).Month() < PopisOdMjeseca || h.izvori.PopisiSredstava == nil {
		skladista = nil
	}
	for _, s := range skladista {
		popisi, _ := h.izvori.PopisiSredstava(ctx, s.Sektor, s.ID, godina) // bez popisa redak kaže da nije započet
		if r, ima := redakPopisa(s, popisi, godina); ima {
			k.Redovi = append(k.Redovi, r)
		}
	}
	k.Broj = len(k.Redovi)
	if k.Broj == 0 {
		k.Prazno = &PraznaKartica{Vrsta: PraznoGotovo, Tekst: "Nema ništa za riješiti."}
	}
	return k
}

// redakPopisa: godišnji popis skladišta koji nije zaključen; ništa kad je
func redakPopisa(s models.Skladiste, popisi []models.Popis, godina int) (RedakKartice, bool) {
	r := RedakKartice{Ikona: "clipboard-list", Glavno: "Godišnji popis · " + s.Naziv,
		Sporedno: fmt.Sprintf("stanje na 31. 12. %d. · nije započet", godina), URL: "/sredstva/skladista/" + s.ID + "/popis"}
	for _, p := range popisi {
		if p.Dan.In(models.Zagreb).Year() != godina {
			continue
		}
		if p.Zakljucen() {
			return RedakKartice{}, false
		}
		r.Sporedno, r.URL = fmt.Sprintf("stanje na 31. 12. %d. · u nacrtu", godina), "/sredstva/popisi/"+p.ID
	}
	return r, true
}

// skladiste: stanje jedinog skladišta po vrstama, a kad ih je više, svako
// skladište u svom retku
func (h *DashboardHandler) skladiste(ctx context.Context, skladista []models.Skladiste) KarticaNaslovne {
	k := glavaKartice(kSkladiste)
	switch len(skladista) {
	case 0:
		k.Prazno = &PraznaKartica{Vrsta: PraznoCeka, Tekst: "Na području nema skladišta u registru."}
		return k
	case 1:
	default:
		k.Naslov = "Skladišta"
		for _, s := range skladista {
			k.Redovi = append(k.Redovi, RedakKartice{Ikona: "package", Glavno: s.PunNaziv(), URL: "/sredstva/skladista/" + s.ID})
		}
		return k
	}
	s := skladista[0]
	k.Naslov, k.Poveznica, k.URL = "Skladište "+s.Naziv, "Stanje", "/sredstva/skladista/"+s.ID
	if h.izvori.StanjeSkladista == nil {
		return nedostupnaUz(k, "Stanje skladišta trenutačno nije dostupno.")
	}
	stanje, err := h.izvori.StanjeSkladista(ctx, s.ID)
	if err != nil {
		return nedostupnaUz(k, "Stanje skladišta trenutačno nije dostupno.")
	}
	return karticaStanjaSkladista(k, stanje)
}

// nedostupnaUz je kartica s glavom kojoj podaci nisu stigli
func nedostupnaUz(k KarticaNaslovne, tekst string) KarticaNaslovne {
	k.Prazno = &PraznaKartica{Vrsta: PraznoNedostupno, Tekst: tekst}
	return k
}

// karticaStanjaSkladista: svaka vrsta koje ima u skladištu, s količinom
// i, kad se vodi u oblicima, količinom po obliku
func karticaStanjaSkladista(k KarticaNaslovne, stanje []models.StanjeVrste) KarticaNaslovne {
	for _, v := range stanje {
		if v.Ukupno == 0 {
			continue
		}
		r := RedakKartice{Glavno: v.Vrsta.Naziv, Vrijednost: strings.TrimSpace(kolicinaHR(v.Ukupno) + " " + v.Vrsta.Jedinica)}
		if v.Vrsta.ImaOblike() {
			var dio []string
			for _, o := range v.Vrsta.Oblici {
				if n := v.PoOblicima[o]; n != 0 {
					dio = append(dio, strings.ToLower(models.OblikNaziv(o))+" "+kolicinaHR(n))
				}
			}
			r.Sporedno = strings.Join(dio, " · ")
		}
		k.Redovi = append(k.Redovi, r)
	}
	if len(k.Redovi) == 0 {
		k.Prazno = &PraznaKartica{Vrsta: PraznoCeka, Tekst: "U skladištu još nema upisanih sredstava."}
	}
	return k
}

// sredstvaNaTerenu: što je izdano na teren područja, a nije vraćeno ni
// ugrađeno
func (h *DashboardHandler) sredstvaNaTerenu(ctx context.Context, sektor string, area int) KarticaNaslovne {
	k := glavaKartice(kSredstvaTeren)
	k.URL = "/sredstva/na-terenu?sektor=" + sektor
	if h.izvori.SredstvaNaTerenu == nil {
		return nedostupnaUz(k, "Sredstva na terenu trenutačno nisu dostupna.")
	}
	stanja, err := h.izvori.SredstvaNaTerenu(ctx, sektor)
	if err != nil {
		return nedostupnaUz(k, "Sredstva na terenu trenutačno nisu dostupna.")
	}
	vrste := h.vrsteSredstava(ctx)
	for _, s := range stanja {
		if area != 0 && s.AreaID != area {
			continue
		}
		k.Redovi = append(k.Redovi, RedakKartice{Ikona: "map-pin", Glavno: kolicinaSredstva(s.Kolicina, vrste[s.VrstaID], s.VrstaID), Sporedno: s.MjestoNaziv()})
	}
	if len(k.Redovi) == 0 {
		k.Prazno = &PraznaKartica{Vrsta: PraznoGotovo, Tekst: "Na terenu nema izdanih sredstava."}
	}
	return k
}

// vrsteSredstava je katalog sredstava po oznaci; prazan bez kataloga
func (h *DashboardHandler) vrsteSredstava(ctx context.Context) map[string]models.VrstaSredstva {
	out := map[string]models.VrstaSredstva{}
	if h.izvori.VrsteSredstava == nil {
		return out
	}
	vrste, _ := h.izvori.VrsteSredstava(ctx) // bez kataloga redak piše oznaku vrste
	for _, v := range vrste {
		out[v.ID] = v
	}
	return out
}

// kolicinaSredstva: „1.800 kom · Vreće za pijesak”
func kolicinaSredstva(n float64, v models.VrstaSredstva, id string) string {
	naziv := v.Naziv
	if naziv == "" {
		naziv = id
	}
	return strings.TrimSpace(kolicinaHR(math.Abs(n))+" "+v.Jedinica) + " · " + naziv
}

// NajvisePrometa je koliko se zadnjih redaka prometa čita po skladištu
const NajvisePrometa = 5

// prometSkladista: zadnji promet skladišta, najnoviji prvi, i gumb „Nova
// primka” kad je skladište jedno
func (h *DashboardHandler) prometSkladista(ctx context.Context, skladista []models.Skladiste, sektor string, sad time.Time) KarticaNaslovne {
	k := glavaKartice(kPrometSredstva)
	k.URL = "/sredstva/promet?sektor=" + sektor
	if len(skladista) == 1 {
		k.URL += "&skladiste=" + skladista[0].ID
		k.Gumbi = []GumbNaslovne{{Tekst: "Nova primka", URL: "/sredstva/skladista/" + skladista[0].ID + "/promet/novo?vrsta=" + models.PrometPrimka, Glavni: true, SamoSiroko: true}}
	}
	if h.izvori.PrometSredstava == nil {
		return nedostupnaUz(k, "Promet sredstava trenutačno nije dostupan.")
	}
	var promet []models.Promet
	for _, s := range skladista {
		p, err := h.izvori.PrometSredstava(ctx, repository.FiltarPrometa{SkladisteID: s.ID, Limit: NajvisePrometa})
		if err != nil {
			return nedostupnaUz(k, "Promet sredstava trenutačno nije dostupan.")
		}
		promet = append(promet, p...)
	}
	slices.SortStableFunc(promet, func(a, b models.Promet) int {
		return cmp.Or(b.Datum.Compare(a.Datum), b.CreatedAt.Compare(a.CreatedAt))
	})
	vrste := h.vrsteSredstava(ctx)
	for _, p := range promet {
		k.Redovi = append(k.Redovi, redakPrometa(p, vrste, sad))
	}
	if len(k.Redovi) == 0 {
		k.Prazno = &PraznaKartica{Vrsta: PraznoCeka, Tekst: "U skladištu još nema prometa."}
	}
	return k
}

// redakPrometa: „Primka: 2.000 kom · Vreće za pijesak” · dan i tko je upisao
func redakPrometa(p models.Promet, vrste map[string]models.VrstaSredstva, sad time.Time) RedakKartice {
	r := RedakKartice{Ikona: "download", Glavno: models.PrometNaziv(p.Vrsta) + ": " + kolicinaSredstva(p.Kolicina, vrste[p.VrstaID], p.VrstaID)}
	if p.Kolicina < 0 {
		r.Ikona = "upload"
	}
	r.Sporedno = strings.TrimSuffix(danNaslovne(p.Datum, sad)+" · "+p.UserName, " · ")
	return r
}
