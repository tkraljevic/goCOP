package ledger

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// serijaStarih je koliko starih vlastitih verzija PotpisiVlastiteStare
// pročita i potpiše u jednoj transakciji
const serijaStarih = 500

// PotpisiVlastiteStare naknadno potpiše verzije koje je ovo računalo
// napisalo bez potpisa, samo za zadane entitete (Odluke (27. 11. 2026.)).
// Primatelj nepotpisanu verziju prava i mreže od autora koji već potpisuje
// ne prima, pa novo računalo bez ovoga stare zapise prava ne bi dobilo
// nikad. Provjera kod primatelja ostaje ista.
//
// Potpisuje se redak točno kako je spremljen u knjizi, istim potpisnikom
// kao pri upisu. U redak se upišu samo potpis i ključ, i to samo dok su
// prazni (uvjet na prazan potpis u UPDATE). Zapis se ne dira.
//
// Ne potpisuje retke pod imenom ovog računala koje je primilo od drugih.
// Od 0.0.70 to se zna: vlastitoj verziji upis stavi primljeno = created_at
// (upisiVlastitu), a primljenoj vrijeme primanja (Apply). Zato ostaje bez
// potpisa i vlastita stara verzija koja se vratila razmjenom nakon što je
// baza ovog računala izgubljena pod 0.0.70 ili novijim, i kad je ključ isti.
//
// Verzije otprije 0.0.70 nemaju primljeno, pa se iz knjige ne vidi tko ih
// je napisao. Mogle su nastati ovdje, vratiti se nakon ponovne instalacije
// (možda pod drugim ključem), ili ih je podmetnuo drugi član: do 0.0.69
// računalo je od svakoga primalo stariju verziju pod svojim imenom, a i
// uvoz .cop i .db je prima. Naknadni potpis podmetnutu verziju pretvori u
// verziju potpisanu ključem ovog računala. Zato se potpisuju samo kad je
// zadano prijePrimanja. Ono se poziva za svaku takvu potpisanu verziju, da
// se može zapisati i pregledati. Pozivatelj odlučuje smije li ih potpisati.
//
// Preskače i retke koje razmjena ne bi primila (ProvjeriPrimljenu: oblik
// oznake, znak | u imenu ili kanalu, vrijeme ispred sata). Redak koji se ne
// da potpisati ostane kakav jest i javi se u grešci, a ostali se svejedno
// potpišu. Drugi poziv ne radi ništa, a bez potpisnika ne radi ništa ni
// prvi. Vraća koliko je redaka potpisano.
func (r *Recorder) PotpisiVlastiteStare(ctx context.Context, entiteti []string, prijePrimanja func(Version)) (int, error) {
	if r.potpisnik.Load() == nil || len(entiteti) == 0 {
		return 0, nil
	}
	sada := Sada()
	var ukupno serijaPotpisa
	od := ""
	for {
		s, err := r.potpisiSeriju(ctx, entiteti, prijePrimanja, od, sada)
		ukupno.potpisano += s.potpisano
		if err != nil {
			return ukupno.potpisano, err
		}
		ukupno.nepotpisivo += s.nepotpisivo
		if ukupno.kvar == nil {
			ukupno.kvar = s.kvar
		}
		if s.procitano < serijaStarih {
			break
		}
		od = s.zadnja
	}
	if ukupno.nepotpisivo > 0 {
		return ukupno.potpisano, fmt.Errorf("%d starih verzija ovog računala nije potpisano: %w", ukupno.nepotpisivo, ukupno.kvar)
	}
	return ukupno.potpisano, nil
}

// serijaPotpisa je ishod jedne serije: koliko je redaka pročitano i koja je
// zadnja oznaka, koliko je potpisano, a koliko se nije dalo potpisati, s
// prvim razlogom
type serijaPotpisa struct {
	procitano, potpisano, nepotpisivo int
	zadnja                            string
	kvar                              error
}

// potpisiSeriju potpiše jednu seriju starih vlastitih verzija iza oznake
// od, u jednoj transakciji. prijePrimanja se zove tek nakon upisa, za svaku
// potpisanu verziju bez vremena primanja.
func (r *Recorder) potpisiSeriju(ctx context.Context, entiteti []string, prijePrimanja func(Version), od string, sada time.Time) (serijaPotpisa, error) {
	var s serijaPotpisa
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return s, err
	}
	defer tx.Rollback()

	serija, bezPrimanja, err := r.citajStare(ctx, tx, entiteti, prijePrimanja != nil, od)
	if err != nil {
		return s, err
	}
	potpisane, err := r.upisiPotpiseStarih(ctx, tx, serija, sada, &s)
	if err != nil {
		return s, err
	}
	if err := tx.Commit(); err != nil {
		return s, err
	}
	for _, v := range potpisane {
		if bezPrimanja[v.VersionID] {
			prijePrimanja(v)
		}
	}
	s.procitano, s.potpisano = len(serija), len(potpisane)
	return s, nil
}

// citajStare pročita jednu seriju nepotpisanih vlastitih verzija zadanih
// entiteta iza oznake od. Verzije bez vremena primanja čita samo uz
// iBezPrimanja, a u mapi uz oznaku javi koja je takva.
func (r *Recorder) citajStare(ctx context.Context, tx *sql.Tx, entiteti []string, iBezPrimanja bool, od string) ([]Version, map[string]bool, error) {
	primanje := `primljeno = created_at`
	if iBezPrimanja {
		primanje = `(primljeno = created_at OR primljeno IS NULL)`
	}
	args := []any{r.nodeID}
	for _, e := range entiteti {
		args = append(args, e)
	}
	args = append(args, od, serijaStarih)
	rows, err := tx.QueryContext(ctx, `SELECT `+columns+`, primljeno IS NULL FROM record_versions
		WHERE node_id = ? AND potpis = '' AND kljuc_autora = ''
		AND entity IN (?`+strings.Repeat(",?", len(entiteti)-1)+`)
		AND `+primanje+` AND version_id > ?
		ORDER BY version_id LIMIT ?`, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("čitanje starih vlastitih verzija: %w", err)
	}
	var serija []Version
	bezPrimanja := map[string]bool{}
	for rows.Next() {
		var bez bool
		v, err := procitajVerziju(rows, &bez)
		if err != nil {
			rows.Close()
			return nil, nil, err
		}
		serija = append(serija, v)
		bezPrimanja[v.VersionID] = bez
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return serija, bezPrimanja, nil
}

// upisiPotpiseStarih potpiše i u tx upiše potpis svake verzije serije koju
// bi razmjena primila. Zadnju pročitanu oznaku i retke koji se ne daju
// potpisati zapiše u s. Vraća verzije kojima je potpis upisan.
func (r *Recorder) upisiPotpiseStarih(ctx context.Context, tx *sql.Tx, serija []Version, sada time.Time, s *serijaPotpisa) ([]Version, error) {
	var potpisane []Version
	for _, v := range serija {
		s.zadnja = v.VersionID
		if ProvjeriPrimljenu(v, r.nodeID, sada) != nil {
			continue
		}
		potpis, kljuc, err := r.potpisiStaru(v)
		if err != nil {
			s.nepotpisivo++
			if s.kvar == nil {
				s.kvar = err
			}
			continue
		}
		res, err := tx.ExecContext(ctx, `UPDATE record_versions SET potpis = ?, kljuc_autora = ?
			WHERE version_id = ? AND potpis = '' AND kljuc_autora = ''`, potpis, kljuc, v.VersionID)
		if err != nil {
			return potpisane, fmt.Errorf("potpis stare verzije %s: %w", v.VersionID, err)
		}
		if k, _ := res.RowsAffected(); k > 0 { //nolint:errcheck // SQLite broj promijenjenih redaka uvijek zna
			potpisane = append(potpisane, v)
		}
	}
	return potpisane, nil
}

// potpisiStaru potpiše verziju potpisnikom knjige; prazan potpis ili ključ
// je greška
func (r *Recorder) potpisiStaru(v Version) (string, string, error) {
	potpis, kljuc, err := r.potpisi(v)
	if err == nil && (potpis == "" || kljuc == "") {
		err = fmt.Errorf("potpis verzije %s/%s je prazan", v.Entity, v.EntityID)
	}
	return potpis, kljuc, err
}
