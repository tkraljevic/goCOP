package arhiva

import (
	"archive/zip"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"
)

// Paket je prijenosni oblik historijata jedne letve — ono što se preda na USB
// ključu, pošalje e-poštom ili preuzme s drugog čvora.
//
// Unutra je ZIP: manifest i po jedan dio za svaku vrstu podatka. Vrijednosti su
// pakirane razlikama, jer je vrijeme gotovo pravilno a vodostaj se mijenja po
// koji centimetar — Batinin niz od 933.686 zapisa tako s 44 MB u bazi padne na
// pola megabajta.
//
// Šifriranja ovdje nema. Kad zatreba — a treba samo da izvođačev čvor može
// raznositi ono što ne smije čitati — cijeli se ZIP zamota u AEAD, pa se
// sakriju i imena dijelova. Zato format iznutra ostaje ovakav.
// PaketInacica 2 dodaje izvori.json. Bez njega je paket bio nepotpuna izjava:
// Ugradi na čvoru primatelju ponovno gradi spojeni niz, pa je isti paket na
// čvoru s drukčije postavljenim izvorima davao druge brojeve.
const PaketInacica = 2

// Manifest je ono što se o paketu zna prije nego se raspakira.
type Manifest struct {
	Inacica int       `json:"inacica"`
	Letva   string    `json:"letva"`
	Izdanje int       `json:"izdanje"`
	Nastalo time.Time `json:"nastalo"`
	Izdao   string    `json:"izdao"` // čvor koji je paket sastavio
	Otisak  string    `json:"otisak"`
	Nizova  int       `json:"nizova"`
	Zapisa  int       `json:"zapisa"`
	Od      string    `json:"od"`
	Do      string    `json:"do"`
}

// nizUPaketu je opis jednog niza. Mjerilo govori kojim je cijelim brojem
// vrijednost pomnožena pri pakiranju; 0 znači da se nije dala svesti na cijeli
// broj pa je zapisana kakva jest.
type nizUPaketu struct {
	Zona      string `json:"zona"`
	Sliv      string `json:"sliv"`
	Izvor     string `json:"izvor"`
	Velicina  string `json:"velicina"`
	Vrsta     string `json:"vrsta"`
	PoDanu    int    `json:"po_danu"`
	Od        string `json:"od"`
	Do        string `json:"do"`
	Zapisa    int    `json:"zapisa"`
	Otisak    string `json:"otisak"`
	Datoteke  string `json:"datoteke"`
	Osvjezeno string `json:"osvjezeno"`
	Napomena  string `json:"napomena"`
	Mjerilo   int    `json:"mjerilo"`
}

type krivuljaUPaketu struct {
	VrijediOd string            `json:"vrijedi_od"`
	VrijediDo string            `json:"vrijedi_do"`
	Izvor     string            `json:"izvor"`
	Napomena  string            `json:"napomena"`
	Odsjecci  []odsjecakUPaketu `json:"odsjecci"`
}

type odsjecakUPaketu struct {
	OdCm  int     `json:"od_cm"`
	DoCm  int     `json:"do_cm"`
	Oblik string  `json:"oblik"`
	P1    float64 `json:"p1"`
	P2    float64 `json:"p2"`
	P3    float64 `json:"p3"`
}

type profilUPaketu struct {
	Datum    string       `json:"datum"`
	Vodostaj int          `json:"vodostaj"`
	KotaNule float64      `json:"kota_nule"`
	PomakM   float64      `json:"pomak_m"`
	Tocke    [][2]float64 `json:"tocke"`
}

type promjenaUPaketu struct {
	Datum    string `json:"datum"`
	PomakCm  int    `json:"pomak_cm"`
	Izvor    string `json:"izvor"`
	Napomena string `json:"napomena"`
}

// dijelovi su imena u ZIP-u, uvijek istim redom — otisak se računa preko njih
// po tom redu, pa isti sadržaj daje isti otisak na svakom čvoru. Popis ovisi o
// inačici, jer bi inače stariji paketi pri provjeri ispali pokvareni.
var (
	dijeloviV1 = []string{"nizovi.json", "ocitanja.bin", "krivulje.json", "profili.json", "promjene.json"}
	dijeloviV2 = []string{"nizovi.json", "ocitanja.bin", "krivulje.json", "profili.json", "promjene.json", "izvori.json"}
)

// NajveceRaspakirano je granica zbroja svih dijelova paketa nakon
// raspakiravanja.
//
// Ulazni .cop ograničen je na 50 MB, ali ZIP može biti malen a raspakirati se u
// koliko god. Cijela arhiva od 39 letvi i deset milijuna zapisa stane u 5,7 MB
// zbijeno; najveća pojedina letva raspakirana je oko 40 MB. Sto megabajta je
// široko za svaku stvarnu letvu i usko za napad.
const NajveceRaspakirano = 100 << 20

// dopusteniDijelovi su imena koja paket smije sadržavati. Sve drugo se odbija:
// ime koje program ne čita ionako ne ulazi u otisak, pa bi bilo mjesto za
// prijevoz nečega što nitko ne gleda.
func dopusteniDijelovi() map[string]bool {
	d := map[string]bool{"manifest.json": true}
	for _, ime := range dijeloviV2 {
		d[ime] = true
	}
	return d
}

// raspakiraj čita dijelove paketa uz granice.
//
// Prije se svaki unos čitao s io.ReadAll bez ikakve granice, pa je malen paket
// mogao pojesti memoriju prije nego ijedna provjera dođe na red. Uz to su se
// imena uzimala u mapu, pa bi drugi manifest.json tiho nadjačao prvi.
func raspakiraj(z *zip.Reader) (map[string][]byte, error) {
	dopusteno := dopusteniDijelovi()
	var ukupno uint64
	for _, f := range z.File {
		if !dopusteno[f.Name] {
			return nil, fmt.Errorf("paket sadrži %q, a to nije dio .cop paketa", f.Name)
		}
		ukupno += f.UncompressedSize64
		if ukupno > NajveceRaspakirano {
			return nil, fmt.Errorf("paket se raspakirava u više od %d MB — odbijen prije čitanja",
				NajveceRaspakirano>>20)
		}
	}

	sadrzaj := map[string][]byte{}
	for _, f := range z.File {
		if _, vec := sadrzaj[f.Name]; vec {
			return nil, fmt.Errorf("paket sadrži %q dvaput", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		// Granica se ne oslanja na ono što ZIP o sebi tvrdi: prijavljena
		// veličina je podatak iz same datoteke i može lagati. Čita se jedan
		// bajt više od dopuštenog, pa se prekoračenje prepozna.
		b, err := io.ReadAll(io.LimitReader(rc, NajveceRaspakirano+1))
		rc.Close()
		if err != nil {
			return nil, err
		}
		if uint64(len(b)) > NajveceRaspakirano {
			return nil, fmt.Errorf("dio %q je veći nego što paket tvrdi", f.Name)
		}
		if uint64(len(b)) != f.UncompressedSize64 {
			return nil, fmt.Errorf("dio %q ima %d bajta, a paket tvrdi %d",
				f.Name, len(b), f.UncompressedSize64)
		}
		sadrzaj[f.Name] = b
	}
	if len(sadrzaj["manifest.json"]) == 0 {
		return nil, fmt.Errorf("paket nema manifest")
	}
	return sadrzaj, nil
}

func dijeloviZa(inacica int) []string {
	if inacica <= 1 {
		return dijeloviV1
	}
	return dijeloviV2
}

// izvorUPaketu su postavke jednog izvora onakve kakve su bile pri sastavljanju.
// Mapa NE putuje: to je putanja na disku onoga tko je paket složio i na drugom
// čvoru ne znači ništa.
type izvorUPaketu struct {
	Naziv    string  `json:"naziv"`
	Tocnost  float64 `json:"tocnost"`
	Red      int     `json:"red"`
	Ukljucen bool    `json:"ukljucen"`
	Napomena string  `json:"napomena"`
}

// mjeriloZa nalazi najmanji cijeli množitelj kojim se sve vrijednosti niza
// svode na cijeli broj bez gubitka. Vraća 0 kad se ne da — tada se vrijednosti
// zapisuju kakve jesu.
//
// Ne pretpostavlja se: koncentracija nanosa doista nosi dvije decimale
// (8,66 g/m³), a vodostaj nijednu. Krivo pogođeno mjerilo tiho bi zaokružilo
// mjerenje, a to je gore od većeg paketa.
func mjeriloZa(v []float64) int {
	for _, m := range []int{1, 10, 100, 1000} {
		dobro := true
		for _, x := range v {
			skalirano := x * float64(m)
			if math.Abs(skalirano) > 1<<52 {
				dobro = false
				break
			}
			if math.Abs(skalirano-math.Round(skalirano)) > 1e-6 {
				dobro = false
				break
			}
			// natrag mora dati isti broj
			if math.Round(skalirano)/float64(m) != x {
				dobro = false
				break
			}
		}
		if dobro {
			return m
		}
	}
	return 0
}

// zapisiNiz pakira jedan niz: broj zapisa, pa razlike vremena i vrijednosti.
// Vrijeme raste, pa je razlika nenegativna; vrijednost ide u obje strane, pa se
// zapisuje cik-cak kodiranjem.
func zapisiNiz(w io.Writer, vrijeme []int64, vrijednost []float64, mjerilo int) error {
	buf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(buf, uint64(len(vrijeme)))
	if _, err := w.Write(buf[:n]); err != nil {
		return err
	}
	var pt int64
	var pv int64
	for i := range vrijeme {
		n = binary.PutUvarint(buf, uint64(vrijeme[i]-pt))
		if _, err := w.Write(buf[:n]); err != nil {
			return err
		}
		pt = vrijeme[i]
		if mjerilo == 0 {
			var osam [8]byte
			binary.LittleEndian.PutUint64(osam[:], math.Float64bits(vrijednost[i]))
			if _, err := w.Write(osam[:]); err != nil {
				return err
			}
			continue
		}
		v := int64(math.Round(vrijednost[i] * float64(mjerilo)))
		n = binary.PutVarint(buf, v-pv)
		if _, err := w.Write(buf[:n]); err != nil {
			return err
		}
		pv = v
	}
	return nil
}

// citajNiz raspakira ono što je zapisiNiz zapisao.
func citajNiz(r io.ByteReader, mjerilo int) (vrijeme []int64, vrijednost []float64, err error) {
	broj, err := binary.ReadUvarint(r)
	if err != nil {
		return nil, nil, err
	}
	var pt, pv int64
	for i := uint64(0); i < broj; i++ {
		dt, err := binary.ReadUvarint(r)
		if err != nil {
			return nil, nil, err
		}
		pt += int64(dt)
		vrijeme = append(vrijeme, pt)
		if mjerilo == 0 {
			var osam [8]byte
			for j := range osam {
				b, err := r.ReadByte()
				if err != nil {
					return nil, nil, err
				}
				osam[j] = b
			}
			vrijednost = append(vrijednost, math.Float64frombits(binary.LittleEndian.Uint64(osam[:])))
			continue
		}
		dv, err := binary.ReadVarint(r)
		if err != nil {
			return nil, nil, err
		}
		pv += dv
		vrijednost = append(vrijednost, float64(pv)/float64(mjerilo))
	}
	return vrijeme, vrijednost, nil
}

// Izvezi sastavlja paket historijata jedne letve. Ne šalje se sve što je u
// arhivi: spoj je izveden iz nizova i gradi se pri ugradnji, pa bi u paketu
// bio dvije trećine tereta bez ijednog novog podatka.
func Izvezi(db *sql.DB, letva string, izdanje int, izdao string, w io.Writer) (Manifest, error) {
	m := Manifest{Inacica: PaketInacica, Letva: letva, Izdanje: izdanje,
		Nastalo: time.Now().UTC(), Izdao: izdao}

	nizovi, vrijeme, vrijednosti, err := ucitajNizove(db, letva)
	if err != nil {
		return m, err
	}
	if len(nizovi) == 0 {
		return m, fmt.Errorf("letva %q nema nizova u arhivi", letva)
	}
	krivulje, err := ucitajKrivulje(db, letva)
	if err != nil {
		return m, err
	}
	profili, err := ucitajProfile(db, letva)
	if err != nil {
		return m, err
	}
	promjene, err := ucitajPromjene(db, letva)
	if err != nil {
		return m, err
	}

	sadrzaj := map[string][]byte{}
	if sadrzaj["nizovi.json"], err = json.Marshal(nizovi); err != nil {
		return m, err
	}
	var vrijednostiBuf bajtovi
	for i := range nizovi {
		if err := zapisiNiz(&vrijednostiBuf, vrijeme[i], vrijednosti[i], nizovi[i].Mjerilo); err != nil {
			return m, err
		}
		m.Zapisa += nizovi[i].Zapisa
	}
	sadrzaj["ocitanja.bin"] = vrijednostiBuf
	if sadrzaj["krivulje.json"], err = json.Marshal(krivulje); err != nil {
		return m, err
	}
	if sadrzaj["profili.json"], err = json.Marshal(profili); err != nil {
		return m, err
	}
	if sadrzaj["promjene.json"], err = json.Marshal(promjene); err != nil {
		return m, err
	}
	// Samo izvori koje ova letva doista koristi — postavke tuđih izvora nisu
	// izjava o njoj i ne bi imale što raditi u njezinu paketu.
	izvori, err := izvoriZaNizove(db, nizovi)
	if err != nil {
		return m, err
	}
	if sadrzaj["izvori.json"], err = json.Marshal(izvori); err != nil {
		return m, err
	}

	m.Nizova = len(nizovi)
	m.Od, m.Do = razdoblje(nizovi)
	h := sha256.New()
	for _, ime := range dijeloviZa(m.Inacica) {
		h.Write(sadrzaj[ime])
	}
	m.Otisak = hex.EncodeToString(h.Sum(nil))

	z := zip.NewWriter(w)
	manifest, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return m, err
	}
	if err := upisiDio(z, "manifest.json", manifest); err != nil {
		return m, err
	}
	for _, ime := range dijeloviZa(m.Inacica) {
		if err := upisiDio(z, ime, sadrzaj[ime]); err != nil {
			return m, err
		}
	}
	return m, z.Close()
}

// bajtovi je io.Writer koji skuplja u memoriju; paket se ne piše u odgovor dok
// nije cijeli složen, jer greška usred pisanja ostavlja pola datoteke.
type bajtovi []byte

func (b *bajtovi) Write(p []byte) (int, error) {
	*b = append(*b, p...)
	return len(p), nil
}

// izvoriZaNizove vadi postavke izvora koje ova letva koristi, poredane po
// nazivu da isti sadržaj uvijek da isti otisak.
func izvoriZaNizove(db *sql.DB, nizovi []nizUPaketu) ([]izvorUPaketu, error) {
	treba := map[string]bool{}
	for _, n := range nizovi {
		treba[n.Izvor] = true
	}
	svi, err := Izvori(db)
	if err != nil {
		return nil, err
	}
	out := []izvorUPaketu{}
	for _, i := range svi {
		if treba[i.Naziv] {
			out = append(out, izvorUPaketu{Naziv: i.Naziv, Tocnost: i.Tocnost,
				Red: i.Red, Ukljucen: i.Ukljucen, Napomena: i.Napomena})
			delete(treba, i.Naziv)
		}
	}
	// Izvor kojeg tablica ne poznaje — arhiva otvorena samo za čitanje ne može
	// se dopuniti — opisuje se zadanim vrijednostima, da paket ipak kaže s čime
	// je složen. Prešutjeti ga značilo bi da primatelj ne zna ni to.
	for naziv := range treba {
		if strings.HasPrefix(naziv, "preracun-") {
			continue // preračun ne ulazi u red povjerenja nego uvijek na kraj
		}
		out = append(out, izvorUPaketu{Naziv: naziv, Tocnost: zadanaTocnost(naziv),
			Red: 900, Napomena: "izvor nije bio na popisu čvora koji je paket izdao"})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Naziv < out[b].Naziv })
	return out, nil
}

// RazlikaIzvora javlja u čemu se postavke ovog čvora razlikuju od onih s
// kojima je paket složen. Ne ispravlja ništa: izvori su zajednički svim
// letvama, pa bi paket jedne letve tiho promijenio brojeve na svim ostalima.
// Čovjek to mora vidjeti i odlučiti.
type RazlikaIzvora struct {
	Naziv        string
	Nepoznat     bool // čvor ga još ne zna
	Paket, Nas   Izvor
	Tocnost, Red bool
	Ukljucen     bool
}

// Vazna javlja mijenja li razlika brojeve. Napomena ih ne mijenja.
func (r RazlikaIzvora) Vazna() bool { return r.Nepoznat || r.Tocnost || r.Red || r.Ukljucen }

func RazlikeIzvora(db *sql.DB, s *Sadrzaj) ([]RazlikaIzvora, error) {
	if s == nil || len(s.Izvori) == 0 {
		return nil, nil
	}
	nasi, err := Izvori(db)
	if err != nil {
		return nil, err
	}
	po := map[string]Izvor{}
	for _, i := range nasi {
		po[i.Naziv] = i
	}
	var out []RazlikaIzvora
	for _, p := range s.Izvori {
		paket := Izvor{Naziv: p.Naziv, Tocnost: p.Tocnost, Red: p.Red, Ukljucen: p.Ukljucen, Napomena: p.Napomena}
		nas, ima := po[p.Naziv]
		if !ima {
			out = append(out, RazlikaIzvora{Naziv: p.Naziv, Nepoznat: true, Paket: paket})
			continue
		}
		r := RazlikaIzvora{Naziv: p.Naziv, Paket: paket, Nas: nas,
			Tocnost: nas.Tocnost != p.Tocnost, Red: nas.Red != p.Red, Ukljucen: nas.Ukljucen != p.Ukljucen}
		if r.Vazna() {
			out = append(out, r)
		}
	}
	return out, nil
}

func upisiDio(z *zip.Writer, ime string, sadrzaj []byte) error {
	f, err := z.Create(ime)
	if err != nil {
		return err
	}
	_, err = f.Write(sadrzaj)
	return err
}

func razdoblje(n []nizUPaketu) (od, do string) {
	for _, x := range n {
		if x.Od != "" && (od == "" || x.Od < od) {
			od = x.Od
		}
		if x.Do != "" && x.Do > do {
			do = x.Do
		}
	}
	return od, do
}

func ucitajNizove(db *sql.DB, letva string) ([]nizUPaketu, [][]int64, [][]float64, error) {
	rows, err := db.Query(`SELECT id, zona, sliv, izvor, velicina, vrsta, po_danu, od, do_,
		zapisa, otisak, datoteke, osvjezeno, napomena FROM nizovi WHERE letva = ? ORDER BY id`, letva)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	var nizovi []nizUPaketu
	var ids []int64
	for rows.Next() {
		var n nizUPaketu
		var id int64
		if err := rows.Scan(&id, &n.Zona, &n.Sliv, &n.Izvor, &n.Velicina, &n.Vrsta, &n.PoDanu,
			&n.Od, &n.Do, &n.Zapisa, &n.Otisak, &n.Datoteke, &n.Osvjezeno, &n.Napomena); err != nil {
			return nil, nil, nil, err
		}
		nizovi = append(nizovi, n)
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, err
	}

	vremena := make([][]int64, len(ids))
	vrijednosti := make([][]float64, len(ids))
	for i, id := range ids {
		v, err := db.Query(`SELECT vrijeme, vrijednost FROM ocitanja WHERE niz = ? ORDER BY vrijeme`, id)
		if err != nil {
			return nil, nil, nil, err
		}
		for v.Next() {
			var t int64
			var x float64
			if err := v.Scan(&t, &x); err != nil {
				v.Close()
				return nil, nil, nil, err
			}
			vremena[i] = append(vremena[i], t)
			vrijednosti[i] = append(vrijednosti[i], x)
		}
		v.Close()
		nizovi[i].Mjerilo = mjeriloZa(vrijednosti[i])
		nizovi[i].Zapisa = len(vremena[i])
	}
	return nizovi, vremena, vrijednosti, nil
}

func ucitajKrivulje(db *sql.DB, letva string) ([]krivuljaUPaketu, error) {
	rows, err := db.Query(`SELECT id, vrijedi_od, vrijedi_do, izvor, napomena
		FROM hq_krivulje WHERE letva = ? ORDER BY vrijedi_od`, letva)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []krivuljaUPaketu
	var ids []int64
	for rows.Next() {
		var k krivuljaUPaketu
		var id int64
		if err := rows.Scan(&id, &k.VrijediOd, &k.VrijediDo, &k.Izvor, &k.Napomena); err != nil {
			return nil, err
		}
		out = append(out, k)
		ids = append(ids, id)
	}
	for i, id := range ids {
		o, err := db.Query(`SELECT od_cm, do_cm, oblik, p1, p2, p3 FROM hq_odsjecci
			WHERE krivulja = ? ORDER BY od_cm`, id)
		if err != nil {
			return nil, err
		}
		for o.Next() {
			var s odsjecakUPaketu
			if err := o.Scan(&s.OdCm, &s.DoCm, &s.Oblik, &s.P1, &s.P2, &s.P3); err != nil {
				o.Close()
				return nil, err
			}
			out[i].Odsjecci = append(out[i].Odsjecci, s)
		}
		o.Close()
	}
	return out, nil
}

func ucitajProfile(db *sql.DB, letva string) ([]profilUPaketu, error) {
	rows, err := db.Query(`SELECT id, datum, vodostaj, kota_nule, pomak_m FROM profili
		WHERE letva = ? ORDER BY datum`, letva)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []profilUPaketu
	var ids []int64
	for rows.Next() {
		var p profilUPaketu
		var id int64
		if err := rows.Scan(&id, &p.Datum, &p.Vodostaj, &p.KotaNule, &p.PomakM); err != nil {
			return nil, err
		}
		out = append(out, p)
		ids = append(ids, id)
	}
	for i, id := range ids {
		t, err := db.Query(`SELECT stacionaza, visina FROM profil_tocke WHERE profil = ? ORDER BY stacionaza`, id)
		if err != nil {
			return nil, err
		}
		for t.Next() {
			var s, v float64
			if err := t.Scan(&s, &v); err != nil {
				t.Close()
				return nil, err
			}
			out[i].Tocke = append(out[i].Tocke, [2]float64{s, v})
		}
		t.Close()
	}
	return out, nil
}

func ucitajPromjene(db *sql.DB, letva string) ([]promjenaUPaketu, error) {
	rows, err := db.Query(`SELECT datum, pomak_cm, izvor, napomena FROM promjene_kote
		WHERE letva = ? ORDER BY datum`, letva)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []promjenaUPaketu
	for rows.Next() {
		var p promjenaUPaketu
		if err := rows.Scan(&p.Datum, &p.PomakCm, &p.Izvor, &p.Napomena); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Datum < out[j].Datum })
	return out, nil
}

// PripremiPraznu stvara shemu u praznoj arhivi. Čvor koji arhivu ne gradi
// sam nego je samo prima mora je ipak imati gdje upisati.
func PripremiPraznu(db *sql.DB) error {
	// Bez ovoga SQLite ne provodi ON DELETE CASCADE, pa brisanje roditelja
	// ostavlja djecu. Zatečena arhiva tako je skupila 330 odsječaka i 164 točke
	// profila bez svoje krivulje odnosno profila.
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		return err
	}
	if _, err := db.Exec(shema); err != nil {
		return err
	}
	return dopuniShemu(db)
}

// pospremiSirotisteva briše djecu kojoj je roditelj davno nestao. Nije samo
// urednost: id se u SQLiteu ponovno dodjeljuje, pa nova krivulja dobije broj
// davno obrisane, a njezini odsječci nalete na tuđe ostatke i upis padne.
func pospremiSirotista(tx *sql.Tx) error {
	for _, q := range []string{
		`DELETE FROM hq_odsjecci WHERE krivulja NOT IN (SELECT id FROM hq_krivulje)`,
		`DELETE FROM profil_tocke WHERE profil NOT IN (SELECT id FROM profili)`,
		`DELETE FROM ocitanja WHERE niz NOT IN (SELECT id FROM nizovi)`,
	} {
		if _, err := tx.Exec(q); err != nil {
			return fmt.Errorf("pospremanje ostataka: %w", err)
		}
	}
	return nil
}

// Sadrzaj je raspakiran paket, spreman za ugradnju.
type Sadrzaj struct {
	Manifest    Manifest
	nizovi      []nizUPaketu
	vrijeme     [][]int64
	vrijednosti [][]float64
	krivulje    []krivuljaUPaketu
	profili     []profilUPaketu
	promjene    []promjenaUPaketu

	// Izvori su postavke s kojima je paket složen. Paket inačice 1 ih nema, pa
	// se ondje ne zna s čime je spojeni niz nastao.
	Izvori []izvorUPaketu
}

// PostavkeIzvora vraća ono s čime je paket složen, za usporedbu s onim što
// čvor ima.
func (s *Sadrzaj) PostavkeIzvora() []izvorUPaketu { return s.Izvori }

// Procitaj raspakira paket i provjerava otisak. Paket kojemu se otisak ne
// poklapa ne ugrađuje se: bolje odbiti nego u arhivu upisati nešto što se
// putem pokvarilo ili izmijenilo.
func Procitaj(r io.ReaderAt, velicina int64) (*Sadrzaj, error) {
	z, err := zip.NewReader(r, velicina)
	if err != nil {
		return nil, fmt.Errorf("paket nije ZIP: %w", err)
	}
	sadrzaj, err := raspakiraj(z)
	if err != nil {
		return nil, err
	}

	var s Sadrzaj
	if err := json.Unmarshal(sadrzaj["manifest.json"], &s.Manifest); err != nil {
		return nil, fmt.Errorf("manifest se ne čita: %w", err)
	}
	if s.Manifest.Inacica > PaketInacica {
		return nil, fmt.Errorf("paket je inačice %d, a ovaj program poznaje %d — nadogradi program",
			s.Manifest.Inacica, PaketInacica)
	}
	h := sha256.New()
	for _, ime := range dijeloviZa(s.Manifest.Inacica) {
		h.Write(sadrzaj[ime])
	}
	if otisak := hex.EncodeToString(h.Sum(nil)); otisak != s.Manifest.Otisak {
		return nil, fmt.Errorf("otisak se ne poklapa: paket kaže %s, izračunato %s",
			kratki(s.Manifest.Otisak), kratki(otisak))
	}

	if err := json.Unmarshal(sadrzaj["nizovi.json"], &s.nizovi); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(sadrzaj["krivulje.json"], &s.krivulje); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(sadrzaj["profili.json"], &s.profili); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(sadrzaj["promjene.json"], &s.promjene); err != nil {
		return nil, err
	}
	if b := sadrzaj["izvori.json"]; len(b) > 0 {
		if err := json.Unmarshal(b, &s.Izvori); err != nil {
			return nil, err
		}
	}

	br := &citac{b: sadrzaj["ocitanja.bin"]}
	for i := range s.nizovi {
		t, v, err := citajNiz(br, s.nizovi[i].Mjerilo)
		if err != nil {
			return nil, fmt.Errorf("niz %d (%s, %s): %w", i+1, s.nizovi[i].Izvor, s.nizovi[i].Velicina, err)
		}
		if len(t) != s.nizovi[i].Zapisa {
			return nil, fmt.Errorf("niz %d obećava %d zapisa, a nosi %d",
				i+1, s.nizovi[i].Zapisa, len(t))
		}
		s.vrijeme = append(s.vrijeme, t)
		s.vrijednosti = append(s.vrijednosti, v)
	}
	return &s, nil
}

func kratki(otisak string) string {
	if len(otisak) > 12 {
		return otisak[:12] + "…"
	}
	return otisak
}

type citac struct {
	b []byte
	i int
}

func (c *citac) ReadByte() (byte, error) {
	if c.i >= len(c.b) {
		return 0, io.EOF
	}
	b := c.b[c.i]
	c.i++
	return b, nil
}

// Ugradi upisuje paket u arhivu i pregrađuje spojeni niz. Sve u jednoj
// transakciji: arhiva ne smije ostati s pola letve.
func Ugradi(db *sql.DB, baza string, s *Sadrzaj) error {
	// Ugradnja briše letvu pa upisuje njezine dijelove i gradi spoj — isti
	// posao kao gradnja, pa ista brava.
	brava, err := Uzmi(baza, "ugradnja paketa "+s.Manifest.Letva, s.Manifest.Izdao)
	if err != nil {
		return err
	}
	defer brava.Pusti()

	if s == nil {
		return fmt.Errorf("nema što ugraditi")
	}
	if err := dopuniShemu(db); err != nil {
		return err
	}
	letva := s.Manifest.Letva
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Izdanje zamjenjuje sve što je o toj letvi bilo — paket je cjelovita
	// izjava o njoj, a ne dodatak.
	for _, q := range []string{
		`DELETE FROM ocitanja WHERE niz IN (SELECT id FROM nizovi WHERE letva = ?)`,
		`DELETE FROM nizovi WHERE letva = ?`,
		`DELETE FROM hq_odsjecci WHERE krivulja IN (SELECT id FROM hq_krivulje WHERE letva = ?)`,
		`DELETE FROM hq_krivulje WHERE letva = ?`,
		`DELETE FROM profil_tocke WHERE profil IN (SELECT id FROM profili WHERE letva = ?)`,
		`DELETE FROM profili WHERE letva = ?`,
		`DELETE FROM promjene_kote WHERE letva = ?`,
		`DELETE FROM spoj WHERE letva = ?`,
	} {
		if _, err := tx.Exec(q, letva); err != nil {
			return fmt.Errorf("čišćenje prethodnog izdanja: %w", err)
		}
	}
	if err := pospremiSirotista(tx); err != nil {
		return err
	}

	upisNiz, err := tx.Prepare(`INSERT INTO nizovi (zona, sliv, letva, izvor, velicina, vrsta,
		po_danu, od, do_, zapisa, otisak, datoteke, osvjezeno, napomena)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer upisNiz.Close()
	upisVrijednost, err := tx.Prepare(`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (?,?,?)`)
	if err != nil {
		return err
	}
	defer upisVrijednost.Close()

	for i, n := range s.nizovi {
		res, err := upisNiz.Exec(n.Zona, n.Sliv, letva, n.Izvor, n.Velicina, n.Vrsta,
			n.PoDanu, n.Od, n.Do, n.Zapisa, n.Otisak, n.Datoteke, n.Osvjezeno, n.Napomena)
		if err != nil {
			return fmt.Errorf("upis niza %s/%s: %w", n.Izvor, n.Velicina, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		for j := range s.vrijeme[i] {
			if _, err := upisVrijednost.Exec(id, s.vrijeme[i][j], s.vrijednosti[i][j]); err != nil {
				return fmt.Errorf("upis vrijednosti niza %s/%s: %w", n.Izvor, n.Velicina, err)
			}
		}
	}

	for _, k := range s.krivulje {
		res, err := tx.Exec(`INSERT INTO hq_krivulje (letva, vrijedi_od, vrijedi_do, izvor, napomena)
			VALUES (?,?,?,?,?)`, letva, k.VrijediOd, k.VrijediDo, k.Izvor, k.Napomena)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("upis krivulje %s: %w", k.VrijediOd, err)
		}
		for _, o := range k.Odsjecci {
			if _, err := tx.Exec(`INSERT INTO hq_odsjecci (krivulja, od_cm, do_cm, oblik, p1, p2, p3)
				VALUES (?,?,?,?,?,?,?)`, id, o.OdCm, o.DoCm, o.Oblik, o.P1, o.P2, o.P3); err != nil {
				return err
			}
		}
	}

	for _, p := range s.profili {
		res, err := tx.Exec(`INSERT INTO profili (letva, datum, vodostaj, kota_nule, pomak_m)
			VALUES (?,?,?,?,?)`, letva, p.Datum, p.Vodostaj, p.KotaNule, p.PomakM)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("upis profila %s: %w", p.Datum, err)
		}
		for _, t := range p.Tocke {
			if _, err := tx.Exec(`INSERT INTO profil_tocke (profil, stacionaza, visina)
				VALUES (?,?,?)`, id, t[0], t[1]); err != nil {
				return err
			}
		}
	}

	for _, p := range s.promjene {
		if _, err := tx.Exec(`INSERT INTO promjene_kote (letva, datum, pomak_cm, izvor, napomena)
			VALUES (?,?,?,?,?)`, letva, p.Datum, p.PomakCm, p.Izvor, p.Napomena); err != nil {
			return err
		}
	}
	// Sve što slijedi teče u ISTOJ transakciji. Prije se ovdje potvrđivalo, pa
	// su izvori i spoj išli izvan nje: kvar u tom drugom dijelu ostavljao je
	// obrisan stari spoj i upisane nove nizove — pola arhive, uz grešku
	// korisniku i suprotno onome što je pisalo iznad funkcije.
	//
	// Spoj je izveden i ne putuje paketom — gradi se ovdje, iz upravo
	// ugrađenih nizova.
	// Paket može donijeti izvor kojeg ovaj čvor još ne poznaje. Takav ulazi s
	// postavkama iz paketa, i to UKLJUČEN ako je ondje bio: podaci tog izvora
	// upravo stižu, nikoga drugoga ne diraju, a čvor koji je paket izdao za
	// njih jamči. Na vratima je obrnuto — ondje za novi izvor nitko ne jamči.
	//
	// Postavke izvora koje čvor VEĆ ima ne mijenjaju se. Izvori su zajednički
	// svim letvama, pa bi paket jedne tiho promijenio brojeve na svim
	// ostalima; razlika se pokazuje čovjeku prije ugradnje.
	for _, i := range s.Izvori {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO izvori (naziv, tocnost, red, ukljucen, napomena)
			VALUES (?,?,?,?,?)`, i.Naziv, i.Tocnost, i.Red, i.Ukljucen, i.Napomena); err != nil {
			return fmt.Errorf("upis izvora %s iz paketa: %w", i.Naziv, err)
		}
	}
	// Ono što paket ne spominje — stariji paket bez izvori.json, ili izvor koji
	// je u nizovima a nije u popisu — ulazi isključeno, kao i inače.
	if err := upisiZadaneIzvore(tx); err != nil {
		return err
	}
	if _, err := SpojiU(tx, letva); err != nil {
		return fmt.Errorf("spajanje nakon ugradnje: %w", err)
	}
	return tx.Commit()
}
