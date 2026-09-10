package web

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"

	"github.com/google/uuid"
)

// Stranice registra postaja: jedna postaja i obrazac. Isti razlog kao kod
// vodotoka: puna stranica radi na telefonu i bez skripte.

// StationPageData je stranica jedne postaje ili njezina obrasca
type StationPageData struct {
	CurrentUser          *models.User
	Permissions          *models.UserPermissions
	Station              models.Station
	ZeroDatumHistoryJSON template.JS // promjene kote nule za obrazac, kao JS literal
	OgradeNizaJSON       template.JS // vlastite ograde uz nizove, za obrazac
	ExtremesJSON         template.JS // zabilježeni ekstremi za obrazac
	ReturnLevelsJSON     template.JS // povratni vodostaji za obrazac
	Sections             []models.Section
	Episodes             []models.DefenseEpisode   // obrane vođene po ovoj letvi, najnovija prva
	Valovi               []models.Val              // valovi obrane izračunati iz niza, najnoviji prvi
	ValoviSvi            []models.Val              // svi valovi, prije rezanja na stranicu — izvješće bira po vrhu
	ValoviPager          Pager                     // listanje valova
	ValoviZbroj          []models.ZbrojStupnja     // koliko je koje stanje ukupno trajalo
	ValoviNiz            models.RazdobljeNiza      // na kojem je nizu računato
	ValoviPragovi        []models.PragObrane       // pragovi koji su ušli u izračun
	Nizovi               []models.HidroNiz         // što o ovoj letvi ima u arhivi
	Pregled              *models.HidroPregled      // karakteristične vrijednosti odabranog niza
	Profili              []models.ProfilKorita     // snimke poprečnog profila korita
	Profil               *models.ProfilKorita      // onaj koji se crta
	Krivulje             []models.HQKrivulja       // krivulje protoka po razdobljima
	PragoviQ             []PragProtok              // isti pragovi iskazani u protoku
	ImaProtok            bool                      // ima li ijedan prag protok, pa tablica treba stupac
	BrojOcitanja         int                       // koliko je očitanja upisano na letvi — za upozorenje pri brisanju
	PragoviKote          []PragKota                // isti pragovi kao apsolutna kota vodne plohe
	Karta                KartaPostavke             // izvor pločica za kartu položaja
	NizID                int64                     // koji je niz odabran
	Spojevi              []models.SpojDoseg        // spojeni nizovi: jedan satni, jedan dnevni
	Sada                 *models.SpojenaVrijednost // zadnja vrijednost spojenog niza
	Sazetak              []models.SazetakVelicine  // jedan redak po veličini
	ArhivaPogled                                   // povijest iz arhive na historijatu letve
	Crtez                *KoritoCrtez              // korito s vodom u njemu
	CrtezUzak            *KoritoCrtez              // isti presjek u obliku za telefon
	Zadnji               *models.HidroTocka        // zadnja vrijednost iz arhive
	ZadnjiProtok         float64                   // preračunat iz krivulje
	ZadnjiIzvor          string
	WaterRegistry        []models.Watercourse
	CanEdit              bool
	CanRecord            bool   // smije li upisati očitanje
	LetvaStranica        string // koja je stranica letve otvorena: kartica, ocitanja, historijat
	HistorijatPrazan     bool   // letva još nema ništa od onoga što historijat pokazuje
	IsEdit               bool
	SuccessMessage       string
	ErrorMessage         string
	ActiveNav            string
	ViewAsBanner
}

// SetPageTemplates daje rukovatelju predloške stranica i servise koje one trebaju
func (h *StationsHandler) SetPageTemplates(detail, form, historijat, histObrazac *template.Template,
	sections *service.SectionService, waters *service.WatercourseService) {
	h.tmplDetail = detail
	h.tmplForm = form
	h.tmplHistorijat = historijat
	h.tmplHistObrazac = histObrazac
	h.sectionService = sections
	h.watercourseService = waters
}

// SetArhiva daje rukovatelju hidrološku arhivu. Uzima se dohvatnik, a ne sama
// arhiva: poslužitelj se sastavlja prije nego što se arhiva otvori, pa bi
// vrijednost predana pri sastavljanju zauvijek ostala prazna.
//
// Arhive smije i ne biti — čvor koji je nije preuzeo prikazuje letvu bez
// povijesti.
func (h *StationsHandler) SetArhiva(f func() *repository.ArhivaRepository) {
	h.arhiva = f
}

// arh vraća arhivu ako je ima
func (h *StationsHandler) arh() *repository.ArhivaRepository {
	if h.arhiva == nil {
		return nil
	}
	return h.arhiva()
}

// SetKarta daje rukovatelju izvor pločica za kartu položaja letve. Uzima se
// dohvatnik, a ne sama vrijednost: poslužitelj se sastavlja i rute se
// registriraju prije nego što se postavke pročitaju, pa bi vrijednost predana
// pri sastavljanju zauvijek ostala prazna. Isto kao kod arhive.
func (h *StationsHandler) SetKarta(f func() KartaPostavke) { h.karta = f }

// SetReadingService daje rukovatelju pravo upisa očitanja, da zajednički
// izbornik letve pokaže isti gumb kao i stranica očitanja.
func (h *StationsHandler) SetReadingService(s *service.ReadingService) { h.readingService = s }

// SetPaket daje rukovatelju sve što treba za pakete historijata: gdje arhiva
// stoji, kako se čvor zove i kako se paket ugrađuje.
func (h *StationsHandler) SetPaket(put func() string, cvor func() string,
	ugradi func(*arhiva.Sadrzaj) error, tmpl *template.Template) {
	h.arhivaPutFn, h.cvorFn, h.ugradi, h.tmplPaket = put, cvor, ugradi, tmpl
}

// SetIspravci daje rukovatelju pohranu ispravaka arhive; bez nje se arhiva i
// dalje prikazuje, samo bez ispravaka.
func (h *StationsHandler) SetIspravci(f func() *repository.IspravakRepository) {
	h.ispravci = f
}

// SetSektor daje rukovatelju zapis sektora, iz kojeg se gradi memorandum na
// izvješću. Dohvatnik iz istog razloga kao kod karte.
func (h *StationsHandler) SetSektor(f func(ctx context.Context, id string) *models.Sector) {
	h.sektor = f
}

// SetEpisodeService daje rukovatelju epizode obrane, da se na kartici letve
// vidi tko je sve po njoj u obrani.
func (h *StationsHandler) SetEpisodeService(episodes *service.EpisodeService) {
	h.episodeService = episodes
}

func (h *StationsHandler) pageData(r *http.Request) StationPageData {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	return StationPageData{
		CurrentUser:    currUser,
		Permissions:    perms,
		SuccessMessage: r.URL.Query().Get("success"),
		ErrorMessage:   r.URL.Query().Get("error"),
		ActiveNav:      "stations",
		ViewAsBanner:   viewBanner(r),
	}
}

// canEditStation: globalni administrator sve; ostali postaju koja im je
// mjerodavna na dionici za koju smiju pisati (isto pravilo kao u servisu)
func (h *StationsHandler) canEditStation(perms *models.UserPermissions, st models.Station) bool {
	if perms == nil {
		return false
	}
	if perms.IsGlobalAdmin {
		return true
	}
	for _, code := range st.SectionCodes {
		if perms.AllowedSections[code] {
			return true
		}
	}
	return false
}

// ShowStation prikazuje jednu postaju s pragovima, kotama i dionicama
func (h *StationsHandler) ShowStation(w http.ResponseWriter, r *http.Request) {
	data, ok := h.podaciLetve(w, r)
	if !ok {
		return
	}
	data.LetvaStranica = "kartica"
	if err := h.tmplDetail.ExecuteTemplate(w, "station_detail.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HistorijatLetve je sve što je na letvi zabilježeno i iz njezina niza
// izračunato. Odvojeno od kartice namjerno: kartica odgovara na pitanje što
// letva jest — gdje stoji, koji su joj pragovi i kota nule — a historijat na
// pitanje što se dogodilo. Prije su oboje stajali na istoj stranici, pa je
// dežurni do pragova dolazio kroz sedam odjeljaka povijesti.
//
// Podaci su isti i skupljaju se istim putem, pa se stranice ne mogu razići.
func (h *StationsHandler) HistorijatLetve(w http.ResponseWriter, r *http.Request) {
	data, ok := h.podaciLetve(w, r)
	if !ok {
		return
	}
	// Arhiva se puni samo ovdje: kartica je operativa i ne treba je, a izvješće
	// bi je platilo bez potrebe.
	if h.arhiva != nil {
		var isp *repository.IspravakRepository
		if h.ispravci != nil {
			isp = h.ispravci()
		}
		popuniArhivu(r.Context(), r, h.arhiva(), isp, &data.ArhivaPogled, &data.Station)
	}
	data.LetvaStranica = "historijat"
	data.HistorijatPrazan = historijatPrazan(data)
	if err := h.tmplHistorijat.ExecuteTemplate(w, "station_history.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// historijatPrazan javlja da letva još nema ništa od onoga što historijat
// pokazuje. Ekstremi se ne broje — oni su na kartici, a praznina se mjeri onim
// što je na ovoj stranici.
func historijatPrazan(d StationPageData) bool {
	return !d.Station.ImaPovratne() && len(d.Station.ZeroDatumHistory) == 0 &&
		len(d.Spojevi) == 0 && len(d.ValoviPragovi) == 0 && len(d.Episodes) == 0
}

// ObrazacHistorijata uređuje ono što historijat prikazuje. Odvojen od obrasca
// kartice iz istog razloga iz kojeg su i stranice odvojene — i zato što obrazac
// prenosi samo svoja polja, pa spremanje jednoga ne dira ono što uređuje drugi.
func (h *StationsHandler) ObrazacHistorijata(w http.ResponseWriter, r *http.Request) {
	data, ok := h.podaciLetve(w, r)
	if !ok {
		return
	}
	if !h.canEditStation(data.Permissions, data.Station) {
		http.Error(w, "Nemate pravo uređivati ovu postaju", http.StatusForbidden)
		return
	}
	data.IsEdit = true
	data.ReturnLevelsJSON = jsonZaObrazac(data.Station.ReturnLevels)
	data.ZeroDatumHistoryJSON = jsonZaObrazac(data.Station.ZeroDatumHistory)
	data.OgradeNizaJSON = jsonZaObrazac(data.Station.OgradeNiza)
	if err := h.tmplHistObrazac.ExecuteTemplate(w, "station_history_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// jsonZaObrazac pretvara popis u JS literal za obrazac; prazan popis je "[]",
// da skripta u obrascu ne mora nagađati.
func jsonZaObrazac(v any) template.JS {
	b, err := json.Marshal(v)
	if err != nil || string(b) == "null" {
		return template.JS("[]")
	}
	return template.JS(b)
}

// podaciLetve prikuplja sve o jednoj letvi: registar, arhivu, korito, krivulje
// i valove obrane. Isti se podaci prikazuju na kartici i sastavljaju u
// izvješće — kad bi svaki skupljao svoje, dokument i stranica razišli bi se
// prvom idućom izmjenom, a nitko ne bi znao koji od njih laže.
//
// Vraća ok=false kad je odgovor već poslan (nema postaje, nema prava).
func (h *StationsHandler) podaciLetve(w http.ResponseWriter, r *http.Request) (StationPageData, bool) {
	ctx := r.Context()
	data := h.pageData(r)

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return data, false
	}
	st, err := h.stationService.GetStation(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return data, false
	}
	if st == nil {
		http.NotFound(w, r)
		return data, false
	}
	data.Station = *st
	data.CanEdit = h.canEditStation(data.Permissions, *st)
	if h.readingService != nil {
		data.CanRecord = h.readingService.CanRecordStation(data.Permissions, st)
	}
	data.BrojOcitanja = h.stationService.BrojOcitanja(ctx, st.ID)
	if h.karta != nil {
		data.Karta = h.karta()
	}

	if h.sectionService != nil {
		for _, code := range st.SectionCodes {
			if sec, err := h.sectionService.GetSectionWithDetails(code); err == nil && sec != nil {
				data.Sections = append(data.Sections, *sec)
			}
		}
	}
	// Ista letva mjerodavna je za više dionica — Batina za cijelo BP 34 — pa
	// se ovdje vide obrane svih njih, svaka sa svojim stupnjem.
	if h.episodeService != nil {
		data.Episodes, _ = h.episodeService.ByStation(ctx, st.ID.String(), 50)
	}
	// Hidrološka arhiva: nizovi, karakteristične vrijednosti, korito i krivulje.
	// Sve se računa pri čitanju, ništa se ne pamti — brojevi se tako ne mogu
	// razići s podacima iz kojih su nastali.
	if a := h.arh(); a != nil && st.Code != "" {
		data.Nizovi, _ = a.Nizovi(ctx, st.Code)
		data.Profili, _ = a.Profili(ctx, st.Code)
		data.Krivulje, _ = a.Krivulje(ctx, st.Code)
		if len(data.Profili) > 0 {
			// Presjek se sastavlja od svih snimaka: novija ima prednost, a
			// starija popunjava ono što novija ne pokriva. Snimke se prije
			// toga svode na zajedničku stacionažu — Batinina iz 2020. počinje
			// 104,5 m desno od one iz 2010.
			spoj := models.SpojiProfile(data.Profili)
			data.Profil = &spoj
		}
		data.Sazetak, _ = a.Sazetak(ctx, st.Code)
		data.Spojevi, _ = a.SpojDosezi(ctx, st.Code)
		data.Sada, _ = a.SpojZadnje(ctx, st.Code, "vodostaj", "satni")
		if data.Sada == nil {
			data.Sada, _ = a.SpojZadnje(ctx, st.Code, "vodostaj", "dnevni")
		}
		data.NizID = odabraniNiz(r, data.Nizovi)
		if data.NizID > 0 {
			data.Pregled, _ = a.Pregled(ctx, data.NizID)
		}
		if data.Sada != nil {
			data.Zadnji = &models.HidroTocka{Kad: data.Sada.Kad, Vrijednost: data.Sada.Vrijednost}
			data.ZadnjiIzvor = data.Sada.Izvor
		}
		if data.Zadnji != nil {
			cm := int(data.Zadnji.Vrijednost)
			if data.Profil != nil {
				data.Crtez = crtajKoritoP(*data.Profil, cm, sirokoKoritoM.uSustavu(*st))
				data.CrtezUzak = crtajKoritoP(*data.Profil, cm, uskoKoritoM.uSustavu(*st))
			}
			dan := data.Zadnji.Kad.Format("2006-01-02")
			for _, k := range data.Krivulje {
				if k.VrijediOd <= dan && (k.VrijediDo == "" || dan <= k.VrijediDo) {
					if q, ok := k.Protok(cm); ok {
						data.ZadnjiProtok = q
					}
					break
				}
			}
		}
	}

	if data.CanEdit && h.watercourseService != nil {
		if waters, err := h.watercourseService.ListWatercourses(ctx, "", "", false); err == nil {
			data.WaterRegistry = waters
		}
	}

	// Valovi obrane iz cijelog niza: kada bi po vodostaju počelo i završilo
	// koje stanje i koliko je trajalo. Računa se iz mjerenja, ne iz proglašenih
	// obrana — odgovara na pitanje što bi po vodostaju bilo, ne što je odlučeno.
	data.ValoviPragovi = data.Station.PragoviObrane()
	if a := h.arh(); a != nil && len(data.ValoviPragovi) > 0 {
		svi, niz := h.valovi.Valovi(ctx, a, st.Code, data.ValoviPragovi)
		data.ValoviNiz = niz
		data.ValoviSvi = svi
		data.ValoviZbroj = models.ZbrojValova(svi, data.ValoviPragovi)
		// vlastiti parametar, da listanje valova ne pomiče ostale popise
		data.ValoviPager = pagerZa(r, "val", len(svi), valovaPoStranici)
		if od := data.ValoviPager.Odmak(); od < len(svi) {
			do := od + valovaPoStranici
			if do > len(svi) {
				do = len(svi)
			}
			data.Valovi = svi[od:do]
		}
	}

	// Stupnjevi obrane iskazani u protoku i u apsolutnoj koti vodne plohe.
	// Računa se tek ovdje, kad su krivulje već dohvaćene iz arhive — a prije
	// iscrtavanja, jer predložak dobiva presliku podataka i ono što se upiše
	// poslije njega nikamo ne stiže. Razliku visinskih sustava predložak zato
	// i traži od same postaje, da o ovom redoslijedu uopće ne ovisi.
	data.PragoviQ = pragoviUProtoku(data.Station, data.Krivulje)
	data.PragoviKote = sProtokom(pragoviUKotama(data.Station), data.PragoviQ)
	data.ImaProtok = imaProtok(data.PragoviKote)
	return data, true
}

// ShowStationForm prikazuje obrazac za novu postaju ili izmjenu postojeće
func (h *StationsHandler) ShowStationForm(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)

	if raw := r.PathValue("id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		st, err := h.stationService.GetStation(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if st == nil {
			http.NotFound(w, r)
			return
		}
		if !h.canEditStation(data.Permissions, *st) {
			http.Error(w, "Nemate pravo uređivati ovu postaju", http.StatusForbidden)
			return
		}
		data.Station = *st
		data.IsEdit = true
	} else if !data.Permissions.IsGlobalAdmin && len(data.Permissions.AllowedSections) == 0 {
		http.Error(w, "Nemate pravo dodavati postaje", http.StatusForbidden)
		return
	}

	// Registar vodotoka za pridruživanje. Popis je potreban tek pri uređivanju
	// postojeće postaje: nova još nema identifikator na koji bi se veza vezala.
	if data.IsEdit && h.watercourseService != nil {
		if waters, err := h.watercourseService.ListWatercourses(r.Context(), "", "", false); err == nil {
			data.WaterRegistry = waters
		}
	}

	data.ExtremesJSON = template.JS("[]")
	if b, err := json.Marshal(data.Station.Extremes); err == nil && len(data.Station.Extremes) > 0 {
		data.ExtremesJSON = template.JS(b)
	}
	data.ReturnLevelsJSON = template.JS("[]")
	if b, err := json.Marshal(data.Station.ReturnLevels); err == nil && len(data.Station.ReturnLevels) > 0 {
		data.ReturnLevelsJSON = template.JS(b)
	}
	data.ZeroDatumHistoryJSON = template.JS("[]")
	if b, err := json.Marshal(data.Station.ZeroDatumHistory); err == nil && len(data.Station.ZeroDatumHistory) > 0 {
		data.ZeroDatumHistoryJSON = template.JS(b)
	}
	if err := h.tmplForm.ExecuteTemplate(w, "station_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// odabraniNiz bira niz čije se karakteristične vrijednosti prikazuju: onaj iz
// upita, inače najpouzdaniji vodostaj koji letva ima.
func odabraniNiz(r *http.Request, nizovi []models.HidroNiz) int64 {
	if s := r.URL.Query().Get("niz"); s != "" {
		if id, err := strconv.ParseInt(s, 10, 64); err == nil {
			for _, n := range nizovi {
				if n.ID == id {
					return id
				}
			}
		}
	}
	for _, n := range nizovi {
		if n.Velicina == "vodostaj" {
			return n.ID
		}
	}
	if len(nizovi) > 0 {
		return nizovi[0].ID
	}
	return 0
}

// PragProtok je jedan stupanj obrane iskazan i u vodostaju i u protoku.
type PragProtok struct {
	Naziv string
	Cm    int
	Q     float64
}

// pragoviUProtoku prevodi pragove obrane u protok krivuljom koja danas vrijedi.
// Bez krivulje se ne vraća ništa: pogađati prag u protoku bilo bi izmišljanje.
func pragoviUProtoku(st models.Station, krivulje []models.HQKrivulja) []PragProtok {
	k := krivuljaZa(krivulje, time.Now())
	if k == nil {
		return nil
	}
	var out []PragProtok
	for _, t := range []struct {
		t models.Threshold
		n string
	}{
		{st.Prep, "Pripremno stanje"},
		{st.Regular, "Redovna obrana"},
		{st.Emergency, "Izvanredna obrana"},
		{st.State, "Izvanredno stanje"},
	} {
		if !t.t.IsUsable() {
			continue
		}
		q, ok := k.Protok(*t.t.Cm)
		if !ok {
			continue
		}
		out = append(out, PragProtok{Naziv: t.n, Cm: *t.t.Cm, Q: q})
	}
	return out
}

// PragKota je jedan stupanj obrane iskazan kao apsolutna kota vodne plohe, i
// — kad letva ima krivulju — u protoku. Sve tri mjere istog praga stoje u
// jednom retku: ista brojka ponovljena u dvije tablice traži od čitatelja da
// ih sam spaja po nazivu stupnja.
type PragKota struct {
	Faza  models.DefensePhase // za boju pilule
	Naziv string
	Cm    int
	Kote  []models.KotaVode
	Q     *float64 // protok po danas važećoj krivulji; nil kad ga nema
}

// ImaProtok javlja treba li tablici stupac protoka.
func imaProtok(p []PragKota) bool {
	for _, x := range p {
		if x.Q != nil {
			return true
		}
	}
	return false
}

// sProtokom veže protok uz prag u koti. Veže se po centimetrima, ne po
// nazivu: naziv je tekst za prikaz i mijenja se, a prag je brojka.
func sProtokom(kote []PragKota, q []PragProtok) []PragKota {
	po := make(map[int]float64, len(q))
	for _, x := range q {
		po[x.Cm] = x.Q
	}
	for i := range kote {
		if v, ok := po[kote[i].Cm]; ok {
			kote[i].Q = &v
		}
	}
	return kote
}

// pragoviUKotama prevodi pragove obrane u apsolutnu visinu vodne plohe, u
// svakom visinskom sustavu koji letva ima. Po tome se na terenu mjeri koliko
// obranu treba nadvisiti.
// Letva bez kote nule i dalje daje redak po pragu, samo bez kota: prag postoji
// i prikazuje se, a apsolutna visina se bez kote nule ne može izračunati.
func pragoviUKotama(st models.Station) []PragKota {
	var out []PragKota
	for _, t := range []struct {
		t models.Threshold
		f models.DefensePhase
	}{
		{st.Prep, models.PhasePrep},
		{st.Regular, models.PhaseRegular},
		{st.Emergency, models.PhaseEmergency},
		{st.State, models.PhaseState},
	} {
		if !t.t.IsUsable() {
			continue
		}
		p := PragKota{Faza: t.f, Naziv: t.f.Label(), Cm: *t.t.Cm}
		if st.ImaKotuNule() {
			p.Kote = st.Kote(*t.t.Cm)
		}
		out = append(out, p)
	}
	return out
}
