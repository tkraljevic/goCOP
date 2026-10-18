package peers

// Ključ čvora uz postojeću bazu. Ključ je identitet računala u mreži: drugi
// čvorovi pamte ga uz ime čvora i razmjenjuju samo s onim tko ga dokaže.
// Na kućnom serveru je 5. 10. 2026. premještanje dijeljene mape (mover) odnijelo
// node-key i gocop.toml s diska na kojem je baza, a čvor je pri sljedećem
// pokretanju tiho napravio novi ključ — novi identitet — pa su ga upareni
// čvorovi odbili („dokazao je drugi ključ od uparenog”). Zato postojeći čvor
// bez ključa ne kreće, a novi identitet dobiva samo na izričit zahtjev.

import (
	"crypto/ed25519"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"gocop/internal/imecvora"
	"gocop/internal/razmjena"
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
	// Postavke su gocop.toml uz bazu kad ni njih nema (zadaje pokretanje,
	// koje jedino zna odakle su postavke); "" kad su pročitane
	Postavke string
}

func (e *KljucNedostaje) Error() string {
	vratite, bezPostavki, ime := "izvornu datoteku "+KeyFileName, "", ""
	if e.Postavke != "" {
		vratite = "izvorne datoteke " + KeyFileName + " i " + filepath.Base(e.Postavke)
		bezPostavki = fmt.Sprintf("\nUz bazu nema ni postavki %s, a u njima je i ime čvora u mreži; vjerojatno su premještene zajedno s ključem.", e.Postavke)
		ime = fmt.Sprintf(" Ako ni postavki više nema, prije toga ih napišite uz bazu s dosadašnjim imenom čvora (redak id = \"…\" pod [node]); bez njih čvor s novim ključem kreće pod zadanim imenom %s.", imecvora.Stari)
	}
	return fmt.Sprintf(`Čvor se ne pokreće: nema ključa čvora %s, a baza %s već ima podatke.%s
Ključ čvora (datoteka %s uz bazu) identitet je ovog računala u mreži. Novi ključ dao bi čvoru novi identitet: upareni čvorovi odbili bi razmjenu s njim („dokazao je drugi ključ od uparenog”), a tajne šifrirane starim ključem (npr. lozinka Exchangea, pristupni računi izvora) više se ne bi dale pročitati.
Što učiniti:
  1. Vratite %s u %s, iz sigurnosne kopije ili s mjesta na koje je premještena (u Dockeru i na NAS-u provjerite montaže i nije li je NAS premjestio na drugi disk), pa ponovno pokrenite čvor.
  2. Samo ako izvornog ključa više nema, a novi identitet je namjeran: zaustavite čvor i pokrenite ga jednom sa zastavicom -%s uz ovu bazu (-db %s), kao korisnik pod kojim inače radi, pa zastavicu uklonite (za Postavu, uslugu sustava, Docker i NAS vidi docs/INSTALACIJA.md, poglavlje 3).%s Zatim ga na čvoru koji ga je primio u mrežu zaboravite, opozovite mu članstvo i ponovno ga primite.`,
		e.Kljuc, e.Baza, bezPostavki, KeyFileName, vratite, filepath.Dir(e.Kljuc), ZastavicaNovogKljuca, e.Baza, ime)
}

// putKljuca je datoteka ključa čvora uz bazu (ista kao u LoadNode)
func putKljuca(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), KeyFileName)
}

// ProvjeriKljucCvora pazi da postojeći čvor ne dobije tiho novi identitet.
// Kad uz bazu nema ključa čvora, a knjiga verzija u njoj nije prazna, vraća
// *KljucNedostaje — osim kad je novi ključ izričito dopušten (dopustiNovi,
// zastavica -novi-kljuc-cvora); tada je noviIdentitet true. Svjež čvor
// (baze nema ili joj je knjiga prazna) prolazi, pa mu LoadNode ključ
// napravi kao i dosad. Poziva se prije otvaranja baze; baza se pritom samo
// čita.
func ProvjeriKljucCvora(dbPath string, dopustiNovi bool) (noviIdentitet bool, err error) {
	kljuc := putKljuca(dbPath)
	if _, err := os.Stat(kljuc); !errors.Is(err, fs.ErrNotExist) {
		// ključ postoji (ili se ne da ni pogledati, pa ga LoadNode ne
		// prepisuje nego javlja grešku)
		if err == nil && dopustiNovi {
			log.Printf("Ključ čvora: %s postoji, pa zastavica -%s nema učinka; uklonite je iz pokretanja", kljuc, ZastavicaNovogKljuca)
		}
		return false, nil
	}
	ima, err := knjigaImaZapisa(dbPath)
	if err != nil {
		return false, fmt.Errorf("ključ čvora %s ne postoji, a ne može se provjeriti ima li baza %s podataka: %w", kljuc, dbPath, err)
	}
	if !ima {
		return false, nil
	}
	if dopustiNovi {
		log.Printf("Ključ čvora: %s ne postoji, a baza ima podatke; zastavicom -%s izričito je dopušten novi ključ, pa čvor dobiva NOVI identitet. Upareni čvorovi odbijat će ga dok ga ne zaborave, ne opozovu mu članstvo i ponovno ga prime u mrežu.", kljuc, ZastavicaNovogKljuca)
		return true, nil
	}
	return false, &KljucNedostaje{Kljuc: kljuc, Baza: dbPath}
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

// ImeIzBaze traži ime ovog čvora u postojećoj bazi, za čvor čije ga
// postavke nemaju (gocop.toml je premješten ili izgubljen): ime pod kojim
// baza pamti javni ključ iz node-key, u članstvima ili među poznatim
// čvorovima. Vraća "" kad baze ili ključa nema, kad baza ključ ne zna i kad
// ga zna pod više imena; tada odlučuje pozivatelj. Baza se samo čita.
func ImeIzBaze(dbPath string) (string, error) {
	if ima, err := postoji(dbPath); !ima {
		return "", err
	}
	kljuc, err := razmjena.LoadKey(putKljuca(dbPath))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	javni := razmjena.PublicKeyString(kljuc.Public().(ed25519.PublicKey))
	var ime string
	err = citajBazu(dbPath, func(baza *sql.DB) (err error) {
		ime, err = imeSKljucem(baza, javni)
		return err
	})
	return ime, err
}

// imeSKljucem vraća ime čvora kojem baza pamti javni ključ (članstvo u
// mreži ili zapis poznatog čvora); "" kad ga ne zna ili ga zna pod više imena
func imeSKljucem(baza *sql.DB, javni string) (ime string, err error) {
	var imena int
	err = baza.QueryRow(`SELECT COUNT(*), COALESCE(MIN(node_id), '') FROM (
		SELECT node_id FROM memberships WHERE public_key = ?
		UNION SELECT node_id FROM peers WHERE public_key = ?)`, javni, javni).Scan(&imena, &ime)
	if imena != 1 {
		return "", err
	}
	return ime, err
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
// upis), preda je fn i zatvori je; javlja i grešku zatvaranja
func citajBazu(dbPath string, fn func(*sql.DB) error) error {
	baza, err := sql.Open("sqlite", dbPath+"?_pragma=query_only(1)&_pragma=busy_timeout(5000)")
	if err == nil {
		err = errors.Join(fn(baza), baza.Close())
	}
	return err
}
