package prognoza

// Nacrt, proba i primjena dnevnog bloka letve dnevnog modela Drave i Dunava.
// Dnevni blok ide svojim putem, neovisno o satnom računu iste letve: nacrt se
// proba na zadnjim godinama arhive, sadašnjim i novim ulazima, i tek proban
// primjenjuje. Nakon primjene model se uči iznova pri sljedećem računu prognoze.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// predmetakDnevnogNacrta razlikuje u tablici nacrta nacrt dnevnog bloka od
// nacrta satnog računa iste letve.
const predmetakDnevnogNacrta = "dnevni:"

// NacrtDnevnog je nacrt dnevnog bloka jedne letve.
type NacrtDnevnog struct {
	Letva string      `json:"letva"`
	Blok  DnevniLetve `json:"blok"`
	Tko   string      `json:"tko"`
	Kad   string      `json:"kad"`
	// Proba je ono što je proba izmjerila za ovaj blok; nema je dok se ne proba
	// i čim se blok promijeni.
	Proba *ProbaDnevnogBloka `json:"proba,omitempty"`
}

// ProbaDnevnogBloka je mjera dnevnog modela po danu unaprijed: s ulazima iz
// nacrta (Dani) i s ulazima koje model ima sada (Prije), na istim danima.
type ProbaDnevnogBloka struct {
	Kad       string      `json:"kad"`
	Dani      []MjeraDana `json:"dani"`
	Prije     []MjeraDana `json:"prije"`
	Natpisi   []string    `json:"natpisi"`
	Razdoblje []string    `json:"razdoblje"`
}

// NatpisBezUzvodnog stoji uz probu letve kojoj primjena gasi uzvodni ispravak.
const NatpisBezUzvodnog = "Ova letva danas ima ispravak po uzvodnim letvama Dunava. On je namješten na model iz programa, pa uz druge ulaze ili slivove prestaje vrijediti"

// NatpisCistiModel kaže što proba dnevnog bloka ne mjeri.
const NatpisCistiModel = "Mjeren je sam dnevni model, bez rezervnih ulaza, bez ispravka po uzvodnim letvama i bez spajanja sa satnim lancem"

// SpremiDnevniNacrt provjeri dnevni blok i upiše ga kao nacrt letve umjesto
// zatečenog. Model se ne mijenja.
func SpremiDnevniNacrt(baza *sql.DB, n NacrtDnevnog) error {
	if err := n.Blok.provjeriGlavniDnevni(n.Letva); err != nil {
		return err
	}
	n.Proba = nil
	return upisiDnevniNacrt(baza, n)
}

func upisiDnevniNacrt(baza *sql.DB, n NacrtDnevnog) error {
	sadrzaj, err := json.Marshal(n)
	if err != nil {
		return fmt.Errorf("%s: nacrt dnevnog modela: %w", n.Letva, err)
	}
	_, err = baza.Exec(`INSERT INTO model_nacrt (letva, sadrzaj) VALUES (?, ?)
		ON CONFLICT(letva) DO UPDATE SET sadrzaj = excluded.sadrzaj`, predmetakDnevnogNacrta+n.Letva, string(sadrzaj))
	return err
}

// ObrisiDnevniNacrt briše nacrt dnevnog bloka letve.
func ObrisiDnevniNacrt(baza *sql.DB, letva string) error {
	_, err := baza.Exec(`DELETE FROM model_nacrt WHERE letva = ?`, predmetakDnevnogNacrta+letva)
	return err
}

// DnevniNacrti čita nacrte dnevnih blokova po letvi. Baza bez tablice nacrta
// nema ih, a redak koji se ne da pročitati se preskače.
func DnevniNacrti(db *sql.DB) (map[string]NacrtDnevnog, error) {
	out := map[string]NacrtDnevnog{}
	var ima int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'model_nacrt'`).Scan(&ima); err != nil || ima == 0 {
		return out, err
	}
	r, err := db.Query(`SELECT letva, sadrzaj FROM model_nacrt WHERE letva LIKE ?`, predmetakDnevnogNacrta+"%")
	if err != nil {
		return nil, err
	}
	defer r.Close()
	for r.Next() {
		var kljuc, sadrzaj string
		if err := r.Scan(&kljuc, &sadrzaj); err != nil {
			return nil, err
		}
		if n, valja := procitajDnevniNacrt(kljuc, sadrzaj); valja {
			out[n.Letva] = n
		}
	}
	return out, r.Err()
}

// procitajDnevniNacrt pretvara redak tablice nacrta u nacrt dnevnog bloka.
func procitajDnevniNacrt(kljuc, sadrzaj string) (NacrtDnevnog, bool) {
	var n NacrtDnevnog
	err := json.Unmarshal([]byte(sadrzaj), &n)
	return n, err == nil && predmetakDnevnogNacrta+n.Letva == kljuc
}

// dnevniNacrtLetve je nacrt dnevnog bloka letve.
func dnevniNacrtLetve(baza *sql.DB, letva string) (NacrtDnevnog, error) {
	nacrti, err := DnevniNacrti(baza)
	if err != nil {
		return NacrtDnevnog{}, err
	}
	n, ima := nacrti[letva]
	if !ima {
		return NacrtDnevnog{}, errors.New("letva nema nacrt dnevnog modela; spremi ga, pa probaj")
	}
	return n, nil
}

// sadasnjiDnevniCilj je cilj dnevnog modela letve kakav sada vrijedi.
func sadasnjiDnevniCilj(letva string) (DnevniCilj, bool) {
	for _, c := range CiljeviDnevnog() {
		if c.Letva == letva {
			return c, true
		}
	}
	return DnevniCilj{}, false
}

// ProbajDnevniGlavni proba dnevni blok iz nacrta: nauči model na arhivi bez
// zadnje dvije godine (ili jedne, kad dana nema dosta) i izmjeri ga na njima,
// pa na istim danima izmjeri i model kakav je sada. tocke su kišomjeri
// registra. Model i prognoza se ne mijenjaju.
func ProbajDnevniGlavni(arhiva, baza *sql.DB, tocke []OborinskaTocka, letva, kad string) (NacrtDnevnog, error) {
	n, err := dnevniNacrtLetve(baza, letva)
	if err != nil {
		return NacrtDnevnog{}, err
	}
	sada, ima := sadasnjiDnevniCilj(letva)
	if !ima {
		return NacrtDnevnog{}, errors.New("letva nije cilj dnevnog modela Drave i Dunava")
	}
	novi := DnevniCilj{Letva: letva, Ulazi: n.Blok.Ulazi, Slivovi: n.Blok.Slivovi, NajmanjeDana: sada.NajmanjeDana}
	nizovi, err := DnevniNizoviCilja(arhiva, tocke, novi)
	if err != nil {
		return NacrtDnevnog{}, err
	}
	m, granica, kraj, err := probajNaKrajuArhive(nizovi, novi)
	if err != nil {
		return NacrtDnevnog{}, err
	}
	dan := func(d int64) string { return time.Unix(d*86400, 0).UTC().Format(time.DateOnly) }
	n.Proba = &ProbaDnevnogBloka{Kad: kad, Dani: m.Dani, Prije: mjeraSadasnjeg(arhiva, tocke, sada, granica, kraj),
		Natpisi: natpisiProbeBloka(m.Natpisi, novi), Razdoblje: []string{dan(granica), dan(kraj - 1)}}
	return n, upisiDnevniNacrt(baza, n)
}

// mjeraSadasnjeg mjeri model kakav je sada na istim danima. Kad se ne da
// izmjeriti, usporedbe nema, a proba novog bloka svejedno vrijedi.
func mjeraSadasnjeg(arhiva *sql.DB, tocke []OborinskaTocka, sada DnevniCilj, granica, kraj int64) []MjeraDana {
	m, err := izmjeriDnevniCilj(arhiva, tocke, sada, granica, kraj)
	if err != nil {
		return nil
	}
	return m.Dani
}

// izmjeriDnevniCilj čita nizove cilja iz arhive, nauči model do granice i
// izmjeri ga od granice do kraja.
func izmjeriDnevniCilj(arhiva *sql.DB, tocke []OborinskaTocka, c DnevniCilj, granica, kraj int64) (MjeraDnevnog, error) {
	nizovi, err := DnevniNizoviCilja(arhiva, tocke, c)
	if err != nil {
		return MjeraDnevnog{}, err
	}
	return ProvjeriDnevni(nizovi, c, granica, kraj)
}

// natpisiProbeBloka su natpisi mjere i ono što proba dnevnog bloka ne mjeri.
func natpisiProbeBloka(mjere []string, novi DnevniCilj) []string {
	out := append(append([]string{}, mjere...), NatpisCistiModel)
	iz, _ := ciljIzPrograma(novi.Letva)
	if _, ima := uzvodniSkupovi[0].koef[novi.Letva]; ima && !istiRacun(novi, iz) {
		out = append(out, NatpisBezUzvodnog)
	}
	return out
}

// PrimijeniDnevniGlavni upiše probani dnevni blok iz nacrta u redak letve i
// odbaci nacrt. Letvi bez retka redak nastaje: s računom iz programa kad je u
// glavnom lancu, inače samo s dnevnim blokom. Razlog je obavezan.
func PrimijeniDnevniGlavni(baza *sql.DB, letva string, promjena PromjenaLetve) error {
	if strings.TrimSpace(promjena.Razlog) == "" {
		return errors.New("upiši razlog; ostaje uz model")
	}
	n, err := probaniDnevniNacrt(baza, letva)
	if err != nil {
		return err
	}
	blok, _ := json.Marshal(n.Blok) // provjeren blok su šifre i razumni brojevi, pa se uvijek da zapisati
	return redomDoGreske(
		func() error { return upisiDnevniBlok(baza, letva, blok, promjena) },
		func() error { return ObrisiDnevniNacrt(baza, letva) },
		func() error { _, err := UcitajDnevneIzZapisa(baza); return err },
	)
}

// probaniDnevniNacrt je nacrt dnevnog bloka letve koji je proban i još prolazi
// provjeru.
func probaniDnevniNacrt(baza *sql.DB, letva string) (NacrtDnevnog, error) {
	n, err := dnevniNacrtLetve(baza, letva)
	if err != nil {
		return NacrtDnevnog{}, err
	}
	if n.Proba == nil {
		return NacrtDnevnog{}, errors.New("dnevni model nije proban; probaj ga, pa primijeni")
	}
	return n, n.Blok.provjeriGlavniDnevni(letva)
}

// redomDoGreske izvodi korake redom i staje na prvoj grešci.
func redomDoGreske(koraci ...func() error) error {
	for _, korak := range koraci {
		if err := korak(); err != nil {
			return err
		}
	}
	return nil
}

// UkloniDnevniGlavni miče dnevni blok iz retka letve, pa se njezin dnevni model
// opet uči iz programa. Razlog je obavezan.
func UkloniDnevniGlavni(baza *sql.DB, letva string, promjena PromjenaLetve) error {
	if strings.TrimSpace(promjena.Razlog) == "" {
		return errors.New("upiši razlog; ostaje uz model")
	}
	zapisi, _, err := ZapisiLetvi(baza)
	if err != nil {
		return err
	}
	if _, err := zapisi[letva].DnevniBlok(); err != nil || !GlavniDnevniCilj(letva) {
		return errors.New("letva nema dnevni model iz zapisa")
	}
	return redomDoGreske(
		func() error { return upisiDnevniBlok(baza, letva, nil, promjena) },
		func() error { _, err := UcitajDnevneIzZapisa(baza); return err },
	)
}

// upisiDnevniBlok upisuje redak letve s novim dnevnim blokom (nil ga miče),
// uz promjenu u povijesti i zatečeni redak kao prethodni.
func upisiDnevniBlok(baza *sql.DB, letva string, blok json.RawMessage, promjena PromjenaLetve) error {
	zapisi, _, err := ZapisiLetvi(baza)
	if err != nil {
		return err
	}
	z := redakZaDnevni(zapisi, letva)
	z.Dnevni = blok
	if blok == nil && z.Metoda == MetodaDnevni {
		// redak je nosio samo dnevni blok, pa bez njega nema što reći
		_, err := baza.Exec(`DELETE FROM model_letve WHERE letva = ?`, letva)
		return err
	}
	return upisiZapisLetve(baza, zapisZaPrimjenu(z, zapisi, promjena), "")
}

// redakZaDnevni je redak letve u koji ide dnevni blok: zatečeni, a kad ga nema,
// račun iz programa za letvu glavnog lanca, inače redak samo s dnevnim blokom.
func redakZaDnevni(zapisi map[string]ZapisLetve, letva string) ZapisLetve {
	if z, ima := zapisi[letva]; ima {
		return z
	}
	if z, ima := ZapisIzLanca(letva); ima {
		return z
	}
	return ZapisLetve{Oblik: OblikModelaLetve, Letva: letva, Metoda: MetodaDnevni, Velicina: "vodostaj"}
}
