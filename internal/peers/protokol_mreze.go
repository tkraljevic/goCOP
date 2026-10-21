package peers

// Ime protokola mreže ([sync] protokol). Prava mreža radi pod imenom
// „gocop”, a zasebna instanca za TESTNU SIMULACIJU pod drugim (npr.
// „gocop-sim”), uvijek s novom mapom podataka. Ime ide u certifikat čvora
// (CN), u pronalaženje i u uparivanje, pa računala s različitim imenom jedno
// drugo odbiju. Baza pamti ime pod kojim je ušla u mrežu (network.protokol):
// kopirana mapa podataka pokrenuta s drugim imenom ne kreće, da simulacija
// ne radi s pravim članstvom ni prava mreža s testnim podacima.

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"regexp"
)

// PostaviProtokol zadaje ime protokola mreže ([sync] protokol). Poziva se
// prije Serve i Announce, i prije osnivanja ili ulaska u mrežu: što je
// pokrenuto prije, radi pod dotadašnjim imenom.
func (s *Service) PostaviProtokol(p string) {
	s.protokolMreze.Store(&p)
}

// protokol je ime protokola mreže: zadano (PostaviProtokol), inače Protocol
func (s *Service) protokol() string {
	if p := s.protokolMreze.Load(); p != nil {
		return *p
	}
	return Protocol
}

// ProtokolDrugeMreze je odbijeno pokretanje: baza je ušla u mrežu pod
// jednim imenom protokola, a postavke zadaju drugo
type ProtokolDrugeMreze struct {
	Upisan   string // ime upisano u bazi
	Postavka string // [sync] protokol iz gocop.toml
}

func (e *ProtokolDrugeMreze) Error() string {
	return fmt.Sprintf("goCOP nije pokrenut: ova mapa podataka pripada mreži „%s”, a u gocop.toml piše [sync] protokol = „%s”. Ispravi postavku ili pokreni goCOP s drugom mapom podataka.", e.Upisan, e.Postavka)
}

// ProvjeriProtokolMreze pazi da baza radi pod imenom protokola pod kojim je
// ušla u mrežu. Neispravno ime odbija se uvijek; drugo ime od upisanog vraća
// *ProtokolDrugeMreze. Svježa mapa (baze, tablice network ili mreže još
// nema) prolazi uz svako ispravno ime. Poziva se prije otvaranja baze, pa i
// prije dodavanja stupca protokol; baza se pritom samo čita.
//
// Bazu koja se ne da pročitati provjera samo zapiše u dnevnik i ne odbija:
// ne otvori se ni poslije, pa čvor stane kao i do sada, a Postava ga pri
// nadogradnji vidi kao pao i vrati prethodno izdanje. Odbijeni čvor pod
// Postavom ostao bi upaljen, kao da je nadogradnja uspjela.
func ProvjeriProtokolMreze(dbPath, protokol string) error {
	if err := imeProtokola(protokol); err != nil {
		return err
	}
	upisan, err := protokolIzBaze(dbPath)
	if err != nil {
		log.Printf("Ime protokola mreže: iz baze %s ne može se pročitati pod kojim je imenom ušla u mrežu (%v), pa se ne provjerava", dbPath, err)
		return nil
	}
	if upisan != "" && upisan != protokol {
		return &ProtokolDrugeMreze{Upisan: upisan, Postavka: protokol}
	}
	return nil
}

// najduzeImeProtokola: ime ide u ime certifikata, u probu pronalaženja i u
// soli koda za uparivanje, pa je kratko
const najduzeImeProtokola = 32

// oblikImenaProtokola: mala slova i brojke, crtica samo između njih
var oblikImenaProtokola = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// imeProtokola odbija ime protokola koje ne smije u certifikat ni u soli
// (npr. prazno: certifikat „-sync”)
func imeProtokola(p string) error {
	if len(p) > najduzeImeProtokola || !oblikImenaProtokola.MatchString(p) {
		return errors.New("goCOP nije pokrenut: [sync] protokol u gocop.toml smije imati samo mala slova, brojke i crtice, npr. „gocop” ili „gocop-sim”. Ispravi postavku.")
	}
	return nil
}

// protokolIzBaze čita ime protokola pod kojim je baza ušla u mrežu; samo
// čita, kao knjigaImaZapisa. "" kad baze, tablice network ili mreže nema.
func protokolIzBaze(dbPath string) (protokol string, err error) {
	if ima, err := postoji(dbPath); !ima {
		return "", err
	}
	err = citajBazu(dbPath, func(baza *sql.DB) (err error) {
		protokol, err = upisaniProtokol(baza)
		return err
	})
	return protokol, err
}

// upisaniProtokol je ime protokola iz retka mreže. Baza 0.0.39 ili starija
// stupca nema, a sve su mreže tada radile pod imenom Protocol.
func upisaniProtokol(baza *sql.DB) (string, error) {
	var stupci, sProtokolom int
	if err := baza.QueryRow(`SELECT COUNT(*), COUNT(*) FILTER (WHERE name = 'protokol') FROM pragma_table_info('network')`).Scan(&stupci, &sProtokolom); err != nil || stupci == 0 {
		return "", err
	}
	upit, argumenti := `SELECT protokol FROM network WHERE id = 1`, []any{}
	if sProtokolom == 0 {
		upit, argumenti = `SELECT ? FROM network WHERE id = 1`, []any{Protocol}
	}
	var protokol string
	err := baza.QueryRow(upit, argumenti...).Scan(&protokol)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return protokol, err
}
