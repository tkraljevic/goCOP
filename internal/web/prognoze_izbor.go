package web

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"gocop/internal/models"
)

// Izbor na pregledu prognoza: sektor, pa vodotok sektora
// (/prognoze?sektor=B&vodotok=drava). Bez parametara je sektor iz dužnosti
// osobe, inače prvi koji ima letve, i prvi vodotok u njemu. Nepoznat
// parametar daje zadano, bez greške. Izvoz u Excel slijedi isti izbor;
// /prognoze.xlsx bez parametara izvozi cijelo izdanje kao i dosad.

// CipPrognoze je čip (ili stavka izbornika na mobitelu) sektora ili vodotoka.
type CipPrognoze struct {
	Oznaka, Naziv, URL string
	Odabran            bool
	Prazan             bool   // sektor bez ijedne letve u izdanju: zasivljen, bez poveznice
	Napomena           string // zašto je sektor prazan, u oblačiću čipa
}

// IzborPrognoze je odabrani sektor i vodotok, s čipovima i natpisima.
type IzborPrognoze struct {
	Sektori     []CipPrognoze
	Vodotoci    []CipPrognoze
	Sektor      SektorPrognoze
	Vodotok     VodotokPrognoze
	Cijeli      bool   // vodotok kroz sve sektore
	CijeliURL   string // „Cijeli vodotok”; prazno kad je vodotok sav u sektoru
	SektorURL   string // iz cijelog vodotoka natrag na dio u sektoru
	Opis        string // „dio u sektoru B, do ušća u Dunav”
	NaslovLetvi string // „Letve na Dravi, od uzvodne prema nizvodnoj”

	kodovi []string   // letve koje stranica pokazuje, redom pregleda
	upit   url.Values // sektor, vodotok i cijeli, za adrese
}

// ulazIzbora je sve iz čega se izbor slaže.
type ulazIzbora struct {
	skupine []SektorPrognoze
	redom   []string // sve letve izdanja redom pregleda
	moji    []string // sektori iz dužnosti osobe
	granica granicaDunava
	usce    map[string]string // šifra letve → voda u koju se njezin vodotok ulijeva
}

// imaLetvi javlja ima li sektor ijednu letvu u izdanju.
func (s SektorPrognoze) imaLetvi() bool { return len(s.Vodotoci) > 0 }

// Ostale javlja je li to skupina „Bez sektora” (letve bez sektora).
func (s SektorPrognoze) Ostale() bool { return s.ID == oznakaOstalih }

// odaberiSektor: traženi sektor ako ima letve, inače prvi iz dužnosti
// osobe s letvama, inače prvi s letvama; −1 kad letvi nema nigdje.
func odaberiSektor(trazeni string, skupine []SektorPrognoze, moji []string) int {
	nadji := func(id string) int {
		for i, s := range skupine {
			if strings.EqualFold(s.ID, id) && s.imaLetvi() {
				return i
			}
		}
		return -1
	}
	for _, id := range append([]string{trazeni}, moji...) {
		if i := nadji(id); id != "" && i >= 0 {
			return i
		}
	}
	return slices.IndexFunc(skupine, SektorPrognoze.imaLetvi)
}

// odaberiVodotok: traženi vodotok sektora, inače prvi.
func odaberiVodotok(trazeni string, s SektorPrognoze) int {
	if i := slices.IndexFunc(s.Vodotoci, func(v VodotokPrognoze) bool { return v.Oznaka == trazeni }); i >= 0 {
		return i
	}
	return 0
}

// upitIzbora su parametri adrese za sektor, vodotok i cijeli vodotok.
func upitIzbora(sektor, vodotok string, cijeli bool) url.Values {
	q := url.Values{"sektor": {sektor}, "vodotok": {vodotok}}
	if cijeli {
		q.Set("cijeli", "1")
	}
	return q
}

// adresaPrognoza je adresa izbora; dan iz adrese ostaje, da promjena
// sektora ili vodotoka ne vrati profil na „sada”.
func adresaPrognoza(q url.Values, dan string) string {
	if dan != "" {
		q.Set("dan", dan)
	}
	return "/prognoze?" + q.Encode()
}

// izborIzAdrese slaže izbor iz upita; nil kad nijedna letva nije ni u
// jednoj skupini (tada stranica ostaje kakva je bila, sa svim letvama).
func izborIzAdrese(q url.Values, ul ulazIzbora) *IzborPrognoze {
	si := odaberiSektor(strings.TrimSpace(q.Get("sektor")), ul.skupine, ul.moji)
	if si < 0 {
		return nil
	}
	s := ul.skupine[si]
	v := s.Vodotoci[odaberiVodotok(strings.TrimSpace(q.Get("vodotok")), s)]
	iz := &IzborPrognoze{Sektor: s, Vodotok: v, kodovi: v.Letve}
	cijeli := cijeliVodotok(ul.skupine, v.Voda, ul.redom)
	iz.Cijeli = q.Get("cijeli") == "1" && len(cijeli) > len(v.Letve)
	iz.upit = upitIzbora(s.ID, v.Oznaka, iz.Cijeli)
	dan := strings.TrimSpace(q.Get("dan"))
	iz.Sektori = cipoviSektora(ul.skupine, s.ID, dan)
	for _, x := range s.Vodotoci {
		iz.Vodotoci = append(iz.Vodotoci, CipPrognoze{Oznaka: x.Oznaka, Naziv: x.Ime,
			URL: adresaPrognoza(upitIzbora(s.ID, x.Oznaka, false), dan), Odabran: x.Oznaka == v.Oznaka})
	}
	iz.NaslovLetvi = naslovLetvi(v, iz.Cijeli)
	switch {
	case iz.Cijeli:
		iz.kodovi = cijeli
		iz.Opis = "cijeli vodotok, kroz sve sektore"
		iz.SektorURL = adresaPrognoza(upitIzbora(s.ID, v.Oznaka, false), dan)
	case len(cijeli) > len(v.Letve):
		iz.CijeliURL = adresaPrognoza(upitIzbora(s.ID, v.Oznaka, true), dan)
		fallthrough
	default:
		iz.Opis = opisDijela(s, v, cijeli, ul.granica, usceDijela(v, ul.usce))
	}
	return iz
}

// usceDijela je voda u koju se ulijeva vodotok zadnje letve dijela.
func usceDijela(v VodotokPrognoze, usce map[string]string) string {
	if len(v.Letve) == 0 {
		return ""
	}
	return usce[v.Letve[len(v.Letve)-1]]
}

// cipoviSektora su čipovi svih sektora; sektor bez letvi je prazan. Prazan
// sektor ne mora biti bez prognoze: letve mu možda još nisu povezane u
// registru (sektor A), pa napomena upućuje na „Bez sektora”.
func cipoviSektora(skupine []SektorPrognoze, odabran, dan string) []CipPrognoze {
	napomena := "Nijedna letva iz prognoze nije povezana s ovim sektorom."
	if slices.ContainsFunc(skupine, SektorPrognoze.Ostale) {
		napomena += " Letve kojima u registru nije upisan sektor su pod „" + BezSektora + "”."
	}
	out := make([]CipPrognoze, 0, len(skupine))
	for _, s := range skupine {
		c := CipPrognoze{Oznaka: s.ID, Naziv: s.ID + " · " + s.Ime, Odabran: s.ID == odabran, Prazan: !s.imaLetvi()}
		if s.Ostale() {
			c.Naziv = s.Ime
		}
		if c.Prazan {
			c.Napomena = napomena
		} else {
			c.URL = adresaPrognoza(url.Values{"sektor": {s.ID}}, dan)
		}
		out = append(out, c)
	}
	return out
}

// AdresaDana je poveznica čipa dana: drži sektor i vodotok iz adrese.
func (d PrognozePageData) AdresaDana(oznaka string) string {
	q := url.Values{}
	if d.Izbor != nil {
		for k, v := range d.Izbor.upit {
			q[k] = v
		}
	}
	q.Set("dan", oznaka)
	return "?" + q.Encode()
}

// AdresaIzvoza je poveznica „Izvoz u Excel”: isti sektor i vodotok, a
// pregledač na nju dodaje t= da ne posluži staru datoteku.
func (d PrognozePageData) AdresaIzvoza() string {
	if d.Izbor == nil {
		return "/prognoze.xlsx?"
	}
	return "/prognoze.xlsx?" + d.Izbor.upit.Encode() + "&"
}

// DanUAdresi je ?dan= iz adrese, za skriveno polje obrasca na mobitelu.
func (d PrognozePageData) DanUAdresi() string {
	if d.Termin.Oznaka == "sada" {
		return ""
	}
	return d.Termin.Oznaka
}

// letveRedom su letve svih tablica redom pregleda.
func letveRedom(tablice []TablicaPrognoza) []LetvaPrognoze {
	var out []LetvaPrognoze
	for _, t := range tablice {
		out = append(out, t.Letve...)
	}
	return out
}

// samoLetve ostavlja letve kojih su šifre u izboru, istim redom.
func samoLetve(letve []LetvaPrognoze, kodovi map[string]bool) []LetvaPrognoze {
	var out []LetvaPrognoze
	for _, l := range letve {
		if kodovi[l.Kod] {
			out = append(out, l)
		}
	}
	return out
}

func skupKodova(kodovi []string) map[string]bool {
	out := make(map[string]bool, len(kodovi))
	for _, k := range kodovi {
		out[k] = true
	}
	return out
}

// suziStranicu ostavlja na stranici letve odabranog vodotoka, pod naslovom
// iz dizajna. Bez izbora stranica ostaje kakva je.
func suziStranicu(data *PrognozePageData) {
	if data.Izbor == nil {
		return
	}
	letve := samoLetve(letveRedom(data.Tablice), skupKodova(data.Izbor.kodovi))
	data.Tablice = []TablicaPrognoza{{Naslov: data.Izbor.NaslovLetvi, Letve: letve}}
}

// izvozPoIzboru javlja traži li adresa izvoza samo jedan vodotok.
func izvozPoIzboru(q url.Values) bool { return q.Has("sektor") || q.Has("vodotok") }

// suziIzvoz ostavlja u izvozu samo letve izbora: listovi po vodi ostaju
// isti, samo bez tuđih letvi, a list bez ijedne letve otpada.
func suziIzvoz(data *PrognozePageData) {
	if data.Izbor == nil {
		return
	}
	kodovi := skupKodova(data.Izbor.kodovi)
	var tablice []TablicaPrognoza
	for _, t := range data.Tablice {
		if letve := samoLetve(t.Letve, kodovi); len(letve) > 0 {
			tablice = append(tablice, TablicaPrognoza{Naslov: t.Naslov, Letve: letve})
		}
	}
	data.Tablice, data.Letve = tablice, samoLetve(data.Letve, kodovi)
}

// imeSuzenogIzvoza dodaje imenu datoteke sektor i vodotok, da se vidi da
// izvoz nije cijelo izdanje: prognoza_2026-11-05_15h_B_drava.xlsx.
func imeSuzenogIzvoza(ime string, iz *IzborPrognoze) string {
	if iz == nil {
		return ime
	}
	dio := "_" + iz.Sektor.ID + "_" + iz.Vodotok.Oznaka
	if iz.Cijeli {
		dio = "_" + oznakaImena(iz.Vodotok.Voda) + "_cijeli"
	}
	return strings.TrimSuffix(ime, ".xlsx") + dio + ".xlsx"
}

// upozorenjaLetvi su upozorenja koja se tiču ijedne od letvi.
func upozorenjaLetvi(u []UpozorenjeStranice, kodovi []string) []UpozorenjeStranice {
	var out []UpozorenjeStranice
	for _, x := range u {
		if slices.ContainsFunc(x.letve, func(k string) bool { return slices.Contains(kodovi, k) }) {
			out = append(out, x)
		}
	}
	return out
}

// izborPrognoze čita registar (sektori, područja, dionice, vodotoci) i
// slaže izbor za zahtjev; nil kad registra sektora s područjima nema.
func (h *PrognozeHandler) izborPrognoze(r *http.Request, letve []LetvaPrognoze, postaje map[string]models.Station) *IzborPrognoze {
	if h.users == nil {
		return nil
	}
	sektori, err := h.users.ListSectors()
	if err != nil {
		return nil
	}
	podrucja, _ := h.users.ListAreas("") // bez područja nema ni sektora za izbor, stranica ostaje sa svim letvama
	if sektori = sektoriSPodrucjima(sektori, podrucja); len(sektori) == 0 {
		return nil // bez registra sektora stranica ostaje kakva je bila, sa svim letvama
	}
	var dionice map[string][]string
	if h.stations != nil {
		dionice, _, _ = h.stations.StationScopes(r.Context()) // bez dionica sektor daje samo upisano praćenje i lanac
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	ul := ulazIzbora{
		skupine: skupinePrognoza(letve, postaje, sektori, vlastitiSektori(postaje, dionice, podrucja)),
		moji:    perms.SektoriRada(), granica: granicaIzRegistra(postaje), usce: h.uscaLetvi(r.Context(), postaje),
	}
	for _, l := range letve {
		ul.redom = append(ul.redom, l.Kod)
	}
	return izborIzAdrese(r.URL.Query(), ul)
}

// uscaLetvi: za svaku letvu voda u koju se ulijeva njezin vodotok, po
// vezi letve na registar vodotoka (dvije Karašice teku u različite vode).
func (h *PrognozeHandler) uscaLetvi(ctx context.Context, postaje map[string]models.Station) map[string]string {
	if h.watercourses == nil {
		return nil
	}
	sve, _ := h.watercourses.ListWatercourses(ctx, "", "", false) // bez registra vodotoka natpis „do ušća” izostaje
	return usceLetvi(postaje, sve)
}

// usceLetvi je čisti dio uscaLetvi: „ulijeva se u” može biti ime ili šifra
// vodotoka, pa se šifra prevodi u ime.
func usceLetvi(postaje map[string]models.Station, vodotoci []models.Watercourse) map[string]string {
	poSifri := map[string]models.Watercourse{}
	for _, w := range vodotoci {
		poSifri[w.Code] = w
	}
	out := map[string]string{}
	for kod, st := range postaje {
		w, ima := poSifri[st.WatercourseCode]
		if !ima || w.FlowsInto == "" {
			continue
		}
		out[kod] = w.FlowsInto
		if u, ima := poSifri[w.FlowsInto]; ima {
			out[kod] = u.Name
		}
	}
	return out
}
