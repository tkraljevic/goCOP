package web

import (
	"bytes"
	"log"
	"net/http"
	"slices"
	"strings"
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
	uSektor    = "sektor"
	uDezurni   = "dezurni"
	uPodrucje  = "podrucje"
	uDionica   = "dionica"
	uVodocuvar = "vodocuvar"
	uSkladiste = "skladiste"
	uUgovor    = "ugovor"  // ovlaštenik za praćenje ugovora A.02 ili A.03
	uUsluga    = "usluga"  // voditelj usluga izvođača
	uObjekt    = "objekt"  // strojar, rukovatelj, voditelj posade
	uPregled   = "pregled" // preglednik: samo čita, uz imena dežurnih i događanja
	uGost      = "gost"    // gost obrane: samo gleda, bez imena
)

// ulogaNaslovne je uloga po kojoj se slaže Naslovna osobe; prazno znači
// dosadašnju Naslovnu
func ulogaNaslovne(perms *models.UserPermissions, sad time.Time) string {
	if perms == nil {
		return ""
	}
	for _, p := range pravilaUloga {
		if p.ima(perms, sad) {
			return p.uloga
		}
	}
	return ""
}

// pravilaUloga su uloge Naslovne redom prednosti, svaka s pravilom
var pravilaUloga = []struct {
	uloga string
	ima   func(*models.UserPermissions, time.Time) bool
}{
	{uAdmin, func(p *models.UserPermissions, _ time.Time) bool { return p.IsGlobalAdmin }},
	{uUprava, func(p *models.UserPermissions, _ time.Time) bool { return p.DrzavnaUprava() }},
	{uSektor, func(p *models.UserPermissions, sad time.Time) bool { return duznostSektora(&p.User, sad) != nil }},
	{uDezurni, func(p *models.UserPermissions, sad time.Time) bool { return dezurnaDuznost(&p.User, sad) != nil }},
	{uPodrucje, func(p *models.UserPermissions, sad time.Time) bool { return duznostPodrucja(&p.User, sad) != nil }},
	{uUgovor, func(p *models.UserPermissions, sad time.Time) bool { return duznostUgovora(&p.User, sad) != nil }},
	{uDionica, func(p *models.UserPermissions, sad time.Time) bool { return len(duznostiDionica(&p.User, sad)) > 0 }},
	{uVodocuvar, func(p *models.UserPermissions, sad time.Time) bool { return terenskaDuznost(&p.User, sad) != nil }},
	{uUsluga, func(p *models.UserPermissions, sad time.Time) bool { return duznostUsluga(&p.User, sad) != nil }},
	{uObjekt, func(p *models.UserPermissions, sad time.Time) bool { return duznostNaObjektu(&p.User, sad) != nil }},
	{uSkladiste, func(p *models.UserPermissions, sad time.Time) bool { return duznostSkladista(&p.User, sad) != nil }},
	{uPregled, func(p *models.UserPermissions, sad time.Time) bool { return imaUlogu(&p.User, models.RoleViewer, sad) }},
	{uGost, func(p *models.UserPermissions, sad time.Time) bool { return imaUlogu(&p.User, models.RoleGuest, sad) }},
}

// karticeZaUlogu su kartice Naslovne po ulozi, redom
var karticeZaUlogu = map[string][]string{
	uAdmin:     {kStanjeObrane, kZaRijesiti, kVodostaji, kVrijeme, kJaceOborine, kDogadjanja, kSustav},
	uUprava:    {kStanjeObrane, kZaRijesiti, kVodostaji, kVrijeme, kJaceOborine, kDogadjanja},
	uSektor:    {kStanjeSektora, kZaRijesiti, kDnevnikCOP, kVodostaji, kVrijeme, kJaceOborine},
	uDezurni:   {kMojeDezurstvo, kZaRijesiti, kStanjeSektora, kDnevnikCOP, kVodostaji, kVrijeme, kJaceOborine},
	uPodrucje:  {kPodrucje, kZaRijesiti, kZadaciPodrucja, kCrpneStanice, kUstave, kVodostaji, kVrijeme, kJaceOborine},
	uDionica:   {kMojeDionice, kZaRijesiti, kObjektiDionica, kVodostaji, kVrijeme, kJaceOborine},
	uVodocuvar: {kDnevniList, kZadaci, kMojeLetve, kVrijeme},
	uUgovor:    {kUgovor, kZaRijesiti, kNalozi, kVodostaji, kVrijeme, kJaceOborine},
	uUsluga:    {kDnevnikRadova, kNaloziOvl, kLjudiNaTerenu, kVrijeme},
	uObjekt:    {kMojiObjekti, kNaloziObjektu, kVrijeme},
	uSkladiste: {kZaRijesiti, kSkladiste, kSredstvaTeren, kPrometSredstva},
	uPregled:   {kStanjeObrane, kDogadjanja, kVodostaji, kVrijeme, kJaceOborine},
	uGost:      {kStanjeObrane, kVodostaji, kVrijeme, kJaceOborine},
}

// imaUlogu: osoba ima valjanu dužnost s ulogom
func imaUlogu(u *models.User, uloga models.Role, sad time.Time) bool {
	return slices.ContainsFunc(models.VazecaZaduzenja(u.Duties, sad), func(d models.Duty) bool { return d.Role == uloga })
}

// TrakaPregleda je napomena iznad kartica gosta i preglednika
const TrakaPregleda = "Račun za pregled: možete gledati, ali ne i upisivati."

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
		out = append(out, poveznicaKojuVidi(h.kartica(r, perms, data, id), data.Modules))
	}
	return out
}

// poveznicaKojuVidi skida poveznicu kartice koja vodi u modul koji račun ne
// vidi, da ne vodi na zabranjenu stranicu (voditelj usluga ima karticu
// Vrijeme, a modul Očitanja s Prognozama zadano nema)
func poveznicaKojuVidi(k KarticaNaslovne, vis models.Visibility) KarticaNaslovne {
	putanja, _, _ := strings.Cut(k.URL, "?")
	if m := moduleForPath(putanja); m != "" && !vis.Sees(m) {
		k.Poveznica, k.URL = "", ""
	}
	return k
}

// kartica slaže jednu karticu po oznaci
func (h *DashboardHandler) kartica(r *http.Request, perms *models.UserPermissions, data *DashboardViewData, id string) KarticaNaslovne {
	switch id {
	case kStanjeObrane:
		return karticaStanjaObrane(data.Stanja, h.imenaSektora(), data.Modules.Sees(models.ModuleJournals), ulogaNaslovne(perms, time.Now()) != uGost)
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
	kVrijeme:     {Naslov: "Vrijeme", Poveznica: "Prognoze", URL: "/prognoze/vrijeme"},
	kJaceOborine: {Naslov: "Jače oborine", Poveznica: "Slivovi", URL: "/slivovi#kisa"},
	kSustav:      {Naslov: "Sustav", Poveznica: "Administracija", URL: "/administracija"},
	kMojeLetve:   {Naslov: "Moje letve", Poveznica: "Sve letve područja", URL: "/teren"},
	kDnevniList:  {Naslov: "Dnevni list"},
	kZadaci:      {Naslov: "Zadaci rukovoditelja"},
	// rukovoditelj branjenog područja
	kPodrucje:       {Naslov: "Branjeno područje", Poveznica: "Akti", URL: "/akti"},
	kZadaciPodrucja: {Naslov: "Zadaci vodočuvarima"},
	kCrpneStanice:   {Naslov: "Crpne stanice"},
	kUstave:         {Naslov: "Ustave"},
	// rukovoditelj dionice
	kMojeDionice:    {Naslov: "Moje dionice"},
	kObjektiDionica: {Naslov: "Objekti na dionicama"},
	// skladištar
	kSkladiste:      {Naslov: "Skladište"},
	kSredstvaTeren:  {Naslov: "Na terenu", Poveznica: "Sve", URL: "/sredstva/na-terenu"},
	kPrometSredstva: {Naslov: "Zadnji promet", Poveznica: "Sve", URL: "/sredstva/promet"},
	// ovlaštenik i voditelj usluga
	kUgovor:        {Naslov: "Ugovor"},
	kNalozi:        {Naslov: "Nalozi"},
	kDnevnikRadova: {Naslov: "Dnevnik radova"},
	kNaloziOvl:     {Naslov: "Nalozi ovlaštenika"},
	kLjudiNaTerenu: {Naslov: "Na terenu danas"},
	// strojar, rukovatelj, voditelj posade
	kMojiObjekti:   {Naslov: "Moji objekti", Poveznica: "Teren", URL: "/teren"},
	kNaloziObjektu: {Naslov: "Nalozi"},
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
	vis, _ := r.Context().Value(contextKeyModules).(models.Visibility)
	k := poveznicaKojuVidi(h.sporaKartica(r, perms, id), vis)
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
	if ulogaNaslovne(perms, sad) == uDionica && id != kVrijeme && id != kJaceOborine {
		return h.karticaRukovoditeljaDionice(r, perms, id, sad)
	}
	if f, ima := sporeKartice[id]; ima {
		return f(h, r, perms, id, sad)
	}
	return h.sustav(r.Context(), sad)
}

// sporeKartice: kako se računa tijelo koje spore kartice; ostalo je Sustav
var sporeKartice = map[string]func(h *DashboardHandler, r *http.Request, perms *models.UserPermissions, id string, sad time.Time) KarticaNaslovne{
	kZaRijesiti: func(h *DashboardHandler, r *http.Request, perms *models.UserPermissions, _ string, sad time.Time) KarticaNaslovne {
		return h.zaRijesiti(r, perms, sad)
	},
	kVodostaji: func(h *DashboardHandler, r *http.Request, perms *models.UserPermissions, _ string, sad time.Time) KarticaNaslovne {
		return h.vodostaji(r.Context(), perms, sad)
	},
	kVrijeme: func(h *DashboardHandler, r *http.Request, _ *models.UserPermissions, _ string, _ time.Time) KarticaNaslovne {
		return h.vrijeme(r)
	},
	kJaceOborine: func(h *DashboardHandler, r *http.Request, _ *models.UserPermissions, _ string, _ time.Time) KarticaNaslovne {
		return h.jaceOborine(r.Context())
	},
	kMojeLetve: func(h *DashboardHandler, r *http.Request, perms *models.UserPermissions, _ string, sad time.Time) KarticaNaslovne {
		return h.mojeLetve(r.Context(), perms, sad)
	},
	kPodrucje:       (*DashboardHandler).karticaRukovoditeljaBP,
	kZadaciPodrucja: (*DashboardHandler).karticaRukovoditeljaBP,
	kCrpneStanice:   (*DashboardHandler).karticaRukovoditeljaBP,
	kUstave:         (*DashboardHandler).karticaRukovoditeljaBP,
	kSkladiste:      (*DashboardHandler).karticaSkladistara,
	kSredstvaTeren:  (*DashboardHandler).karticaSkladistara,
	kPrometSredstva: (*DashboardHandler).karticaSkladistara,
	kUgovor:         (*DashboardHandler).karticaUsluga,
	kNalozi:         (*DashboardHandler).karticaUsluga,
	kDnevnikRadova:  (*DashboardHandler).karticaUsluga,
	kNaloziOvl:      (*DashboardHandler).karticaUsluga,
	kLjudiNaTerenu:  (*DashboardHandler).karticaUsluga,
	kMojiObjekti:    (*DashboardHandler).karticaNaObjektu,
	kNaloziObjektu:  (*DashboardHandler).karticaNaObjektu,
}
