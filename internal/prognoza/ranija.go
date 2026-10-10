package prognoza

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Ranija izdanja. Baza prognoza čuva svako izdanje — satne vrijednosti,
// dnevnu prognozu, izbor inačica, zapis o izdanju s upozorenjima i kišu uz
// njega — i ništa od toga ne prorjeđuje; knjiga razmjene drži izdanja
// tjedan dana, baza sva. Iz nje se izdanje traženog sata izvozi kakvo je
// bilo: za ponovno izdani sat vrijedi zadnja verzija, ona koja je i tada
// zamijenila prethodnu.

// IzdanjeDo vraća sat najnovijeg izdanja koje nije kasnije od zadanog sata:
// taj sat kad je tada izdano, inače najbliže ranije. ok je false kad do tog
// sata nema nijednog izdanja.
func IzdanjeDo(db *sql.DB, sat int64) (izdano int64, ok bool, err error) {
	return satIzdanja(db, `SELECT max(izdano) FROM izdane WHERE izdano <= ?`, sat)
}

// PrvoIzdanje vraća sat najstarijeg izdanja u bazi; ok je false dok izdanja
// nema.
func PrvoIzdanje(db *sql.DB) (izdano int64, ok bool, err error) {
	return satIzdanja(db, `SELECT min(izdano) FROM izdane`)
}

// Pocetci su satovi od kojih baza ima koji dio izdanja; 0 je dio kojeg nema
// ni uz jedno izdanje. Baza je počela samo sa satnim vrijednostima, a
// dnevnu prognozu, zapis o izdanju (kiša po međuslivovima, tuđe prognoze u
// rukama, vrijeme računa) i upozorenja pamti tek od kad ih program bilježi,
// pa najstarija izdanja nemaju sve.
type Pocetci struct {
	Izdane     int64 // satne vrijednosti: prvo izdanje
	Dnevne     int64 // dnevna prognoza
	Zapis      int64 // zapis o izdanju, s kišom po međuslivovima
	Upozorenja int64 // zapis s upozorenjima
}

// PocetciZapisa čita od kojeg sata baza ima koji dio izdanja.
func PocetciZapisa(db *sql.DB) (Pocetci, error) {
	var n [4]sql.NullInt64
	err := db.QueryRow(`SELECT (SELECT min(izdano) FROM izdane), (SELECT min(izdano) FROM dnevne),
		(SELECT min(izdano) FROM izdanja), (SELECT min(izdano) FROM izdanja WHERE upozorenja != '')`).
		Scan(&n[0], &n[1], &n[2], &n[3])
	return Pocetci{Izdane: n[0].Int64, Dnevne: n[1].Int64, Zapis: n[2].Int64, Upozorenja: n[3].Int64}, err
}

// satIzdanja čita jedan sat izdanja upitom koji vraća max ili min.
func satIzdanja(db *sql.DB, upit string, args ...any) (int64, bool, error) {
	var sat sql.NullInt64
	if err := db.QueryRow(upit, args...).Scan(&sat); err != nil {
		return 0, false, err
	}
	return sat.Int64, sat.Valid, nil
}

// TudeIzdanja vraća koje je tuđe prognoze izdavač imao u rukama pri zadnjoj
// verziji izdanja: izvor → sat izdanja zadnje tuđe prognoze. Prazna je kad
// za taj sat zapisa nema (izdanje starije od zapisa o izdanjima).
func TudeIzdanja(db *sql.DB, izdano int64) (map[string]int64, error) {
	var s string
	err := db.QueryRow(`SELECT tude FROM izdanja WHERE izdano = ? ORDER BY verzija DESC LIMIT 1`, izdano).Scan(&s)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]int64{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("tuđe prognoze izdanja %d: %w", izdano, err)
	}
	return out, nil
}
