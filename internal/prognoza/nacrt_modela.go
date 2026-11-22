package prognoza

// Nacrt računa nove letve: definicija koju stručnjak slaže na stranici „Model
// prognoze” prije nego što je pripremi i primijeni. Ostaje na ovom računalu, jer
// tablica model_nacrt nije među tablicama modela: ne putuje, ne ulazi u otisak i
// izdanje je ne čita. Dok se nacrt ne primijeni, prognoza se ne mijenja.

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// NacrtLetve je nacrt računa jedne letve: zapis kakav bi išao u pripremu, tko ga
// je zadnji spremio i kad.
type NacrtLetve struct {
	Zapis ZapisLetve `json:"zapis"`
	Tko   string     `json:"tko"`
	Kad   string     `json:"kad"`
	// Priprema je ono što je priprema dala za ovu definiciju; brojevi i doseg su
	// tada u zapisu. Nema je dok se nacrt ne pripremi i čim se definicija promijeni.
	Priprema *PripremaNacrta `json:"priprema,omitempty"`
	// Proba je ono što je proba izmjerila za pripremljeni račun; promašaji su tada
	// u zapisu. Nema je dok se račun ne proba i čim se pripremi iznova.
	Proba *ProbaNacrta `json:"proba,omitempty"`
}

// ProbaNacrta je ishod probe uz nacrt: po dosegu promašaj računa i promašaj bez
// modela, s ocjenom.
type ProbaNacrta struct {
	Kad    string        `json:"kad"`
	Dosezi []MjeraDosega `json:"dosezi"`
	// Dani su mjera dnevnog modela, po danu unaprijed; uz satni račun ih nema.
	Dani      []MjeraDana `json:"dani,omitempty"`
	Natpisi   []string    `json:"natpisi"`
	Razdoblje []string    `json:"razdoblje"`
	// Lanac je usporedba s sadašnjim modelom i letve nizvodno; ima je samo
	// proba letve glavnog lanca.
	Lanac *ProbaLanca `json:"lanac,omitempty"`
}

// PripremaNacrta je ishod pripreme uz nacrt, koliko ga stranica pokazuje.
type PripremaNacrta struct {
	Kad      string       `json:"kad"`
	Cvorovi  []float64    `json:"cvorovi"`
	Ucenje   UcenjeRacuna `json:"ucenje"`
	Napomene []string     `json:"napomene"`
}

// ProvjeriNacrt gleda sve što se o definiciji zna prije pripreme, istim provjerama
// kojima počinje priprema. Tako se ne sprema nacrt koji se ne bi dao pripremiti.
func ProvjeriNacrt(baza *sql.DB, z ZapisLetve, o OkvirModela) error {
	var err error
	if o.Glavne, err = SveInacice(baza); err != nil {
		return err
	}
	if z.Metoda == MetodaDnevni {
		return z.provjeriDnevni(o)
	}
	if GlavnaLetva(z.Letva) {
		zapisi, _, err := ZapisiLetvi(baza)
		if err != nil {
			return err
		}
		return z.ProvjeriGlavni(o, zapisi)
	}
	if err := z.provjeriPrijePripreme(o); err != nil {
		return err
	}
	if z.Racun.Rucno.Nagib != nil {
		return z.provjeriRucniRacun()
	}
	return nil
}

// SpremiNacrt provjeri definiciju i upiše nacrt letve umjesto zatečenog. Ono što
// je priprema dala ostaje dok se definicija ne promijeni; promijenjenu treba
// pripremiti iznova.
func SpremiNacrt(baza *sql.DB, n NacrtLetve, o OkvirModela) error {
	if err := ProvjeriNacrt(baza, n.Zapis, o); err != nil {
		return err
	}
	stari, err := Nacrti(baza)
	if err != nil {
		return err
	}
	n.Zapis.Racun.Doseg, n.Zapis.Brojevi, n.Priprema, n.Proba = 0, BrojeviLetve{}, nil, nil
	if s, ima := stari[n.Zapis.Letva]; ima && (s.Priprema != nil || s.Proba != nil) && istaDefinicija(s.Zapis, n.Zapis) {
		n.Zapis.Racun.Doseg, n.Zapis.Brojevi, n.Priprema, n.Proba = s.Zapis.Racun.Doseg, s.Zapis.Brojevi, s.Priprema, s.Proba
	}
	return upisiNacrt(baza, n)
}

// istaDefinicija javlja slažu li se dva zapisa u onome što stručnjak slaže:
// veličini, ulazima, rezervama, razinama, ručnim vrijednostima i dnevnom modelu.
// Doseg i brojeve daje priprema, pa se ne gledaju.
func istaDefinicija(a, b ZapisLetve) bool {
	a.Racun.Doseg, b.Racun.Doseg = 0, 0
	ja, errA := json.Marshal(a.Racun)
	jb, errB := json.Marshal(b.Racun)
	return errA == nil && errB == nil && a.Velicina == b.Velicina && a.Metoda == b.Metoda && string(ja) == string(jb) &&
		bytes.Equal(dnevniBezNull(a.Dnevni), dnevniBezNull(b.Dnevni))
}

// dnevniBezNull svodi prazan blok dnevnog modela na jedno: iz baze se čita kao
// null, a iz obrasca stiže prazan.
func dnevniBezNull(d json.RawMessage) []byte {
	if string(d) == "null" {
		return nil
	}
	return d
}

// PripremiNacrt namjesti račun iz nacrta letve i upiše u isti nacrt što je
// priprema dala. Model i prognoza se ne mijenjaju. kad je vrijeme za prikaz.
func PripremiNacrt(arhiva, baza *sql.DB, letva, kad string, o OkvirModela) (NacrtLetve, error) {
	nacrti, err := Nacrti(baza)
	if err != nil {
		return NacrtLetve{}, err
	}
	n, ima := nacrti[letva]
	if !ima {
		return NacrtLetve{}, errors.New("letva nema nacrt; složi račun i spremi ga, pa pripremi")
	}
	z := n.Zapis
	z.Racun.Doseg, z.Brojevi = 0, BrojeviLetve{}
	pripremi := PripremiLetvu
	if GlavnaLetva(letva) {
		pripremi = PripremiGlavnuLetvu
	}
	p, err := pripremi(arhiva, baza, z, o)
	if err != nil {
		return NacrtLetve{}, err
	}
	n.Zapis, n.Proba = p.Zapis, nil
	n.Priprema = &PripremaNacrta{Kad: kad, Cvorovi: p.Cvorovi, Ucenje: p.Ucenje, Napomene: p.Napomene}
	return n, upisiNacrt(baza, n)
}

// ProbajNacrt mjeri pripremljeni račun iz nacrta unatrag, na vremenu na kojem je
// učio, i upiše u nacrt što je izmjerio. privremena je nova, prazna baza prognoza
// u koju proba piše; baza se samo čita, osim nacrta. Model i prognoza se ne
// mijenjaju.
func ProbajNacrt(arhiva, baza, privremena *sql.DB, letva, kad string) (NacrtLetve, error) {
	nacrti, err := Nacrti(baza)
	if err != nil {
		return NacrtLetve{}, err
	}
	n, ima := nacrti[letva]
	if !ima || n.Priprema == nil {
		return NacrtLetve{}, errors.New("račun nije pripremljen; pripremi ga pa probaj")
	}
	m, err := MjeriLetvu(arhiva, baza, privremena, n.Zapis, OpcijeMjere(n.Priprema.Ucenje))
	if err != nil {
		return NacrtLetve{}, err
	}
	n.Zapis.Brojevi.Promasaji, n.Zapis.Brojevi.Razdoblje = m.Promasaji, m.Razdoblje
	n.Proba = &ProbaNacrta{Kad: kad, Dosezi: m.Dosezi, Natpisi: m.Natpisi, Razdoblje: m.Razdoblje}
	return n, upisiNacrt(baza, n)
}

func upisiNacrt(baza *sql.DB, n NacrtLetve) error {
	sadrzaj, err := json.Marshal(n)
	if err != nil {
		return fmt.Errorf("%s: nacrt modela: %w", n.Zapis.Letva, err)
	}
	_, err = baza.Exec(`INSERT INTO model_nacrt (letva, sadrzaj) VALUES (?, ?)
		ON CONFLICT(letva) DO UPDATE SET sadrzaj = excluded.sadrzaj`, n.Zapis.Letva, string(sadrzaj))
	return err
}

// Nacrti čita nacrte po letvi. Baza nastala prije tablice nema nacrta: stranica je
// čita samo za čitanje, pa je shema ne dograđuje. Redak koji se ne da pročitati
// preskače se, jer je nacrt radna bilješka koju sljedeće spremanje zamijeni.
func Nacrti(db *sql.DB) (map[string]NacrtLetve, error) {
	out := map[string]NacrtLetve{}
	var ima int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'model_nacrt'`).Scan(&ima); err != nil || ima == 0 {
		return out, err
	}
	r, err := db.Query(`SELECT letva, sadrzaj FROM model_nacrt`)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	for r.Next() {
		var letva, sadrzaj string
		if err := r.Scan(&letva, &sadrzaj); err != nil {
			return nil, err
		}
		if n, valja := procitajNacrt(letva, sadrzaj); valja {
			out[letva] = n
		}
	}
	return out, r.Err()
}

// procitajNacrt čita jedan redak; nacrt koji nosi drugu letvu ne valja.
func procitajNacrt(letva, sadrzaj string) (NacrtLetve, bool) {
	var n NacrtLetve
	err := json.Unmarshal([]byte(sadrzaj), &n)
	return n, err == nil && n.Zapis.Letva == letva
}

// ObrisiNacrt odbacuje nacrt letve; nacrt kojeg nema nije greška.
func ObrisiNacrt(baza *sql.DB, letva string) error {
	_, err := baza.Exec(`DELETE FROM model_nacrt WHERE letva = ?`, letva)
	return err
}

// godineProbeDnevnog je koliko se zadnjih godina arhive ostavlja za probu
// dnevnog modela; na svemu prije toga model uči. Kad uz dvije godine ne ostane
// dosta dana za učenje, proba se mjeri na zadnjoj godini.
var godineProbeDnevnog = []int64{2, 1}

// ProbajDnevniNacrt proba dnevni model iz nacrta letve: nauči ga na arhivi do
// dvije godine prije njezina kraja (ili godinu, kad dana nema dosta), izmjeri na
// ostatku i upiše u nacrt što je izmjerio. tocke su kišomjeri registra (OborinskeTocke). Model i prognoza se
// ne mijenjaju.
func ProbajDnevniNacrt(arhiva, baza *sql.DB, tocke []OborinskaTocka, letva, kad string) (NacrtLetve, error) {
	nacrti, err := Nacrti(baza)
	if err != nil {
		return NacrtLetve{}, err
	}
	n, ima := nacrti[letva]
	if !ima || n.Zapis.Metoda != MetodaDnevni {
		return NacrtLetve{}, errors.New("letva nema nacrt dnevnog modela; složi ga i spremi, pa probaj")
	}
	c, err := n.Zapis.DnevniCiljZapisa()
	if err != nil {
		return NacrtLetve{}, err
	}
	nizovi, err := DnevniNizoviCilja(arhiva, tocke, c)
	if err != nil {
		return NacrtLetve{}, err
	}
	m, granica, kraj, err := probajNaKrajuArhive(nizovi, c)
	if err != nil {
		return NacrtLetve{}, err
	}
	dan := func(d int64) string { return time.Unix(d*86400, 0).UTC().Format(time.DateOnly) }
	n.Proba = &ProbaNacrta{Kad: kad, Dani: m.Dani, Natpisi: m.Natpisi, Razdoblje: []string{dan(granica), dan(kraj - 1)}}
	return n, upisiNacrt(baza, n)
}

// probajNaKrajuArhive uči model na arhivi bez zadnjih godina i mjeri ga na njima:
// na dvije, a kad uz njih nema dosta dana za učenje, na jednoj. Vraća i granice
// razdoblja probe.
func probajNaKrajuArhive(nizovi map[string]DnevniNiz, c DnevniCilj) (m MjeraDnevnog, granica, kraj int64, err error) {
	kraj = zadnjiDan(nizovi[c.Letva]) + 1
	for _, godina := range godineProbeDnevnog {
		granica = kraj - godina*365
		if m, err = ProvjeriDnevni(nizovi, c, granica, kraj); err == nil {
			break
		}
	}
	return m, granica, kraj, err
}

// zadnjiDan je zadnji dan dnevnog niza; nula kad je niz prazan.
func zadnjiDan(n DnevniNiz) int64 {
	var zadnji int64
	for d := range n {
		zadnji = max(zadnji, d)
	}
	return zadnji
}

// MetodaNacrta je metoda nacrta letve; prazno kad nacrta nema ili se ne da pročitati.
func MetodaNacrta(baza *sql.DB, letva string) string {
	nacrti, err := Nacrti(baza)
	if err != nil {
		return ""
	}
	return nacrti[letva].Zapis.Metoda
}
