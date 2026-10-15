package arhiva

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Popis kvarova drži odsječke nizova za koje se zna da su krivi: zaglavljeno
// mjerilo, mjerilo koje je nasjelo pri maloj vodi, pomak, zanošenje, ispadi.
// Takvi retci ostaju u izvornoj datoteci, kakvu je izvor dao, a gradnja ih
// sama odvaja u zaseban niz istog izvora s nastavkom -kvar. Taj izvor ulazi
// isključen, pa kriva vrijednost ne stiže u spojeni niz, a ipak se ne gubi.
//
// Prije su se krivi retci ručno selili u zasebnu datoteku. Svaki novi uvoz
// telemetrije s letva.voda.hr napiše datoteku iznova, cijelu, i kvar bi se
// tiho vratio. Popis vrijedi bez obzira na to koliko se puta podaci uvezu.
const datotekaKvarova = "kvarovi.csv"

// pravilo je jedan redak popisa kvarova. Vremena su u sekundama UTC-a, uključivo; nula
// znači da s te strane granice nema. Najmanje i najviše suzuju odsječak na
// vrijednosti u tom rasponu — tako se hvata ispad poput 1550 cm na Belišću,
// koji se ponavlja svake zime, a ne zna se unaprijed kada.
type pravilo struct {
	izvor, velicina   string
	od, do            int64
	najmanje, najvise *float64
}

// nastavakKvara je oznaka izvora u koji se kriv redak odvaja.
const nastavakKvara = "-kvar"

func jeKvar(izvor string) bool { return strings.HasSuffix(izvor, nastavakKvara) }

// pogada javlja pripada li zapis ovom kvaru.
func (k pravilo) pogada(z zapis) bool {
	return uGranicama(k.od, z.t, k.do) && uRasponu(k.najmanje, z.v, k.najvise)
}

// uGranicama: nula znači da s te strane granice nema.
func uGranicama(od, t, do int64) bool {
	return (od == 0 || t >= od) && (do == 0 || t <= do)
}

func uRasponu(najmanje *float64, v float64, najvise *float64) bool {
	return (najmanje == nil || v >= *najmanje) && (najvise == nil || v <= *najvise)
}

// odvojiKvarove vraća zapise niza koji nisu u popisu kvarova, a one koji
// jesu dodaje u n.losi. Niz -kvar se ne dijeli dalje.
func odvojiKvarove(n *niz, z []zapis) []zapis {
	moja := pravilaNiza(n)
	if len(moja) == 0 {
		return z
	}
	dobri := z[:0:0]
	for _, x := range z {
		if slices.ContainsFunc(moja, func(k pravilo) bool { return k.pogada(x) }) {
			n.losi = append(n.losi, x)
		} else {
			dobri = append(dobri, x)
		}
	}
	return dobri
}

// pravilaNiza izdvaja iz popisa letve retke koji se tiču baš ovog niza.
func pravilaNiza(n *niz) []pravilo {
	if jeKvar(n.izvor) {
		return nil
	}
	var moja []pravilo
	for _, k := range n.pravila {
		if k.izvor == n.izvor && k.velicina == n.velicina {
			moja = append(moja, k)
		}
	}
	return moja
}

// citajKvarove čita popis kvarova i vraća ga po letvi. Datoteke ne mora biti.
//
// Stupci: letva;izvor;velicina;od;do;najmanje;najvise;napomena. Od i do su
// datum ili datum sa satom, onako kako stoje u izvornoj datoteci — u lokalnom
// satu za hrvatske izvore — a do uključuje cijeli dan kad sata nema. Prazno
// polje znači da s te strane granice nema.
func citajKvarove(koren string) (map[string][]pravilo, error) {
	b, err := os.ReadFile(filepath.Join(koren, datotekaKvarova))
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return procitajKvarove(bytes.NewReader(b))
}

func procitajKvarove(r io.Reader) (map[string][]pravilo, error) {
	cr := csv.NewReader(r)
	cr.Comma = ';'
	cr.FieldsPerRecord = -1
	sve, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("popis kvarova: %w", err)
	}
	out := map[string][]pravilo{}
	for i, r := range sve {
		if i == 0 || strings.TrimSpace(strings.Join(r, "")) == "" {
			continue
		}
		letva, k, err := procitajKvar(r)
		if err != nil {
			return nil, fmt.Errorf("popis kvarova, redak %d: %w", i+1, err)
		}
		out[letva] = append(out[letva], k)
	}
	return out, nil
}

// procitajKvar čita jedan redak popisa.
func procitajKvar(r []string) (string, pravilo, error) {
	var k pravilo
	if len(r) < 5 {
		return "", k, fmt.Errorf("treba barem letva;izvor;velicina;od;do")
	}
	letva := strings.TrimSpace(strings.TrimPrefix(r[0], "\ufeff"))
	k.izvor, k.velicina = strings.TrimSpace(r[1]), strings.TrimSpace(r[2])
	if slices.Contains([]string{letva, k.izvor, k.velicina}, "") {
		return "", k, fmt.Errorf("letva, izvor i velicina ne smiju biti prazni")
	}
	if jeKvar(k.izvor) {
		return "", k, fmt.Errorf("izvor %q već je kvar; upiši izvor iz kojeg se odvaja", k.izvor)
	}
	if err := k.granice(r[3], r[4]); err != nil {
		return "", k, err
	}
	if err := k.raspon(nth(r, 5), nth(r, 6)); err != nil {
		return "", k, err
	}
	if k.bezGranica() {
		return "", k, fmt.Errorf("kvar bez vremena i bez vrijednosti odnio bi cijeli niz")
	}
	return letva, k, nil
}

func (k pravilo) bezGranica() bool {
	return k.od == 0 && k.do == 0 && k.najmanje == nil && k.najvise == nil
}

func (k *pravilo) granice(od, do string) error {
	zona := zonaIzvora(k.izvor)
	var err error
	if k.od, err = granica(od, zona, false); err != nil {
		return fmt.Errorf("od %q: %w", od, err)
	}
	if k.do, err = granica(do, zona, true); err != nil {
		return fmt.Errorf("do %q: %w", do, err)
	}
	if k.od != 0 && k.do != 0 && k.do < k.od {
		return fmt.Errorf("do je prije od")
	}
	return nil
}

func (k *pravilo) raspon(najmanje, najvise string) error {
	var err error
	if k.najmanje, err = brojIliNista(najmanje); err != nil {
		return fmt.Errorf("najmanje %q: %w", najmanje, err)
	}
	if k.najvise, err = brojIliNista(najvise); err != nil {
		return fmt.Errorf("najvise %q: %w", najvise, err)
	}
	return nil
}

// granica pretvara od ili do u sekunde UTC-a. Do bez sata vrijedi do kraja dana.
func granica(s string, zona *time.Location, kraj bool) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	for _, oblik := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(oblik, s, zona); err == nil {
			return t.UTC().Unix(), nil
		}
	}
	t, err := time.ParseInLocation("2006-01-02", s, zona)
	if err != nil {
		return 0, fmt.Errorf("treba GGGG-MM-DD ili GGGG-MM-DD SS:MM")
	}
	if kraj {
		t = t.AddDate(0, 0, 1).Add(-time.Second)
	}
	return t.UTC().Unix(), nil
}

func brojIliNista(s string) (*float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// predvidiKvarne dodaje u popis nizova one koji nastaju iz popisa kvarova, a
// datoteke nemaju. Bez toga bi ih provjera sirotana prijavila kao nizove kojima
// je nestala datoteka.
func predvidiKvarne(nizovi map[string]*niz, kvarovi map[string][]pravilo) {
	for _, n := range nizovi {
		if jeKvar(n.izvor) {
			continue
		}
		for _, k := range kvarovi[n.letva] {
			if k.izvor != n.izvor || k.velicina != n.velicina {
				continue
			}
			kk := kljucNiza(n.letva, n.izvor+nastavakKvara, n.velicina, n.vrsta)
			if nizovi[kk] == nil {
				nizovi[kk] = &niz{sliv: n.sliv, letva: n.letva, izvor: n.izvor + nastavakKvara,
					velicina: n.velicina, vrsta: n.vrsta}
			}
			break
		}
	}
}

func kljucNiza(letva, izvor, velicina, vrsta string) string {
	return strings.Join([]string{letva, izvor, velicina, vrsta}, "|")
}

// dodajZateceneKvarne vraća u popis nizove -kvar koji stoje u arhivi, a ni
// datoteke ni retka u popisu kvarova više nemaju, dok im izvorni niz postoji.
// Takav je niz nastao iz popisa koji se u međuvremenu promijenio. Gradnja ga
// tada nađe prazna i makne, a retke vrati izvornom nizu; bez ovoga bi ostao
// u arhivi sa starim sadržajem i javljao se kao sirotan.
func dodajZateceneKvarne(db *sql.DB, nizovi map[string]*niz, nedostupni map[string]bool) error {
	rows, err := db.Query(`SELECT sliv, letva, izvor, velicina, vrsta FROM nizovi WHERE izvor LIKE '%` + nastavakKvara + `'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() && err == nil {
		var n niz
		if err = rows.Scan(&n.sliv, &n.letva, &n.izvor, &n.velicina, &n.vrsta); err == nil && zatecen(nizovi, nedostupni, n) {
			nizovi[kljucNiza(n.letva, n.izvor, n.velicina, n.vrsta)] = &n
		}
	}
	if err == nil {
		err = rows.Err()
	}
	return err
}

// zatecen javlja je li niz -kvar iz arhive ostao bez datoteke i bez retka u
// popisu, dok mu se izvorni niz gradi.
func zatecen(nizovi map[string]*niz, nedostupni map[string]bool, n niz) bool {
	izvorni := strings.TrimSuffix(n.izvor, nastavakKvara)
	return nizovi[kljucNiza(n.letva, n.izvor, n.velicina, n.vrsta)] == nil && !nedostupni[n.izvor] &&
		nizovi[kljucNiza(n.letva, izvorni, n.velicina, n.vrsta)] != nil
}

// kvaroviGradnje čita popis kvarova i u popis nizova dodaje nizove -kvar koji
// iz njega nastaju ili su iz njega prije nastali.
func kvaroviGradnje(db *sql.DB, koren string, nizovi map[string]*niz, nedostupni map[string]bool) (map[string][]pravilo, error) {
	kvarovi, err := citajKvarove(koren)
	if err != nil {
		return nil, err
	}
	predvidiKvarne(nizovi, kvarovi)
	return kvarovi, dodajZateceneKvarne(db, nizovi, nedostupni)
}

// redGradnje vraća ključeve nizova redom kojim se grade. Nizovi -kvar idu
// zadnji: u njih se slijeva ono što je popis kvarova izdvojio iz izvornih
// nizova, pa izvorni moraju biti pročitani prije.
func redGradnje(nizovi map[string]*niz) []string {
	var kljucevi, kvarni []string
	for k, n := range nizovi {
		if jeKvar(n.izvor) {
			kvarni = append(kvarni, k)
		} else {
			kljucevi = append(kljucevi, k)
		}
	}
	sort.Strings(kljucevi)
	sort.Strings(kvarni)
	return append(kljucevi, kvarni...)
}

// gradiNiz čita jedan niz, predaje kvarove njegovu nizu -kvar i upisuje ga.
// Vraća koliko je zapisa upisano.
func gradiNiz(db *sql.DB, nizovi map[string]*niz, k string, kv []pravilo, promjene []promjena, zapisi io.Writer) (int, error) {
	n := nizovi[k]
	if jeKvar(n.izvor) && len(n.datoteke) == 0 && len(n.odvojeni) == 0 {
		return 0, makniPrazanKvar(db, n, zapisi)
	}
	n.pravila = kv
	upisano, od, do, otisak, err := ubaci(db, n, promjene)
	if err != nil && len(n.losi) > 0 {
		err = fmt.Errorf("popis kvarova odvaja sve retke niza: %w", err)
	}
	predajKvaru(nizovi, n, zapisi)
	if err != nil {
		return 0, err
	}
	_, _ = fmt.Fprintf(zapisi, "%-8s %-16s %-22s %-12s %-10s %8d  %s .. %s  %s\n",
		n.sliv, n.letva, n.izvor, n.velicina, n.vrsta, upisano, od, do, otisak[:8])
	return upisano, nil
}

// predajKvaru seli ono što je čitanje izdvojilo iz niza u njegov niz -kvar.
func predajKvaru(nizovi map[string]*niz, n *niz, zapisi io.Writer) {
	if len(n.losi) == 0 {
		return
	}
	kk := kljucNiza(n.letva, n.izvor+nastavakKvara, n.velicina, n.vrsta)
	nizovi[kk].odvojeni = append(nizovi[kk].odvojeni, n.losi...)
	_, _ = fmt.Fprintf(zapisi, "%-8s %-16s %-22s odvojeno po popisu kvarova: %d zapisa\n",
		"", n.letva, n.izvor, len(n.losi))
	n.losi = nil
}

// popisiSKvarovima popisuje datoteke stabla i dodaje nizove -kvar koji iz
// popisa kvarova nastaju, da ih provjera bez gradnje ne proglasi nizovima
// kojima je nestala datoteka.
func popisiSKvarovima(koren string) (map[string]*niz, error) {
	nizovi, err := popisi(koren, "")
	if err != nil {
		return nil, err
	}
	kvarovi, err := citajKvarove(koren)
	if err != nil {
		return nil, err
	}
	predvidiKvarne(nizovi, kvarovi)
	return nizovi, nil
}
