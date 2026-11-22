package web

// Stranica „Slaganje računa” (plan uređivanja modela, PR 4, drugi ekran; crtež
// 13b · Prognoze · uređivanje modela, „2 · Slaganje računa”): letve vodotoka
// redom i, za odabranu, njezin račun: vodostaj ili protok, ulazi, rezerve i
// razine vode. Što je složeno vide svi prijavljeni. Nacrt sprema i odbacuje
// globalni administrator, svojim očima, na računalu koje izdaje prognozu.
// Nacrt ostaje na tom računalu i prognozu ne mijenja; priprema, proba i
// Primijeni dolaze poslije.

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
	"gocop/internal/prognoza"
)

// UrednikModela sprema nacrte u bazu prognoza. Daje ga poslužitelj, jer on drži
// bazu otvorenu za pisanje; stranica bez njega samo čita.
type UrednikModela interface {
	// Izdaje kaže izdaje li ovo računalo prognozu: samo se na njemu slaže model.
	Izdaje() bool
	SpremiNacrt(n prognoza.NacrtLetve) error
	ObrisiNacrt(letva string) error
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
	// Zakljucano kaže zašto se račun ove letve ovdje ne slaže; prazno kad se slaže.
	Zakljucano string
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
	if n, ima := rm.nacrti[kod]; ima {
		o.Tko, o.Kad = n.Tko, n.Kad
	}
	if len(z.Racun.Ulazi) == 0 {
		return o
	}
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
	redirectWith(w, r, natrag, "success", poruka)
}

// izvrsiRadnjuRacuna odbaci nacrt ili ga složi iz obrasca i spremi; vraća poruku
// o uspjehu.
func (h *PrognozeHandler) izvrsiRadnjuRacuna(r *http.Request, letva string) (string, error) {
	ime := imeLetve(h.postaje(r.Context()), letva)
	if r.PostForm.Get("radnja") == "odbaci" {
		return "Nacrt za letvu " + ime + " je odbačen.", h.urednik.ObrisiNacrt(letva)
	}
	stari, _ := h.racuniModela().definicija(letva)
	z, err := zapisIzObrasca(r.PostForm, letva, stari)
	if err != nil {
		return "", err
	}
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	tko := ""
	if u != nil {
		tko = u.FullName
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
	for _, u := range f["ulaz"] {
		if u = strings.TrimSpace(u); u != "" {
			z.Racun.Ulazi = append(z.Racun.Ulazi, u)
		}
	}
	for _, r := range f["rezerva"] {
		if r = strings.TrimSpace(r); r != "" {
			z.Racun.Rezerve = append(z.Racun.Rezerve, []string{r})
		}
	}
	razine, err := razineIzObrasca(f.Get("razine_nacin"), f.Get("razine"))
	if err != nil {
		return z, err
	}
	z.Racun.Razine = razine
	if slices.Equal(razine.Postoci, stari.Racun.Razine.Postoci) && slices.Equal(razine.Granice, stari.Racun.Razine.Granice) {
		z.Racun.Razine.Iskljucene, z.Racun.Rucno = stari.Racun.Razine.Iskljucene, stari.Racun.Rucno
	}
	return z, nil
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

var errBezUrednika = errors.New("baza prognoza nije otvorena na ovom računalu")
