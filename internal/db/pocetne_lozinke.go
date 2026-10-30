package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Računi iz prvog punjenja nemaju zajedničku lozinku. Svaki dobije svoju
// nasumičnu privremenu lozinku; u bazu ide samo njezin sažetak, a čitljiv
// popis program drži u memoriji dok ga globalni administrator jednom ne
// preuzme (web/handlers_pocetne_lozinke.go) i ne preda ljudima. Osoba je
// mijenja pri prvoj prijavi (must_change_password). Popis vrijedi 24 sata
// od prvog punjenja (Odluke (30. 10. 2026.)); nakon toga se briše, a
// lozinke se daju poništenjem.

// RokPopisaLozinki je koliko popis privremenih lozinki vrijedi od prvog
// punjenja
const RokPopisaLozinki = 24 * time.Hour

// PocetnaLozinka je privremena lozinka jednog računa iz prvog punjenja
type PocetnaLozinka struct {
	Korisnik string
	Ime      string
	Lozinka  string
}

// znakoviLozinke su znakovi privremene lozinke: mala slova i znamenke bez
// onih koji se pri prepisivanju zamijene (0 i o, 1, i i l)
const znakoviLozinke = "abcdefghjkmnpqrstuvwxyz23456789"

// PrivremenaLozinka slaže privremenu lozinku oblika "k7mx-2pqa-hr4t-wz9c":
// četiri skupine po četiri znaka, oko 79 bita. Prepisuje se s ekrana ili
// papira, pa smije biti jača od one koja se čita preko telefona.
func PrivremenaLozinka() (string, error) {
	var b strings.Builder
	for i := 0; i < 16; i++ {
		if i > 0 && i%4 == 0 {
			b.WriteByte('-')
		}
		v, err := rand.Int(rand.Reader, big.NewInt(int64(len(znakoviLozinke))))
		if err != nil {
			return "", fmt.Errorf("greška pri stvaranju privremene lozinke: %w", err)
		}
		b.WriteByte(znakoviLozinke[v.Int64()])
	}
	return b.String(), nil
}

// sazmiLozinke računa bcrypt sažetke lozinki, više njih odjednom: imenik
// centra ima stotinjak ljudi, a jedan sažetak traje desetinke sekunde
func sazmiLozinke(lozinke []string) ([]string, error) {
	sazeci := make([]string, len(lozinke))
	greske := make([]error, len(lozinke))
	mjesta := make(chan struct{}, runtime.GOMAXPROCS(0))
	var wg sync.WaitGroup
	for i, l := range lozinke {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mjesta <- struct{}{}
			defer func() { <-mjesta }()
			h, err := bcrypt.GenerateFromPassword([]byte(l), bcrypt.DefaultCost)
			sazeci[i], greske[i] = string(h), err
		}()
	}
	wg.Wait()
	return sazeci, errors.Join(greske...)
}

// citajImenik čita imenik uz bazu; bez datoteke vraća prazan popis
func citajImenik() ([]seedUser, error) {
	raw, err := os.ReadFile(ImenikPath)
	if err != nil {
		log.Printf("Imenik %s nije nađen — stvara se samo račun admin; djelatnici stižu sinkronizacijom ili uvozom", ImenikPath)
		return nil, nil
	}
	var users []seedUser
	if err := json.Unmarshal(raw, &users); err != nil {
		return nil, fmt.Errorf("greška pri čitanju %s: %w", ImenikPath, err)
	}
	log.Printf("Imenik: %d djelatnika iz %s", len(users), ImenikPath)
	return users, nil
}

// sektorDirekcija je doseg dužnosti računa admin
var sektorDirekcija = "DIREKCIJA"

// racunAdmin je početni račun admin, kad ga imenik nema
var racunAdmin = seedUser{
	Username: "admin", FullName: "Administrator Sustava", Title: "admin", IsGlobalAdmin: true,
	OrgType: "HRVATSKE_VODE", OrgName: "Centar obrane od poplava", Email: "admin@example.invalid",
	Duties: []seedDuty{{Title: "Glavni administrator COP-a", Role: "GLOBAL_ADMIN", ScopeType: "ALL",
		SectorID: &sektorDirekcija, IsPrimary: true}},
}

// seedUsers upisuje djelatnike iz imenika i račun admin u bazu bez
// djelatnika, svakoga sa svojom privremenom lozinkom. Popis lozinki vraća
// samo uz imenik: na svježem čvoru bez imenika administrator se postavlja na
// stranici Postavljanje, a lozinku računa admin nitko ne zna.
func seedUsers(database *sql.DB) ([]PocetnaLozinka, error) {
	var userCount int
	if err := database.QueryRow("SELECT COUNT(*) FROM users").Scan(&userCount); err != nil {
		return nil, err
	}
	if userCount != 0 {
		return nil, nil
	}
	users, err := citajImenik()
	if err != nil {
		return nil, err
	}
	lozinke, err := noveLozinke(len(users) + 1)
	if err != nil {
		return nil, err
	}
	sazeci, err := sazmiLozinke(lozinke)
	if err != nil {
		return nil, err
	}
	adminUpisan, err := upisiRacune(database, users, sazeci, !imaRacun(users, racunAdmin.Username))
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, nil
	}
	return popisLozinki(users, lozinke, adminUpisan), nil
}

// imaRacun javlja ima li imenik račun s tim korisničkim imenom
func imaRacun(users []seedUser, korisnik string) bool {
	for _, u := range users {
		if u.Username == korisnik {
			return true
		}
	}
	return false
}

// noveLozinke slaže n privremenih lozinki
func noveLozinke(n int) ([]string, error) {
	lozinke := make([]string, n)
	for i := range lozinke {
		l, err := PrivremenaLozinka()
		if err != nil {
			return nil, err
		}
		lozinke[i] = l
	}
	return lozinke, nil
}

// popisLozinki slaže popis za preuzimanje: lozinke[i] pripada users[i], a
// zadnja računu admin, kad ga je program dodao i upisao
func popisLozinki(users []seedUser, lozinke []string, dodanAdmin bool) []PocetnaLozinka {
	popis := make([]PocetnaLozinka, 0, len(lozinke))
	for i, u := range users {
		popis = append(popis, PocetnaLozinka{Korisnik: u.Username, Ime: u.FullName, Lozinka: lozinke[i]})
	}
	if dodanAdmin {
		popis = append(popis, PocetnaLozinka{Korisnik: racunAdmin.Username, Ime: racunAdmin.FullName, Lozinka: lozinke[len(users)]})
	}
	return popis
}

// upisiRacune upisuje račune i njihove dužnosti u jednoj transakciji;
// sazeci[i] je sažetak lozinke računa users[i], a zadnji je za račun admin.
// Uz imenik zapamti i da popis lozinki čeka preuzimanje. Javlja je li račun
// admin upisan.
func upisiRacune(database *sql.DB, users []seedUser, sazeci []string, dodajAdmina bool) (bool, error) {
	tx, err := database.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback() // nakon Commita ne radi ništa; prije njega poništava upis

	insertUserStmt, err := tx.Prepare(`
		INSERT INTO users (
			id, username, password_hash, full_name, title, is_global_admin,
			must_change_password, org_type, org_name, phone, mobile_phone, short_phone, short_mobile, email,
			is_active, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`)
	if err != nil {
		return false, err
	}
	defer insertUserStmt.Close()

	insertDutyStmt, err := tx.Prepare(`
		INSERT INTO duties (
			id, user_id, title, role, scope_type, sector_id, area_id, section_codes,
			is_primary, is_temporary, is_active, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 1, ?)
	`)
	if err != nil {
		return false, err
	}
	defer insertDutyStmt.Close()

	now := time.Now().UTC()
	for i, u := range users {
		if err := upisiRacun(insertUserStmt, insertDutyStmt, u, sazeci[i], now); err != nil {
			return false, err
		}
	}
	adminUpisan := dodajAdmina && upisiAdmina(insertUserStmt, insertDutyStmt, sazeci[len(users)], now)
	if len(users) > 0 {
		if err := zapamtiCekanje(tx, users, sazeci, adminUpisan, now); err != nil {
			return false, err
		}
	}
	return adminUpisan, tx.Commit()
}

// upisiAdmina upisuje račun admin. Kao i dosad je pomoćni, pa njegova greška
// ne zaustavlja punjenje, samo ide u dnevnik.
func upisiAdmina(insertUserStmt, insertDutyStmt *sql.Stmt, sazetak string, now time.Time) bool {
	if err := upisiRacun(insertUserStmt, insertDutyStmt, racunAdmin, sazetak, now); err != nil {
		log.Printf("Početni račun admin nije upisan: %v", err)
		return false
	}
	return true
}

// zapamtiCekanje upisuje račune čiji popis lozinki čeka preuzimanje, s
// vremenom punjenja od kojega teče rok popisa
func zapamtiCekanje(tx *sql.Tx, users []seedUser, sazeci []string, adminUpisan bool, napunjeno time.Time) error {
	racuni := users
	if adminUpisan {
		racuni = append(append([]seedUser(nil), users...), racunAdmin)
	}
	for i, u := range racuni {
		if _, err := tx.Exec(`INSERT INTO pocetne_lozinke_cekaju (user_id, password_hash, napunjeno) VALUES (?, ?, ?)`,
			StableID("user", u.Username).String(), sazeci[i], napunjeno); err != nil {
			return err
		}
	}
	return nil
}

// upisiRacun upisuje jedan račun s dužnostima
func upisiRacun(insertUserStmt, insertDutyStmt *sql.Stmt, u seedUser, sazetak string, now time.Time) error {
	// isti korisnik na svakom čvoru — identitet slijedi iz korisničkog imena
	uIDStr := StableID("user", u.Username).String()
	adminFlag := 0
	if u.IsGlobalAdmin {
		adminFlag = 1
	}
	if _, err := insertUserStmt.Exec(
		uIDStr, u.Username, sazetak, u.FullName, u.Title, adminFlag,
		u.OrgType, u.OrgName, u.Phone, u.MobilePhone, u.ShortPhone, u.ShortMobile, u.Email,
		now, now,
	); err != nil {
		return fmt.Errorf("greška pri unosu korisnika %s: %w", u.Username, err)
	}
	for i, d := range u.Duties {
		dID := StableID("duty", fmt.Sprintf("%s|%d", u.Username, i))
		isPrimaryInt := 0
		if d.IsPrimary {
			isPrimaryInt = 1
		}
		if _, err := insertDutyStmt.Exec(
			dID.String(), uIDStr, d.Title, d.Role, d.ScopeType, d.SectorID, d.AreaID, d.SectionCodes, isPrimaryInt, now,
		); err != nil {
			return fmt.Errorf("greška pri unosu dužnosti '%s' za %s: %w", d.Title, u.Username, err)
		}
	}
	return nil
}

// Popis lozinki živi samo u memoriji. Ako se čvor ugasi prije nego što ga
// netko preuzme (npr. nakon jednokratnog načina rada kao -ugovor, ili kad
// prvo pokretanje padne), pri sljedećem pokretanju računi koji su još
// netaknuti dobiju nove privremene lozinke i novi popis. Netaknut je račun
// sa sažetkom iz punjenja i bez ijedne verzije u knjizi: svaku drugu
// promjenu lozinke, s ovog ili s drugog čvora, obnova ne dira.

// ObnoviPocetneLozinke slaže nove privremene lozinke za račune iz prvog
// punjenja čiji popis nije preuzet. Bez takvih računa vraća prazan popis.
func ObnoviPocetneLozinke(database *sql.DB) ([]PocetnaLozinka, error) {
	cekaju, err := netaknutiRacuni(database)
	if err != nil {
		return nil, err
	}
	if len(cekaju) == 0 {
		// nijedan račun više nije netaknut: popis nema što čekati
		return nil, PocetneLozinkePreuzete(database)
	}
	lozinke, err := noveLozinke(len(cekaju))
	if err != nil {
		return nil, err
	}
	sazeci, err := sazmiLozinke(lozinke)
	if err != nil {
		return nil, err
	}
	if err := zamijeniLozinke(database, cekaju, sazeci); err != nil {
		return nil, err
	}
	popis := make([]PocetnaLozinka, len(cekaju))
	for i, r := range cekaju {
		popis[i] = PocetnaLozinka{Korisnik: r.korisnik, Ime: r.ime, Lozinka: lozinke[i]}
	}
	return popis, nil
}

// PocetneLozinkePreuzete zapisuje da je popis preuzet: nakon toga ga
// ponovno pokretanje više ne slaže
func PocetneLozinkePreuzete(database *sql.DB) error {
	_, err := database.Exec(`DELETE FROM pocetne_lozinke_cekaju`)
	return err
}

// racunNaCekanju je račun iz punjenja čiji popis još nije preuzet
type racunNaCekanju struct {
	id, korisnik, ime, sazetak string
}

// netaknutiRacuni su računi s popisa na čekanju koji još imaju sažetak iz
// punjenja i nemaju verziju u knjizi (entitet "users", repository.EntityUsers)
func netaknutiRacuni(database *sql.DB) ([]racunNaCekanju, error) {
	rows, err := database.Query(`
		SELECT u.id, u.username, u.full_name, u.password_hash
		FROM pocetne_lozinke_cekaju c JOIN users u ON u.id = c.user_id
		WHERE u.password_hash = c.password_hash
		  AND NOT EXISTS (SELECT 1 FROM record_versions v WHERE v.entity = 'users' AND v.entity_id = u.id)
		ORDER BY c.rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var racuni []racunNaCekanju
	for rows.Next() {
		var r racunNaCekanju
		if err := rows.Scan(&r.id, &r.korisnik, &r.ime, &r.sazetak); err != nil {
			return nil, err
		}
		racuni = append(racuni, r)
	}
	return racuni, rows.Err()
}

// zamijeniLozinke upisuje nove sažetke u jednoj transakciji, pri otvaranju
// baze, prije poslužitelja i razmjene. Popis na čekanju nakon toga nosi samo
// nove sažetke, a vrijeme prvog punjenja ostaje, pa nov popis ne produljuje
// rok.
func zamijeniLozinke(database *sql.DB, racuni []racunNaCekanju, sazeci []string) error {
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() // nakon Commita ne radi ništa; prije njega poništava upis
	napunjeno, err := prvoPunjenje(tx)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM pocetne_lozinke_cekaju`); err != nil {
		return err
	}
	for i, r := range racuni {
		if _, err := tx.Exec(`UPDATE users SET password_hash = ?, must_change_password = 1 WHERE id = ? AND password_hash = ?`,
			sazeci[i], r.id, r.sazetak); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO pocetne_lozinke_cekaju (user_id, password_hash, napunjeno) VALUES (?, ?, ?)`,
			r.id, sazeci[i], sql.NullTime{Time: napunjeno, Valid: !napunjeno.IsZero()}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// citac je baza ili transakcija, za upit s jednim retkom
type citac interface {
	QueryRow(query string, args ...any) *sql.Row
}

// prvoPunjenje je vrijeme prvog punjenja popisa na čekanju; bez popisa (ili
// bez zapisanog vremena) nulto vrijeme
func prvoPunjenje(q citac) (time.Time, error) {
	var t sql.NullTime
	err := q.QueryRow(`SELECT napunjeno FROM pocetne_lozinke_cekaju
		WHERE napunjeno IS NOT NULL ORDER BY napunjeno LIMIT 1`).Scan(&t)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	return t.Time, err
}

// RokPocetnihLozinki je trenutak do kojega se popis na čekanju smije
// preuzeti: RokPopisaLozinki nakon prvog punjenja. Bez popisa vraća nulto
// vrijeme.
func RokPocetnihLozinki(database *sql.DB) (time.Time, error) {
	t, err := prvoPunjenje(database)
	if err != nil || t.IsZero() {
		return time.Time{}, err
	}
	return t.Add(RokPopisaLozinki), nil
}

// IstekniPocetneLozinke briše popis na čekanju kojemu je u trenutku sada
// prošao rok, uz redak u dnevniku, i javlja je li ga obrisala. Zove se pri
// pokretanju, prije ObnoviPocetneLozinke: istekao popis se ne slaže iznova.
// Popis iz ranijeg izdanja, bez vremena punjenja, dobije ga iz vremena upisa
// računa (dopuniVrijemePunjenja).
func IstekniPocetneLozinke(database *sql.DB, sada time.Time) (bool, error) {
	if err := dopuniVrijemePunjenja(database, sada); err != nil {
		return false, err
	}
	rok, err := RokPocetnihLozinki(database)
	if err != nil || rok.IsZero() || sada.Before(rok) {
		return false, err
	}
	if err := PocetneLozinkePreuzete(database); err != nil {
		return false, err
	}
	log.Printf("Postavljanje: popis privremenih lozinki iz imenika nitko nije preuzeo do %s (24 sata od prvog punjenja), pa je obrisan; lozinke se sad daju poništenjem",
		rok.Local().Format("02.01.2006. 15:04"))
	return true, nil
}

// dopuniVrijemePunjenja upisuje vrijeme punjenja popisu iz ranijeg izdanja,
// koji ga nema: to je vrijeme upisa računa (users.created_at), jer se računi
// iz imenika upisuju pri punjenju, pa takav star popis istječe čim prođe 24
// sata od tada. Redak bez računa dobije sada.
func dopuniVrijemePunjenja(database *sql.DB, sada time.Time) error {
	if _, err := database.Exec(`UPDATE pocetne_lozinke_cekaju SET napunjeno =
		(SELECT u.created_at FROM users u WHERE u.id = pocetne_lozinke_cekaju.user_id)
		WHERE napunjeno IS NULL`); err != nil {
		return err
	}
	_, err := database.Exec(`UPDATE pocetne_lozinke_cekaju SET napunjeno = ? WHERE napunjeno IS NULL`, sada.UTC())
	return err
}
