package web

import (
	"context"
	"html/template"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"gocop/internal/dhmz"
	"gocop/internal/models"
)

// Prognoze › Vrijeme (goCOP-m3, 13 · Prognoze): šest regija DHMZ-a, mjesto
// i ispod njega upozorenja DHMZ-a, „Sada”, kišomjer, kiša po slivovima i
// prognoza regije. Stranica odmah pokaže izbor i mjesto, a vrijeme stiže
// naknadno (/prognoze/vrijeme/dio), da spor DHMZ ne zadrži stranicu.
//
// Mjesto je, redom: glavno mjesto regije kad je regija izabrana
// (?regija=istocna), ured branjenog područja iz registra (?podrucje=15),
// lokacija preglednika (?lat&lon), područje iz dužnosti osobe, Osijek.
// Nepoznata regija ili područje daju zadano, bez greške.

// RegijaVremena je regija DHMZ-ove prognoze s glavnim mjestom i
// međuslivovima koji leže u njoj.
type RegijaVremena struct {
	Kljuc   string // ključ u regije_danas.xml
	Naziv   string // na čipu: „Istočna Hrvatska”
	ZaNaziv string // „Prognoza za istočnu Hrvatsku”
	Mjesto  string // glavno mjesto: „Sada” i kišomjer prelaze na njega
	Lat     float64
	Lon     float64
	Slivovi []string // međuslivovi kiše po slivovima u regiji
}

// regijeVremena su regije redom kao u dizajnu. Glavna mjesta su ondje gdje
// ih DHMZ ima kao postaju; točka je ured branjenog područja iz registra gdje
// ga ima (Osijek, Zagreb, Gospić, Rijeka, Split), a za Pazin sam grad.
// Međuslivovi kiše postoje samo za Dravu, Muru i Dunav: istočnoj pripadaju
// oni od Terezina Polja nizvodno i Dunav od Mohácsa, središnjoj Drava od
// Borla i Letenyea do Terezina Polja. Ostale regije ih nemaju.
var regijeVremena = []RegijaVremena{
	{"istocna", "Istočna Hrvatska", "istočnu Hrvatsku", "Osijek", 45.5549, 18.6954, []string{"E", "F", "G", "J"}},
	{"sredisnja", "Središnja Hrvatska", "središnju Hrvatsku", "Zagreb", 45.8131, 15.9773, []string{"C", "D"}},
	{"gorska", "Gorska Hrvatska", "gorsku Hrvatsku", "Gospić", 44.5464, 15.3754, nil},
	{"sjjadran", "Sjeverni Jadran", "sjeverni Jadran", "Rijeka", 45.3268, 14.4422, nil},
	{"istra", "Istra", "Istru", "Pazin", 45.2400, 13.9370, nil},
	{"dalmacija", "Dalmacija", "Dalmaciju", "Split", 43.5116, 16.4400, nil},
}

// regijaVremena vraća regiju po ključu
func regijaVremena(kljuc string) (RegijaVremena, bool) {
	for _, r := range regijeVremena {
		if r.Kljuc == kljuc {
			return r, true
		}
	}
	return RegijaVremena{}, false
}

// MjestoVremena je točka za koju stranica pokazuje vrijeme.
type MjestoVremena struct {
	Naziv    string
	Lat, Lon float64
	Lokacija bool // iz preglednika
	Podrucje int  // izabrani ured branjenog područja; 0 kad nije izabran
	Zupanija dhmz.Zupanija
	Regija   RegijaVremena
	upit     url.Values // regija, podrucje ili lat i lon, za adresu dijela
}

// mjestoU je točka sa županijom najbližeg središta i njezinom regijom
func mjestoU(naziv string, lat, lon float64) MjestoVremena {
	z := dhmz.ZupanijaZa(lat, lon)
	rg, _ := regijaVremena(z.Regija)
	return MjestoVremena{Naziv: naziv, Lat: lat, Lon: lon, Zupanija: z, Regija: rg, upit: url.Values{}}
}

// mjestoVremena čita mjesto iz adrese; bez izbora je to mjesto osobe
func (h *VrijemeHandler) mjestoVremena(r *http.Request, podrucja []models.Area) MjestoVremena {
	q := r.URL.Query()
	if rg, ok := regijaVremena(q.Get("regija")); ok {
		m := mjestoU(rg.Mjesto, rg.Lat, rg.Lon)
		m.upit.Set("regija", rg.Kljuc)
		return m
	}
	if a := podrucjeIzUpita(q.Get("podrucje"), podrucja); a != nil {
		m := mjestoU(a.Name, a.Latitude, a.Longitude)
		m.Podrucje = a.ID
		m.upit.Set("podrucje", strconv.Itoa(a.ID))
		return m
	}
	naziv, lat, lon, lok := h.tocka(r)
	m := mjestoU(naziv, lat, lon)
	if m.Lokacija = lok; lok {
		m.upit.Set("lat", q.Get("lat"))
		m.upit.Set("lon", q.Get("lon"))
	}
	return m
}

// podrucjeIzUpita je područje s koordinatama čiji je broj u adresi
func podrucjeIzUpita(id string, podrucja []models.Area) *models.Area {
	n, err := strconv.Atoi(id)
	if err != nil {
		return nil
	}
	for i := range podrucja {
		if podrucja[i].ID == n && podrucja[i].ImaKoordinate() {
			return &podrucja[i]
		}
	}
	return nil
}

// NaslovMjesta je mjesto s velikim početnim slovom („Vaša lokacija”)
func (m MjestoVremena) NaslovMjesta() string { return velikoSlovo(m.Naziv) }

// NaslovZupanije: „Osječko-baranjska županija”; Grad Zagreb bez riječi
// „županija”
func (m MjestoVremena) NaslovZupanije() string {
	if strings.HasPrefix(m.Zupanija.Naziv, "Grad ") {
		return m.Zupanija.Naziv
	}
	return m.Zupanija.Naziv + " županija"
}

// VrijemePageData je stranica Prognoze › Vrijeme.
type VrijemePageData struct {
	PrognozePageData
	Regije   []CipPrognoze
	Podrucja []CipPrognoze // izbor mjesta: uredi branjenih područja
	Mjesto   MjestoVremena
	DioURL   string
}

// VrijemePrognozaHandler slaže stranicu Prognoze › Vrijeme i njezin dio.
type VrijemePrognozaHandler struct {
	tmpl     *template.Template
	glava    func(*http.Request) PrognozePageData
	vrijeme  func() *VrijemeHandler
	kisa     func(context.Context) kisaSlivovaPodaci
	podrucja func(context.Context) ([]models.Area, error)
	sada     func() time.Time
}

// vh je rukovatelj vremena; prazan dok ga poslužitelj nije složio
func (h *VrijemePrognozaHandler) vh() *VrijemeHandler {
	if h.vrijeme != nil {
		if v := h.vrijeme(); v != nil {
			return v
		}
	}
	return &VrijemeHandler{}
}

// svaPodrucja su branjena područja iz registra; bez registra nijedno
func (h *VrijemePrognozaHandler) svaPodrucja(ctx context.Context) []models.Area {
	if h.podrucja == nil {
		return nil
	}
	a, _ := h.podrucja(ctx) // bez registra mjesto se bira regijom ili lokacijom
	return a
}

// podrucjaVremena su sva branjena područja, za izbor mjesta na Prognozama ›
// Vrijeme; bez registra nijedno
func (s *Server) podrucjaVremena(ctx context.Context) ([]models.Area, error) {
	if s.orgService == nil {
		return nil, nil
	}
	return s.orgService.ListAreas(ctx, "")
}

// ShowVrijeme je stranica: kartice, regije, mjesto i okvir za dio
func (h *VrijemePrognozaHandler) ShowVrijeme(w http.ResponseWriter, r *http.Request) {
	data := VrijemePageData{}
	if h.glava != nil {
		data.PrognozePageData = h.glava(r)
	}
	pod := h.svaPodrucja(r.Context())
	m := h.vh().mjestoVremena(r, pod)
	data.Mjesto, data.Regije, data.Podrucja = m, cipoviRegija(m.Regija.Kljuc), cipoviPodrucja(pod, m.Podrucje)
	data.DioURL = "/prognoze/vrijeme/dio"
	if len(m.upit) > 0 {
		data.DioURL += "?" + m.upit.Encode()
	}
	if err := h.tmpl.ExecuteTemplate(w, "prognoze_vrijeme.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// cipoviRegija su čipovi šest regija; odabrana je regija mjesta
func cipoviRegija(odabrana string) []CipPrognoze {
	out := make([]CipPrognoze, 0, len(regijeVremena))
	for _, r := range regijeVremena {
		out = append(out, CipPrognoze{Oznaka: r.Kljuc, Naziv: r.Naziv, URL: "/prognoze/vrijeme?regija=" + r.Kljuc, Odabran: r.Kljuc == odabrana})
	}
	return out
}

// cipoviPodrucja su uredi branjenih područja s koordinatama, za izbor mjesta
func cipoviPodrucja(podrucja []models.Area, odabrano int) []CipPrognoze {
	var out []CipPrognoze
	for _, a := range podrucja {
		if a.ImaKoordinate() {
			out = append(out, CipPrognoze{Oznaka: strconv.Itoa(a.ID), Naziv: a.Name, Odabran: a.ID == odabrano})
		}
	}
	return out
}

// ShowDio vraća vrijeme za mjesto kao dio stranice. Kiša po slivovima i
// dani prognoze računaju se uz DHMZ, svaki sa svojim rokom, da spor DHMZ
// ne odnese dane iz Open-Meteo koji o njemu ne ovise.
func (h *VrijemePrognozaHandler) ShowDio(w http.ResponseWriter, r *http.Request) {
	ctx, otkazi := context.WithTimeout(r.Context(), 12*time.Second)
	defer otkazi()
	vh := h.vh()
	m := vh.mjestoVremena(r, h.svaPodrucja(ctx))
	sada := h.sat()
	kisa := h.kisaUsporedno(ctx)
	dani := make(chan daniVremena, 1)
	go func() {
		dctx, gotovo := context.WithTimeout(r.Context(), 8*time.Second)
		defer gotovo()
		dani <- vh.daniPrognoze(dctx, m.Lat, m.Lon, sada)
	}()
	p := vh.vrijemeMjesta(ctx, m, sada)
	svc, sp := vh.izvoriKise()
	dio := slaziDio(m, p, <-kisa, sada)
	dio.Kisomjer, dio.KisomjerPrazno = kisomjerDijela(p, svc != nil && sp != nil, sada)
	d := <-dani
	dio.Dani, dio.DaniIzvor, dio.DaniPrazno = d.Dani, d.Izvor, d.Prazno
	posaljiDio(w, h.tmpl.Lookup("vrijemeDio"), dio)
}

// sat je sada; testovi ga mogu postaviti
func (h *VrijemePrognozaHandler) sat() time.Time {
	if h.sada != nil {
		return h.sada()
	}
	return time.Now()
}

// kisaUsporedno računa kišu po slivovima uz ostalo, jer prvi izračun
// pragova zna potrajati; bez izvora stiže nil
func (h *VrijemePrognozaHandler) kisaUsporedno(ctx context.Context) <-chan *kisaSlivovaPodaci {
	kisa := make(chan *kisaSlivovaPodaci, 1)
	if h.kisa == nil {
		kisa <- nil
		return kisa
	}
	go func() {
		k := h.kisa(ctx)
		kisa <- &k
	}()
	return kisa
}

// vrijemeMjesta skuplja ono što ploča vremena ima, bez biltena: upozorenja,
// trenutno vrijeme, prognozu regije i kišomjer. Bez DHMZ-a ništa od DHMZ-a.
func (h *VrijemeHandler) vrijemeMjesta(ctx context.Context, m MjestoVremena, sada time.Time) PlocaVremena {
	p := PlocaVremena{Mjesto: m.Naziv, Lat: m.Lat, Lon: m.Lon, Lokacija: m.Lokacija, Zupanija: m.Zupanija}
	if h.dhmz != nil {
		h.upozorenja(ctx, &p)
		h.sada(ctx, &p)
		h.prognozaRegije(ctx, &p)
	} else {
		p.BezUpozorenja = true
	}
	h.kisa(&p, sada)
	return p
}

// VrijemeDio je sve što dio stranice pokazuje, već napisano riječima.
type VrijemeDio struct {
	Regija           RegijaVremena
	Upozorenja       []UpozorenjeVremena
	UpozorenjaPrazno *PraznaKartica
	Procitano        string // „pročitano u 07:30”
	Ostalih          int    // upozorenja za druge županije

	Sada       *SadaVremena
	SadaPrazno *PraznaKartica

	Kisomjer       *KisomjerVremena
	KisomjerPrazno *PraznaKartica

	Slivovi       []RedakSliva
	SlivoviPrazno *PraznaKartica

	Prognoza       string
	PrognozaDatum  string // „DHMZ, 5. 11.”
	PrognozaPrazno *PraznaKartica
	Dani           []DanVremena
	DaniIzvor      string // odakle su dani; prazno kad dana nema
	DaniPrazno     *PraznaKartica
}

// UpozorenjeVremena je upozorenje DHMZ-a: crta i natpis, bez plohe u boji
type UpozorenjeVremena struct {
	Razina int    // 2 žuto, 3 narančasto, 4 crveno (razred crte)
	Naslov string // „DHMZ · Žuto upozorenje za kišu” ili „DHMZ · žuto upozorenje: kiša”
	Opis   string // „Danas od 14 do 20 h · …”
}

// SadaVremena je trenutno vrijeme na najbližoj postaji DHMZ-a; Sat je
// termin opažanja („08:00”), za kratki redak na mobitelu
type SadaVremena struct {
	Postaja, Km, Temp, Opis, Detalji, Ikona, Sat string
}

// KisomjerVremena je najbliži kišomjer koji se javlja
type KisomjerVremena struct {
	Mjesto   string // „Osijek, 2 km”; ispred ide „Kiša · kišomjer” (na mobitelu „Kišomjer”)
	Redovi   []RedakKise
	Javljeno string // „javljeno u 08:10 · pljusak”
}

// RedakKise je jedan redak kišomjera: natpis, vrijednost i napomena
type RedakKise struct{ Natpis, Vrijednost, Napomena string }

// RedakSliva je međusliv u kiši po slivovima; Ukratko je palo i
// očekivano u jednom („18 · 32 mm”), za mobitel
type RedakSliva struct {
	Naziv, Palo24, Dolazi48, Ukratko, Rijetko, Raste string
	Klasa                                            string // kisa-zuto…; prazno kad je uobičajeno
}

// DanVremena je dan prognoze po satnim vrijednostima Open-Meteo
type DanVremena struct {
	Naslov, Ikona, Temp, Kisa string
}

// slaziDio slaže upozorenja, Sada, kišu po slivovima i prognozu regije
func slaziDio(m MjestoVremena, p PlocaVremena, kisa *kisaSlivovaPodaci, sada time.Time) VrijemeDio {
	d := VrijemeDio{Regija: m.Regija, Ostalih: brojUpozorenja(p.Ostala)}
	d.Upozorenja, d.UpozorenjaPrazno = upozorenjaDijela(p, m, sada)
	if !p.UpozorenjaProcitano.IsZero() {
		d.Procitano = "pročitano u " + satIliDan(p.UpozorenjaProcitano, sada)
	}
	d.Sada, d.SadaPrazno = sadaDijela(p, sada)
	d.Slivovi, d.SlivoviPrazno = slivoviDijela(m.Regija, kisa)
	d.Prognoza = p.Prognoza
	if p.Prognoza == "" {
		d.PrognozaPrazno = &PraznaKartica{Vrsta: PraznoNedostupno, Tekst: "Prognoza DHMZ-a trenutačno nije dostupna."}
	}
	d.PrognozaDatum = "DHMZ"
	if dat := datumDHMZ(p.PrognozaDatum); dat != "" {
		d.PrognozaDatum += ", " + dat
	}
	return d
}

// brojUpozorenja broji upozorenja bez ponavljanja: DHMZ isto upozorenje
// šalje za svako područje posebno
func brojUpozorenja(u []dhmz.Upozorenje) int {
	type kljuc struct {
		dogadjaj string
		razina   int
		od, do   int64
	}
	vidjeno := map[kljuc]bool{}
	for _, x := range u {
		vidjeno[kljuc{x.Dogadjaj, x.Razina, x.Od.Unix(), x.Do.Unix()}] = true
	}
	return len(vidjeno)
}

// upozorenjaDijela: upozorenja za županiju mjesta; bez njih potvrda da ih
// nema (s razdobljem bez upozorenja), a kad DHMZ ne odgovori, crta
// upozorenja s porukom
func upozorenjaDijela(p PlocaVremena, m MjestoVremena, sada time.Time) ([]UpozorenjeVremena, *PraznaKartica) {
	if p.BezUpozorenja {
		return nil, &PraznaKartica{Vrsta: PraznoNedostupno, Tekst: "Upozorenja DHMZ-a trenutačno nisu dostupna."}
	}
	var out []UpozorenjeVremena
	for _, u := range p.Upozorenja {
		opis := []string{}
		if r := razdobljeUpozorenja(u.Od, u.Do, sada); r != "" {
			opis = append(opis, r)
		}
		if u.Opis != "" {
			opis = append(opis, u.Opis)
		}
		out = append(out, UpozorenjeVremena{Razina: u.Razina, Naslov: "DHMZ · " + nazivUpozorenja(u), Opis: strings.Join(opis, " · ")})
	}
	if len(out) > 0 {
		return out, nil
	}
	tekst := m.NaslovZupanije() + ": DHMZ nema upozorenja"
	if !p.ZelenoDo.IsZero() {
		tekst += " do " + danISat(p.ZelenoDo, sada)
	}
	return nil, &PraznaKartica{Vrsta: PraznoGotovo, Tekst: tekst + "."}
}

// razdobljeUpozorenja: „Danas od 14 do 20 h”, „Danas od 14 h do sutra 8 h”
func razdobljeUpozorenja(od, do, sada time.Time) string {
	if do.IsZero() {
		return ""
	}
	if od.IsZero() {
		return velikoSlovo("do " + danISat(do, sada))
	}
	d1, d2 := imeDana(od, sada), imeDana(do, sada)
	if d1 == d2 {
		return velikoSlovo(d1 + " od " + sat(od) + " do " + sat(do) + " h")
	}
	return velikoSlovo(d1 + " od " + sat(od) + " h do " + d2 + " " + sat(do) + " h")
}

// imeDana: „danas”, „sutra” ili „6. 11.”, po našem vremenu (dan pomaka
// sata ima 23 ili 25 sati, pa se dani zaokružuju)
func imeDana(t, sada time.Time) string {
	switch int(math.Round(ponocDana(t).Sub(ponocDana(sada)).Hours() / 24)) {
	case 0:
		return "danas"
	case 1:
		return "sutra"
	}
	l := t.In(models.Zagreb)
	return strconv.Itoa(l.Day()) + ". " + strconv.Itoa(int(l.Month())) + "."
}

// sat: „14” za puni sat, inače „14:30”
func sat(t time.Time) string {
	l := t.In(models.Zagreb)
	if l.Minute() == 0 {
		return strconv.Itoa(l.Hour())
	}
	return l.Format("15:04")
}

// danISat: „sutra u 8 h”, „6. 11. u 23 h”
func danISat(t, sada time.Time) string { return imeDana(t, sada) + " u " + sat(t) + " h" }

// satIliDan: „07:30” danas, inače „4. 11. u 07:30”
func satIliDan(t, sada time.Time) string {
	if d := imeDana(t, sada); d != "danas" {
		return d + " u " + t.In(models.Zagreb).Format("15:04")
	}
	return t.In(models.Zagreb).Format("15:04")
}

// datumDHMZ: „05.11.2026.” → „5. 11.”; nepoznat oblik ostaje kakav je
func datumDHMZ(s string) string {
	t, err := time.Parse("02.01.2006.", strings.TrimSpace(s))
	if err != nil {
		return strings.TrimSpace(s)
	}
	return strconv.Itoa(t.Day()) + ". " + strconv.Itoa(int(t.Month())) + "."
}

// sadaDijela: postaja, temperatura, opis s vjetrom u km/h i riječima
func sadaDijela(p PlocaVremena, sada time.Time) (*SadaVremena, *PraznaKartica) {
	pos := p.Postaja
	if pos == nil {
		return nil, &PraznaKartica{Vrsta: PraznoNedostupno, Tekst: "Podaci postaje DHMZ-a trenutačno nisu dostupni."}
	}
	s := &SadaVremena{Postaja: strings.TrimPrefix(pos.Ime, "RC "), Km: brojHRf(p.PostajaKm, 0), Temp: brojHRd(pos.Temp, 1) + " °C"}
	// DHMZ za postaju bez opažanja piše „-”: tada nema ni opisa ni ikone
	if opis := strings.Trim(pos.Opis, "- "); opis != "" {
		s.Opis, s.Ikona = opis, ikonaVremena(opis)
	}
	s.Opis = spoji(", ", s.Opis, vjetarRijecima(pos.VjetarSmjer, pos.VjetarBrzina))
	var det []string
	if pos.Vlaga != nil {
		det = append(det, "vlaga "+brojHRf(*pos.Vlaga, 0)+" %")
	}
	if pos.Tlak != nil {
		det = append(det, "tlak "+brojHRf(*pos.Tlak, 0)+" hPa")
	}
	if !p.Termin.IsZero() {
		s.Sat = satIliDan(p.Termin, sada)
		det = append(det, "u "+s.Sat)
	}
	s.Detalji = strings.Join(det, " · ")
	return s, nil
}

// spoji spaja neprazne dijelove
func spoji(razmak string, dijelovi ...string) string {
	var out []string
	for _, d := range dijelovi {
		if d != "" {
			out = append(out, d)
		}
	}
	return strings.Join(out, razmak)
}

// smjeroviVjetra su DHMZ-ove kratice smjera riječima
var smjeroviVjetra = map[string]string{"N": "sjeverni", "NE": "sjeveroistočni", "E": "istočni", "SE": "jugoistočni",
	"S": "južni", "SW": "jugozapadni", "W": "zapadni", "NW": "sjeverozapadni"}

// vjetarRijecima: „sjeverni vjetar 20 km/h”; DHMZ daje m/s, a „C” je tišina
func vjetarRijecima(smjer string, brzina *float64) string {
	if smjer == "C" {
		return "bez vjetra"
	}
	rijec := strings.TrimSpace(smjeroviVjetra[smjer] + " vjetar")
	if brzina == nil {
		if smjeroviVjetra[smjer] == "" {
			return ""
		}
		return rijec
	}
	return rijec + " " + brojHRf(*brzina*3.6, 0) + " km/h"
}

// kisomjerDijela: zadnji sat, danas i jučer na najbližem kišomjeru. Bez
// njega se razlikuje mjesto bez kišomjera do 40 km od kišomjera koji se ne
// javljaju.
func kisomjerDijela(p PlocaVremena, imaIzvora bool, sada time.Time) (*KisomjerVremena, *PraznaKartica) {
	iz := p.Izmjereno
	switch {
	case iz != nil:
	case !imaIzvora:
		return nil, &PraznaKartica{Vrsta: PraznoNedostupno, Tekst: "Podaci kišomjera trenutačno nisu dostupni."}
	case p.KisomjerBlizu:
		return nil, &PraznaKartica{Vrsta: PraznoNedostupno, Tekst: "Kišomjeri do 40 km od mjesta trenutačno se ne javljaju."}
	default:
		return nil, &PraznaKartica{Vrsta: PraznoCeka, Tekst: "Do 40 km od mjesta nemamo kišomjer."}
	}
	k := &KisomjerVremena{Mjesto: iz.Naziv + ", " + brojHRf(iz.Km, 0) + " km"}
	k.Redovi = append(k.Redovi, RedakKise{Natpis: "zadnji sat", Vrijednost: mmKise(iz.ZadnjiSat)})
	if iz.OdPonoci {
		k.Redovi = append(k.Redovi, RedakKise{Natpis: "danas od ponoći", Vrijednost: mmKise(iz.Danas)},
			RedakKise{Natpis: "jučer", Vrijednost: mmKise(iz.Jucer)})
	} else {
		k.Redovi = append(k.Redovi, RedakKise{Natpis: "danas od 7 h", Vrijednost: mmKise(iz.DanasOd7), Napomena: bezSati(iz.BezSatiDanas)},
			RedakKise{Natpis: "jučer, od 7 do 7 h", Vrijednost: mmKise(iz.Jucer), Napomena: bezSati(iz.BezSatiJucer)})
	}
	javljeno := []string{}
	if !iz.Javljeno.IsZero() {
		javljeno = append(javljeno, "javljeno u "+satIliDan(iz.Javljeno, sada))
	}
	k.Javljeno = spoji(" · ", strings.Join(javljeno, ""), izvorKisomjera(iz.Izvor))
	return k, nil
}

// naziviIzvora su izvori kišomjera kako ih korisnik zna
var naziviIzvora = map[string]string{"dhmz": "DHMZ", "pljusak": "pljusak", "danubehis": "DanubeHIS"}

// izvorKisomjera je izvor kakav korisnik zna; nepoznat ostaje kakav jest
func izvorKisomjera(izvor string) string {
	if n, ima := naziviIzvora[izvor]; ima {
		return n
	}
	return izvor
}

// mmKise: „0,4 mm”; bez vrijednosti crtica
func mmKise(v *float64) string {
	if v == nil {
		return "—"
	}
	return brojHRf(*v, 1) + " mm"
}

// bezSati: „bez 2 h” kad satnih vrijednosti nedostaje
func bezSati(n int) string {
	if n <= 0 {
		return ""
	}
	return "bez " + strconv.Itoa(n) + " h"
}

// slivoviDijela su međuslivovi regije iz kiše po slivovima. Regija bez
// međuslivova dobije „U izradi”, a izvor koji ne odgovori crtu upozorenja.
func slivoviDijela(rg RegijaVremena, kisa *kisaSlivovaPodaci) ([]RedakSliva, *PraznaKartica) {
	if len(rg.Slivovi) == 0 {
		return nil, &PraznaKartica{Vrsta: PraznoUIzradi, Tekst: "U izradi: kiša po slivovima zasad se računa samo za slivove Drave, Mure i Dunava."}
	}
	nedostupno := &PraznaKartica{Vrsta: PraznoNedostupno, Tekst: "Podaci o kiši po slivovima trenutačno nisu dostupni."}
	if kisa == nil {
		return nil, nedostupno
	}
	var out []RedakSliva
	for _, s := range kisa.Slivovi {
		if slices.Contains(rg.Slivovi, s.Sliv) {
			out = append(out, redakSliva(s))
		}
	}
	if len(out) > 0 {
		return out, nil
	}
	if kisa.Greska != "" {
		nedostupno.Tekst = kisa.Greska
	}
	return nil, nedostupno
}

// redakSliva: palo 24 h, očekivano 48 h, koliko je rijetko i gdje raste voda
func redakSliva(s SlivUKisi) RedakSliva {
	r := RedakSliva{Naziv: s.Naziv, Palo24: "—", Dolazi48: "—", Ukratko: "—", Rijetko: "—", Raste: "—", Klasa: s.Klasa}
	if r.Naziv == "" {
		r.Naziv = s.Sliv
	}
	if s.Ima {
		r.Palo24, r.Dolazi48 = mmSliva(s.Palo24), mmSliva(s.Dolazi48)
		r.Ukratko = strings.TrimSuffix(r.Palo24, " mm") + " · " + r.Dolazi48
		r.Rijetko = rijetkostKise(s.Razina, s.Ucestalost, s.Zasto)
	}
	if raste := porastiSliva(s.Letve); raste != "" {
		r.Raste = raste
	}
	return r
}

// mmSliva: cijeli milimetri, a ispod jednoga desetinka (da 0,4 ne bude 0)
func mmSliva(v float64) string {
	if v > 0 && v < 1 {
		return brojHRf(v, 1) + " mm"
	}
	return brojHRf(v, 0) + " mm"
}

// prozoriKise kratko kažu za koje je razdoblje kiša prešla prag kad to
// nije zadnja 24 h, jer stupci pokazuju samo palo 24 h i očekivano 48 h
var prozoriKise = map[string]string{"palo u zadnja 72 sata": "palo u 72 h", "palo i očekuje se u 72 sata": "palo i očekivano u 72 h"}

// rijetkostKise: „uobičajeno” ispod svih pragova, inače koliko često
// toliko padne i, kad nije riječ o zadnja 24 h, u kojem razdoblju
// („jednom godišnje · palo u 72 h”)
func rijetkostKise(razina int, ucestalost, zasto string) string {
	if razina == 0 || ucestalost == "" {
		return "uobičajeno"
	}
	return spoji(" · ", strings.TrimPrefix(ucestalost, "toliko padne u prosjeku "), prozoriKise[zasto])
}

// porastiSliva: „Osijek +45 cm u pon.” za letve s porastom od 5 cm i više
func porastiSliva(letve []PorastLetve) string {
	var out []string
	for _, l := range letve {
		if !l.Ima || l.Cm < 5 {
			continue
		}
		kad := " (" + strconv.Itoa(l.Dan) + ". dan)"
		if !l.Kad.IsZero() {
			kad = " u " + daniUTjednu[l.Kad.In(models.Zagreb).Weekday()] + "."
		}
		out = append(out, l.Naziv+" +"+brojHRf(l.Cm, 0)+" cm"+kad)
	}
	return strings.Join(out, ", ")
}
