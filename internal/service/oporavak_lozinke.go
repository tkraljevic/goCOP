package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"

	"github.com/google/uuid"
)

// Oporavak lozinke s konzole čvora. Na mreži je katkad samo jedan globalni
// administrator; kad on zaboravi lozinku, nitko mu je kroz program ne može
// poništiti (rezervni kodovi zamjenjuju PIN, ne lozinku). Tko sjedi za konzolom računala na kojem
// čvor radi ionako drži i bazu, pa mu naredba `gocop -ponisti-lozinku ime`
// daje isto što i Korisnici → Poništi lozinku, samo bez prijave; uz to
// uklanja osobni potpisni ključ, koji nova lozinka ne otvara, i spremljenu
// lozinku sandučića e-pošte, koju bi inače otvarala. Kroz web i
// razmjenu toga nema: naredba čita samo bazu na disku.

// PonistenjeSKonzole je ishod poništenja s konzole
type PonistenjeSKonzole struct {
	Korisnik        *models.User // račun nakon poništenja
	Lozinka         string       // privremena lozinka; pokazuje se jednom i nigdje ne zapisuje
	Opozvano        bool         // prijave, zapamćena računala, prijave na čekanju i privremeni kodovi su ugašeni
	KljucUklonjen   bool         // osobni potpisni ključ, zaključan starom lozinkom, je uklonjen
	SanducicObrisan bool         // spremljena lozinka sandučića e-pošte na ovom čvoru je obrisana
	Aktiviran       bool         // račun je bio isključen, a -aktiviraj ga je uključio
	// AdresaZauzeta je adresa e-pošte uključenog računa koju ima i drugi
	// aktivni račun. Kroz program se takav račun ne uključuje; konzola je
	// alat za oporavak pa ga uključi, ali to javi, jer zajednička adresa
	// gasi PIN objema osobama.
	AdresaZauzeta string
}

// znakoviKonzole su znakovi lozinke s konzole: mala slova i znamenke bez
// onih koji se pri prepisivanju zamijene (0 i o, 1, i i l)
const znakoviKonzole = "abcdefghjkmnpqrstuvwxyz23456789"

// GenerirajLozinkuZaKonzolu slaže privremenu lozinku oblika
// "k7mx-2pqa-hr4t-wz9c": četiri skupine po četiri znaka, oko 79 bita. Ne čita
// se preko telefona nego prepisuje s ekrana, pa smije biti jača od one iz
// Korisnika — vrijedi i izvana, a pripada globalnom administratoru.
func GenerirajLozinkuZaKonzolu() (string, error) {
	var b strings.Builder
	for i := 0; i < 16; i++ {
		if i > 0 && i%4 == 0 {
			b.WriteByte('-')
		}
		idx, err := randomInt(len(znakoviKonzole))
		if err != nil {
			return "", err
		}
		b.WriteByte(znakoviKonzole[idx])
	}
	return b.String(), nil
}

// oporavakLozinke je ono malo servisa što poništenje s konzole treba
type oporavakLozinke struct {
	users   *repository.UserRepository
	auth    *AuthService
	potpisi *repository.PotpisRepository
	akti    *repository.AktiRepository // spremljena lozinka sandučića e-pošte
}

// racun nalazi račun kao prijava: bez obzira na velika i mala slova. Stupac
// username razlikuje slova, pa imenu može odgovarati više računa (tkraljevic
// i TKraljevic); tada vrijedi samo točno upisano ime, a inače je greška s
// popisom, da se ne poništi lozinka tuđeg računa.
func (o *oporavakLozinke) racun(ime string) (*models.User, error) {
	racuni, err := o.users.RacuniPoImenu(ime)
	if err != nil {
		return nil, err
	}
	var id uuid.UUID
	switch len(racuni) {
	case 0:
		return nil, fmt.Errorf("%w: na ovom čvoru nema računa %q", ErrUserNotFound, ime)
	case 1:
		id = racuni[0].ID
	default:
		popis := make([]string, 0, len(racuni))
		for _, r := range racuni {
			if r.Username == ime {
				id = r.ID
			}
			popis = append(popis, fmt.Sprintf("%q (%s)", r.Username, r.FullName))
		}
		if id == uuid.Nil {
			return nil, fmt.Errorf("imenu %q odgovara više računa koji se razlikuju samo u velikim i malim slovima: %s — zadajte ime točno kako je upisano",
				ime, strings.Join(popis, ", "))
		}
	}
	u, err := o.users.GetUserByID(id)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, fmt.Errorf("%w: na ovom čvoru nema računa %q", ErrUserNotFound, ime)
	}
	return u, nil
}

// ponisti daje računu novu privremenu lozinku, kao ResetPassword, ali bez
// administratora: ovlast je pristup konzoli. Ime se traži kao pri prijavi
// (bez razmaka okolo, mala i velika slova svejedno).
//
// Lozinka ide kroz repozitorij, pa ostaje verzija u knjizi i sažetak stiže na
// druge čvorove; račun se zaključava na promjenu lozinke. Na ovom čvoru gase
// se otvorene prijave osobe, zapamćena računala, prijave na čekanju i
// privremeni kodovi. Osobni potpisni ključ, zaključan starom lozinkom,
// uklanja se kroz knjigu. is_active i is_global_admin se ne diraju, osim što
// aktiviraj uključuje isključen račun.
//
// Redoslijed je takav da zastoj ostavlja račun zatvorenim: lozinka prva,
// uključivanje zadnje. Ne uspije li lozinka, ništa nije promijenjeno. Kad
// lozinka jest upisana, a nešto iza nje ne uspije, vraća se ishod s
// lozinkom i greška: lozinka tada vrijedi i mora se pokazati, isključen
// račun ostaje isključen, a naredba se može ponoviti.
func (o *oporavakLozinke) ponisti(ime string, aktiviraj bool) (*PonistenjeSKonzole, error) {
	ctx := context.Background()
	ime = strings.TrimSpace(ime)
	if ime == "" {
		return nil, fmt.Errorf("zadajte korisničko ime: gocop -ponisti-lozinku <korisničko ime>")
	}
	u, err := o.racun(ime)
	if err != nil {
		return nil, err
	}

	temp, err := GenerirajLozinkuZaKonzolu()
	if err != nil {
		return nil, err
	}
	hash, err := o.auth.HashPassword(temp)
	if err != nil {
		return nil, err
	}
	if err := o.users.ResetPassword(u.ID, hash); err != nil {
		return nil, err
	}
	log.Printf("oporavak: lozinka računa %s poništena je s konzole čvora", u.Username)

	ishod := &PonistenjeSKonzole{Korisnik: u, Lozinka: temp}
	osvjezi := func() {
		if svjez, err := o.users.GetUserByID(u.ID); err == nil && svjez != nil {
			ishod.Korisnik = svjez
		}
	}
	osvjezi()

	// Isto što i ResetPassword: otvorene sesije, zapamćena računala, prijave
	// na čekanju i privremeni kodovi te osobe na ovom čvoru više ne vrijede.
	// Obavijest preglednicima (SSE) se ne šalje: ovaj proces nema slušatelja.
	if err := o.auth.EndAllSessions(u.ID); err != nil {
		return ishod, fmt.Errorf("lozinka je poništena, ali otvorene prijave nisu ugašene (ponovite naredbu): %w", err)
	}
	if err := o.auth.opozoviPrijave(u.ID); err != nil {
		return ishod, fmt.Errorf("lozinka je poništena, ali zapamćena računala i kodovi nisu opozvani (ponovite naredbu): %w", err)
	}
	ishod.Opozvano = true

	// Potpisni ključ zaključan je lozinkom računa, a promjena lozinke ga
	// prekljucava trenutnom, ovdje privremenom lozinkom koja ga ne otvara.
	// Ostane li, osoba ne može ni promijeniti lozinku ni ući u program. Već
	// potpisani dokumenti ostaju provjerljivi: certifikat je u svakom PDF-u.
	k, err := o.potpisi.GetKljuc(ctx, u.ID.String())
	if err == nil && k != nil {
		err = o.potpisi.DeleteKljuc(ctx, u.ID.String())
		if err == nil {
			ishod.KljucUklonjen = true
			log.Printf("oporavak: osobni potpisni ključ računa %s uklonjen je (bio je zaključan starom lozinkom)", u.Username)
		}
	}
	if err != nil {
		return ishod, fmt.Errorf("lozinka je poništena, ali potpisni ključ nije uklonjen pa se lozinka ne da promijeniti (ponovite naredbu): %w", err)
	}

	// Kao ResetPassword: lozinka sandučića e-pošte spremljena na ovom čvoru
	// otvarala bi poštu osobe onome tko zna privremenu lozinku. Ne uspije
	// li brisanje, otisak lozinke računa ionako je više ne otključava.
	if o.akti != nil {
		if obrisan, err := o.akti.DeleteRacunPoste(ctx, u.ID.String()); err != nil {
			log.Printf("oporavak: spremljena lozinka sandučića računa %s nije obrisana: %v (otisak lozinke računa je svejedno više ne otključava)", u.Username, err)
		} else if obrisan {
			ishod.SanducicObrisan = true
			log.Printf("oporavak: spremljena lozinka sandučića e-pošte računa %s obrisana je", u.Username)
		}
	}

	if aktiviraj && !ishod.Korisnik.IsActive {
		// svjež zapis: UpdateUser piše sva polja profila
		uk := *ishod.Korisnik
		uk.IsActive = true
		uk.PasswordHash = "" // bez sažetka UpdateUser lozinku ne dira
		if err := o.users.UpdateUser(&uk); err != nil {
			return ishod, fmt.Errorf("lozinka je poništena, ali račun %s nije uključen (ponovite uz -aktiviraj): %w", u.Username, err)
		}
		ishod.Aktiviran = true
		log.Printf("oporavak: račun %s uključen je s konzole čvora", u.Username)
		osvjezi()
		if adresa := strings.TrimSpace(ishod.Korisnik.Email); adresa != "" {
			if n, err := o.users.AktivnihSAdresom(adresa, u.ID); err == nil && n > 0 {
				ishod.AdresaZauzeta = adresa
				log.Printf("oporavak: adresu %s uključenog računa %s ima i drugi aktivni račun", adresa, u.Username)
			}
		}
	}
	return ishod, nil
}

// tabliceOporavka su tablice koje poništenje s konzole piše; bez njih baza
// nije baza ovog izdanja programa
var tabliceOporavka = []string{"users", "sessions", "record_versions",
	"zapamcena_racunala", "prijave_na_cekanju", "kodovi_prijave", "potpisni_kljucevi", "posta_racuni"}

// stupciOporavka su najnoviji stupci koje čitaju upiti računa (tablica,
// stupac); bez njih je bazu pripremilo starije izdanje
var stupciOporavka = [][2]string{{"users", "pin_adresa_potvrdena"}}

// PonistiLozinkuNaCvoru sastavlja ono malo servisa što poništenje treba i
// poništava lozinku računa u bazi čvora. Shemu ne stvara ni ne mijenja: baza
// može biti otvorena u poslužitelju koji radi, a naredba se ne smije
// ponašati kao pokretanje. Verzija u knjizi nastaje u ime čvora cvor.
func PonistiLozinkuNaCvoru(database *sql.DB, cvor, ime string, aktiviraj bool) (*PonistenjeSKonzole, error) {
	if strings.TrimSpace(cvor) == "" {
		return nil, errors.New("čvor nema identifikator (node.id u gocop.toml ili -node)")
	}
	ctx := context.Background()
	for _, t := range tabliceOporavka {
		var n int
		if err := database.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, t).Scan(&n); err != nil {
			return nil, fmt.Errorf("baza se ne može pročitati: %w", err)
		}
		if n == 0 {
			return nil, fmt.Errorf("u bazi nema tablice %s: to nije baza čvora ili ju je pripremilo starije izdanje — pokrenite čvor ovim izdanjem pa ponovite", t)
		}
	}
	// i stupce koje upiti računa čitaju: baza starijeg izdanja ima tablice,
	// ali ne i najnovije stupce, a naredba shemu ne mijenja
	for _, st := range stupciOporavka {
		var n int
		if err := database.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, st[0], st[1]).Scan(&n); err != nil {
			return nil, fmt.Errorf("baza se ne može pročitati: %w", err)
		}
		if n == 0 {
			return nil, fmt.Errorf("u tablici %s nema stupca %s: bazu je pripremilo starije izdanje — pokrenite čvor ovim izdanjem pa ponovite", st[0], st[1])
		}
	}

	rec := ledger.New(database, cvor)
	userRepo := repository.NewUserRepository(database, rec)
	auth := NewAuthService(userRepo, repository.NewSessionRepository(database))
	// Drugom koraku za opoziv treba samo njegova tablica; ključ čvora,
	// pošiljatelj i postavke e-pošte ovdje nisu potrebni
	auth.SetZastitaPrijave(NewDrugiKorak(repository.NewDrugiKorakRepository(database), nil, userRepo, nil))
	o := &oporavakLozinke{users: userRepo, auth: auth, potpisi: repository.NewPotpisRepository(database, rec),
		akti: repository.NewAktiRepository(database, rec)}
	return o.ponisti(ime, aktiviraj)
}
