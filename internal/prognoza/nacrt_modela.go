package prognoza

// Nacrt računa nove letve: definicija koju stručnjak slaže na stranici „Model
// prognoze” prije nego što je pripremi i primijeni. Ostaje na ovom računalu, jer
// tablica model_nacrt nije među tablicama modela: ne putuje, ne ulazi u otisak i
// izdanje je ne čita. Dok se nacrt ne primijeni, prognoza se ne mijenja.

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// NacrtLetve je nacrt računa jedne letve: zapis kakav bi išao u pripremu, tko ga
// je zadnji spremio i kad.
type NacrtLetve struct {
	Zapis ZapisLetve `json:"zapis"`
	Tko   string     `json:"tko"`
	Kad   string     `json:"kad"`
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

// SpremiNacrt provjeri definiciju i upiše nacrt letve umjesto zatečenog.
func SpremiNacrt(baza *sql.DB, n NacrtLetve, o OkvirModela) error {
	if err := ProvjeriNacrt(baza, n.Zapis, o); err != nil {
		return err
	}
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
