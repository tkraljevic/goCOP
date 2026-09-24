package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"sort"
	"time"

	"gocop/internal/hydro"
	"gocop/internal/javnivodostaji"
	"gocop/internal/models"
	"gocop/internal/prognoza"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Odjeljak za prognoze. Jedna stranica za cijeli sliv, složena kako voda teče —
// uzvodne letve prije nizvodnih, a ne po abecedi. Uz svaku vrijednost stoji
// raspon unutar kojeg se očekuje da ostane, a ondje gdje prognoza ne pobjeđuje
// postojanost to i piše: prognoza koja ne zna više od "bit će kao i sad" nije
// prognoza nego trošak.

// Dosezi su vremena koja se na pregledu pokazuju. Dalje od toga prognoza i
// dalje postoji, ali se na jednom retku ne da čitati.
var DoseziPregleda = []int{6, 12, 24, 48, 72}

// BliziDosezi su satni stupci pregleda prije stupaca po danima.
var BliziDosezi = []int{6, 12}

type PrognozeHandler struct {
	tmpl     *template.Template
	citac    func() *CitacPrognoza
	stations *service.StationService
	users    *service.UserService    // zaglavlje i potpisnici izvoza
	readings *service.ReadingService // mjerenja unatrag, za klizač vremena na profilu

	watercourses *service.WatercourseService // geometrija tokova, za ušća na profilu

	javni func() *javnivodostaji.Uvoznik // krug preuzimanja, za gumb „Generiraj”

	metodaTmpl *template.Template // stranica „O prognozi”
}

// SetUsers daje izvozu sektore i osobe za zaglavlje i potpise.
func (h *PrognozeHandler) SetUsers(u *service.UserService) { h.users = u }

// SetReadings daje profilu mjerenja unatrag, da klizač vremena pokaže odakle je val došao.
func (h *PrognozeHandler) SetReadings(r *service.ReadingService) { h.readings = r }

// SetJavniUvoz daje stranici krug preuzimanja vodostaja, da ga dežurni može
// pokrenuti gumbom; traži se pri svakom pozivu jer se uvoznik veže poslije ruta.
func (h *PrognozeHandler) SetJavniUvoz(u func() *javnivodostaji.Uvoznik) { h.javni = u }

// SetWatercourses daje profilu registar vodotoka s geometrijom, za ušća.
func (h *PrognozeHandler) SetWatercourses(w *service.WatercourseService) { h.watercourses = w }

// SetMetoda daje predložak stranice „O prognozi”.
func (h *PrognozeHandler) SetMetoda(t *template.Template) { h.metodaTmpl = t }

func NewPrognozeHandler(tmpl *template.Template, citac func() *CitacPrognoza,
	stations *service.StationService) *PrognozeHandler {
	return &PrognozeHandler{tmpl: tmpl, citac: citac, stations: stations}
}

// VrijednostPrognoze je jedna brojka na pregledu, u obje veličine. Vodostaj je
// glavni jer se u obrani čita on; protok stoji ispod, sitnije. Ondje gdje
// krivulje nema, druge veličine nema ni na pregledu.
type VrijednostPrognoze struct {
	CmV, QV  *float64 // iste vrijednosti kao broj, za izvoz
	DosegH   int
	Ima      bool
	Cm       string
	CmRaspon string
	Q        string
	QRaspon  string
	Slabija  bool // na tom dosegu postojanost je bolja
}

// LetvaPrognoze je jedan redak pregleda.
type LetvaPrognoze struct {
	Kod, Naziv      string
	Voda            string
	Stacionaza      string
	Racuna          string // u čemu model radi; druga veličina dolazi iz krivulje
	Vrh             bool   // vrh lanca: stoji samo mjerenje, prognoze nema
	URL             string
	SadaCm          string
	SadaQ           string
	SadaCmV, SadaQV *float64
	Vrijednosti     []VrijednostPrognoze
	Doseg           int  // dokle prognoza ide, u satima
	Ulaz            bool // ulaz dnevne prognoze: stoji samo mjerenje
	Pregledna       bool // u model ne ulazi, stoji radi pregleda
	Ulazi           []string
	ImaTermina      bool // ima ijednu prognozu, svoju ili tuđu
	TudiVrh         bool // vrh lanca koji dalje ide po mađarskoj prognozi
	Dani            []CelijaDana
}

// TudaCelija je tuđa prognoza u ćeliji dana: mađarska ili srpska.
type TudaCelija struct {
	CmV                   float64
	Oznaka, Klasa, Naslov string
	Cm, Raspon            string
}

// tudiIzvori su tuđe prognoze koje se na pregledu stavljaju uz naše.
var tudiIzvori = []struct{ izvor, oznaka, klasa, naslov string }{
	{prognoza.Podrijetlo, "HU", "hu", "Mađarska prognoza (hydroinfo.hu) za isti termin"},
	{prognoza.PodrijetloHidmet, "RS", "rs", "Srpska prognoza (hidmet.gov.rs) za isti termin"},
}

// CelijaDana je jedan dan pregleda, za 07 h — termin u kojem prognozu daju i
// Mađari i uredska tablica. Vrijednost je iz satnog lanca ili iz dnevnog
// modela, već prema tome koji je na tom danu za tu letvu provjerom točniji;
// ispod stoji mađarska prognoza za isti termin, gdje je imaju.
type CelijaDana struct {
	CmV, QV        *float64 // iste vrijednosti kao broj, za izvoz
	Naslov         string   // dan u tjednu i datum
	Cm, Raspon     string
	Q, QRaspon     string       // protok, gdje letva ima krivulju
	Tude           []TudaCelija // tuđe prognoze za isti termin, svaka svojom bojom
	Dnevna         bool         // vrijednost daje dnevni model
	Slabija        bool
	Razina, Moguce string
}

type PrognozePageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner

	Izdano     string
	IzdanoSat  int64 // sat izdanja od epohe, za klizač vremena na profilu
	Nema       bool
	Razlog     string
	Udio       int
	Dosezi     []int
	Letve      []LetvaPrognoze
	Profili    []*UzduzniProfil
	BezProfila string // zašto profila nema, kad ga nema

	Bliski   []int
	Dani     []string // naslovi stupaca po danima
	ImaTudih bool
	Tablice  []TablicaPrognoza
	Izdaje   string // centar koji prognozu izdaje, npr. COP Osijek

	MozeGenerirati bool   // ima krug preuzimanja, pa gumb „Generiraj” ima što pokrenuti
	Generira       bool   // krug upravo traje
	ZadnjiKrug     string // kad je zadnji krug prošao i što je donio
	Napredak       javnivodostaji.Napredak
}

// TablicaPrognoza je jedna voda na pregledu, letve od uzvodne prema nizvodnoj.
type TablicaPrognoza struct {
	Naslov string
	Letve  []LetvaPrognoze
}

func (h *PrognozeHandler) ShowPrognoze(w http.ResponseWriter, r *http.Request) {
	h.iscrtaj(w, h.podaci(r))
}

// podaci slaže sve što pregled prognoza pokazuje; isto služi i izvozu.
func (h *PrognozeHandler) podaci(r *http.Request) PrognozePageData {
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	data := PrognozePageData{
		CurrentUser: u, Permissions: perms,
		ActiveNav: "prognoze", ViewAsBanner: viewBanner(r),
		SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error"),
		Dosezi: DoseziPregleda, Udio: int(math.Round(prognoza.UdioURasponu * 100)),
	}
	if u := h.uvoznik(); u != nil {
		data.MozeGenerirati, data.Generira = true, u.UTijeku()
		data.Napredak = u.Napredak()
		if k, ima := u.ZadnjiKrug(); ima {
			data.ZadnjiKrug = fmt.Sprintf("%s: %d letvi, %d novih očitanja, %s",
				k.Kad.In(models.Zagreb).Format("2.1. u 15:04"), k.Letvi, k.Novih, trajanjeKruga(k.Trajanje))
		}
	}

	var c *CitacPrognoza
	if h.citac != nil {
		c = h.citac()
	}
	if c == nil {
		data.Nema, data.Razlog = true, "Baza prognoza nije otvorena."
		return data
	}
	izdano, letve, err := c.Pregled()
	if err != nil || len(letve) == 0 {
		data.Nema, data.Razlog = true, "Nijedna prognoza još nije izdana."
		return data
	}
	data.Izdano = izdano.In(models.Zagreb).Format("2.1.2006. u 15:04")
	data.IzdanoSat = izdano.Unix() / 3600
	postaje := h.postaje(r.Context())
	data.Letve = h.opisiLetve(postaje, letve)
	data.Bliski = BliziDosezi
	_, dnevne, _ := c.Dnevno()
	tude := map[string]map[string]map[int64]TudaVrijednost{}
	for _, t := range tudiIzvori {
		tude[t.izvor] = c.Tude(t.izvor, izdano)
	}
	var ciljevi []int64
	data.Dani, ciljevi = daniPregleda(izdano)
	for i := range data.Letve {
		kod := data.Letve[i].Kod
		poIzvoru := map[string]map[int64]TudaVrijednost{}
		for izvor, sve := range tude {
			poIzvoru[izvor] = sve[kod]
		}
		data.Letve[i].Dani = celijeDana(kod, letve[i], ciljevi, dnevne[kod], poIzvoru, postaje[kod])
		for k := range data.Letve[i].Dani {
			d := &data.Letve[i].Dani[k]
			d.Naslov = data.Dani[k]
			if len(d.Tude) > 0 {
				data.ImaTudih = true
			}
			if d.Cm != "" || len(d.Tude) > 0 {
				data.Letve[i].ImaTermina = true
			}
		}
		for _, v := range data.Letve[i].Vrijednosti {
			if v.Ima {
				data.Letve[i].ImaTermina = true
			}
		}
	}
	data.Tablice = poVodama(data.Letve, postaje)
	data.Izdaje = h.centar(u)
	data.Profili = uzduzniProfili(postaje, letve, dnevne, izdano,
		h.mjerenoUnatrag(r.Context(), postaje, letve, izdano), h.usca(r.Context(), postaje, letve))
	if len(data.Profili) == 0 {
		data.BezProfila = "Za uzdužni profil treba barem dvije letve s poznatom " +
			"stacionažom i kotom nule u novom visinskom sustavu."
	}
	return data
}

func (h *PrognozeHandler) uvoznik() *javnivodostaji.Uvoznik {
	if h.javni == nil {
		return nil
	}
	return h.javni()
}

func trajanjeKruga(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d s", int(d.Seconds()))
	}
	return fmt.Sprintf("%d min %d s", int(d.Minutes()), int(d.Seconds())%60)
}

// Generiraj pokreće cijeli krug rukom, kao što ga poslužitelj pokreće svaki
// sat: preuzme vodostaje svih povezanih letvi, tuđe prognoze, pa izračuna i
// zapiše našu. Krug ide u pozadini, jer traje i po minutu; stranica se sama
// osvježi kad prođe. Dok jedan krug traje, drugi se ne pokreće.
func (h *PrognozeHandler) Generiraj(w http.ResponseWriter, r *http.Request) {
	u := h.uvoznik()
	if u == nil {
		redirectWith(w, r, "/prognoze#generiraj", "error", "Preuzimanje vodostaja nije uključeno na ovom čvoru.")
		return
	}
	if u.UTijeku() {
		redirectWith(w, r, "/prognoze#generiraj", "success", "Krug preuzimanja već traje.")
		return
	}
	go func() {
		ctx, otkazi := context.WithTimeout(context.Background(), 15*time.Minute)
		defer otkazi()
		u.PreuzmiSve(ctx)
	}()
	redirectWith(w, r, "/prognoze#generiraj", "success", "Pokrenuto: preuzimanje vodostaja svih letvi, tuđih prognoza i izračun naše.")
}

// NapredakJSON daje stanje kruga koji traje, za traku napretka na stranici.
func (h *PrognozeHandler) NapredakJSON(w http.ResponseWriter, r *http.Request) {
	u := h.uvoznik()
	if u == nil {
		http.Error(w, "nema kruga preuzimanja", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(u.Napredak())
}

func (h *PrognozeHandler) iscrtaj(w http.ResponseWriter, data PrognozePageData) {
	if err := h.tmpl.ExecuteTemplate(w, "prognoze.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// opisiLetve dodaje ono što u bazi prognoza ne stoji: kako se letva zove, na
// kojoj je vodi i gdje joj je stranica.
func (h *PrognozeHandler) postaje(ctx context.Context) map[string]models.Station {
	popis := map[string]models.Station{}
	if h.stations == nil {
		return popis
	}
	if sve, err := h.stations.ListStations(ctx, "", "", "", false); err == nil {
		for _, st := range sve {
			popis[st.Code] = st
		}
	}
	return popis
}

func (h *PrognozeHandler) opisiLetve(popis map[string]models.Station, letve []PregledLetve) []LetvaPrognoze {
	out := make([]LetvaPrognoze, 0, len(letve))
	for _, l := range letve {
		ulaz := l.Racuna == "" && !l.UlazLanca && jeDnevniUlaz(l.Letva)
		pregledna := l.Racuna == "" && !l.UlazLanca && !ulaz && jePregledna(l.Letva)
		_, tudi := prognoza.VrhoviSTudomPrognozom[l.Letva]
		sadaCm, imaSadaCm := l.Sada["vodostaj"]
		sadaQ, imaSadaQ := l.Sada["protok"]
		red := LetvaPrognoze{
			Kod: l.Letva, Naziv: l.Letva, Racuna: l.Racuna, Doseg: l.Doseg,
			Vrh: l.Racuna == "" && !ulaz && !pregledna, Ulaz: ulaz, Pregledna: pregledna,
			TudiVrh: l.Racuna == "" && tudi,
			SadaCm:  uVelicini(l.Sada, "vodostaj"), SadaQ: uVelicini(l.Sada, "protok"),
			Ulazi:   l.Ulazi,
			SadaCmV: ptr(sadaCm, imaSadaCm), SadaQV: ptr(sadaQ, imaSadaQ),
		}
		if st, ima := popis[l.Letva]; ima {
			red.Naziv, red.Voda, red.Stacionaza = st.Name, st.Watercourse, st.Stationing
			red.URL = "/readings/station/" + st.ID.String()
		}
		for _, d := range BliziDosezi {
			cm, imaCm := l.Po["vodostaj"][d]
			q, imaQ := l.Po["protok"][d]
			v := VrijednostPrognoze{DosegH: d, Ima: imaCm || imaQ}
			if imaCm {
				v.Cm, v.CmRaspon = brojHRf(cm.Vrijednost, 0), granice(cm)
				v.CmV = ptr(cm.Vrijednost, true)
				v.Slabija = !cm.BoljaOdPostojanosti
			}
			if imaQ {
				v.Q, v.QRaspon = brojHRf(q.Vrijednost, 0), granice(q)
				v.QV = ptr(q.Vrijednost, true)
				if !imaCm {
					v.Slabija = !q.BoljaOdPostojanosti
				}
			}
			red.Vrijednosti = append(red.Vrijednosti, v)
		}
		out = append(out, red)
	}
	return out
}

// granice ispisuje raspon uz prognozu.
func granice(v PregledVrijednost) string {
	return rasponUz(v.Vrijednost, v.Dolje, v.Gore)
}

// rasponUz piše raspon kao ±, kao i mađarska prognoza, kad je simetričan;
// kad nije — a nije ondje gdje je vrijednost prošla kroz krivulju protoka, pa
// je Botovu u centimetrima raspon ispod vrijednosti kraći nego iznad — piše
// obje granice, jer bi ± ondje lagao.
func rasponUz(v, dolje, gore float64) string {
	if gore <= dolje {
		return ""
	}
	d, g := math.Round(v)-math.Round(dolje), math.Round(gore)-math.Round(v)
	if math.Abs(d-g) <= 1 {
		return "±" + brojHRf(math.Max(d, g), 0)
	}
	return rasponHR(dolje, gore, 0)
}

func uVelicini(sada map[string]float64, velicina string) string {
	v, ima := sada[velicina]
	if !ima {
		return ""
	}
	return brojHRf(v, 0)
}

// PregledVrijednost je jedna prognozirana vrijednost na pregledu.
type PregledVrijednost struct {
	Vrijednost          float64
	Dolje, Gore         float64
	BoljaOdPostojanosti bool
}

// PregledLetve je prognoza jedne letve, po veličini pa po dosegu. Racuna kaže
// u kojoj veličini model doista radi; druga dolazi iz krivulje.
type PregledLetve struct {
	Letva     string
	Racuna    string
	Sada      map[string]float64
	Doseg     int
	Po        map[string]map[int]PregledVrijednost
	Satno     map[int64]PregledVrijednost // vodostaj po ciljnom satu, za dane na pregledu
	SatnoQ    map[int64]PregledVrijednost // protok po ciljnom satu
	UlazLanca bool                        // letva ulazi u neki pojas satnog lanca
	Ulazi     []string                    // letve iz kojih se ova računa
}

// Pregled čita najnovije izdanje: za svaku letvu vrijednost u satu izdavanja i
// na svakom dosegu, uz podatak pobjeđuje li ondje postojanost.
func (c *CitacPrognoza) Pregled() (time.Time, []PregledLetve, error) {
	if c == nil || c.db == nil {
		return time.Time{}, nil, nil
	}
	izdano, ima, err := prognoza.ZadnjeIzdanje(c.db)
	if err != nil || !ima {
		return time.Time{}, nil, err
	}
	sve, err := prognoza.Izdanje(c.db, izdano)
	if err != nil {
		return time.Time{}, nil, err
	}
	promasaji, err := prognoza.Promasaji(c.db)
	if err != nil {
		return time.Time{}, nil, err
	}
	pojasi, err := prognoza.SviPojasi(c.db)
	if err != nil {
		return time.Time{}, nil, err
	}

	ulazLanca := map[string]bool{}
	for _, ps := range pojasi {
		for _, p := range ps {
			for _, u := range p.Ulazi {
				ulazLanca[u.Letva] = true
			}
		}
	}
	redom := prognoza.Redom(pojasi)
	mjesto := map[string]int{}
	for i, l := range redom {
		mjesto[l] = i
	}
	out := make([]PregledLetve, 0, len(sve))
	for letva, niz := range sve {
		if len(niz) == 0 {
			continue
		}
		p := PregledLetve{Letva: letva, UlazLanca: ulazLanca[letva], Sada: map[string]float64{},
			Po: map[string]map[int]PregledVrijednost{}, Satno: map[int64]PregledVrijednost{},
			SatnoQ: map[int64]PregledVrijednost{}}
		if len(pojasi[letva]) > 0 {
			p.Racuna = pojasi[letva][0].Velicina
			for _, u := range pojasi[letva][0].Ulazi {
				p.Ulazi = append(p.Ulazi, u.Letva)
			}
		}
		for _, i := range niz {
			d := int(i.Ciljni - izdano)
			if d > p.Doseg {
				p.Doseg = d
			}
			if d == 0 {
				p.Sada[i.Velicina] = i.Vrijednost
			}
			// Je li prognoza bolja od postojanosti mjereno je u veličini u
			// kojoj model radi; krivulja to ne mijenja, samo preslikava.
			bolja := true
			if pr, ima := promasaji[letva][d]; ima {
				bolja = pr.BoljaOdPostojanosti()
			}
			if d > 0 {
				v := PregledVrijednost{Vrijednost: i.Vrijednost,
					Dolje: i.Dolje, Gore: i.Gore, BoljaOdPostojanosti: bolja}
				if i.Velicina == "vodostaj" {
					p.Satno[i.Ciljni] = v
				} else {
					p.SatnoQ[i.Ciljni] = v
				}
			}
			if !uDosezima(d) {
				continue
			}
			if p.Po[i.Velicina] == nil {
				p.Po[i.Velicina] = map[int]PregledVrijednost{}
			}
			p.Po[i.Velicina][d] = PregledVrijednost{Vrijednost: i.Vrijednost,
				Dolje: i.Dolje, Gore: i.Gore, BoljaOdPostojanosti: bolja}
		}
		out = append(out, p)
	}
	// Popis se čita kao lanac, kako voda teče. Letva koja u lancu ne stoji
	// ide na kraj, da red ostane razumljiv i kad se pojavi nešto novo.
	red := func(l string) int {
		if i, ima := mjesto[l]; ima {
			return i
		}
		return len(redom)
	}
	sort.Slice(out, func(i, j int) bool {
		if a, b := red(out[i].Letva), red(out[j].Letva); a != b {
			return a < b
		}
		return out[i].Letva < out[j].Letva
	})
	return time.Unix(izdano*3600, 0).UTC(), out, nil
}

func uDosezima(d int) bool {
	for _, x := range DoseziPregleda {
		if x == d {
			return true
		}
	}
	return false
}

// uzduzniProfili slaže profil za svaki tok na kojem ima dovoljno letvi.
// Rijeke se ne miješaju: Drava i Dunav imaju svoje kote i svoj nagib, a jedan
// crtež kroz obje pokazivao bi skok na ušću koji nije val nego spoj dvaju
// tokova.
//
// Uz prognozu po dosezima svaka letva nosi i niz po satu za klizač vremena:
// mjerenja unatrag, satni lanac do 96 h, dnevni model dalje. Peti i šesti dan
// na crtežu dolaze iz dnevnog modela, jer satni lanac dotle ne seže.
func uzduzniProfili(postaje map[string]models.Station, letve []PregledLetve,
	dnevne map[string][]prognoza.DnevnaIzdana, izdano time.Time,
	mjereno map[string]map[int]float64, usca map[string][]UsceUlaz) []*UzduzniProfil {
	poVodi := map[string][]LetvaProfila{}
	var redom []string
	izdanoH := izdano.Unix() / 3600
	for _, l := range letve {
		st, ima := postaje[l.Letva]
		if !ima || st.ZeroDatumNew == nil {
			continue
		}
		rkm, ok := hydro.ParseStationingKm(st.Stationing)
		if !ok {
			continue
		}
		lp := LetvaProfila{
			Letva: l.Letva, Naziv: st.Name, Rkm: rkm, KotaNule: *st.ZeroDatumNew,
			Cm: map[int]float64{}, Granice: map[int][2]float64{},
			Pragovi: map[string]float64{}, Niz: map[int]float64{},
		}
		if v, ima := l.Sada["vodostaj"]; ima {
			lp.SadaCm, lp.ImaSada = v, true
			lp.Razina = razinaObrane(st, v)
		}
		for d, v := range l.Po["vodostaj"] {
			lp.Cm[d] = v.Vrijednost
			lp.Granice[d] = [2]float64{v.Dolje, v.Gore}
		}
		for h, v := range mjereno[l.Letva] {
			lp.Niz[h] = v
		}
		for t, v := range l.Satno {
			if h := int(t - izdanoH); h > 0 {
				lp.Niz[h] = v.Vrijednost
			}
		}
		// Pregled po dosezima ne nosi 96 h, ali satni niz nosi: satni lanac
		// ima prednost pred dnevnim modelom svugdje gdje seže.
		for _, d := range dosezniProfila {
			if _, ima := lp.Cm[d]; ima {
				continue
			}
			if v, ima := l.Satno[izdanoH+int64(d)]; ima {
				lp.Cm[d] = v.Vrijednost
				lp.Granice[d] = [2]float64{v.Dolje, v.Gore}
				continue
			}
			if v, ima := dnevniU(dnevne[l.Letva], izdanoH+int64(d)); ima {
				lp.Cm[d] = v.Vrijednost
				lp.Granice[d] = [2]float64{v.Dolje, v.Gore}
			}
		}
		for h := 1; h <= prognoza.DnevniDosezi*24; h++ {
			if _, ima := lp.Niz[h]; ima {
				continue
			}
			if v, ima := dnevniU(dnevne[l.Letva], izdanoH+int64(h)); ima {
				lp.Niz[h] = v.Vrijednost
			}
		}
		for kljuc, prag := range map[string]models.Threshold{
			"prep": st.Prep, "regular": st.Regular, "emerg": st.Emergency} {
			if prag.IsUsable() {
				lp.Pragovi[kljuc] = float64(*prag.Cm)
			}
		}
		voda := st.Watercourse
		if voda == "" {
			voda = "ostalo"
		}
		if _, bilo := poVodi[voda]; !bilo {
			redom = append(redom, voda)
		}
		poVodi[voda] = append(poVodi[voda], lp)
	}
	// Kraj pritoke dobije vrijednosti najbliže letve glavnog toka: vodostaj
	// Drave na ušću diktira Dunav, pa se krivulja Drave provuče do ušća s
	// promjenom Aljmaša. To nije letva — natpisa i oznake nema, ali crta,
	// raspon i klizač do ušća idu.
	svaka := map[string]LetvaProfila{}
	for _, ls := range poVodi {
		for _, l := range ls {
			svaka[l.Letva] = l
		}
	}
	for voda, us := range usca {
		for _, u := range us {
			src, ima := svaka[u.Letva]
			if !ima || u.Letva == "" || !src.ImaSada {
				continue
			}
			kopija := src
			kopija.Letva, kopija.Naziv, kopija.Rkm, kopija.Usce = "usce:"+src.Letva, u.Naziv+" · "+src.Naziv, u.Rkm, true
			kopija.Pragovi = map[string]float64{} // pragovi druge rijeke ovdje ne vrijede
			poVodi[voda] = append(poVodi[voda], kopija)
		}
	}
	var out []*UzduzniProfil
	for _, voda := range redom {
		if p := crtajUzduzni(voda, poVodi[voda], usca[voda]); p != nil {
			out = append(out, p)
		}
	}
	return out
}

// usca nalazi ušća za svaki tok koji ima letve na pregledu: pritoke koje se
// u njega ulijevaju, s kilometrom ušća iz geometrije ili iz opisa ušća u
// registru. Na tok ide oznaka s vodostajem zadnje letve pritoke, a na pritoku
// oznaka na njezinu kraju s vodostajem najbliže letve toka — tako Osijek i
// Aljmaš stoje jedan uz drugoga ondje gdje se Drava i Dunav sastaju.
func (h *PrognozeHandler) usca(ctx context.Context, postaje map[string]models.Station,
	letve []PregledLetve) map[string][]UsceUlaz {
	if h.watercourses == nil {
		return nil
	}
	sve, err := h.watercourses.ListWatercourses(ctx, "", "", false)
	if err != nil {
		return nil
	}
	poImenu := map[string]models.Watercourse{}
	for _, w := range sve {
		poImenu[w.Name] = w
	}
	type letvaToka struct {
		sifra, naziv string
		rkm, sada    float64
	}
	poToku := map[string][]letvaToka{}
	for _, l := range letve {
		st, ima := postaje[l.Letva]
		if !ima {
			continue
		}
		rkm, ok := hydro.ParseStationingKm(st.Stationing)
		if !ok {
			continue
		}
		sada, ima := l.Sada["vodostaj"]
		if !ima {
			continue
		}
		poToku[st.Watercourse] = append(poToku[st.Watercourse], letvaToka{l.Letva, st.Name, rkm, sada})
	}
	geometrije := map[string]*geoTok{}
	geometrija := func(code string) (geoTok, bool) {
		if g, bilo := geometrije[code]; bilo {
			return *g, g.linija != nil
		}
		g := geoTok{}
		if b, err := h.watercourses.GetWatercourseGeometry(ctx, code); err == nil {
			g, _ = citajGeoTok(b)
		}
		geometrije[code] = &g
		return g, g.linija != nil
	}
	kilometar := func(pritoka, glavni models.Watercourse) (float64, bool) {
		if gp, ok := geometrija(pritoka.Code); ok {
			if gg, ok := geometrija(glavni.Code); ok {
				if rkm, ok := usceNaToku(gp, gg); ok {
					return rkm, true
				}
			}
		}
		return kilometarIzOpisa(pritoka.Mouth)
	}
	vodostaj := func(l letvaToka) string {
		ime, _ := imeIDrzava(l.naziv) // „Letenye (Mađarska)” → „Letenye”; država je u oblačiću letve
		return ime + " " + brojHRf(l.sada, 0) + " cm"
	}
	out := map[string][]UsceUlaz{}
	for ime := range poToku {
		glavni, ima := poImenu[ime]
		if !ima {
			continue
		}
		for _, w := range sve {
			if w.FlowsInto != ime || w.Name == ime {
				continue
			}
			rkm, ok := kilometar(w, glavni)
			if !ok {
				continue
			}
			u := UsceUlaz{Naziv: "ušće " + genitiv(w.Name), Rkm: rkm}
			if pl := poToku[w.Name]; len(pl) > 0 {
				u.Vezano = true
				zadnja := pl[0]
				for _, l := range pl[1:] {
					if l.rkm < zadnja.rkm {
						zadnja = l
					}
				}
				u.Tekst = vodostaj(zadnja)
			}
			out[ime] = append(out[ime], u)
			if _, ima := poToku[w.Name]; ima {
				gl := poToku[ime]
				najbliza := gl[0]
				for _, l := range gl[1:] {
					if math.Abs(l.rkm-rkm) < math.Abs(najbliza.rkm-rkm) {
						najbliza = l
					}
				}
				out[w.Name] = append(out[w.Name], UsceUlaz{Naziv: "ušće u " + akuzativ(ime), Rkm: 0,
					Tekst: vodostaj(najbliza), Vezano: true, Letva: najbliza.sifra})
			}
		}
	}
	return out
}

// mjerenoUnatrag čita vodostaje zadnjih dva dana za letve na profilu, po
// satu prema izdanju: klizač vremena njima pokazuje odakle je val došao. Kad
// očitanja nisu na puni sat, uzima se najbliže punom satu.
func (h *PrognozeHandler) mjerenoUnatrag(ctx context.Context, postaje map[string]models.Station,
	letve []PregledLetve, izdano time.Time) map[string]map[int]float64 {
	if h.readings == nil {
		return nil
	}
	out := map[string]map[int]float64{}
	od := izdano.Add(time.Duration(KlizacOd) * time.Hour).Add(-30 * time.Minute)
	for _, l := range letve {
		st, ima := postaje[l.Letva]
		if !ima || st.ZeroDatumNew == nil {
			continue
		}
		rs, err := h.readings.List(ctx, repository.ReadingFilter{StationID: st.ID.String(), From: od, To: izdano})
		if err != nil {
			continue
		}
		poSatu := map[int]float64{}
		odmak := map[int]time.Duration{}
		for _, rd := range rs {
			if rd.LevelCm == nil {
				continue
			}
			razmak := rd.MeasuredAt.Sub(izdano)
			sat := int(math.Round(razmak.Hours()))
			if sat >= 0 || sat < KlizacOd {
				continue
			}
			o := (razmak - time.Duration(sat)*time.Hour).Abs()
			if prije, bio := odmak[sat]; !bio || o < prije {
				poSatu[sat], odmak[sat] = float64(*rd.LevelCm), o
			}
		}
		if len(poSatu) > 0 {
			out[l.Letva] = poSatu
		}
	}
	return out
}

// razinaObrane je najviša faza obrane čiji je prag dosegnut.
func razinaObrane(st models.Station, cm float64) string {
	razina := ""
	for _, p := range []struct {
		ime  string
		prag models.Threshold
	}{{"prep", st.Prep}, {"regular", st.Regular}, {"emerg", st.Emergency}, {"crit", st.State}} {
		if p.prag.IsUsable() && cm >= float64(*p.prag.Cm) {
			razina = p.ime
		}
	}
	return razina
}

var daniUTjednu = []string{"ned", "pon", "uto", "sri", "čet", "pet", "sub"}

// daniPregleda su termini stupaca po danima: prvih šest 07 h poslije izdanja.
func daniPregleda(izdano time.Time) ([]string, []int64) {
	lok := izdano.In(models.Zagreb)
	jutro := time.Date(lok.Year(), lok.Month(), lok.Day(), 7, 0, 0, 0, models.Zagreb)
	if !jutro.After(lok) {
		jutro = jutro.AddDate(0, 0, 1)
	}
	var naslovi []string
	var sati []int64
	for k := 0; k < prognoza.DnevniDosezi; k++ {
		d := jutro.AddDate(0, 0, k)
		naslovi = append(naslovi, daniUTjednu[d.Weekday()]+" "+d.Format("2.1."))
		sati = append(sati, d.UTC().Unix()/3600)
	}
	return naslovi, sati
}

func jeDnevniUlaz(letva string) bool {
	for _, u := range prognoza.DnevniUlazi() {
		if u == letva {
			return true
		}
	}
	return false
}

// celijeDana slaže dane jedne letve: za svaki termin vrijednost iz modela koji
// je ondje točniji, raspon, mađarsku prognozu i fazu obrane.
func celijeDana(letva string, l PregledLetve, ciljevi []int64, dnevne []prognoza.DnevnaIzdana,
	tude map[string]map[int64]TudaVrijednost, st models.Station) []CelijaDana {
	odDana := prognoza.DnevnaOdDana[letva]
	out := make([]CelijaDana, len(ciljevi))
	for k, t := range ciljevi {
		c := &out[k]
		var v, dolje, gore float64
		ima := false
		satni, imaSatni := l.Satno[t]
		dnevni, imaDnevni := dnevniU(dnevne, t)
		var q PregledVrijednost
		imaQ := false
		switch {
		case imaDnevni && odDana > 0 && k+1 >= odDana:
			v, dolje, gore, ima, c.Dnevna = dnevni.Vrijednost, dnevni.Dolje, dnevni.Gore, true, true
			q, imaQ = dnevniQU(dnevne, t)
		case imaSatni:
			v, dolje, gore, ima = satni.Vrijednost, satni.Dolje, satni.Gore, true
			c.Slabija = !satni.BoljaOdPostojanosti
			q, imaQ = l.SatnoQ[t]
		case imaDnevni:
			v, dolje, gore, ima, c.Dnevna = dnevni.Vrijednost, dnevni.Dolje, dnevni.Gore, true, true
			q, imaQ = dnevniQU(dnevne, t)
		}
		if imaQ {
			c.Q = brojHRf(q.Vrijednost, 0)
			c.QV = ptr(q.Vrijednost, true)
			c.QRaspon = rasponUz(q.Vrijednost, q.Dolje, q.Gore)
		}
		if ima {
			c.Cm = brojHRf(v, 0)
			c.CmV = ptr(v, true)
			c.Raspon = rasponUz(v, dolje, gore)
			c.Razina = razinaObrane(st, v)
			if g := razinaObrane(st, gore); g != c.Razina {
				c.Moguce = g
			}
		}
		for _, iz := range tudiIzvori {
			v, ima := tude[iz.izvor][t]
			if !ima {
				continue
			}
			tc := TudaCelija{Oznaka: iz.oznaka, Klasa: iz.klasa, Naslov: iz.naslov, Cm: brojHRf(v.Cm, 0), CmV: v.Cm}
			if v.PlusMin > 0 {
				tc.Raspon = "±" + brojHRf(v.PlusMin, 0)
			}
			c.Tude = append(c.Tude, tc)
		}
	}
	return out
}

// dnevniU procjenjuje dnevnu prognozu u zadanom satu. Dnevna vrijednost je
// srednjak 24 sata, pa se pripisuje njihovoj sredini, a između dviju sredina
// ide pravocrtno.
func dnevniU(dnevne []prognoza.DnevnaIzdana, t int64) (PregledVrijednost, bool) {
	for i := 0; i+1 < len(dnevne); i++ {
		a, b := dnevne[i], dnevne[i+1]
		ca, cb := a.Ciljni-12, b.Ciljni-12
		if t < ca || t > cb || cb == ca {
			continue
		}
		u := float64(t-ca) / float64(cb-ca)
		ip := func(x, y float64) float64 { return x + u*(y-x) }
		return PregledVrijednost{Vrijednost: ip(a.Vrijednost, b.Vrijednost),
			Dolje: ip(a.Dolje, b.Dolje), Gore: ip(a.Gore, b.Gore), BoljaOdPostojanosti: true}, true
	}
	return PregledVrijednost{}, false
}

func jePregledna(letva string) bool {
	for _, l := range prognoza.PregledneLetve {
		if l == letva {
			return true
		}
	}
	return false
}

// poVodama dijeli pregled u tablice po vodi — Dunav, Drava s Murom, pa
// pritoke — i u svakoj slaže letve od uzvodne prema nizvodnoj. Letve u lancu
// već dolaze redom toka. Vrh lanca stavlja se neposredno ispred prve letve
// kojoj je ulaz; letva koja je samo za pregled ili ulaz dnevnog modela umeće
// se po riječnom kilometru ispred prve nizvodnije na istoj vodi, a bez
// kilometra na početak. Pritoke idu voda po voda.
func poVodama(letve []LetvaPrognoze, postaje map[string]models.Station) []TablicaPrognoza {
	skupina := func(voda string) int {
		switch voda {
		case "Dunav":
			return 0
		case "Drava", "Mura":
			return 1
		}
		return 2
	}
	naslovi := []string{"Dunav", "Drava i Mura", "Pritoke"}
	var lanac, vrhovi, izvan [3][]LetvaPrognoze
	for _, l := range letve {
		g := skupina(l.Voda)
		switch {
		case l.Pregledna || l.Ulaz:
			izvan[g] = append(izvan[g], l)
		case l.Racuna == "":
			vrhovi[g] = append(vrhovi[g], l)
		default:
			lanac[g] = append(lanac[g], l)
		}
	}
	umetni := func(redovi []LetvaPrognoze, i int, x LetvaPrognoze) []LetvaPrognoze {
		return append(redovi[:i], append([]LetvaPrognoze{x}, redovi[i:]...)...)
	}
	rkm := func(l LetvaPrognoze) (float64, bool) {
		return hydro.ParseStationingKm(postaje[l.Kod].Stationing)
	}
	// Više vrhova istog cilja ide redom ulaza u lancu: glavni tok prvi,
	// pritoka iza njega — HE Dubrava, pa Mura, pa Botovo.
	redUlaza := func(kod string) int {
		for _, r := range letve {
			for i, u := range r.Ulazi {
				if u == kod {
					return i
				}
			}
		}
		return 0
	}
	var out []TablicaPrognoza
	for g := range naslovi {
		redovi := lanac[g]
		sort.SliceStable(vrhovi[g], func(i, j int) bool { return redUlaza(vrhovi[g][i].Kod) < redUlaza(vrhovi[g][j].Kod) })
		for _, v := range vrhovi[g] {
			mjesto := len(redovi)
		trazi:
			for i, r := range redovi {
				for _, u := range r.Ulazi {
					if u == v.Kod {
						mjesto = i
						break trazi
					}
				}
			}
			redovi = umetni(redovi, mjesto, v)
		}
		for _, x := range izvan[g] {
			km, imaKm := rkm(x)
			mjesto := 0
			if imaKm {
				mjesto = len(redovi)
				for i, r := range redovi {
					if r.Voda != x.Voda {
						continue
					}
					if k, ok := rkm(r); ok && k < km {
						mjesto = i
						break
					}
				}
			}
			redovi = umetni(redovi, mjesto, x)
		}
		if g == 2 {
			// Pritoke voda po voda, a unutar vode redom kojim su već složene.
			sort.SliceStable(redovi, func(i, j int) bool { return redovi[i].Voda < redovi[j].Voda })
		}
		if len(redovi) > 0 {
			out = append(out, TablicaPrognoza{Naslov: naslovi[g], Letve: redovi})
		}
	}
	return out
}

// dnevniQU je protok dnevne prognoze u zadanom satu, pravocrtno između
// sredina dvaju dana; samo gdje oba dana imaju protok iz krivulje.
func dnevniQU(dnevne []prognoza.DnevnaIzdana, t int64) (PregledVrijednost, bool) {
	for i := 0; i+1 < len(dnevne); i++ {
		a, b := dnevne[i], dnevne[i+1]
		ca, cb := a.Ciljni-12, b.Ciljni-12
		if t < ca || t > cb || cb == ca {
			continue
		}
		if !a.ImaQ || !b.ImaQ {
			return PregledVrijednost{}, false
		}
		u := float64(t-ca) / float64(cb-ca)
		ip := func(x, y float64) float64 { return x + u*(y-x) }
		return PregledVrijednost{Vrijednost: ip(a.Q, b.Q), Dolje: ip(a.QDolje, b.QDolje),
			Gore: ip(a.QGore, b.QGore)}, true
	}
	return PregledVrijednost{}, false
}

func ptr(v float64, ima bool) *float64 {
	if !ima {
		return nil
	}
	return &v
}
