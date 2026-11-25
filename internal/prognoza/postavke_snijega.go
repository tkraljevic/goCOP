package prognoza

// Postavke modela snijega (Postavke prognoze › Model snijega): je li model
// uključen, koliko se snijega otopi po °C na dan i iznad koje temperature se
// topi. Mijenjaju se istim putem kao model letve: nacrt, proba na povijesti i
// primjena s razlogom.
//
// Nacrt stoji u tablici nacrta (model_nacrt) i ostaje na ovom računalu.
// Primijenjene postavke su redak tablice model_postavke. Ona je među tablicama
// modela, pa putuje s modelom izdavača kao i model letve (PrimiModel). Bez
// retka vrijede postavke iz programa (ZadanePostavkeSnijega).
//
// Račun prognoze čita primijenjene postavke iz baze prije svakog računa
// (OsvjeziDnevnuPostavu). Kad se promijene, naučeni dnevni modeli se bacaju, pa
// ih sljedeći račun uči iznova s novim postavkama. Proba radi s postavkama
// koje dobije, pa za vrijeme probe prognoza računa kao i dosad.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// PostavkeSnijega su postavke modela snijega.
type PostavkeSnijega struct {
	// Ukljucen kaže ide li oborina kroz zalihu snijega. Isključen model daje
	// dnevnom modelu oborinu kakva je pala, u učenju i uživo.
	Ukljucen bool `json:"ukljucen"`
	// Topljenje je koliko se mm vode iz zalihe otopi na dan po °C srednje
	// dnevne temperature zraka iznad praga.
	Topljenje float64 `json:"topljenje"`
	// Prag je srednja dnevna temperatura zraka (°C) iznad koje se snijeg topi.
	Prag float64 `json:"prag"`
}

// ZadanePostavkeSnijega vrijede dok postavke nisu primijenjene. Provjereno
// učenjem do 2012. i provjerom 2012.–2025. (vidi snijeg.go).
var ZadanePostavkeSnijega = PostavkeSnijega{Ukljucen: true, Topljenje: 3, Prag: 0}

// Granice postavki. Faktor topljenja u literaturi ide od 1 do 8 mm po °C na
// dan; prag oko nule, jer se srednja dnevna temperatura uzima s točke koja
// ne stoji na visini cijelog sliva.
const (
	NajmanjeTopljenje  = 1.0
	NajviseTopljenje   = 8.0
	NajnizaTemperatura = -2.0
	NajvisaTemperatura = 3.0
)

// Provjeri javlja jesu li brojevi u granicama.
func (p PostavkeSnijega) Provjeri() error {
	if math.IsNaN(p.Topljenje) || p.Topljenje < NajmanjeTopljenje || p.Topljenje > NajviseTopljenje {
		return fmt.Errorf("brzina topljenja mora biti od %s do %s mm vode po °C na dan", brojSnijega(NajmanjeTopljenje), brojSnijega(NajviseTopljenje))
	}
	if math.IsNaN(p.Prag) || p.Prag < NajnizaTemperatura || p.Prag > NajvisaTemperatura {
		return fmt.Errorf("temperatura topljenja mora biti od %s do %s °C", brojSnijega(NajnizaTemperatura), brojSnijega(NajvisaTemperatura))
	}
	return nil
}

// brojSnijega piše broj s decimalnim zarezom, za poruke.
func brojSnijega(v float64) string {
	return strings.Replace(strings.Replace(strconv.FormatFloat(v, 'f', -1, 64), ".", ",", 1), "-", "−", 1)
}

// Korak provede jedan dan kroz zalihu snijega ovim postavkama (vidi
// KorakSnijega). Isključene postavke zalihu vode isto: o njima ovisi samo što
// dnevni model dobiva.
func (p PostavkeSnijega) Korak(zaliha float64, d DanOborine) (nova, palo, topi float64) {
	if !d.ImaSnijega {
		return zaliha, 0, 0
	}
	palo = math.Min(math.Max(d.Snijeg, 0), math.Max(d.Oborina, 0))
	zaliha += palo
	topi = math.Min(zaliha, math.Max(0, p.Topljenje*(d.Temperatura-p.Prag)))
	return zaliha - topi, palo, topi
}

// postavkeSnijega su primijenjene postavke koje račun prognoze sada koristi;
// nil dok se ne učitaju iz baze, a tada vrijede zadane.
var postavkeSnijega atomic.Pointer[PostavkeSnijega]

// PostavkeSnijegaURadu su postavke s kojima prognoza sada računa.
func PostavkeSnijegaURadu() PostavkeSnijega {
	if p := postavkeSnijega.Load(); p != nil {
		return *p
	}
	return ZadanePostavkeSnijega
}

// tablicaPostavki je tablica primijenjenih postavki modela, a kljucSnijega
// redak postavki snijega u njoj.
const (
	tablicaPostavki = "model_postavke"
	kljucSnijega    = "snijeg"
	// kljucNacrtaSnijega je redak nacrta postavki snijega u tablici nacrta;
	// predmetak ga dijeli od nacrta računa letve.
	kljucNacrtaSnijega = "postavke:snijeg"
)

// ZapisSnijega su primijenjene postavke snijega s poviješću promjena.
type ZapisSnijega struct {
	Postavke PostavkeSnijega `json:"postavke"`
	Promjene []PromjenaLetve `json:"promjene"`
}

// Zadnja je zadnja promjena; prazna dok postavke nisu primijenjene.
func (z ZapisSnijega) Zadnja() PromjenaLetve {
	if len(z.Promjene) == 0 {
		return PromjenaLetve{}
	}
	return z.Promjene[len(z.Promjene)-1]
}

// najvisePromjenaSnijega je koliko se zadnjih promjena čuva uz postavke.
const najvisePromjenaSnijega = 20

// PrimijenjeneSnijega čita primijenjene postavke snijega. Bez tablice ili
// retka vrijede zadane, a ima je false. Redak koji se ne da pročitati ili ne
// prolazi provjeru je greška; uz nju se vraćaju zadane.
func PrimijenjeneSnijega(db *sql.DB) (ZapisSnijega, bool, error) {
	zadane := ZapisSnijega{Postavke: ZadanePostavkeSnijega}
	sadrzaj, ima, err := redakTablice(db, tablicaPostavki, "kljuc", kljucSnijega)
	if !ima {
		return zadane, false, err
	}
	z, err := procitajZapisSnijega(sadrzaj)
	if err != nil {
		return zadane, false, err
	}
	return z, true, nil
}

// procitajZapisSnijega čita redak postavki i provjerava brojeve.
func procitajZapisSnijega(sadrzaj string) (ZapisSnijega, error) {
	var z ZapisSnijega
	if err := json.Unmarshal([]byte(sadrzaj), &z); err != nil {
		return z, fmt.Errorf("postavke snijega: %w", err)
	}
	if err := z.Postavke.Provjeri(); err != nil {
		return z, fmt.Errorf("postavke snijega: %w", err)
	}
	return z, nil
}

// redakTablice čita stupac sadrzaj retka tablice po ključu. Baza nastala
// prije tablice nema retka: stranica bazu čita samo za čitanje, pa je shema
// ne dograđuje.
func redakTablice(db *sql.DB, tablica, stupac, kljuc string) (sadrzaj string, ima bool, err error) {
	var postoji int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = ?`, tablica).Scan(&postoji); err != nil || postoji == 0 {
		return "", false, err
	}
	err = db.QueryRow(`SELECT sadrzaj FROM `+tablica+` WHERE `+stupac+` = ?`, kljuc).Scan(&sadrzaj)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return sadrzaj, err == nil, err
}

// UcitajPostavkeSnijega čita primijenjene postavke iz baze u postavke s
// kojima račun radi. Kad se promijene, naučeni dnevni modeli se bacaju, pa ih
// sljedeći račun uči iznova. Pokvaren redak vraća grešku, a račun ide sa
// zadanim postavkama.
func UcitajPostavkeSnijega(baza *sql.DB) error {
	z, _, err := PrimijenjeneSnijega(baza)
	if nova := z.Postavke; nova != PostavkeSnijegaURadu() {
		postavkeSnijega.Store(&nova)
		ZaboraviDnevneModele()
	}
	return err
}

// osvjeziPostavkeSnijega čita postavke prije računa; greška ide u dnevnik.
func osvjeziPostavkeSnijega(baza *sql.DB) {
	if err := UcitajPostavkeSnijega(baza); err != nil {
		log.Printf("prognoza: %v; vrijede postavke snijega iz programa", err)
	}
}

// NacrtSnijega je nacrt postavki snijega: kakve bi bile, tko ih je spremio i
// kad, i što je proba izmjerila.
type NacrtSnijega struct {
	Postavke PostavkeSnijega `json:"postavke"`
	Tko      string          `json:"tko"`
	Kad      string          `json:"kad"`
	// Proba je ono što je proba izmjerila za ove postavke; nema je dok se ne
	// proba i čim se postavke promijene.
	Proba *ProbaSnijega `json:"proba,omitempty"`
}

// ProbaSnijega je ishod probe postavki snijega: po letvi promašaj dnevnog
// modela s primijenjenim postavkama i s nacrtom, na istim danima.
type ProbaSnijega struct {
	Kad string `json:"kad"`
	// Primijenjene su postavke s kojima je nacrt uspoređen. Kad se do
	// primjene promijene, proba više ne kaže ništa o razlici.
	Primijenjene PostavkeSnijega     `json:"primijenjene"`
	Razdoblje    []string            `json:"razdoblje"`
	Letve        []ProbaSnijegaLetve `json:"letve"`
	Natpisi      []string            `json:"natpisi"`
}

// ProbaSnijegaLetve je proba na jednoj letvi.
type ProbaSnijegaLetve struct {
	Letva string      `json:"letva"`
	Sada  []MjeraDana `json:"sada"`
	Nacrt []MjeraDana `json:"nacrt"`
	// Ocjena uspoređuje nacrt s primijenjenim postavkama.
	Ocjena    StanjeMjere `json:"ocjena"`
	Razdoblje []string    `json:"razdoblje,omitempty"`
	// Greska kaže zašto se letva nije dala izmjeriti.
	Greska string `json:"greska,omitempty"`
}

// Ocjene probe postavki snijega: nacrt prema primijenjenim postavkama.
const (
	SnijegBolje  StanjeMjere = "Bolje nego sada"
	SnijegIsto   StanjeMjere = "Kao sada"
	SnijegLosije StanjeMjere = "Lošije nego sada"
)

// NatpisSnjezneZime kaže na kojim je danima proba mjerena.
const NatpisSnjezneZime = "Mjereno samo na danima od studenoga do lipnja, kad snijeg može ležati ili se topiti"

// istoKaoSada je razlika ispod koje se nacrt i primijenjene postavke ne
// razlikuju: srednji promašaj unutar 2 % sadašnjeg. Model se uči na istim
// danima s obje postavke i mjeri na istim danima koje nije vidio, pa i mala
// razlika nešto znači; manja od toga je šum zaokruživanja.
const istoKaoSada = 0.02

// CiljeviProbeSnijega su letve na kojima se proba postavki snijega mjeri:
// Drava, Mura i Dunav iznad nas, gdje snijeg iz Alpa najviše znači.
var CiljeviProbeSnijega = []string{"botovo", "mursko-sredisce", "komarom"}

// SpremiNacrtSnijega provjeri postavke i upiše ih kao nacrt umjesto
// zatečenog. Proba ostaje dok se postavke ne promijene. Prognoza se ne mijenja.
func SpremiNacrtSnijega(baza *sql.DB, n NacrtSnijega) error {
	if err := n.Postavke.Provjeri(); err != nil {
		return err
	}
	stari, ima, err := NacrtSnijegaIz(baza)
	if err != nil {
		return err
	}
	n.Proba = nil
	if ima && stari.Postavke == n.Postavke {
		n.Proba = stari.Proba
	}
	return upisiNacrtSnijega(baza, n)
}

func upisiNacrtSnijega(baza *sql.DB, n NacrtSnijega) error {
	sadrzaj, err := json.Marshal(n)
	if err != nil {
		return fmt.Errorf("nacrt postavki snijega: %w", err)
	}
	_, err = baza.Exec(`INSERT INTO model_nacrt (letva, sadrzaj) VALUES (?, ?)
		ON CONFLICT(letva) DO UPDATE SET sadrzaj = excluded.sadrzaj`, kljucNacrtaSnijega, string(sadrzaj))
	return err
}

// NacrtSnijegaIz čita nacrt postavki snijega; ima je false kad ga nema. Redak
// koji se ne da pročitati se preskače, kao i nacrt računa letve: sljedeće
// spremanje ga zamijeni.
func NacrtSnijegaIz(db *sql.DB) (NacrtSnijega, bool, error) {
	sadrzaj, ima, err := redakTablice(db, "model_nacrt", "letva", kljucNacrtaSnijega)
	var n NacrtSnijega
	if !ima || json.Unmarshal([]byte(sadrzaj), &n) != nil {
		return NacrtSnijega{}, false, err
	}
	return n, true, nil
}

// ObrisiNacrtSnijega odbacuje nacrt postavki snijega; nacrt kojeg nema nije
// greška.
func ObrisiNacrtSnijega(baza *sql.DB) error {
	_, err := baza.Exec(`DELETE FROM model_nacrt WHERE letva = ?`, kljucNacrtaSnijega)
	return err
}

// nacrtIPrimijenjene čita nacrt koji prolazi provjeru i primijenjene
// postavke; bez nacrta greška je bezNacrta.
func nacrtIPrimijenjene(baza *sql.DB, bezNacrta string) (NacrtSnijega, ZapisSnijega, error) {
	n, ima, err := NacrtSnijegaIz(baza)
	switch {
	case err != nil:
		return n, ZapisSnijega{}, err
	case !ima:
		return n, ZapisSnijega{}, errors.New(bezNacrta)
	}
	if err := n.Postavke.Provjeri(); err != nil {
		return n, ZapisSnijega{}, err
	}
	z, _, err := PrimijenjeneSnijega(baza)
	return n, z, err
}

// ProbajNacrtSnijega proba nacrt postavki snijega na povijesti: na svakoj
// letvi probe (CiljeviProbeSnijega) nauči dnevni model s primijenjenim
// postavkama i s nacrtom na arhivi bez zadnje dvije godine (ili jedne, kad
// dana nema dosta) i izmjeri oba na danima snježne sezone u njima. tocke su
// kišomjeri registra. Primijenjene postavke i prognoza se ne mijenjaju.
func ProbajNacrtSnijega(arhiva, baza *sql.DB, tocke []OborinskaTocka, kad string) (NacrtSnijega, error) {
	n, z, err := nacrtIPrimijenjene(baza, "nema nacrta postavki snijega; spremi ga, pa probaj")
	if err != nil {
		return NacrtSnijega{}, err
	}
	p, err := probajNaLetvama(arhiva, tocke, z.Postavke, n.Postavke)
	if err != nil {
		return NacrtSnijega{}, err
	}
	p.Kad = kad
	n.Proba = p
	return n, upisiNacrtSnijega(baza, n)
}

// probajNaLetvama mjeri obje postavke na svakoj letvi probe koja ima kišu.
// Letva koja se ne da izmjeriti ostaje u probi s greškom; kad nijedna nije
// izmjerena, proba je greška.
func probajNaLetvama(arhiva *sql.DB, tocke []OborinskaTocka, sada, nacrt PostavkeSnijega) (*ProbaSnijega, error) {
	p := &ProbaSnijega{Primijenjene: sada, Natpisi: []string{NatpisSnjezneZime, NatpisTocnaKisa}}
	izmjereno := 0
	for _, letva := range CiljeviProbeSnijega {
		c, ima := sadasnjiDnevniCilj(letva)
		if !ima || len(c.Slivovi) == 0 {
			continue // bez kiše snijeg u model ne ulazi
		}
		l := probajLetvuSnijega(arhiva, tocke, c, sada, nacrt)
		if l.Greska == "" {
			izmjereno++
		}
		p.Letve = append(p.Letve, l)
	}
	if izmjereno == 0 {
		return nil, errors.New("proba se nije dala izmjeriti ni na jednoj letvi" + greskeProbe(p.Letve))
	}
	p.Razdoblje = ukupnoRazdoblje(p.Letve)
	return p, nil
}

// probajLetvuSnijega je proba jedne letve; greška ostaje uz letvu.
func probajLetvuSnijega(arhiva *sql.DB, tocke []OborinskaTocka, c DnevniCilj, sada, nacrt PostavkeSnijega) ProbaSnijegaLetve {
	l, err := probajSnijegNaCilju(arhiva, tocke, c, sada, nacrt)
	if err != nil {
		return ProbaSnijegaLetve{Letva: c.Letva, Ocjena: MjeraNijeProvjereno, Greska: err.Error()}
	}
	return l
}

// greskeProbe su greške letava probe, za poruku.
func greskeProbe(letve []ProbaSnijegaLetve) string {
	var out []string
	for _, l := range letve {
		if l.Greska != "" {
			out = append(out, l.Letva+": "+l.Greska)
		}
	}
	if len(out) == 0 {
		return ""
	}
	return " (" + strings.Join(out, "; ") + ")"
}

// ukupnoRazdoblje je od najranijeg početka do najkasnijeg kraja razdoblja
// letava probe; datumi su ISO, pa se uspoređuju kao tekst.
func ukupnoRazdoblje(letve []ProbaSnijegaLetve) []string {
	var od, do string
	for _, l := range letve {
		if len(l.Razdoblje) != 2 {
			continue
		}
		if od == "" || l.Razdoblje[0] < od {
			od = l.Razdoblje[0]
		}
		do = max(do, l.Razdoblje[1])
	}
	if od == "" {
		return nil
	}
	return []string{od, do}
}

// probajSnijegNaCilju uči i mjeri dnevni model cilja s obje postavke, na
// istoj granici: zadnje dvije godine, a kad se tako ne da izmjeriti, zadnja.
func probajSnijegNaCilju(arhiva *sql.DB, tocke []OborinskaTocka, c DnevniCilj, sada, nacrt PostavkeSnijega) (ProbaSnijegaLetve, error) {
	var nizovi [2]map[string]DnevniNiz
	for i, p := range [2]PostavkeSnijega{sada, nacrt} {
		var err error
		if nizovi[i], err = dnevniNizoviCiljaUz(arhiva, tocke, c, p); err != nil {
			return ProbaSnijegaLetve{}, err
		}
	}
	kraj := zadnjiDan(nizovi[0][c.Letva]) + 1
	var m [2]MjeraDnevnog
	var granica int64
	var err error
	for _, godina := range godineProbeDnevnog {
		granica = kraj - godina*365
		if m, err = izmjeriObje(nizovi, c, granica, kraj); err == nil {
			break
		}
	}
	dan := func(d int64) string { return time.Unix(d*86400, 0).UTC().Format(time.DateOnly) }
	return ProbaSnijegaLetve{Letva: c.Letva, Sada: m[0].Dani, Nacrt: m[1].Dani,
		Ocjena: ocijeniProbuSnijega(m[0].Dani, m[1].Dani), Razdoblje: []string{dan(granica), dan(kraj - 1)}}, err
}

// izmjeriObje mjeri model naučen na nizovima sa sadašnjim postavkama i s
// nacrtom, na istim danima snježne sezone.
func izmjeriObje(nizovi [2]map[string]DnevniNiz, c DnevniCilj, granica, kraj int64) (m [2]MjeraDnevnog, err error) {
	for i := range nizovi {
		if m[i], err = provjeriDnevniDane(nizovi[i], c, granica, kraj, snjeznaSezona); err != nil {
			return m, err
		}
	}
	return m, nil
}

// snjeznaSezona javlja je li dan (od 1970.) od studenoga do lipnja, kad snijeg
// na slivovima može ležati ili se topiti.
func snjeznaSezona(dan int64) bool {
	m := time.Unix(dan*86400, 0).UTC().Month()
	return m >= time.November || m <= time.June
}

// ocijeniProbuSnijega uspoređuje srednji promašaj nacrta i sadašnjih postavki
// preko dana koji su s obje strane izmjereni na dovoljno slučajeva.
func ocijeniProbuSnijega(sada, nacrt []MjeraDana) StanjeMjere {
	var zbrojSada, zbrojNacrt float64
	for i := range min(len(sada), len(nacrt)) {
		if min(sada[i].Slucaja, nacrt[i].Slucaja) >= najmanjeSlucajeva {
			zbrojSada += sada[i].Promasaj
			zbrojNacrt += nacrt[i].Promasaj
		}
	}
	switch razlika := zbrojNacrt - zbrojSada; {
	case zbrojSada == 0:
		return MjeraNijeProvjereno
	case razlika < -istoKaoSada*zbrojSada:
		return SnijegBolje
	case razlika > istoKaoSada*zbrojSada:
		return SnijegLosije
	}
	return SnijegIsto
}

// PrimijeniNacrtSnijega upiše probani nacrt kao primijenjene postavke snijega,
// odbaci nacrt i odmah ih učita u račun: sljedeći račun dnevne modele uči
// iznova. Razlog je obavezan i ostaje uz postavke. Proba mora biti napravljena
// prema postavkama koje su sada primijenjene.
func PrimijeniNacrtSnijega(baza *sql.DB, promjena PromjenaLetve) error {
	if strings.TrimSpace(promjena.Razlog) == "" {
		return errors.New("upiši razlog; ostaje uz postavke")
	}
	n, z, err := probaniNacrtSnijega(baza)
	if err != nil {
		return err
	}
	z.Postavke = n.Postavke
	z.Promjene = append(z.Promjene, promjena)
	z.Promjene = z.Promjene[max(0, len(z.Promjene)-najvisePromjenaSnijega):]
	return redomDoGreske(
		func() error { return upisiPrimijenjeneSnijega(baza, z, promjena.Kad) },
		func() error { return ObrisiNacrtSnijega(baza) },
		func() error { return UcitajPostavkeSnijega(baza) },
	)
}

// probaniNacrtSnijega je nacrt koji je proban prema sadašnjim primijenjenim
// postavkama, uz njih.
func probaniNacrtSnijega(baza *sql.DB) (NacrtSnijega, ZapisSnijega, error) {
	const neproban = "nacrt postavki snijega nije proban; isprobaj ga na povijesti, pa primijeni"
	n, z, err := nacrtIPrimijenjene(baza, neproban)
	switch {
	case err != nil:
		return n, z, err
	case n.Proba == nil:
		return n, z, errors.New(neproban)
	case n.Proba.Primijenjene != z.Postavke:
		return n, z, errors.New("primijenjene postavke su se promijenile otkad je nacrt proban; isprobaj ga iznova")
	}
	return n, z, nil
}

// upisiPrimijenjeneSnijega upisuje redak primijenjenih postavki.
func upisiPrimijenjeneSnijega(baza *sql.DB, z ZapisSnijega, kad string) error {
	sadrzaj, _ := json.Marshal(z) // brojevi su provjereni, a tekst se uvijek da zapisati
	_, err := baza.Exec(`INSERT INTO model_postavke (kljuc, kad, sadrzaj) VALUES (?, ?, ?)
		ON CONFLICT(kljuc) DO UPDATE SET kad = excluded.kad, sadrzaj = excluded.sadrzaj`, kljucSnijega, kad, string(sadrzaj))
	return err
}
