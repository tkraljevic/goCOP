package web

import (
	"bytes"
	"log"
	"net/http"
	"slices"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Kartice Naslovne po ulozi. Globalni administrator ima sve; glavni
// rukovoditelj i voditelj Glavnog centra isto, bez Sustava (Odluke
// (31. 10. 2026.)). Ostale uloge zasad imaju dosadašnju Naslovnu, dok ne
// dođu na red.
const (
	kStanjeObrane = "stanje-obrane"
	kZaRijesiti   = "za-rijesiti"
	kVodostaji    = "vodostaji"
	kVrijeme      = "vrijeme"
	kJaceOborine  = "jace-oborine"
	kDogadjanja   = "dogadjanja"
	kSustav       = "sustav"
)

// Uloge s vlastitom Naslovnom. Osoba s više dužnosti dobije Naslovnu prve
// po ovom redu.
const (
	uAdmin     = "admin"
	uUprava    = "uprava"
	uDezurni   = "dezurni"
	uPodrucje  = "podrucje"
	uVodocuvar = "vodocuvar"
)

// ulogaNaslovne je uloga po kojoj se slaže Naslovna osobe; prazno znači
// dosadašnju Naslovnu
func ulogaNaslovne(perms *models.UserPermissions, sad time.Time) string {
	switch {
	case perms == nil:
		return ""
	case perms.IsGlobalAdmin:
		return uAdmin
	case perms.DrzavnaUprava():
		return uUprava
	case dezurnaDuznost(&perms.User, sad) != nil:
		return uDezurni
	case duznostPodrucja(&perms.User, sad) != nil:
		return uPodrucje
	case terenskaDuznost(&perms.User, sad) != nil:
		return uVodocuvar
	}
	return ""
}

// karticeZaUlogu su kartice Naslovne po ulozi, redom
var karticeZaUlogu = map[string][]string{
	uAdmin:     {kStanjeObrane, kZaRijesiti, kVodostaji, kVrijeme, kJaceOborine, kDogadjanja, kSustav},
	uUprava:    {kStanjeObrane, kZaRijesiti, kVodostaji, kVrijeme, kJaceOborine, kDogadjanja},
	uDezurni:   {kMojeDezurstvo, kZaRijesiti, kStanjeSektora, kDnevnikCOP, kVodostaji, kVrijeme, kJaceOborine},
	uPodrucje:  {kPodrucje, kZaRijesiti, kZadaciPodrucja, kCrpneStanice, kUstave, kVodostaji, kVrijeme, kJaceOborine},
	uVodocuvar: {kDnevniList, kZadaci, kMojeLetve, kVrijeme},
}

// karticeUloge su kartice Naslovne za ulogu osobe, redom; nil znači
// dosadašnju Naslovnu
func karticeUloge(perms *models.UserPermissions) []string {
	return karticeZaUlogu[ulogaNaslovne(perms, time.Now())]
}

// kartice slaže kartice Naslovne za osobu. Spore kartice dobiju samo glavu i
// adresu s koje se tijelo učita nakon stranice.
func (h *DashboardHandler) kartice(r *http.Request, perms *models.UserPermissions, data *DashboardViewData) []KarticaNaslovne {
	var out []KarticaNaslovne
	for _, id := range karticeUloge(perms) {
		out = append(out, h.kartica(r, perms, data, id))
	}
	return out
}

// kartica slaže jednu karticu po oznaci
func (h *DashboardHandler) kartica(r *http.Request, perms *models.UserPermissions, data *DashboardViewData, id string) KarticaNaslovne {
	switch id {
	case kStanjeObrane:
		return karticaStanjaObrane(data.Stanja, h.imenaSektora(), data.Modules.Sees(models.ModuleJournals))
	case kDogadjanja:
		return karticaDogadjanja(data.Zid, time.Now())
	case kDnevniList, kZadaci:
		return h.karticaKnjige(r, id)
	case kMojeDezurstvo, kDnevnikCOP, kStanjeSektora:
		return h.karticaDezurnog(r, perms, data, id)
	case kPodrucje:
		return lijenaKarticaPodrucja(perms)
	}
	return lijenaKartica(id)
}

// karticaKnjige su kartice iz vodočuvarskog dnevnika: današnji list i
// otvoreni zadaci rukovoditelja
func (h *DashboardHandler) karticaKnjige(r *http.Request, id string) KarticaNaslovne {
	sad := time.Now()
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	var vs *service.VodocuvarService
	if h.vodocuvar != nil {
		vs = h.vodocuvar()
	}
	if vs == nil || u == nil {
		return nedostupna(id, "Dnevnik trenutačno nije dostupan.")
	}
	if id == kZadaci {
		return karticaZadataka(vs.Zadaci(r.Context(), u.ID.String()), sad)
	}
	l, err := vs.DanasnjiList(r.Context(), u)
	if err != nil {
		return nedostupna(id, "Dnevnik trenutačno nije dostupan.")
	}
	return karticaDnevnogLista(l, sad)
}

// imenaSektora su imena sektora iz registra po oznaci
func (h *DashboardHandler) imenaSektora() map[string]string {
	out := map[string]string{}
	sektori, _ := h.userService.ListSectors() // bez registra kartica piše samo „Sektor B”
	for _, s := range sektori {
		out[s.ID] = s.Name
	}
	return out
}

// glaveKartica su naslov i poveznica spore kartice, prije nego se tijelo učita
var glaveKartica = map[string]KarticaNaslovne{
	kZaRijesiti:  {Naslov: "Za riješiti"},
	kVodostaji:   {Naslov: "Vodostaji", Poveznica: "Prognoze", URL: "/prognoze"},
	kVrijeme:     {Naslov: "Vrijeme"},
	kJaceOborine: {Naslov: "Jače oborine", Poveznica: "Slivovi", URL: "/slivovi"},
	kSustav:      {Naslov: "Sustav", Poveznica: "Administracija", URL: "/administracija"},
	kMojeLetve:   {Naslov: "Moje letve", Poveznica: "Sve letve područja", URL: "/teren"},
	kDnevniList:  {Naslov: "Dnevni list"},
	kZadaci:      {Naslov: "Zadaci rukovoditelja"},
	// rukovoditelj branjenog područja
	kPodrucje:       {Naslov: "Branjeno područje", Poveznica: "Akti", URL: "/akti"},
	kZadaciPodrucja: {Naslov: "Zadaci vodočuvarima"},
	kCrpneStanice:   {Naslov: "Crpne stanice"},
	kUstave:         {Naslov: "Ustave"},
}

// lijenaKartica je glava spore kartice s adresom tijela
func lijenaKartica(id string) KarticaNaslovne {
	k := glaveKartica[id]
	k.ID, k.Lijeno = id, "/naslovna/kartica/"+id
	return k
}

// ShowKartica šalje jednu sporu karticu Naslovne, cijelu, kad je stranica
// već na zaslonu. Kartica koju uloga nema na Naslovnoj ne postoji (404).
func (h *DashboardHandler) ShowKartica(w http.ResponseWriter, r *http.Request) {
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	id := r.PathValue("id")
	if !slices.Contains(karticeUloge(perms), id) || glaveKartica[id].Naslov == "" {
		http.NotFound(w, r)
		return
	}
	k := h.sporaKartica(r, perms, id)
	var b bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&b, "karticaNaslovne", k); err != nil {
		log.Printf("kartica naslovne %s: %v", id, err)
		http.Error(w, "Kartica se nije dala složiti.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = b.WriteTo(w) // greška znači samo da je preglednik otišao
}

// sporaKartica računa tijelo spore kartice
func (h *DashboardHandler) sporaKartica(r *http.Request, perms *models.UserPermissions, id string) KarticaNaslovne {
	sad := time.Now()
	switch id {
	case kZaRijesiti:
		return h.zaRijesiti(r, perms, sad)
	case kVodostaji:
		return h.vodostaji(r.Context(), perms, sad)
	case kVrijeme:
		return h.vrijeme(r)
	case kJaceOborine:
		return h.jaceOborine(r.Context())
	case kMojeLetve:
		return h.mojeLetve(r.Context(), perms, sad)
	case kPodrucje, kZadaciPodrucja, kCrpneStanice, kUstave:
		return h.karticaRukovoditeljaBP(r, perms, id, sad)
	}
	return h.sustav(r.Context(), sad)
}
