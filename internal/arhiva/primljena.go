package arhiva

// Evidencija primljenih izdanja i pravilo protiv vraćanja na starije.
//
// Paket je cjelovita izjava o letvi, pa čvor uzima samo zadnje izdanje — nikad
// niz starijih. Iz toga slijedi jednostavno pravilo: prima se ono što je novije
// od onoga što već imamo.
//
// Bez te evidencije staro ali ispravno potpisano izdanje prolazi kao i svako
// drugo. To je klasičan povratak unatrag: netko podmetne lanjski paket i letva
// tiho izgubi godinu dana, a ništa u njemu nije krivotvoreno — samo je staro.

import (
	"database/sql"
	"fmt"
	"time"
)

const shemaPrimljenih = `
CREATE TABLE IF NOT EXISTS primljena_izdanja (
  letva        TEXT PRIMARY KEY,
  izdanje      INTEGER NOT NULL,
  otisak       TEXT NOT NULL,
  izdao        TEXT NOT NULL DEFAULT '',
  potpis_valjan INTEGER NOT NULL DEFAULT 0,
  primljeno    TEXT NOT NULL,
  napomena     TEXT NOT NULL DEFAULT ''
);`

// PrimljenoIzdanje je ono što čvor ima o jednoj letvi.
type PrimljenoIzdanje struct {
	Letva        string
	Izdanje      int
	Otisak       string
	Izdao        string
	PotpisValjan bool
	Primljeno    time.Time
	Napomena     string
}

// Primljeno vraća zadnje primljeno izdanje letve; nil kad ga nema.
func Primljeno(db Izvrsitelj, letva string) (*PrimljenoIzdanje, error) {
	var p PrimljenoIzdanje
	var kad string
	var valjan int
	err := db.QueryRow(`SELECT letva, izdanje, otisak, izdao, potpis_valjan, primljeno, napomena
		FROM primljena_izdanja WHERE letva = ?`, letva).
		Scan(&p.Letva, &p.Izdanje, &p.Otisak, &p.Izdao, &valjan, &kad, &p.Napomena)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.PotpisValjan = valjan == 1
	p.Primljeno, _ = time.Parse(time.RFC3339, kad)
	return &p, nil
}

// Unatrag je odbijanje paketa koji nije noviji od onoga što čvor već ima.
type Unatrag struct {
	Letva    string
	Imamo    *PrimljenoIzdanje
	Stize    Manifest
	IstiBroj bool // isti broj izdanja, a drukčiji sadržaj
}

func (u *Unatrag) Error() string {
	if u.IstiBroj {
		return fmt.Sprintf("letva %s: izdanje %d već je primljeno s drugim sadržajem (%s naspram %s) — "+
			"dva različita paketa pod istim brojem",
			u.Letva, u.Stize.Izdanje, kratki(u.Imamo.Otisak), kratki(u.Stize.Otisak))
	}
	return fmt.Sprintf("letva %s: stiže izdanje %d, a čvor ima %d — starije se ne ugrađuje samo od sebe",
		u.Letva, u.Stize.Izdanje, u.Imamo.Izdanje)
}

// SmijeUgraditi javlja smije li paket zamijeniti ono što čvor ima.
//
// Isti broj i isti otisak nije greška: to je isti paket, primljen dvaput.
// Ugradnja tad ne mijenja ništa, ali se ne odbija — prijenos USB-om zna se
// ponoviti, a odbijanje bi izgledalo kao kvar.
func SmijeUgraditi(db Izvrsitelj, m Manifest) error {
	imamo, err := Primljeno(db, m.Letva)
	if err != nil {
		return err
	}
	if imamo == nil {
		return nil
	}
	if m.Izdanje > imamo.Izdanje {
		return nil
	}
	if m.Izdanje == imamo.Izdanje && m.Otisak == imamo.Otisak {
		return nil
	}
	return &Unatrag{Letva: m.Letva, Imamo: imamo, Stize: m,
		IstiBroj: m.Izdanje == imamo.Izdanje}
}

// zapisiPrimljeno pamti što je ugrađeno. Zove se unutar transakcije ugradnje,
// da se evidencija ne razmine sa sadržajem.
func zapisiPrimljeno(db Izvrsitelj, m Manifest, potpisValjan bool, napomena string) error {
	valjan := 0
	if potpisValjan {
		valjan = 1
	}
	_, err := db.Exec(`INSERT INTO primljena_izdanja
		(letva, izdanje, otisak, izdao, potpis_valjan, primljeno, napomena)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(letva) DO UPDATE SET izdanje=excluded.izdanje, otisak=excluded.otisak,
			izdao=excluded.izdao, potpis_valjan=excluded.potpis_valjan,
			primljeno=excluded.primljeno, napomena=excluded.napomena`,
		m.Letva, m.Izdanje, m.Otisak, m.Izdao, valjan,
		time.Now().UTC().Format(time.RFC3339), napomena)
	return err
}
