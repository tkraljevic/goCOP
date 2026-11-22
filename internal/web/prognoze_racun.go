package web

// Stranica „Slaganje računa” (plan uređivanja modela, PR 4, drugi ekran; crtež
// 13b · Prognoze · uređivanje modela, „2 · Slaganje računa”): letve vodotoka
// redom i, za odabranu, njezin račun: vodostaj ili protok, ulazi, rezerve i
// razine vode. Što je složeno vide svi prijavljeni. Nacrt sprema i odbacuje
// globalni administrator, svojim očima, na računalu koje izdaje prognozu.
// Nacrt ostaje na tom računalu i prognozu ne mijenja. Pripremi namjesti račun
// iz arhive kao posao i u nacrt upiše brojeve po razini; uz razinu se zatim
// upisuje ručno kašnjenje ili se razina isključi. Probaj mjeri pripremljeni račun
// unatrag i po dosegu kaže je li bolji nego bez modela. Primijeni upiše probani
// račun kao model letve, uz obavezan razlog; od tada ga izdanje računa i model
// putuje razmjenom. Ukloni briše model.

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/poslovi"
	"gocop/internal/prognoza"
)

// UrednikModela sprema nacrte u bazu prognoza. Daje ga poslužitelj, jer on drži
// bazu otvorenu za pisanje; stranica bez njega samo čita.
type UrednikModela interface {
	// Izdaje kaže izdaje li ovo računalo prognozu: samo se na njemu slaže model.
	Izdaje() bool
	SpremiNacrt(n prognoza.NacrtLetve) error
	ObrisiNacrt(letva string) error
	// PripremiNacrt namjesti račun iz spremljenog nacrta; traje, pa ide kao posao.
	PripremiNacrt(letva string, p *poslovi.Posao) error
	// ProbajNacrt mjeri pripremljeni račun unatrag; traje, pa ide kao posao.
	ProbajNacrt(letva string, p *poslovi.Posao) error
	// PrimijeniNacrt upiše probani račun iz nacrta kao model letve; razlog je obavezan.
	PrimijeniNacrt(letva, tko, razlog string) error
	// UkloniModel briše primijenjeni model letve.
	UkloniModel(letva string) error
}

// Načini zadavanja razina u obrascu.
const (
	razineZadane  = "zadano"
	razinePostoci = "postoci"
	razineGranice = "granice"
)

// Koliko ulaza i rezervi obrazac nudi. Drugi ulaz služi kad vodu diže i uspor
// rijeke u koju se vodotok ulijeva.
const (
	najviseUlazaObrasca   = 2
	najviseRezerviObrasca = 2
)

var (
	oznakaNacrt     = StanjeModela{"Nacrt", "pozor"}
	oznakaModel     = StanjeModela{"Model", "dobro"}
	oznakaULancu    = StanjeModela{"Glavni lanac", ""}
	oznakaBezRac    = StanjeModela{"Još nema račun", ""}
	porukaULancu    = "Ova letva računa se u glavnom lancu, pa joj se račun ovdje ne slaže. Smije biti ulaz drugoj letvi."
	porukaNeIzdaje  = "Model se slaže na računalu koje izdaje prognozu. Ovdje se vidi što je složeno."
	porukaNijeAdmin = "Model mijenja globalni administrator."
	porukaTudjeOci  = "Model se ne mijenja tuđim očima. Vrati se sebi pa pokušaj ponovno."
)

// LetvaRacuna je redak popisa „Letve redom”.
type LetvaRacuna struct {
	Kod, Ime, URL, Opis string
	Odabrana            bool
	Stanje              StanjeModela
}

// IzborLetve je jedna letva u izboru ulaza ili rezerve.
type IzborLetve struct{ Kod, Ime string }

// SkupinaIzbora su letve jedne vode u izboru.
type SkupinaIzbora struct {
	Voda  string
	Letve []IzborLetve
}

// ObrazacRacuna je račun odabrane letve, kako stoji u obrascu.
type ObrazacRacuna struct {
	Kod, Ime    string
	Stanje      StanjeModela
	Velicina    string
	Ulazi       []string // uvijek najviseUlazaObrasca polja; prvi je obavezan
	Rezerve     []string // uvijek najviseRezerviObrasca polja, prazno je „bez rezerve”
	NacinRazina string
	Razine      string // granice između razina, odvojene zarezom
	Tko, Kad    string // tko je i kad spremio nacrt
	// Pripremljen je kad je priprema dala brojeve; prazno dok nacrt nije pripremljen.
	Pripremljen string
	Doseg       int
	Ucenje      string
	Napomene    []string
	Redci       []RazinaRacuna
	Proba       *ProbaRacuna // što je proba izmjerila; nil dok račun nije proban
	// ImaModel kaže da letva ima primijenjen model; Promjene su tko ga je, kad i
	// zašto mijenjao, od najnovije.
	ImaModel bool
	Promjene []prognoza.PromjenaLetve
	// Zakljucano kaže zašto se račun ove letve ovdje ne slaže; prazno kad se slaže.
	Zakljucano string
}

// RazinaRacuna je redak tablice razina: što je priprema dala i što se uz razinu
// upisuje ručno.
type RazinaRacuna struct {
	Broj      int    // redni broj razine, od 1
	Granica   string // „do 40”, „40–90”, „iznad 150”
	Kasnjenje string // po ulazu, npr. „9 h + 30 h”
	Rucno     string // ručno upisano kašnjenje, prazno kad ga nema
	Prozor    string
	Slaganje  string
	Sati      int
	Racuna    bool // razina izdaje prognozu
	BezRacuna bool // priprema joj nije dala račun: premalo sati
}

// ProbaRacuna je ishod probe na stranici.
type ProbaRacuna struct {
	Kad, Razdoblje string
	Jedinica       string
	Dosezi         []DosegProbe
	Natpisi        []string
	// Losijih je na koliko je dosega račun lošiji nego bez modela.
	Losijih int
}

// DosegProbe je redak tablice probe: promašaj računa i promašaj bez modela.
type DosegProbe struct {
	Doseg            int
	Racun, BezModela string
	Slucaja          int
	Ocjena           StanjeModela
}

// RacunPrognozePageData je stranica „Slaganje računa”.
type RacunPrognozePageData struct {
	PrognozePageData
	Sektor       SektorPrognoze
	Vodotok      VodotokPrognoze
	Natrag       string
	Letve        []LetvaRacuna
	Obrazac      *ObrazacRacuna
	Ulazi        []SkupinaIzbora
	SmijeUrediti bool
	ZastoNe      string
}

// IzborSOdabranim je izbor letvi s onom koja je u polju već odabrana.
type IzborSOdabranim struct {
	Skupine []SkupinaIzbora
	Odabran string
}

// IzborZa daje izbor letvi za jedno polje obrasca.
func (d RacunPrognozePageData) IzborZa(odabran string) IzborSOdabranim {
	return IzborSOdabranim{Skupine: d.Ulazi, Odabran: odabran}
}

// racuniModela je ono što stranica zna o računima letvi.
type racuniModela struct {
	nacrti map[string]prognoza.NacrtLetve
	zapisi map[string]prognoza.ZapisLetve
	glavni map[string]bool
}

// SetUrednikModela daje stranici spremanje nacrta i njezin predložak.
func (h *PrognozeHandler) SetUrednikModela(u UrednikModela, t *template.Template) {
	h.urednik, h.racunTmpl = u, t
}

// adresaRacuna je adresa letve na stranici slaganja računa.
func adresaRacuna(sektor, vodotok, letva string) string {
	q := url.Values{"sektor": {sektor}, "vodotok": {vodotok}}
	if letva != "" {
		q.Set("letva", letva)
	}
	return "/prognoze/model/racun?" + q.Encode()
}

// ShowRacun je stranica „Slaganje računa” (/prognoze/model/racun?sektor=&vodotok=&letva=).
func (h *PrognozeHandler) ShowRacun(w http.ResponseWriter, r *http.Request) {
	if h.racunTmpl == nil {
		http.NotFound(w, r)
		return
	}
	postaje := h.postaje(r.Context())
	q := r.URL.Query()
	sektor, vodotok, ima := nadjiVodotok(h.skupineModela(r, postaje), q.Get("sektor"), q.Get("vodotok"))
	if !ima {
		http.Redirect(w, r, "/prognoze/model", http.StatusSeeOther)
		return
	}
	data := RacunPrognozePageData{PrognozePageData: h.zaglavlje(r), Sektor: sektor, Vodotok: vodotok,
		Natrag: adresaModela(sektor.ID, vodotok.Oznaka)}
	racuni := h.racuniModela()
	odabrana := odabranaLetva(vodotok.Letve, q.Get("letva"))
	for _, kod := range vodotok.Letve {
		data.Letve = append(data.Letve, LetvaRacuna{Kod: kod, Ime: imeLetve(postaje, kod), Odabrana: kod == odabrana,
			URL: adresaRacuna(sektor.ID, vodotok.Oznaka, kod), Stanje: racuni.stanje(kod), Opis: racuni.opis(kod, postaje)})
	}
	if odabrana != "" {
		o := racuni.obrazac(odabrana, imeLetve(postaje, odabrana))
		data.Obrazac = &o
		data.Ulazi = izborUlaza(postaje, odabrana, vodotok.Ime)
	}
	data.SmijeUrediti, data.ZastoNe = h.smijeUreditiModel(r)
	if err := h.racunTmpl.ExecuteTemplate(w, "prognoze_racun.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// nadjiVodotok traži vodotok sektora po oznakama iz adrese.
func nadjiVodotok(skupine []SektorPrognoze, sektor, vodotok string) (SektorPrognoze, VodotokPrognoze, bool) {
	for _, s := range skupine {
		if s.ID != strings.TrimSpace(sektor) {
			continue
		}
		for _, v := range s.Vodotoci {
			if v.Oznaka == strings.TrimSpace(vodotok) {
				return s, v, true
			}
		}
	}
	return SektorPrognoze{}, VodotokPrognoze{}, false
}

// odabranaLetva je tražena letva ako je na vodotoku, inače prva.
func odabranaLetva(letve []string, trazena string) string {
	if slices.Contains(letve, trazena) {
		return trazena
	}
	if len(letve) > 0 {
		return letve[0]
	}
	return ""
}

// imeLetve je ime iz registra, a šifra kad letve u registru nema.
func imeLetve(postaje map[string]models.Station, kod string) string {
	if st, ima := postaje[kod]; ima && st.Name != "" {
		return st.Name
	}
	return kod
}

// racuniModela čita nacrte, zapise i letve glavnog lanca iz baze prognoza.
func (h *PrognozeHandler) racuniModela() racuniModela {
	rm := racuniModela{glavni: map[string]bool{}}
	if h.citac == nil {
		return rm
	}
	c := h.citac()
	rm.nacrti, rm.zapisi = c.NacrtiModela(), c.ZapisiModela()
	pojasi, _ := c.Namjesteno()
	for letva := range pojasi {
		rm.glavni[letva] = true
	}
	return rm
}

// definicija je račun letve koji stranica pokazuje: nacrt, pa primijenjeni model.
func (rm racuniModela) definicija(kod string) (prognoza.ZapisLetve, StanjeModela) {
	if n, ima := rm.nacrti[kod]; ima {
		return n.Zapis, oznakaNacrt
	}
	if z, ima := rm.zapisi[kod]; ima {
		return z, oznakaModel
	}
	if rm.glavni[kod] {
		return prognoza.ZapisLetve{}, oznakaULancu
	}
	return prognoza.ZapisLetve{}, oznakaBezRac
}

func (rm racuniModela) stanje(kod string) StanjeModela {
	_, s := rm.definicija(kod)
	return s
}

// opis je redak ispod imena letve na popisu: veličina, ulaz i rezerve.
func (rm racuniModela) opis(kod string, postaje map[string]models.Station) string {
	z, s := rm.definicija(kod)
	if s == oznakaULancu {
		return "računa se u glavnom lancu"
	}
	if len(z.Racun.Ulazi) == 0 {
		return ""
	}
	imena := func(kodovi []string) string {
		out := make([]string, 0, len(kodovi))
		for _, k := range kodovi {
			out = append(out, imeLetve(postaje, k))
		}
		return strings.Join(out, " + ")
	}
	dijelovi := []string{z.Velicina, "ulaz: " + imena(z.Racun.Ulazi)}
	for _, r := range z.Racun.Rezerve {
		dijelovi = append(dijelovi, "rezerva: "+imena(r))
	}
	return strings.Join(dijelovi, " · ")
}

// obrazac slaže polja obrasca iz računa letve; letva bez računa dobiva prazan
// obrazac u vodostaju i sa zadanim razinama.
func (rm racuniModela) obrazac(kod, ime string) ObrazacRacuna {
	z, s := rm.definicija(kod)
	o := ObrazacRacuna{Kod: kod, Ime: ime, Stanje: s, Velicina: "vodostaj", NacinRazina: razineZadane,
		Ulazi: make([]string, najviseUlazaObrasca), Rezerve: make([]string, najviseRezerviObrasca)}
	if s == oznakaULancu {
		o.Zakljucano = porukaULancu
		return o
	}
	if model, ima := rm.zapisi[kod]; ima {
		o.ImaModel, o.Promjene = true, slices.Clone(model.Promjene)
		slices.Reverse(o.Promjene)
	}
	var cvorovi []float64
	if n, ima := rm.nacrti[kod]; ima {
		o.Tko, o.Kad = n.Tko, n.Kad
		if p := n.Priprema; p != nil {
			o.Pripremljen, o.Napomene, cvorovi = p.Kad, p.Napomene, p.Cvorovi
			o.Ucenje = opisUcenja(p.Ucenje, z.Velicina)
		}
		o.Proba = probaRacuna(n.Proba, z.Velicina)
	}
	if len(z.Racun.Ulazi) == 0 {
		return o
	}
	o.Doseg, o.Redci = z.Racun.Doseg, razineRacuna(z, cvorovi)
	o.Velicina = z.Velicina
	copy(o.Ulazi, z.Racun.Ulazi)
	for i, r := range z.Racun.Rezerve {
		if i < najviseRezerviObrasca && len(r) > 0 {
			o.Rezerve[i] = r[0]
		}
	}
	o.NacinRazina, o.Razine = razineUObrascu(z.Racun.Razine)
	return o
}

// jedinicaRacuna je jedinica veličine u kojoj račun radi.
func jedinicaRacuna(velicina string) string {
	if velicina == "protok" {
		return "m³/s"
	}
	return "cm"
}

// opisUcenja kaže na kojoj je vodi i na koliko sati račun učio.
func opisUcenja(u prognoza.UcenjeRacuna, velicina string) string {
	if u.Sati == 0 {
		return ""
	}
	return fmt.Sprintf("Račun je učio na %s sati, na vodi od %.0f do %.0f %s.", brojHR(u.Sati), u.Od, u.Do, jedinicaRacuna(velicina))
}

// probaRacuna slaže ishod probe za stranicu; nil kad račun nije proban.
func probaRacuna(p *prognoza.ProbaNacrta, velicina string) *ProbaRacuna {
	if p == nil {
		return nil
	}
	out := &ProbaRacuna{Kad: p.Kad, Jedinica: jedinicaRacuna(velicina), Natpisi: p.Natpisi}
	if len(p.Razdoblje) == 2 {
		out.Razdoblje = datumHR(p.Razdoblje[0]) + " – " + datumHR(p.Razdoblje[1])
	}
	for _, d := range p.Dosezi {
		r := DosegProbe{Doseg: d.Doseg, Slucaja: d.Slucaja, Ocjena: oznakaOcjene(d.Stanje), Racun: "—", BezModela: "—"}
		if d.Stanje != prognoza.MjeraNijeProvjereno {
			r.Racun, r.BezModela = decimalaHR(d.Promasaj), decimalaHR(d.BezModela)
		}
		if d.Stanje == prognoza.MjeraLosije {
			out.Losijih++
		}
		out.Dosezi = append(out.Dosezi, r)
	}
	return out
}

// oznakaOcjene je čip ocjene: bolje je dobro, lošije traži pozor.
func oznakaOcjene(s prognoza.StanjeMjere) StanjeModela {
	switch s {
	case prognoza.MjeraBolje:
		return StanjeModela{string(s), "dobro"}
	case prognoza.MjeraLosije:
		return StanjeModela{string(s), "pozor"}
	}
	return StanjeModela{string(s), ""}
}

// decimalaHR piše broj s jednom decimalom i decimalnim zarezom.
func decimalaHR(v float64) string {
	return strings.Replace(strconv.FormatFloat(v, 'f', 1, 64), ".", ",", 1)
}

// razineRacuna slaže tablicu razina iz glavne inačice računa. Razine se mjere
// na prvom ulazu, pa su i granice u njegovoj vodi. Granice daje priprema; kad ih
// nema (primijenjeni model, ručni račun), razine su sami pojasi. Razina bez
// pojasa nije dobila račun.
func razineRacuna(z prognoza.ZapisLetve, cvorovi []float64) []RazinaRacuna {
	if len(z.Brojevi.Inacice) == 0 {
		return nil
	}
	glavna := z.Brojevi.Inacice[0]
	if len(cvorovi) < 2 {
		cvorovi = nil
		for _, p := range glavna {
			cvorovi = append(cvorovi, p.Od)
		}
		if len(glavna) > 0 {
			cvorovi = append(cvorovi, glavna[len(glavna)-1].Do)
		}
	}
	rucno := map[int]int{}
	for _, k := range z.Racun.Rucno.Kasnjenje {
		rucno[k.Razina] = k.Sati
	}
	jedinica := jedinicaRacuna(velicinaUlaza(glavna, z.Velicina))
	var out []RazinaRacuna
	for i := 0; i+1 < len(cvorovi); i++ {
		r := RazinaRacuna{Broj: i + 1, Granica: granicaRazine(cvorovi, i, jedinica), BezRacuna: true}
		if sati, ima := rucno[i]; ima {
			r.Rucno = strconv.Itoa(sati)
		}
		if j := slices.IndexFunc(glavna, func(p prognoza.Pojas) bool { return p.Od == cvorovi[i] }); j >= 0 {
			popuniRazinu(&r, glavna[j])
		}
		out = append(out, r)
	}
	return out
}

// velicinaUlaza je veličina u kojoj se vodi prvi ulaz; kad pojasa nema, veličina
// računa.
func velicinaUlaza(glavna []prognoza.Pojas, velicina string) string {
	if len(glavna) > 0 && len(glavna[0].Ulazi) > 0 && glavna[0].Ulazi[0].Velicina != "" {
		return glavna[0].Ulazi[0].Velicina
	}
	return velicina
}

// popuniRazinu upisuje u redak ono što je priprema namjestila za pojas.
func popuniRazinu(r *RazinaRacuna, p prognoza.Pojas) {
	var kasnjenja, prozori []string
	for _, u := range p.Ulazi {
		kasnjenja = append(kasnjenja, strconv.Itoa(u.PomakH)+" h")
		prozori = append(prozori, strconv.Itoa(u.Sirina)+" h")
	}
	r.BezRacuna, r.Racuna, r.Sati = false, !p.Nepovezan, p.Sati
	r.Kasnjenje, r.Prozor = strings.Join(kasnjenja, " + "), strings.Join(prozori, " + ")
	r.Slaganje = strings.Replace(strconv.FormatFloat(p.R, 'f', 2, 64), ".", ",", 1)
}

// granicaRazine je natpis granica razine i: najniža je „do”, najviša „iznad”.
func granicaRazine(cvorovi []float64, i int, jedinica string) string {
	od, do := strconv.FormatFloat(cvorovi[i], 'f', 0, 64), strconv.FormatFloat(cvorovi[i+1], 'f', 0, 64)
	switch {
	case len(cvorovi) == 2:
		return "sva voda"
	case i == 0:
		return "do " + do + " " + jedinica
	case i+2 == len(cvorovi):
		return "iznad " + od + " " + jedinica
	}
	return od + "–" + do + " " + jedinica
}

// razineUObrascu su način i granice kako ih obrazac pokazuje. Postoci u zapisu
// nose i krajeve 0 i 100, a u obrascu se upisuju samo granice između razina.
func razineUObrascu(r prognoza.RazineLetve) (nacin, granice string) {
	brojevi := func(v []float64) string {
		out := make([]string, 0, len(v))
		for _, x := range v {
			out = append(out, strconv.FormatFloat(x, 'f', -1, 64))
		}
		return strings.Join(out, ", ")
	}
	switch {
	case len(r.Granice) > 0:
		return razineGranice, brojevi(r.Granice)
	case len(r.Postoci) > 2:
		return razinePostoci, brojevi(r.Postoci[1 : len(r.Postoci)-1])
	case len(r.Postoci) == 2:
		return razinePostoci, ""
	}
	return razineZadane, ""
}

// izborUlaza su letve koje mogu biti ulaz ili rezerva: sve letve registra osim
// same letve, po vodama, a voda same letve prva.
func izborUlaza(postaje map[string]models.Station, letva, voda string) []SkupinaIzbora {
	var out []SkupinaIzbora
	for _, l := range letveRegistra(postaje) {
		if l.Kod == letva {
			continue
		}
		st := postaje[l.Kod]
		if n := len(out); n == 0 || out[n-1].Voda != st.Watercourse {
			out = append(out, SkupinaIzbora{Voda: st.Watercourse})
		}
		out[len(out)-1].Letve = append(out[len(out)-1].Letve, IzborLetve{Kod: l.Kod, Ime: imeLetve(postaje, l.Kod)})
	}
	slices.SortStableFunc(out, func(a, b SkupinaIzbora) int {
		return map[bool]int{true: 0, false: 1}[a.Voda == voda] - map[bool]int{true: 0, false: 1}[b.Voda == voda]
	})
	return out
}

// smijeUreditiModel kaže smije li osoba s ovog računala slagati model i, ako ne,
// zašto.
func (h *PrognozeHandler) smijeUreditiModel(r *http.Request) (bool, string) {
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	gleda, _ := r.Context().Value(contextKeyViewing).(bool)
	switch {
	case perms == nil || !perms.IsGlobalAdmin:
		return false, porukaNijeAdmin
	case gleda:
		return false, porukaTudjeOci
	case h.urednik == nil || !h.urednik.Izdaje():
		return false, porukaNeIzdaje
	}
	return true, ""
}

// SpremiRacun sprema ili odbacuje nacrt računa letve (POST /prognoze/model/racun).
func (h *PrognozeHandler) SpremiRacun(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "obrazac se ne da pročitati", http.StatusBadRequest)
		return
	}
	letva := strings.TrimSpace(r.PostForm.Get("letva"))
	natrag := adresaRacuna(r.PostForm.Get("sektor"), r.PostForm.Get("vodotok"), letva)
	if smije, zasto := h.smijeUreditiModel(r); !smije {
		redirectWith(w, r, natrag, "error", zasto)
		return
	}
	poruka, err := h.izvrsiRadnjuRacuna(r, letva)
	if err != nil {
		redirectWith(w, r, natrag, "error", velikoPrvo(err.Error())+".")
		return
	}
	if naziv, posao := h.posaoNacrta(r.PostForm.Get("radnja")); posao != nil && h.poslovi != nil && h.tmplPosao != nil {
		h.pokreniPosaoNacrta(w, r, naziv, letva, natrag, posao)
		return
	}
	redirectWith(w, r, natrag, "success", poruka)
}

// posaoNacrta je posao koji radnja pokreće nakon spremanja nacrta: priprema ili
// proba. Ostale radnje nemaju posao.
func (h *PrognozeHandler) posaoNacrta(radnja string) (string, func(string, *poslovi.Posao) error) {
	switch radnja {
	case "pripremi":
		return "Priprema računa", h.urednik.PripremiNacrt
	case "probaj":
		return "Proba računa", h.urednik.ProbajNacrt
	}
	return "", nil
}

// pokreniPosaoNacrta pokreće pripremu ili probu upravo spremljenog nacrta kao
// posao i pokazuje stranicu koja ga prati; kad završi, vraća na račun letve.
func (h *PrognozeHandler) pokreniPosaoNacrta(w http.ResponseWriter, r *http.Request, naziv, letva, natrag string,
	posao func(string, *poslovi.Posao) error) {
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	korisnik := ""
	if u != nil {
		korisnik = u.ID.String()
	}
	p := h.poslovi.Pokreni(naziv+": "+imeLetve(h.postaje(r.Context()), letva), korisnik, natrag,
		func(p *poslovi.Posao) error { return posao(letva, p) })
	d := PosaoData{CurrentUser: u, Permissions: perms, ActiveNav: "prognoze", ViewAsBanner: viewBanner(r),
		PosaoID: p.ID, PosaoNaziv: p.Naziv, Natrag: natrag}
	if err := h.tmplPosao.ExecuteTemplate(w, "posao.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// izvrsiRadnjuRacuna odbaci nacrt ili ga složi iz obrasca i spremi; vraća poruku
// o uspjehu.
func (h *PrognozeHandler) izvrsiRadnjuRacuna(r *http.Request, letva string) (string, error) {
	ime := imeLetve(h.postaje(r.Context()), letva)
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	tko := ""
	if u != nil {
		tko = u.FullName
	}
	switch r.PostForm.Get("radnja") {
	case "odbaci":
		return "Nacrt za letvu " + ime + " je odbačen.", h.urednik.ObrisiNacrt(letva)
	case "primijeni":
		return "Model letve " + ime + " je primijenjen. Prva prognoza stiže pri sljedećem računu prognoze.",
			h.urednik.PrimijeniNacrt(letva, tko, strings.TrimSpace(r.PostForm.Get("razlog")))
	case "ukloni":
		return "Model letve " + ime + " je uklonjen. Od sljedećeg računa prognoze letva radi kao prije njega.", h.urednik.UkloniModel(letva)
	}
	return h.spremiNacrtIzObrasca(r, letva, ime, tko)
}

// spremiNacrtIzObrasca složi nacrt iz polja obrasca i spremi ga.
func (h *PrognozeHandler) spremiNacrtIzObrasca(r *http.Request, letva, ime, tko string) (string, error) {
	stari, _ := h.racuniModela().definicija(letva)
	z, err := zapisIzObrasca(r.PostForm, letva, stari)
	if err != nil {
		return "", err
	}
	n := prognoza.NacrtLetve{Zapis: z, Tko: tko, Kad: time.Now().In(models.Zagreb).Format("2. 1. 2006. 15:04")}
	return "Nacrt za letvu " + ime + " je spremljen. Prognoza se ne mijenja dok se model ne primijeni.", h.urednik.SpremiNacrt(n)
}

// velikoPrvo diže prvo slovo poruke, da greška iz provjere stoji kao rečenica.
func velikoPrvo(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// zapisIzObrasca slaže zapis iz polja obrasca. Ručne vrijednosti i isključene
// razine staroga računa ostaju samo dok se razine ne promijene, jer se vode po
// rednom broju razine.
func zapisIzObrasca(f url.Values, letva string, stari prognoza.ZapisLetve) (prognoza.ZapisLetve, error) {
	z := prognoza.ZapisLetve{Oblik: prognoza.OblikModelaLetve, Letva: letva, Metoda: prognoza.MetodaLanac,
		Velicina: strings.TrimSpace(f.Get("velicina"))}
	z.Racun.Ulazi = odabraneLetve(f["ulaz"])
	for _, r := range odabraneLetve(f["rezerva"]) {
		z.Racun.Rezerve = append(z.Racun.Rezerve, []string{r})
	}
	razine, err := razineIzObrasca(f.Get("razine_nacin"), f.Get("razine"))
	if err != nil {
		return z, err
	}
	z.Racun.Razine = razine
	if !slices.Equal(razine.Postoci, stari.Racun.Razine.Postoci) || !slices.Equal(razine.Granice, stari.Racun.Razine.Granice) {
		return z, nil
	}
	z.Racun.Razine.Iskljucene, z.Racun.Rucno = stari.Racun.Razine.Iskljucene, stari.Racun.Rucno
	redaka, err := strconv.Atoi(f.Get("razina"))
	if err != nil || redaka < 1 || redaka > 20 {
		return z, nil // tablica razina nije bila na stranici
	}
	z.Racun.Razine.Iskljucene, z.Racun.Rucno.Kasnjenje, err = razineIzTablice(f, redaka)
	return z, err
}

// odabraneLetve su neprazna polja izbora, redom kojim stoje u obrascu.
func odabraneLetve(polja []string) []string {
	var out []string
	for _, p := range polja {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// razineIzTablice čita tablicu razina: razina bez kvačice „Računa” je isključena,
// a upisani sati su ručno kašnjenje te razine.
func razineIzTablice(f url.Values, redaka int) (iskljucene []int, kasnjenja []prognoza.RucnoKasnjenje, err error) {
	for i := range redaka {
		broj := strconv.Itoa(i)
		if !slices.Contains(f["racuna"], broj) && f.Get("ima_racun_"+broj) == "1" {
			iskljucene = append(iskljucene, i)
		}
		upisano := strings.TrimSpace(f.Get("rucno_" + broj))
		if upisano == "" {
			continue
		}
		sati, err := strconv.Atoi(upisano)
		if err != nil || sati < 0 {
			return nil, nil, fmt.Errorf("kašnjenje razine %d nije cijeli broj sati", i+1)
		}
		kasnjenja = append(kasnjenja, prognoza.RucnoKasnjenje{Razina: i, Sati: sati})
	}
	return iskljucene, kasnjenja, nil
}

// razineIzObrasca čita način i granice između razina. Postocima se dodaju
// krajevi 0 i 100, kako ih zapis traži.
func razineIzObrasca(nacin, tekst string) (prognoza.RazineLetve, error) {
	var r prognoza.RazineLetve
	if nacin != razinePostoci && nacin != razineGranice {
		return r, nil
	}
	brojevi, err := brojeviIzTeksta(tekst)
	if err != nil {
		return r, err
	}
	if nacin == razineGranice {
		if len(brojevi) == 0 {
			return r, errors.New("upiši barem jednu granicu između razina ili izaberi zadane razine")
		}
		r.Granice = brojevi
		return r, nil
	}
	r.Postoci = slices.Concat([]float64{0}, brojevi, []float64{100})
	return r, nil
}

// brojeviIzTeksta čita brojeve odvojene zarezom, točkom sa zarezom ili razmakom.
func brojeviIzTeksta(tekst string) ([]float64, error) {
	var out []float64
	for _, dio := range strings.FieldsFunc(tekst, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' }) {
		v, err := strconv.ParseFloat(dio, 64)
		if err != nil {
			return nil, fmt.Errorf("„%s” nije broj; granice razina upiši kao brojeve odvojene zarezom", dio)
		}
		out = append(out, v)
	}
	return out, nil
}

// urednikPosluzitelja je urednik koji poslužitelj dobije tek kad otvori baze,
// pa ga stranica pita pri svakom zahtjevu.
type urednikPosluzitelja struct{ s *Server }

func urednikIliNista(s *Server) UrednikModela { return urednikPosluzitelja{s} }

func (u urednikPosluzitelja) Izdaje() bool {
	return u.s.urednikModela != nil && u.s.urednikModela.Izdaje()
}

func (u urednikPosluzitelja) SpremiNacrt(n prognoza.NacrtLetve) error {
	if u.s.urednikModela == nil {
		return errBezUrednika
	}
	return u.s.urednikModela.SpremiNacrt(n)
}

func (u urednikPosluzitelja) ObrisiNacrt(letva string) error {
	if u.s.urednikModela == nil {
		return errBezUrednika
	}
	return u.s.urednikModela.ObrisiNacrt(letva)
}

func (u urednikPosluzitelja) PripremiNacrt(letva string, p *poslovi.Posao) error {
	if u.s.urednikModela == nil {
		return errBezUrednika
	}
	return u.s.urednikModela.PripremiNacrt(letva, p)
}

func (u urednikPosluzitelja) ProbajNacrt(letva string, p *poslovi.Posao) error {
	if u.s.urednikModela == nil {
		return errBezUrednika
	}
	return u.s.urednikModela.ProbajNacrt(letva, p)
}

func (u urednikPosluzitelja) PrimijeniNacrt(letva, tko, razlog string) error {
	if u.s.urednikModela == nil {
		return errBezUrednika
	}
	return u.s.urednikModela.PrimijeniNacrt(letva, tko, razlog)
}

func (u urednikPosluzitelja) UkloniModel(letva string) error {
	if u.s.urednikModela == nil {
		return errBezUrednika
	}
	return u.s.urednikModela.UkloniModel(letva)
}

var errBezUrednika = errors.New("baza prognoza nije otvorena na ovom računalu")
