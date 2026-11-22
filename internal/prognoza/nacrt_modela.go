package prognoza

// Nacrt računa nove letve: definicija koju stručnjak slaže na stranici „Model
// prognoze” prije nego što je pripremi i primijeni. Ostaje na ovom računalu, jer
// tablica model_nacrt nije među tablicama modela: ne putuje, ne ulazi u otisak i
// izdanje je ne čita. Dok se nacrt ne primijeni, prognoza se ne mijenja.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	Kad       string        `json:"kad"`
	Dosezi    []MjeraDosega `json:"dosezi"`
	Natpisi   []string      `json:"natpisi"`
	Razdoblje []string      `json:"razdoblje"`
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
	return z.provjeriPrijePripreme(o)
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
	if s, ima := stari[n.Zapis.Letva]; ima && s.Priprema != nil && istaDefinicija(s.Zapis, n.Zapis) {
		n.Zapis.Racun.Doseg, n.Zapis.Brojevi, n.Priprema, n.Proba = s.Zapis.Racun.Doseg, s.Zapis.Brojevi, s.Priprema, s.Proba
	}
	return upisiNacrt(baza, n)
}

// istaDefinicija javlja slažu li se dva zapisa u onome što stručnjak slaže:
// veličini, ulazima, rezervama, razinama i ručnim vrijednostima. Doseg i brojeve
// daje priprema, pa se ne gledaju.
func istaDefinicija(a, b ZapisLetve) bool {
	a.Racun.Doseg, b.Racun.Doseg = 0, 0
	ja, errA := json.Marshal(a.Racun)
	jb, errB := json.Marshal(b.Racun)
	return errA == nil && errB == nil && a.Velicina == b.Velicina && a.Metoda == b.Metoda && string(ja) == string(jb)
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
	p, err := PripremiLetvu(arhiva, baza, z, o)
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
