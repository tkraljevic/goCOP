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
	"sort"
	"time"
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
func Izdaj(db *sql.DB, uMapu, izdao, samo string, probno bool, zapisi io.Writer) (IzvjestajIzdanja, error) {
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

	novi := Katalog{Inacica: PaketInacica, Nastalo: time.Now().UTC(), Izdao: izdao}
	for i, letva := range letve {
		javi(zapisi, "sastavljam "+letva, i, len(letve))
		prije, imaPrije := poLetvi[letva]
		izdanje := prije.Izdanje
		// Letva koju je ovaj čvor primio paketom, a sam je još nije izdavao,
		// nastavlja niz izdanja onoga od koga je primljena. Inače bi čvor koji
		// preuzme izdavanje krenuo od v1, a čvorovi koji već drže v3 odbili bi
		// „starije” izdanje i arhiva bi im stala.
		var primljeno *PrimljenoIzdanje
		if !imaPrije {
			if p, err := Primljeno(db, letva); err == nil && p != nil {
				primljeno = p
				izdanje = p.Izdanje
			}
		}
		if izdanje < 1 {
			izdanje = 1
		}

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
		if imaPrije && m.Otisak == prije.Otisak {
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
		if imaPrije || (primljeno != nil && m.Otisak != primljeno.Otisak) {
			if imaPrije {
				izdanje = prije.Izdanje + 1
			} else {
				izdanje = primljeno.Izdanje + 1
			}
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
			if err := ZapisiSaStrane(filepath.Join(uMapu, ime), b.Bytes()); err != nil {
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
		case !imamo[p.Letva]:
			novi = append(novi, p)
		}
	}
	return novi
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
		err = f.Sync()
	}
	if errZ := f.Close(); err == nil {
		err = errZ
	}
	if err == nil {
		err = os.Rename(privremeno, put)
	}
	if err == nil {
		spustiMapu(filepath.Dir(put))
	}
	return err
}

// spustiMapu spušta mapu na disk, da novo ime datoteke u njoj preživi
// nestanak struje. Windows mapu ne da spustiti, pa se greška ne javlja.
func spustiMapu(mapa string) {
	if d, err := os.Open(mapa); err == nil {
		_ = d.Sync()  // Windows mapu ne da spustiti, a datoteka je već na disku
		_ = d.Close() // mapa je otvorena samo za čitanje
	}
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
