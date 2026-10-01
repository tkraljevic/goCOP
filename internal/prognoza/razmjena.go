package prognoza

// Izdanje prognoze putuje razmjenom. Prognozu izdaje jedan čvor (uloga
// „izdaje prognozu”); svako njegovo izdanje ide u knjigu verzija kao zapis
// EntitetIzdanja s ključem sata izdanja, a ostali čvorovi ga upišu u svoju
// bazu prognoza kao da su ga sami izračunali. Uz izdanje idu tuđe prognoze
// koje je izdavač tada imao, a namješteni model (pojasi, ulazi, promašaji,
// modeli ispuštanja) ide zasebno, samo kad se promijeni.
//
// Sadržaj je u knjizi sažet (gzip) jer izdanje ima nekoliko tisuća
// vrijednosti, a stiže svaki sat; stara izdanja knjiga nakon dva tjedna
// prorijedi, a u bazi prognoza ostaju kao i dosad.

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

// Entiteti prognoze u knjizi verzija
const (
	EntitetIzdanja = "prognoza_izdanje"
	EntitetModela  = "prognoza_model"
	// KljucModela je jedini zapis modela: svaka promjena je nova verzija
	KljucModela = "lanac"
)

// TudeUnatragSati je koliko sati tuđih prognoza ide uz izdanje: dovoljno da
// čvor koji je dan-dva bio isključen ne ostane bez usporedbe.
const TudeUnatragSati = 48

// Tablica su redci jedne tablice baze prognoza, kakvi jesu: stupci po imenu,
// pa izdanje novije inačice programa s dodatnim stupcem i dalje stiže.
type Tablica struct {
	Stupci []string `json:"s"`
	Redci  [][]any  `json:"r"`
}

// PaketIzdanja je jedno izdanje prognoze kako putuje razmjenom.
// Vrijednosti idu kao redci tablica, kako stoje u bazi izdavača.
type PaketIzdanja struct {
	Zapis  ZapisIzdanja `json:"zapis"`
	Izdane Tablica      `json:"izdane"`
	Dnevne Tablica      `json:"dnevne"`
	Izbor  Tablica      `json:"izbor"`
	Tude   Tablica      `json:"tude"`
	// Oborine su kiša koju je izdavač imao u bazi oborina: mjerenja
	// kišomjera i satna kiša Open-Meteo po točkama slivova, s prognozom.
	// Čvor koji ne izdaje prognozu kišu ne preuzima, pa je ima odavde.
	Oborine map[string]Tablica `json:"oborine,omitempty"`
}

// PaketModela je namješteni model izdavača.
type PaketModela struct {
	Tablice map[string]Tablica `json:"tablice"`
}

// tabliceModela su tablice namještenog modela, redom upisa
var tabliceModela = []string{"pojasi", "ulazi", "promasaji", "operateri", "operater"}

// Omotnica je sadržaj zapisa u knjizi: sažeti paket i ono što se o njemu
// vidi bez raspakiravanja.
type Omotnica struct {
	Izdano int64  `json:"izdano,omitempty"` // sat izdanja, UTC
	Cvor   string `json:"cvor,omitempty"`
	Otisak string `json:"otisak,omitempty"` // SHA-256 paketa modela, da se isti ne šalje dvaput
	Gz     string `json:"gz"`
}

// Zamotaj sažima paket u omotnicu.
func Zamotaj(o Omotnica, paket any) (Omotnica, error) {
	sirovo, err := json.Marshal(paket)
	if err != nil {
		return o, err
	}
	var b bytes.Buffer
	w, _ := gzip.NewWriterLevel(&b, gzip.BestCompression)
	if _, err := w.Write(sirovo); err != nil {
		return o, err
	}
	if err := w.Close(); err != nil {
		return o, err
	}
	o.Gz = base64.StdEncoding.EncodeToString(b.Bytes())
	return o, nil
}

// Odmotaj raspakira paket iz sadržaja zapisa u knjizi.
func Odmotaj(payload []byte, paket any) (Omotnica, error) {
	var o Omotnica
	if err := json.Unmarshal(payload, &o); err != nil {
		return o, err
	}
	z, err := base64.StdEncoding.DecodeString(o.Gz)
	if err != nil {
		return o, err
	}
	r, err := gzip.NewReader(bytes.NewReader(z))
	if err != nil {
		return o, err
	}
	sirovo, err := io.ReadAll(io.LimitReader(r, 256<<20))
	if err != nil {
		return o, err
	}
	d := json.NewDecoder(bytes.NewReader(sirovo))
	d.UseNumber()
	return o, d.Decode(paket)
}

// SastaviIzdanje slaže paket iz upravo zapisanog izdanja.
func SastaviIzdanje(db *sql.DB, ishod *Ishod) (PaketIzdanja, error) {
	var p PaketIzdanja
	z := ZapisIzdanja{Izdano: ishod.Sada, Verzija: ishod.Verzija, Kisa: ishod.Kisa}
	var nastalo int64
	var tude, izbor string
	if err := db.QueryRow(`SELECT nastalo, model, operateri, tude, izbor, cvor FROM izdanja WHERE izdano = ? AND verzija = ?`,
		ishod.Sada, ishod.Verzija).Scan(&nastalo, &z.Model, &z.Operat, &tude, &izbor, &z.Cvor); err != nil {
		return p, fmt.Errorf("zapis o izdanju: %w", err)
	}
	z.Nastalo = time.Unix(nastalo, 0)
	_ = json.Unmarshal([]byte(tude), &z.Tude)
	_ = json.Unmarshal([]byte(izbor), &z.Izbor)
	p.Zapis = z
	for _, t := range []struct {
		u   *Tablica
		sql string
		od  int64
	}{
		{&p.Izdane, `SELECT * FROM izdane WHERE izdano = ?`, ishod.Sada},
		{&p.Dnevne, `SELECT * FROM dnevne WHERE izdano = ?`, ishod.Sada},
		{&p.Izbor, `SELECT * FROM izbor WHERE izdano = ?`, ishod.Sada},
		{&p.Tude, `SELECT * FROM tude WHERE izdano > ?`, ishod.Sada - TudeUnatragSati},
	} {
		var err error
		if *t.u, err = citajTablicu(db, t.sql, t.od); err != nil {
			return p, err
		}
		zaokruzi(t.u)
	}
	return p, nil
}

// OborineUnatragSati je koliko kiše unatrag ide uz izdanje. Izdanje izlazi
// svaki sat, a izvor zna dopuniti i dan unatrag (dnevni zbroj DHMZ-a), pa je
// 48 sati dovoljno da čvor koji prima sva izdanja nema rupa.
const OborineUnatragSati = 48

// SastaviOborine čita kišu iz baze oborina za izdanje u satu sada.
func SastaviOborine(ob *sql.DB, sada int64) (map[string]Tablica, error) {
	out := map[string]Tablica{}
	for ime, upit := range map[string]struct {
		sql string
		od  int64
	}{
		"izmjerene": {`SELECT * FROM izmjerene WHERE kraj > ?`, (sada - OborineUnatragSati) * 3600},
		"satne":     {`SELECT * FROM satne WHERE sat > ?`, sada - OborineUnatragSati},
	} {
		t, err := citajTablicu(ob, upit.sql, upit.od)
		if err != nil {
			return nil, fmt.Errorf("oborine %s: %w", ime, err)
		}
		zaokruzi(&t)
		out[ime] = t
	}
	return out, nil
}

// PrimiOborine upisuje kišu iz izdanja; novije prepisuje starije, kao kad je
// izvor sam dopuni.
func PrimiOborine(ob *sql.DB, t map[string]Tablica) error {
	if ob == nil || len(t) == 0 {
		return nil
	}
	tx, err := ob.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, ime := range []string{"izmjerene", "satne"} {
		if err := upisiTablicu(tx, ime, t[ime], "OR REPLACE"); err != nil {
			return fmt.Errorf("oborine %s: %w", ime, err)
		}
	}
	return tx.Commit()
}

// ImaIzdanjeIzKnjige javlja je li izdanje s tom verzijom knjige već upisano.
func ImaIzdanjeIzKnjige(db *sql.DB, knjiga string) bool {
	var n int
	return db.QueryRow(`SELECT count(*) FROM izdanja WHERE knjiga = ?`, knjiga).Scan(&n) == nil && n > 0
}

// PrimiIzdanje upisuje izdanje drugog čvora u bazu prognoza. knjiga je
// verzija zapisa u knjizi: isto izdanje primljeno drugi put se preskače.
// Vraća je li išta upisano.
func PrimiIzdanje(db *sql.DB, knjiga string, p PaketIzdanja) (bool, error) {
	if ImaIzdanjeIzKnjige(db, knjiga) {
		return false, nil
	}
	if len(p.Tude.Redci) > 0 {
		tx, err := db.Begin()
		if err != nil {
			return false, err
		}
		if err := upisiTablicu(tx, "tude", p.Tude, "OR IGNORE"); err != nil {
			tx.Rollback()
			return false, fmt.Errorf("tuđe prognoze: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
	}
	// Isti sat izdan iznova: staro izdanje seli među ranije, kao kod izdavača.
	if err := ObrisiIzdanje(db, p.Zapis.Izdano); err != nil {
		return false, err
	}
	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	for ime, t := range map[string]Tablica{"izdane": p.Izdane, "dnevne": p.Dnevne, "izbor": p.Izbor} {
		if err := upisiTablicu(tx, ime, t, "OR REPLACE"); err != nil {
			return false, fmt.Errorf("%s: %w", ime, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	z := p.Zapis
	z.Knjiga = knjiga
	if _, err := SpremiZapisIzdanja(db, z); err != nil {
		return false, err
	}
	return true, nil
}

// SastaviModel slaže namješteni model i njegov otisak.
func SastaviModel(db *sql.DB) (PaketModela, string, error) {
	p := PaketModela{Tablice: map[string]Tablica{}}
	h := sha256.New()
	for _, ime := range tabliceModela {
		t, err := citajTablicu(db, `SELECT * FROM `+ime+` ORDER BY 1, 2, 3`)
		if err != nil {
			return p, "", fmt.Errorf("%s: %w", ime, err)
		}
		p.Tablice[ime] = t
		b, _ := json.Marshal(t)
		h.Write([]byte(ime))
		h.Write(b)
	}
	return p, hex.EncodeToString(h.Sum(nil)), nil
}

// PrimiModel zamjenjuje namješteni model modelom izdavača.
func PrimiModel(db *sql.DB, p PaketModela) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, ime := range tabliceModela {
		t, ima := p.Tablice[ime]
		if !ima {
			continue
		}
		if _, err := tx.Exec(`DELETE FROM ` + ime); err != nil {
			return err
		}
		if err := upisiTablicu(tx, ime, t, ""); err != nil {
			return fmt.Errorf("%s: %w", ime, err)
		}
	}
	return tx.Commit()
}

// ZadnjiIzdavac kaže koji je čvor izdao zadnje izdanje i je li ono stiglo
// razmjenom.
func ZadnjiIzdavac(db *sql.DB) (cvor string, primljeno bool, err error) {
	var knjiga string
	err = db.QueryRow(`SELECT cvor, knjiga FROM izdanja ORDER BY izdano DESC, verzija DESC LIMIT 1`).Scan(&cvor, &knjiga)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return cvor, knjiga != "", err
}

// ZadnjaKisa vraća kišu po međuslivovima kakvu je imalo zadnje izdanje, u
// obliku OborineOkoSada (ključ OborinaKljuc, dan 0 su 24 sata do izdanja).
// Čvor koji ne izdaje prognozu kišu ne preuzima, pa je ima samo odavde.
func ZadnjaKisa(db *sql.DB) (map[string]DnevniNiz, int64, error) {
	var izdano, verzija int64
	err := db.QueryRow(`SELECT izdano, verzija FROM kisa_izdanja ORDER BY izdano DESC, verzija DESC LIMIT 1`).Scan(&izdano, &verzija)
	if err == sql.ErrNoRows {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	r, err := db.Query(`SELECT sliv, dan, mm FROM kisa_izdanja WHERE izdano = ? AND verzija = ?`, izdano, verzija)
	if err != nil {
		return nil, 0, err
	}
	defer r.Close()
	out := map[string]DnevniNiz{}
	for r.Next() {
		var sliv string
		var dan int64
		var mm float64
		if err := r.Scan(&sliv, &dan, &mm); err != nil {
			return nil, 0, err
		}
		if out[sliv] == nil {
			out[sliv] = DnevniNiz{}
		}
		out[sliv][dan] = mm
	}
	return out, izdano, r.Err()
}

// zaokruzi skraćuje realne brojeve na tisućinku: vodostaj u cm i protok u
// m³/s ne trebaju više, a znamenke iza toga su polovica veličine izdanja.
func zaokruzi(t *Tablica) {
	for _, red := range t.Redci {
		for i, v := range red {
			if f, ok := v.(float64); ok {
				red[i] = math.Round(f*1000) / 1000
			}
		}
	}
}

func citajTablicu(db *sql.DB, upit string, args ...any) (Tablica, error) {
	r, err := db.Query(upit, args...)
	if err != nil {
		return Tablica{}, err
	}
	defer r.Close()
	stupci, err := r.Columns()
	if err != nil {
		return Tablica{}, err
	}
	t := Tablica{Stupci: stupci}
	for r.Next() {
		red := make([]any, len(stupci))
		ptr := make([]any, len(stupci))
		for i := range red {
			ptr[i] = &red[i]
		}
		if err := r.Scan(ptr...); err != nil {
			return Tablica{}, err
		}
		for i, v := range red {
			if b, ok := v.([]byte); ok {
				red[i] = string(b)
			}
		}
		t.Redci = append(t.Redci, red)
	}
	return t, r.Err()
}

// upisiTablicu upisuje retke; stupce kojih ova baza nema preskače, da
// izdanje novije inačice programa ne zapne na starijoj.
func upisiTablicu(tx *sql.Tx, ime string, t Tablica, nacin string) error {
	if len(t.Redci) == 0 {
		return nil
	}
	postoje := map[string]bool{}
	r, err := tx.Query(`SELECT name FROM pragma_table_info(?)`, ime)
	if err != nil {
		return err
	}
	for r.Next() {
		var s string
		if err := r.Scan(&s); err != nil {
			r.Close()
			return err
		}
		postoje[s] = true
	}
	r.Close()
	var stupci []string
	var indeksi []int
	for i, s := range t.Stupci {
		if postoje[s] {
			stupci = append(stupci, s)
			indeksi = append(indeksi, i)
		}
	}
	if len(stupci) == 0 {
		return fmt.Errorf("nijedan stupac ne postoji")
	}
	upit := `INSERT ` + nacin + ` INTO ` + ime + ` (` + strings.Join(stupci, ", ") + `) VALUES (?` + strings.Repeat(", ?", len(stupci)-1) + `)`
	stmt, err := tx.Prepare(upit)
	if err != nil {
		return err
	}
	defer stmt.Close()
	args := make([]any, len(stupci))
	for _, red := range t.Redci {
		if len(red) != len(t.Stupci) {
			return fmt.Errorf("redak s %d vrijednosti za %d stupaca", len(red), len(t.Stupci))
		}
		for j, i := range indeksi {
			args[j] = vrijednost(red[i])
		}
		if _, err := stmt.Exec(args...); err != nil {
			return err
		}
	}
	return nil
}

// vrijednost vraća broj iz JSON-a kao cijeli ili realni broj
func vrijednost(v any) any {
	n, ok := v.(json.Number)
	if !ok {
		return v
	}
	if i, err := strconv.ParseInt(string(n), 10, 64); err == nil {
		return i
	}
	f, _ := n.Float64()
	return f
}

// OpisIzdanja je zadnje izdanje u bazi: za koji sat, kad je nastalo, koji
// ga je čvor izdao i je li stiglo razmjenom.
type OpisIzdanja struct {
	Izdano    time.Time
	Nastalo   time.Time
	Cvor      string
	Primljeno bool
}

// OpisZadnjegIzdanja opisuje zadnje izdanje; ok je false dok izdanja nema.
func OpisZadnjegIzdanja(db *sql.DB) (OpisIzdanja, bool) {
	var o OpisIzdanja
	var izdano, nastalo int64
	var knjiga string
	if db == nil || db.QueryRow(`SELECT izdano, nastalo, cvor, knjiga FROM izdanja ORDER BY izdano DESC, verzija DESC LIMIT 1`).
		Scan(&izdano, &nastalo, &o.Cvor, &knjiga) != nil {
		return o, false
	}
	o.Izdano, o.Nastalo, o.Primljeno = time.Unix(izdano*3600, 0), time.Unix(nastalo, 0), knjiga != ""
	return o, true
}
