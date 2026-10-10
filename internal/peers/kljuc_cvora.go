package peers

// Ključ čvora uz postojeću bazu. Ključ je identitet računala u mreži: drugi
// čvorovi pamte ga uz ime čvora i razmjenjuju samo s onim tko ga dokaže.
// Na Unraidu je 5. 10. 2026. premještanje dijeljene mape (mover) odnijelo
// node-key i gocop.toml s diska na kojem je baza, a čvor je pri sljedećem
// pokretanju tiho napravio novi ključ — novi identitet — pa su ga upareni
// čvorovi odbili („dokazao je drugi ključ od uparenog”). Zato postojeći čvor
// bez ključa ne kreće, a novi identitet dobiva samo na izričit zahtjev.

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
)

// ZastavicaNovogKljuca je zastavica naredbenog retka kojom administrator
// izričito dopušta da postojeća baza dobije novi ključ čvora, dakle novi
// identitet. Nikad se ne podrazumijeva i ne čita se iz postavki.
const ZastavicaNovogKljuca = "novi-kljuc-cvora"

// KljucNedostaje je odbijeno pokretanje: baza već ima podatke, a ključa
// čvora uz nju nema
type KljucNedostaje struct {
	Kljuc string // datoteka ključa, gdje se očekuje
	Baza  string // baza s podacima
}

func (e *KljucNedostaje) Error() string {
	return fmt.Sprintf(`Čvor se ne pokreće: nema ključa čvora %s, a baza %s već ima podatke.
Ključ čvora (datoteka %s uz bazu) identitet je ovog računala u mreži. Novi ključ dao bi čvoru novi identitet: upareni čvorovi odbili bi razmjenu s njim („dokazao je drugi ključ od uparenog”), a tajne šifrirane starim ključem (npr. lozinka Exchangea, pristupni računi izvora) više se ne bi dale pročitati.
Što učiniti:
  1. Vratite izvornu datoteku %s u %s, iz sigurnosne kopije ili s mjesta na koje je premještena (na Unraidu i u Dockeru provjerite montaže i nije li je mover odnio na drugi disk), pa ponovno pokrenite čvor.
  2. Samo ako izvornog ključa više nema, a novi identitet je namjeran: pokrenite čvor jednom sa zastavicom -%s, pa je uklonite. Zatim ga na čvoru koji ga je primio u mrežu zaboravite, opozovite mu članstvo i ponovno ga primite.`,
		e.Kljuc, e.Baza, KeyFileName, KeyFileName, filepath.Dir(e.Kljuc), ZastavicaNovogKljuca)
}

// putKljuca je datoteka ključa čvora uz bazu (ista kao u LoadNode)
func putKljuca(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), KeyFileName)
}

// ProvjeriKljucCvora pazi da postojeći čvor ne dobije tiho novi identitet.
// Kad uz bazu nema ključa čvora, a knjiga verzija u njoj nije prazna, vraća
// *KljucNedostaje — osim kad je novi ključ izričito dopušten (dopustiNovi,
// zastavica -novi-kljuc-cvora). Svjež čvor (baze nema ili joj je knjiga
// prazna) prolazi, pa mu LoadNode ključ napravi kao i dosad. Poziva se prije
// otvaranja baze; baza se pritom samo čita.
func ProvjeriKljucCvora(dbPath string, dopustiNovi bool) error {
	kljuc := putKljuca(dbPath)
	if _, err := os.Stat(kljuc); !errors.Is(err, fs.ErrNotExist) {
		// ključ postoji (ili se ne da ni pogledati, pa ga LoadNode ne
		// prepisuje nego javlja grešku)
		if err == nil && dopustiNovi {
			log.Printf("Ključ čvora: %s postoji, pa zastavica -%s nema učinka; uklonite je iz pokretanja", kljuc, ZastavicaNovogKljuca)
		}
		return nil
	}
	ima, err := knjigaImaZapisa(dbPath)
	if err != nil {
		return fmt.Errorf("ključ čvora %s ne postoji, a ne može se provjeriti ima li baza %s podataka: %w", kljuc, dbPath, err)
	}
	if !ima {
		return nil
	}
	if dopustiNovi {
		log.Printf("Ključ čvora: %s ne postoji, a baza ima podatke; zastavicom -%s izričito je dopušten novi ključ, pa čvor dobiva NOVI identitet. Upareni čvorovi odbijat će ga dok ga ne zaborave, ne opozovu mu članstvo i ponovno ga prime u mrežu.", kljuc, ZastavicaNovogKljuca)
		return nil
	}
	return &KljucNedostaje{Kljuc: kljuc, Baza: dbPath}
}

// knjigaImaZapisa javlja ima li baza ijednu verziju u knjizi: tada je to
// postojeći čvor. Svjež čvor ni nakon prvog punjenja nema verzija (početni
// podaci ne ulaze u knjigu), a baza koje nema ili je prazna nema ni knjige.
func knjigaImaZapisa(dbPath string) (ima bool, err error) {
	if ima, err = postoji(dbPath); !ima {
		return false, err
	}
	err = citajBazu(dbPath, func(baza *sql.DB) error {
		if err := baza.QueryRow(`SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'record_versions')`).Scan(&ima); err != nil || !ima {
			return err
		}
		return baza.QueryRow(`SELECT EXISTS (SELECT 1 FROM record_versions)`).Scan(&ima)
	})
	return ima, err
}

// postoji javlja postoji li datoteka; to što je nema nije greška
func postoji(put string) (bool, error) {
	_, err := os.Stat(put)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// citajBazu otvori postojeću bazu samo za čitanje (query_only odbija svaki
// upis), preda je fn i zatvori je
func citajBazu(dbPath string, fn func(*sql.DB) error) error {
	baza, err := sql.Open("sqlite", dbPath+"?_pragma=query_only(1)&_pragma=busy_timeout(5000)")
	if err == nil {
		defer baza.Close()
		err = fn(baza)
	}
	return err
}
