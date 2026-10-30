package arhiva

// Izdavanje arhive: mapa .cop paketa s katalogom.
//
// Izdanje se ne upisuje rukom nego raste samo kad se sadržaj promijenio.
// Otisak paketa računa se preko podataka, ne preko manifesta — ne ovisi ni o
// vremenu ni o izdavaču — pa dva čvora koja imaju isto stanje dođu do istog
// broja izdanja bez ikakvog dogovora.
//
// Ovo je ovdje, a ne u naredbenom retku, jer isti posao radi i stranica. Dva
// izvedbe bi se s vremenom razišle, a razlika bi se vidjela tek kad dva čvora
// isti sadržaj nazovu različitim izdanjem — i onda više nitko ne bi znao koje
// je pravo.

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"gocop/internal/trajno"
)

// ImeKataloga je datoteka koja u mapi izdanja kaže što je izdano.
const ImeKataloga = "katalog.json"

// UPaketu je jedan redak kataloga.
type UPaketu struct {
	Letva    string `json:"letva"`
	Izdanje  int    `json:"izdanje"`
	Otisak   string `json:"otisak"`
	Datoteka string `json:"datoteka"`
	Nizova   int    `json:"nizova"`
	Zapisa   int    `json:"zapisa"`
	Od       string `json:"od"`
	Do       string `json:"do"`
	Bajtova  int64  `json:"bajtova"`
}

// Katalog je popis izdanih paketa. Čvor koji dobije mapu po njemu vidi što
// ima i je li novije od onoga što drži.
type Katalog struct {
	Inacica int       `json:"inacica"`
	Nastalo time.Time `json:"nastalo"`
	Izdao   string    `json:"izdao"`
	Paketi  []UPaketu `json:"paketi"`
}

// PoLetvi daje katalog kao mapu, jer se po letvi i traži.
func (k Katalog) PoLetvi() map[string]UPaketu {
	m := make(map[string]UPaketu, len(k.Paketi))
	for _, p := range k.Paketi {
		m[p.Letva] = p
	}
	return m
}

// IzdanjeZa kaže koje je zadnje izdano izdanje te letve. Letva koje u katalogu
// nema nije nikad izdana, pa je sljedeće izdanje prvo.
func (k Katalog) IzdanjeZa(letva string) int {
	for _, p := range k.Paketi {
		if p.Letva == letva {
			return p.Izdanje
		}
	}
	return 0
}

// UcitajKatalog čita prošlo izlaganje. Kataloga nema pri prvom izdavanju i to
// nije greška — tada su sve letve nove. Katalog koji se ne čita također ne ruši
// izdavanje, ali se javlja: inače bi tiho sve letve postale "nove" i izdanja bi
// se vratila na jedan.
func UcitajKatalog(mapa string) (Katalog, error) {
	var k Katalog
	b, err := os.ReadFile(filepath.Join(mapa, ImeKataloga))
	if err != nil {
		if os.IsNotExist(err) {
			return k, nil
		}
		return k, err
	}
	if err := json.Unmarshal(b, &k); err != nil {
		return Katalog{}, fmt.Errorf("katalog se ne čita: %w", err)
	}
	return k, nil
}

// RedIzdanja je što se s jednom letvom dogodilo pri izdavanju.
type RedIzdanja struct {
	Letva    string
	Prije    int // 0 = nikad izdana
	Izdanje  int
	Otisak   string
	Novo     bool // sadržaj se promijenio, pa je izdano
	Nizova   int
	Zapisa   int
	Od, Do   string
	Bajtova  int64
	Datoteka string
	Greska   string
}

// IzvjestajIzdanja je pregled cijelog izdavanja.
type IzvjestajIzdanja struct {
	Katalog       Katalog
	Redci         []RedIzdanja
	Promijenjenih int
	Istih         int
	Preskocenih   int
	Zapisa        int64
	Bajtova       int64
	Probno        bool
	Mapa          string
}

// Izdaj sastavlja pakete za sve letve (ili samo za jednu) i osvježava katalog.
// Probno izdavanje sve izračuna ali ništa ne zapiše — tako se prije klika vidi
// što bi se promijenilo.
//
// Starije izdanje se ne briše, ali ne zato da bi ga netko preuzeo: paket je
// cjelovita izjava o letvi, pa svaki čvor uzima samo zadnje. Stara izdanja su
// ZAPIS — ako je netko u ožujku odlučivao po v2, treba v2 da bi se vidjelo što
// je tad imao pred sobom.
//
// Broj izdanja uzima se i iz kazala u knjizi (uMrezi; nil = samo katalog).
// Katalog u mapi paketa može biti stariji od onoga što je već izašlo: vraćen
// iz kopije, ili je mapa paketa zamijenjena. Isti broj tada bi dobio
// drukčiji sadržaj, a čvorovi koji taj broj već imaju preskočili bi paket
// bez traga. Zato novi broj dolazi iza najvećeg iz kataloga i kazala.
func Izdaj(db *sql.DB, uMapu, izdao, samo string, probno bool, uMrezi UMrezi, zapisi io.Writer) (IzvjestajIzdanja, error) {
	if zapisi == nil {
		zapisi = io.Discard
	}
	iz := IzvjestajIzdanja{Probno: probno, Mapa: uMapu}

	// Izdavanje čita arhivu i piše katalog. Dva istodobna oba pročitaju katalog
	// pa oba zapišu, i jedno se izgubi; uz to oba pišu isto *.novo ime, pa
	// jedan paket pregazi drugi usred pisanja. Probno ne piše ništa, ali i ono
	// mora vidjeti cjelovitu arhivu, pa i ono uzima bravu.
	if err := pripremiMapuIzdanja(uMapu, probno); err != nil {
		return iz, err
	}
	brava, err := Uzmi(uMapu, "izdavanje arhive", izdao)
	if err != nil {
		return iz, err
	}
	defer brava.Pusti()
	if bila, opis := brava.Preuzeta(); bila {
		fmt.Fprintf(zapisi, "preuzeta brava prekinutog posla: %s — provjeriti mapu izdanja\n", opis)
	}

	letve, err := LetveUArhivi(db, samo)
	if err != nil {
		return iz, err
	}
	if len(letve) == 0 {
		return iz, fmt.Errorf("arhiva nema nijednu letvu")
	}
	prijasnji, err := UcitajKatalog(uMapu)
	if err != nil {
		return iz, err
	}
	poLetvi := prijasnji.PoLetvi()
	letve = ispravneLetve(letve, &iz, zapisi)
	letve = bezIstihPoSlovima(letve, prijasnji.Paketi, &iz, zapisi)

	novi := Katalog{Inacica: PaketInacica, Nastalo: time.Now().UTC(), Izdao: izdao}
	for i, letva := range letve {
		javi(zapisi, "sastavljam "+letva, i, len(letve))
		z := zadnjeIzdanjeLetve(db, poLetvi, letva, uMrezi)
		prije, imaPrije := z.prije, z.imaPrije
		izdanje := z.broj()

		var b bytes.Buffer
		m, err := Izvezi(db, letva, izdanje, izdao, &b)
		if err != nil {
			fmt.Fprintf(zapisi, "  %-18s preskačem: %v\n", letva, err)
			// Letva koja se ne da izvesti ostaje u katalogu s onim što je
			// zadnje izdano. Da ispadne, sljedeće bi je izdavanje vidjelo kao
			// novu i vratilo na v1 — a pod tim imenom drugdje već stoji nešto
			// drugo.
			if imaPrije {
				novi.Paketi = append(novi.Paketi, prije)
			}
			iz.Redci = append(iz.Redci, RedIzdanja{Letva: letva, Prije: prije.Izdanje, Greska: err.Error()})
			iz.Preskocenih++
			continue
		}
		if z.isto(m.Otisak) {
			fmt.Fprintf(zapisi, "  %-18s v%-3d nepromijenjeno\n", letva, prije.Izdanje)
			novi.Paketi = append(novi.Paketi, prije)
			iz.Redci = append(iz.Redci, RedIzdanja{Letva: letva, Prije: prije.Izdanje, Izdanje: prije.Izdanje,
				Otisak: prije.Otisak, Nizova: prije.Nizova, Zapisa: prije.Zapisa,
				Od: prije.Od, Do: prije.Do, Bajtova: prije.Bajtova, Datoteka: prije.Datoteka})
			iz.Istih++
			continue
		}
		// Sadržaj je drukčiji, pa i izdanje mora biti — a manifest nosi broj
		// izdanja, što znači da se paket mora složiti iznova.
		if z.treba(izdanje, m.Otisak) {
			izdanje = max(izdanje, z.mreza) + 1
			b.Reset()
			if m, err = Izvezi(db, letva, izdanje, izdao, &b); err != nil {
				// Već izdanu letvu u katalog vraća zadrziPrijasnje. Letva
				// primljena paketom u katalogu još nema redak, a ovdje bi
				// dobila prazan: bez letve, izdanja i datoteke.
				fmt.Fprintf(zapisi, "  %-18s preskačem: %v\n", letva, err)
				iz.Redci = append(iz.Redci, RedIzdanja{Letva: letva, Prije: prije.Izdanje, Greska: err.Error()})
				iz.Preskocenih++
				continue
			}
		}
		ime := fmt.Sprintf("%s_v%d.cop", letva, izdanje)
		red := UPaketu{Letva: letva, Izdanje: izdanje, Otisak: m.Otisak, Datoteka: ime,
			Nizova: m.Nizova, Zapisa: m.Zapisa, Od: m.Od, Do: m.Do, Bajtova: int64(b.Len())}

		stanje := "novo"
		if imaPrije {
			stanje = fmt.Sprintf("v%d → v%d", prije.Izdanje, izdanje)
		}
		fmt.Fprintf(zapisi, "  %-18s v%-3d %-10s %8d zapisa  %s .. %s  %5.0f kB\n",
			letva, izdanje, stanje, m.Zapisa, m.Od, m.Do, float64(b.Len())/1e3)

		if !probno {
			if err := zapisiPaket(uMapu, ime, b.Bytes()); err != nil {
				return iz, err
			}
		}
		novi.Paketi = append(novi.Paketi, red)
		iz.Redci = append(iz.Redci, RedIzdanja{Letva: letva, Prije: prije.Izdanje, Izdanje: izdanje,
			Otisak: m.Otisak, Novo: true, Nizova: m.Nizova, Zapisa: m.Zapisa,
			Od: m.Od, Do: m.Do, Bajtova: int64(b.Len()), Datoteka: ime})
		iz.Promijenjenih++
	}

	novi.Paketi = zadrziPrijasnje(novi.Paketi, prijasnji.Paketi, zapisi)

	sort.Slice(novi.Paketi, func(a, b int) bool { return novi.Paketi[a].Letva < novi.Paketi[b].Letva })
	for _, p := range novi.Paketi {
		iz.Zapisa += int64(p.Zapisa)
		iz.Bajtova += p.Bajtova
	}
	iz.Katalog = novi
	javi(zapisi, "zapisujem katalog", len(letve), len(letve))

	if probno {
		return iz, nil
	}
	if err := zapisiKatalog(uMapu, novi); err != nil {
		return iz, err
	}
	return iz, nil
}

// UMrezi daje zadnje izdanje letve koje je već izašlo u mrežu, kako ga
// pamti kazalo u knjizi: broj i otisak; 0 kad ga nema.
type UMrezi func(letva string) (izdanje int, otisak string)

// zadnjeIzdanje je ono što se o letvi zna prije izdavanja: redak kataloga,
// primljeni paket i kazalo u knjizi
type zadnjeIzdanje struct {
	prije       UPaketu
	imaPrije    bool
	primljeno   *PrimljenoIzdanje
	mreza       int
	mrezaOtisak string
}

// zadnjeIzdanjeLetve skuplja što se o letvi zna. Letva koju je ovaj čvor
// primio paketom, a sam je još nije izdavao, nastavlja niz izdanja onoga od
// koga je primljena. Inače bi čvor koji preuzme izdavanje krenuo od v1, a
// čvorovi koji već drže v3 odbili bi „starije” izdanje i arhiva bi im stala.
func zadnjeIzdanjeLetve(db *sql.DB, poLetvi map[string]UPaketu, letva string, uMrezi UMrezi) zadnjeIzdanje {
	z := zadnjeIzdanje{}
	z.prije, z.imaPrije = poLetvi[letva]
	if !z.imaPrije {
		z.primljeno, _ = Primljeno(db, letva) // bez zapisa o primanju letva je nova
	}
	if uMrezi != nil {
		z.mreza, z.mrezaOtisak = uMrezi(letva)
	}
	return z
}

// broj je izdanje s kojim se letva prvo izvozi: iz kataloga, iz primljenog
// paketa ili, kad letve nema ni u jednom, iz kazala
func (z zadnjeIzdanje) broj() int {
	n := z.mreza
	switch {
	case z.imaPrije:
		n = z.prije.Izdanje
	case z.primljeno != nil:
		n = z.primljeno.Izdanje
	}
	return max(n, 1)
}

// sudar javlja da je pod tim brojem u mreži drukčiji sadržaj: paket s tim
// brojem nitko tko ga već ima ne bi uzeo
func (z zadnjeIzdanje) sudar(broj int, otisak string) bool {
	return z.mreza == broj && z.mrezaOtisak != otisak
}

// isto javlja da se sadržaj nije promijenio od zadnjeg izdanja u katalogu i
// da ga mreža pod tim brojem ne zna drukčijeg
func (z zadnjeIzdanje) isto(otisak string) bool {
	return z.imaPrije && otisak == z.prije.Otisak && !z.sudar(z.broj(), otisak)
}

// treba javlja da sadržaj traži novi broj izdanja
func (z zadnjeIzdanje) treba(broj int, otisak string) bool {
	return z.imaPrije || (z.primljeno != nil && otisak != z.primljeno.Otisak) || z.sudar(broj, otisak)
}

// zadrziPrijasnje vraća u novi katalog retke prijašnjeg čije letve ovo
// izdavanje nije izdalo.
//
// Katalog je popis onoga što je izdano, a ne onoga što arhiva trenutno ima.
// Redak se zato ne briše: izdavanje jedne letve ne dira ostale, a letva koje
// u arhivi više nema i dalje leži kao paket u mapi i drže je drugi čvorovi.
// Da redak ispadne, sljedeće bi izdavanje tu letvu vidjelo kao novu i vratilo
// je na v1 — pod imenom pod kojim drugdje stoji nešto drugo.
//
// Ispada samo redak bez letve. Takav je zapisivalo izdanje 0.0.37 kad letva
// primljena paketom nije dala paket s novim brojem: ne kaže ništa o izdanom,
// a objava ga je svaki krug pokušavala pročitati kao paket.
func zadrziPrijasnje(novi, prijasnji []UPaketu, zapisi io.Writer) []UPaketu {
	imamo := make(map[string]bool, len(novi))
	for _, p := range novi {
		imamo[p.Letva] = true
	}
	for _, p := range prijasnji {
		switch {
		case p.Letva == "":
			_, _ = fmt.Fprintln(zapisi, "  izbacujem iz kataloga prazan redak bez letve")
		case ProvjeriRedKataloga(p) != nil:
			_, _ = fmt.Fprintf(zapisi, "  izbacujem iz kataloga redak %q: %v\n", p.Letva, ProvjeriRedKataloga(p)) // dnevnik izdavanja, greška pisanja ne mijenja ishod
		case !imamo[p.Letva]:
			novi = append(novi, p)
		}
	}
	return novi
}

// ispravneLetve izbacuje letve čiji naziv nije upotrebljiv kao ime paketa
// (ProvjeriLetvu). Takva je mogla u arhivu ući paketom prije te provjere; ne
// izdaje se, a izdavanje se javlja kao preskočeno.
func ispravneLetve(letve []string, iz *IzvjestajIzdanja, zapisi io.Writer) []string {
	out := letve[:0:0]
	for _, l := range letve {
		if err := ProvjeriLetvu(l); err != nil {
			_, _ = fmt.Fprintf(zapisi, "  %-18q preskačem: %v\n", l, err) // dnevnik izdavanja, greška pisanja ne mijenja ishod
			iz.Redci = append(iz.Redci, RedIzdanja{Letva: l, Greska: err.Error()})
			iz.Preskocenih++
			continue
		}
		out = append(out, l)
	}
	return out
}

// bezIstihPoSlovima izbacuje letvu čiji se naziv od druge letve razlikuje
// samo po velikim i malim slovima. Na macOS-u i Windowsu obje bi pisale u
// istu datoteku paketa. Ostaje letva koja je već u katalogu; kad nijedna nije,
// ne izdaje se nijedna.
func bezIstihPoSlovima(letve []string, katalog []UPaketu, iz *IzvjestajIzdanja, zapisi io.Writer) []string {
	sk := skupinePoSlovima(letve, katalog)
	out := letve[:0:0]
	for _, l := range letve {
		if g := sk.letve[strings.ToLower(l)]; !sk.ostaje(l) {
			druga := g[0]
			if druga == l {
				druga = g[1]
			}
			razlog := fmt.Sprintf("druga letva ima isti naziv uz drukčija velika i mala slova (%s)", druga)
			_, _ = fmt.Fprintf(zapisi, "  %-18s preskačem: %s\n", l, razlog) // dnevnik izdavanja, greška pisanja ne mijenja ishod
			iz.Redci = append(iz.Redci, RedIzdanja{Letva: l, Greska: razlog})
			iz.Preskocenih++
			continue
		}
		out = append(out, l)
	}
	return out
}

// skupinaPoSlovima su letve istog naziva bez obzira na velika i mala slova,
// po nazivu malim slovima; letva iz kataloga je prva u svojoj skupini.
type skupinaPoSlovima struct {
	letve      map[string][]string
	izKataloga map[string]bool
}

func skupinePoSlovima(letve []string, katalog []UPaketu) skupinaPoSlovima {
	sk := skupinaPoSlovima{letve: map[string][]string{}, izKataloga: map[string]bool{}}
	for _, p := range katalog {
		if k := strings.ToLower(p.Letva); !sk.izKataloga[k] {
			sk.izKataloga[k] = true
			sk.letve[k] = []string{p.Letva}
		}
	}
	for _, l := range letve {
		k := strings.ToLower(l)
		if !slices.Contains(sk.letve[k], l) {
			sk.letve[k] = append(sk.letve[k], l)
		}
	}
	return sk
}

// ostaje: letva je sama u skupini, ili je ona iz kataloga.
func (sk skupinaPoSlovima) ostaje(l string) bool {
	k := strings.ToLower(l)
	g := sk.letve[k]
	return g[0] == l && (len(g) == 1 || sk.izKataloga[k])
}

// ProvjeriRedKataloga javlja je li redak kataloga upotrebljiv: letva po
// pravilu naziva i datoteka, ako je ima, kao obično ime u mapi paketa, bez
// mape ispred. Redak bez datoteke ostaje kakav jest (izdanje 0.0.37).
func ProvjeriRedKataloga(p UPaketu) error {
	if err := ProvjeriLetvu(p.Letva); err != nil {
		return err
	}
	d := p.Datoteka
	if d != "" && (filepath.Base(d) != d || strings.ContainsAny(d, `/\`) || strings.Trim(d, ".") == "") {
		return fmt.Errorf("datoteka %q nije obično ime u mapi paketa", p.Datoteka)
	}
	return nil
}

// zapisiPaket zapisuje paket pod imenom ime, samo izravno u mapu paketa.
func zapisiPaket(uMapu, ime string, b []byte) error {
	put := filepath.Join(uMapu, ime)
	if filepath.Dir(put) != filepath.Clean(uMapu) {
		return fmt.Errorf("paket %s ne stoji u mapi paketa", ime)
	}
	return ZapisiSaStrane(put, b)
}

// LetveUArhivi vraća letve koje imaju barem jedan niz, poredane.
func LetveUArhivi(db *sql.DB, samo string) ([]string, error) {
	q := `SELECT DISTINCT letva FROM nizovi`
	var args []any
	if samo != "" {
		q += ` WHERE letva = ?`
		args = append(args, samo)
	}
	q += ` ORDER BY letva`
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ZapisiSaStrane piše sa strane pa preimenuje: prekid usred pisanja ne smije
// ostaviti pola paketa pod imenom koje izgleda cjelovito. Datoteka se prije
// preimenovanja spusti na disk, a mapa poslije njega. Bez toga je nestanak
// struje nakon izdavanja mogao vratiti stari katalog, pa bi sljedeće
// izdavanje istim brojem izdanja nazvalo drukčiji sadržaj, a čvorovi koji
// već imaju taj broj ne bi ga uzeli.
func ZapisiSaStrane(put string, sadrzaj []byte) error {
	privremeno := put + ".novo"
	f, err := os.OpenFile(privremeno, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(sadrzaj)
	if err == nil {
		err = trajno.SpustiDatoteku(f)
	}
	if errZ := f.Close(); err == nil {
		err = errZ
	}
	if err == nil {
		err = os.Rename(privremeno, put)
	}
	if err == nil {
		trajno.SpustiMapu(filepath.Dir(put))
	}
	return err
}

func zapisiKatalog(mapa string, k Katalog) error {
	b, err := json.MarshalIndent(k, "", "  ")
	if err != nil {
		return err
	}
	return ZapisiSaStrane(filepath.Join(mapa, ImeKataloga), append(b, '\n'))
}

// SljedeceIzdanje sastavlja paket jedne letve s brojem koji joj po katalogu
// pripada: isti broj ako se sadržaj nije promijenio, sljedeći ako jest.
//
// Postoji zato što je izvoz s letvine stranice dosad uzimao broj iz URL-a, pa
// je čovjek mogao izdati vukovar_v1.cop s današnjim sadržajem — a drugi čvorovi
// već drže pravi v1 pod tim imenom. Broj sad odlučuje otisak, isto pravilo kao
// pri izdavanju cijele arhive, samo bez pisanja u mapu.
func SljedeceIzdanje(db *sql.DB, mapa, letva, izdao string, w io.Writer) (Manifest, error) {
	prije, err := izdanjeUKatalogu(mapa, letva)
	if err != nil {
		return Manifest{}, err
	}
	izdanje := prije
	if izdanje < 1 {
		izdanje = 1
	}

	var b bytes.Buffer
	m, err := Izvezi(db, letva, izdanje, izdao, &b)
	if err != nil {
		return m, err
	}
	if prije > 0 {
		if red, ima := poLetvi(mapa, letva); ima && red.Otisak != m.Otisak {
			b.Reset()
			if m, err = Izvezi(db, letva, prije+1, izdao, &b); err != nil {
				return m, err
			}
		}
	}
	_, err = w.Write(b.Bytes())
	return m, err
}

// izdanjeUKatalogu je zadnje izdanje letve u katalogu mape paketa; 0 kad
// mapa nije zadana ili letva još nije izdana. Mapa paketa koja je vjerojatno
// drugdje odbija se kao pri izdavanju: prazan katalog dao bi paketu prvo
// izdanje, a drugi čvorovi već drže v1 pod tim imenom.
func izdanjeUKatalogu(mapa, letva string) (int, error) {
	if mapa == "" {
		return 0, nil
	}
	if err := provjeriMapu(mapa, "pakete"); err != nil {
		return 0, err
	}
	k, err := UcitajKatalog(mapa)
	if err != nil {
		return 0, err
	}
	return k.IzdanjeZa(letva), nil
}

// poLetvi vraća zadnji izdani redak za letvu.
func poLetvi(mapa, letva string) (UPaketu, bool) {
	k, err := UcitajKatalog(mapa)
	if err != nil {
		return UPaketu{}, false
	}
	for _, p := range k.Paketi {
		if p.Letva == letva {
			return p, true
		}
	}
	return UPaketu{}, false
}
