package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/repository"
)

// Slanje ovjerenog akta primateljima "na znanje" preko poslužitelja e-pošte
// tvrtke (Exchange), s adrese i računa prijavljenog korisnika. Šalje se tek
// kad je akt ovjeren: privitak je sken s potpisom i žigom kad postoji, inače
// PDF koji program izradi s elektroničkom ovjerom.

// SmijeSlati javlja smije li korisnik slati akt: tko smije pripremati akt
func (s *AktService) SmijeSlati(perms *models.UserPermissions, u *models.User, a *models.Akt) bool {
	return s.smijePripremiti(perms, u, a)
}

// SetPosta daje servisu zadane postavke poslužitelja e-pošte iz gocop.toml;
// postavke spremljene u programu (Administracija) imaju prednost
func (s *AktService) SetPosta(p posta.Postavke) { s.posta = p }

// Posta vraća važeće postavke poslužitelja: iz baze ako su spremljene, inače zadane
func (s *AktService) Posta(ctx context.Context) posta.Postavke {
	v, err := s.repo.GetPostavka(ctx, repository.PostavkaPosta)
	if err != nil || v == "" {
		return s.posta
	}
	var p posta.Postavke
	if json.Unmarshal([]byte(v), &p) != nil {
		return s.posta
	}
	p.TLS, p.DopustiBasic, p.Istek = s.posta.TLS, s.posta.DopustiBasic, s.posta.Istek
	return p
}

// PostaSpremljena javlja jesu li postavke spremljene u programu (a ne samo u datoteci)
func (s *AktService) PostaSpremljena(ctx context.Context) bool {
	v, _ := s.repo.GetPostavka(ctx, repository.PostavkaPosta)
	return v != ""
}

// SpremiPostu sprema postavke poslužitelja; smije samo administrator
func (s *AktService) SpremiPostu(ctx context.Context, perms *models.UserPermissions, p posta.Postavke) error {
	if perms == nil || !perms.IsGlobalAdmin {
		return ErrUnauthorized
	}
	p.Posluzitelj = strings.TrimSpace(p.Posluzitelj)
	p.Domena = strings.TrimSpace(p.Domena)
	if p.Nacin != posta.NacinSMTP {
		p.Nacin = posta.NacinEWS
	}
	if p.Sigurnost != posta.TLS {
		p.Sigurnost = posta.STARTTLS
	}
	if p.Port < 0 || p.Port > 65535 {
		return fmt.Errorf("port mora biti između 1 i 65535")
	}
	b, err := json.Marshal(struct {
		Nacin, Posluzitelj, Sigurnost, Domena string
		Port                                  int
	}{p.Nacin, p.Posluzitelj, p.Sigurnost, p.Domena, p.Port})
	if err != nil {
		return err
	}
	return s.repo.SavePostavka(ctx, repository.PostavkaPosta, string(b))
}

// PostaPodesena javlja je li slanje e-poštom uključeno
func (s *AktService) PostaPodesena(ctx context.Context) bool { return s.Posta(ctx).Podesena() }

// PostaSpremaPoslano javlja ostaje li poslana poruka u korisnikovoj mapi Poslano (EWS)
func (s *AktService) PostaSpremaPoslano(ctx context.Context) bool {
	return s.Posta(ctx).SpremaPoslano()
}

// PostaDomena je domena sustava Windows za prijavu
func (s *AktService) PostaDomena(ctx context.Context) string { return s.Posta(ctx).Domena }

// PostaPosluzitelj je naziv poslužitelja, za prikaz
func (s *AktService) PostaPosluzitelj(ctx context.Context) string { return s.Posta(ctx).Posluzitelj }

func (s *AktService) kljucPoste() []byte {
	if len(s.kljuc) == 0 {
		return nil
	}
	return posta.Kljuc(s.kljuc.Seed())
}

// RacunPoste vraća korisničko ime i vrijeme zadnje promjene lozinke; prazno kad lozinka nije upisana
func (s *AktService) RacunPoste(ctx context.Context, userID string) (string, time.Time) {
	r, err := s.repo.GetRacunPoste(ctx, userID)
	if err != nil || r == nil {
		return "", time.Time{}
	}
	return r.Korisnik, r.UpdatedAt
}

// SpremiRacunPoste provjeri prijavu na poslužitelju i spremi lozinku
// šifrirano. Kad poslužitelj odbije lozinku, ne sprema se ništa; kad nije
// dostupan (npr. izvan mreže tvrtke), sprema se uz upozorenje.
func (s *AktService) SpremiRacunPoste(ctx context.Context, u *models.User, korisnik, lozinka string) (string, error) {
	korisnik = strings.TrimSpace(korisnik)
	if korisnik == "" {
		korisnik = u.Email
	}
	if korisnik == "" || lozinka == "" {
		return "", fmt.Errorf("upišite korisničko ime i lozinku")
	}
	k := s.kljucPoste()
	if k == nil {
		return "", fmt.Errorf("ključ čvora nije učitan; lozinka se ne može sigurno spremiti")
	}
	upozorenje := ""
	pp := s.Posta(ctx)
	if pp.Podesena() {
		// pokušaj upisano ime, pa DOMENA\korisnik; spremi ono koje prođe
		var err error
		var pokusano []string
		for _, ime := range pp.Imena(korisnik) {
			var proslo string
			proslo, err = posta.Prijavi(ctx, pp, posta.Racun{Korisnik: ime, Lozinka: lozinka})
			pokusano = append(pokusano, ime)
			if err == nil {
				korisnik = proslo
				break
			}
			if !errors.Is(err, posta.ErrPrijava) {
				break
			}
		}
		if errors.Is(err, posta.ErrPrijava) {
			return "", fmt.Errorf("poslužitelj %s je odbio korisničko ime ili lozinku (pokušano: %s, i s domenom poslužitelja); ništa nije spremljeno. Provjerite lozinku prijavom na https://%s u pregledniku; više krivih pokušaja zaključava račun", pp.Posluzitelj, strings.Join(pokusano, ", "), pp.Posluzitelj)
		}
		if err != nil {
			upozorenje = "Lozinka je spremljena, ali prijava nije provjerena: " + err.Error()
		}
	}
	z, err := posta.Zakljucaj(k, lozinka)
	if err != nil {
		return "", err
	}
	if err := s.repo.SaveRacunPoste(ctx, &repository.RacunPoste{UserID: u.ID.String(), Korisnik: korisnik, Lozinka: z}); err != nil {
		return "", err
	}
	return upozorenje, nil
}

// ObrisiRacunPoste briše spremljenu lozinku s ovog čvora
func (s *AktService) ObrisiRacunPoste(ctx context.Context, u *models.User) error {
	return s.repo.DeleteRacunPoste(ctx, u.ID.String())
}

// Adresat je jedna adresa primatelja "na znanje"
type Adresat struct {
	Naziv, Skupina, Adresa string
	Zadnje                 *models.SlanjeAkta // zadnji pokušaj slanja na ovu adresu
}

// Poslano javlja je li akt na ovu adresu već stigao do poslužitelja
func (a Adresat) Poslano() bool { return a.Zadnje != nil && a.Zadnje.Uspjelo }

// AdresatiAkta vraća adrese primatelja "na znanje" sa stanjem slanja, i
// primatelje bez adrese
func (s *AktService) AdresatiAkta(ctx context.Context, a *models.Akt) ([]Adresat, []models.AktPrimatelj, []models.SlanjeAkta) {
	slanja, _ := s.repo.ListSlanja(ctx, a.ID)
	var out []Adresat
	var bez []models.AktPrimatelj
	vidjeno := map[string]bool{}
	for _, p := range a.Primatelji {
		adrese := posta.Adrese(p.Email)
		if len(adrese) == 0 {
			bez = append(bez, p)
			continue
		}
		for _, adr := range adrese {
			if vidjeno[adr] {
				continue
			}
			vidjeno[adr] = true
			x := Adresat{Naziv: p.Naziv, Skupina: p.Skupina, Adresa: adr}
			for i := range slanja {
				if slanja[i].Adresa == adr {
					x.Zadnje = &slanja[i]
				}
			}
			out = append(out, x)
		}
	}
	return out, bez, slanja
}

// PorukaAkta je predmet, tekst i privitak poruke kojom se akt šalje
type PorukaAkta struct {
	Predmet, Tekst, ImeDatoteke string
	PDF                         []byte // PDF ovjerenog akta koji program izradi; sken ima prednost kad postoji
}

// IshodSlanja kaže koliko je adresa primilo akt i koje nisu
type IshodSlanja struct {
	Poslano int
	Greske  []string // "adresa: razlog"
	Kopija  bool     // kopija je stigla pošiljatelju
}

// PosaljiNaZnanje šalje izvornik akta odabranim adresama s popisa "na znanje"
func (s *AktService) PosaljiNaZnanje(ctx context.Context, perms *models.UserPermissions, u *models.User, id string, adrese []string, kopijaMeni bool, poruka PorukaAkta) (*IshodSlanja, error) {
	a, err := s.repo.GetAkt(ctx, id)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, fmt.Errorf("akt ne postoji")
	}
	if !a.Ovjeren() {
		return nil, fmt.Errorf("akt se šalje tek kad je ovjeren")
	}
	if !s.smijePripremiti(perms, u, a) {
		return nil, ErrUnauthorized
	}
	pp := s.Posta(ctx)
	if !pp.Podesena() {
		return nil, fmt.Errorf("slanje e-poštom nije uključeno: administrator upisuje poslužitelj u Administraciji › E-pošta")
	}
	if u.Email == "" {
		return nil, fmt.Errorf("u vašem profilu nema adrese e-pošte; s nje se akt šalje")
	}
	racun, err := s.racunKorisnika(ctx, u)
	if err != nil {
		return nil, err
	}
	pdf, err := s.Izvornik(ctx, a.ID)
	if err != nil || pdf == nil {
		pdf = poruka.PDF // program izrađuje PDF ovjerenog akta
	}
	if len(pdf) == 0 {
		return nil, fmt.Errorf("PDF akta nije dostupan")
	}

	// šalje se samo na adrese s popisa "na znanje"
	svi, _, _ := s.AdresatiAkta(ctx, a)
	var odabrani []Adresat
	for _, x := range svi {
		if slices.Contains(adrese, x.Adresa) {
			odabrani = append(odabrani, x)
		}
	}
	if len(odabrani) == 0 && !kopijaMeni {
		return nil, fmt.Errorf("odaberite barem jednu adresu")
	}
	od := mail.Address{Name: u.FullName, Address: u.Email}
	privitak := []posta.Privitak{{Ime: poruka.ImeDatoteke, Vrsta: "application/pdf", Podaci: pdf}}
	var poruke []posta.Poruka
	for _, x := range odabrani {
		poruke = append(poruke, posta.Poruka{Od: od, Za: mail.Address{Address: x.Adresa}, Predmet: poruka.Predmet, Tekst: poruka.Tekst, Privitci: privitak})
	}
	if kopijaMeni {
		poruke = append(poruke, posta.Poruka{Od: od, Za: od, Predmet: poruka.Predmet, Tekst: poruka.Tekst, Privitci: privitak})
	}
	greske, err := posta.Posalji(ctx, pp, racun, poruke)
	if errors.Is(err, posta.ErrPrijava) {
		return nil, fmt.Errorf("poslužitelj je odbio vašu lozinku e-pošte; ako ste je promijenili, upišite novu u profilu")
	}
	if err != nil {
		return nil, err
	}
	ishod := &IshodSlanja{}
	kad := time.Now()
	var zapisi []models.SlanjeAkta
	for i, x := range odabrani {
		z := models.SlanjeAkta{AktID: a.ID, Adresa: x.Adresa, Naziv: x.Naziv, Skupina: x.Skupina, PoslaoID: u.ID.String(), Poslao: u.FullName,
			Posiljatelj: u.Email, Kad: kad, Uspjelo: greske[i] == nil, Cvor: s.cvor}
		if greske[i] != nil {
			z.Greska = greske[i].Error()
			ishod.Greske = append(ishod.Greske, x.Adresa+": "+z.Greska)
		} else {
			ishod.Poslano++
		}
		zapisi = append(zapisi, z)
	}
	if kopijaMeni {
		ishod.Kopija = greske[len(greske)-1] == nil
	}
	if err := s.repo.SaveSlanja(ctx, zapisi); err != nil {
		return ishod, fmt.Errorf("poslano, ali zapis slanja nije spremljen: %w", err)
	}
	return ishod, nil
}

// racunKorisnika vraća korisnikov račun e-pošte s otključanom lozinkom
func (s *AktService) racunKorisnika(ctx context.Context, u *models.User) (posta.Racun, error) {
	if u == nil {
		return posta.Racun{}, ErrUnauthorized
	}
	racun, err := s.repo.GetRacunPoste(ctx, u.ID.String())
	if err != nil {
		return posta.Racun{}, err
	}
	if racun == nil {
		return posta.Racun{}, ErrNemaLozinkePoste
	}
	lozinka, err := posta.Otkljucaj(s.kljucPoste(), racun.Lozinka)
	if err != nil {
		return posta.Racun{}, err
	}
	return posta.Racun{Korisnik: racun.Korisnik, Lozinka: lozinka}, nil
}

// ErrNemaLozinkePoste: korisnik još nije upisao lozinku e-pošte
var ErrNemaLozinkePoste = errors.New("upišite lozinku e-pošte u profilu (Profil › E-pošta za slanje akata)")

// Sanducic vraća stranicu korisnikove pošte iz mape, najnovije prvo
func (s *AktService) Sanducic(ctx context.Context, u *models.User, mapa, trazi string, stranica, poStranici int) ([]posta.Pismo, int, error) {
	pp := s.Posta(ctx)
	if !pp.Podesena() {
		return nil, 0, fmt.Errorf("e-pošta nije uključena: administrator upisuje poslužitelj u Administraciji › E-pošta")
	}
	r, err := s.racunKorisnika(ctx, u)
	if err != nil {
		return nil, 0, err
	}
	if stranica < 1 {
		stranica = 1
	}
	return posta.Sanducic(ctx, pp, r, mapa, trazi, (stranica-1)*poStranici, poStranici)
}

// MapeSanducica vraća mape s brojem nepročitanih
func (s *AktService) MapeSanducica(ctx context.Context, u *models.User) ([]posta.Mapa, error) {
	r, err := s.racunKorisnika(ctx, u)
	if err != nil {
		return nil, err
	}
	return posta.SveMape(ctx, s.Posta(ctx), r)
}

// PremjestiPisma seli pisma u mapu (deleteditems, archive ili Id korisnikove mape)
func (s *AktService) PremjestiPisma(ctx context.Context, u *models.User, ids []string, mapa string) error {
	r, err := s.racunKorisnika(ctx, u)
	if err != nil {
		return err
	}
	return posta.Premjesti(ctx, s.Posta(ctx), r, ids, mapa)
}

// ObrisiTrajno briše pisma iz Obrisanog (u oporavljive stavke Exchangea)
func (s *AktService) ObrisiTrajno(ctx context.Context, u *models.User, ids []string) error {
	r, err := s.racunKorisnika(ctx, u)
	if err != nil {
		return err
	}
	return posta.ObrisiTrajno(ctx, s.Posta(ctx), r, ids)
}

// OznaciProcitanoVise označi više pisama
func (s *AktService) OznaciProcitanoVise(ctx context.Context, u *models.User, stavke []posta.Stavka, procitano bool) error {
	r, err := s.racunKorisnika(ctx, u)
	if err != nil {
		return err
	}
	return posta.OznaciProcitanoVise(ctx, s.Posta(ctx), r, stavke, procitano)
}

// OznaciProcitano označi pismo pročitanim ili nepročitanim
func (s *AktService) OznaciProcitano(ctx context.Context, u *models.User, id, changeKey string, procitano bool) error {
	r, err := s.racunKorisnika(ctx, u)
	if err != nil {
		return err
	}
	return posta.OznaciProcitano(ctx, s.Posta(ctx), r, id, changeKey, procitano)
}

// ObrisiPismo premješta pismo u Obrisano
func (s *AktService) ObrisiPismo(ctx context.Context, u *models.User, id string) error {
	r, err := s.racunKorisnika(ctx, u)
	if err != nil {
		return err
	}
	return posta.Obrisi(ctx, s.Posta(ctx), r, id)
}

// NovoPismo je pismo koje korisnik šalje iz sandučića
type NovoPismo struct {
	Za, Kopija string // adrese odvojene zarezom
	Predmet    string
	Tekst      string
	HTML       string // oblikovano tijelo iz uređivača; Tekst se tada izvodi iz njega
	OdgovorNa  string // Message-ID
	Privitci   []posta.Privitak
	Proslijedi []string // ID-ovi privitaka iz pisma koje se prosljeđuje
}

// PosaljiPismo šalje pismo s korisnikova računa
func (s *AktService) PosaljiPismo(ctx context.Context, u *models.User, n NovoPismo) error {
	pp := s.Posta(ctx)
	if !pp.Podesena() {
		return fmt.Errorf("e-pošta nije uključena")
	}
	if u.Email == "" {
		return fmt.Errorf("u vašem profilu nema adrese e-pošte; s nje se šalje")
	}
	r, err := s.racunKorisnika(ctx, u)
	if err != nil {
		return err
	}
	m := posta.Poruka{Od: mail.Address{Name: u.FullName, Address: u.Email}, Predmet: strings.TrimSpace(n.Predmet), Tekst: n.Tekst, OdgovorNa: n.OdgovorNa, Privitci: n.Privitci}
	if h := posta.OcistiHTML(n.HTML); h != "" {
		m.HTML = h
		if strings.TrimSpace(m.Tekst) == "" {
			m.Tekst = posta.TekstIzHTML(h)
		}
		ugradiSlike(&m)
	}
	for _, a := range posta.Adrese(n.Za) {
		m.Primatelji = append(m.Primatelji, mail.Address{Address: a})
	}
	for _, a := range posta.Adrese(n.Kopija) {
		m.Kopija = append(m.Kopija, mail.Address{Address: a})
	}
	if len(m.Primatelji) == 0 {
		return fmt.Errorf("upišite barem jednu ispravnu adresu primatelja")
	}
	if m.Predmet == "" {
		return fmt.Errorf("upišite predmet")
	}
	for _, id := range n.Proslijedi {
		p, podaci, err := posta.PreuzmiPrivitak(ctx, pp, r, id)
		if err != nil {
			return fmt.Errorf("privitak za prosljeđivanje: %w", err)
		}
		m.Privitci = append(m.Privitci, posta.Privitak{Ime: p.Ime, Vrsta: p.Vrsta, Podaci: podaci})
	}
	greske, err := posta.Posalji(ctx, pp, r, []posta.Poruka{m})
	if err != nil {
		if errors.Is(err, posta.ErrPrijava) {
			return fmt.Errorf("poslužitelj je odbio vašu lozinku e-pošte; ako ste je promijenili, upišite novu u profilu")
		}
		return err
	}
	if greske[0] != nil {
		return greske[0]
	}
	return nil
}

// Pismo otvara jedno pismo iz korisnikova sandučića
func (s *AktService) Pismo(ctx context.Context, u *models.User, id string) (*posta.Pismo, error) {
	r, err := s.racunKorisnika(ctx, u)
	if err != nil {
		return nil, err
	}
	return posta.ProcitajPismo(ctx, s.Posta(ctx), r, id)
}

// Privitak preuzima datoteku privitka iz korisnikova sandučića
func (s *AktService) Privitak(ctx context.Context, u *models.User, id string) (*posta.PrivitakPisma, []byte, error) {
	r, err := s.racunKorisnika(ctx, u)
	if err != nil {
		return nil, nil, err
	}
	return posta.PreuzmiPrivitak(ctx, s.Posta(ctx), r, id)
}

// ---- adresar tvrtke ----

// Imenik traži osobe u adresaru tvrtke (Exchange) po dijelu imena ili adrese
func (s *AktService) Imenik(ctx context.Context, u *models.User, upit string) ([]posta.Kontakt, error) {
	r, err := s.racunKorisnika(ctx, u)
	if err != nil {
		return nil, err
	}
	return posta.Imenik(ctx, s.Posta(ctx), r, upit)
}

// RazlikaKontakta je jedno polje kontakta uspoređeno s adresarom
type RazlikaKontakta struct {
	Polje, Naziv, GoCOP, Exchange string
	Stanje                        string // isto, novo (goCOP prazan), drugacije, nema (adresar nema)
}

// Razlikuje javlja treba li odluka: novo ili drugačije
func (r RazlikaKontakta) Razlikuje() bool { return r.Stanje == "novo" || r.Stanje == "drugacije" }

// UsporedbaKontakta je jedan djelatnik prema adresaru tvrtke
type UsporedbaKontakta struct {
	User      models.User
	Kontakt   *posta.Kontakt    // najbolji pogodak; nil kad nije pronađen
	Kandidati int               // koliko je osoba adresar vratio za ime
	Polja     []RazlikaKontakta // e-pošta, mobitel, fiksni, redom
	Razlike   []RazlikaKontakta // samo polja koja traže odluku
}

// UsporediImenik prolazi djelatnike i za svakoga u adresaru tvrtke nađe
// osobu istog imena, pa usporedi adresu i telefone. Čita se preko računa
// prijavljenog korisnika.
// napredak, kad je zadan, javlja koliko je djelatnika obrađeno.
func (s *AktService) UsporediImenik(ctx context.Context, perms *models.UserPermissions, u *models.User, sektor string, napredak func(sto string, gotovo, ukupno int)) ([]UsporedbaKontakta, error) {
	if perms == nil || (!perms.IsGlobalAdmin && len(perms.AdminSectors) == 0) {
		return nil, ErrUnauthorized
	}
	r, err := s.racunKorisnika(ctx, u)
	if err != nil {
		return nil, err
	}
	pp := s.Posta(ctx)
	svi, err := s.users.ListUsers(sektor, 0, "", "", "active")
	if err != nil {
		return nil, err
	}
	// adresar se pita za svakoga zasebno; nekoliko upita ide usporedno, jer
	// ih je stotine, a svaki traje koliko i jedan zahtjev poslužitelju
	out := make([]UsporedbaKontakta, len(svi))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var prva error
	gotovo := 0
	javi := func(ime string) {
		mu.Lock()
		gotovo++
		g := gotovo
		mu.Unlock()
		if napredak != nil {
			napredak(ime, g, len(svi))
		}
	}
	red := make(chan int)
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range red {
				x := svi[i]
				u := UsporedbaKontakta{User: x}
				var kandidati []posta.Kontakt
				var err error
				if x.Email != "" {
					kandidati, err = posta.Imenik(ctx, pp, r, x.Email)
				}
				if err == nil && len(kandidati) == 0 {
					kandidati, err = posta.Imenik(ctx, pp, r, x.FullName)
				}
				if err != nil {
					mu.Lock()
					if prva == nil {
						prva = err
					}
					mu.Unlock()
					out[i] = u
					javi(x.FullName)
					continue
				}
				u.Kandidati = len(kandidati)
				if k := najboljiKontakt(kandidati, x); k != nil {
					u.Kontakt = k
					u.Polja = poljaKontakta(x, *k)
					for _, p := range u.Polja {
						if p.Razlikuje() {
							u.Razlike = append(u.Razlike, p)
						}
					}
				}
				out[i] = u
				javi(x.FullName)
			}
		}()
	}
	for i := range svi {
		if strings.TrimSpace(svi[i].FullName) == "" {
			continue
		}
		red <- i
	}
	close(red)
	wg.Wait()
	if prva != nil {
		return nil, prva
	}
	var puni []UsporedbaKontakta
	for i := range out {
		if strings.TrimSpace(svi[i].FullName) != "" {
			puni = append(puni, out[i])
		}
	}
	return puni, nil
}

// najboljiKontakt bira osobu iz adresara: istu adresu, pa isto ime; kad
// je više osoba istog imena, ne pogađa
func najboljiKontakt(k []posta.Kontakt, u models.User) *posta.Kontakt {
	for i := range k {
		if u.Email != "" && strings.EqualFold(k[i].Email, u.Email) {
			return &k[i]
		}
	}
	var istoIme []int
	for i := range k {
		if kljucImena(k[i].Ime) == kljucImena(u.FullName) {
			istoIme = append(istoIme, i)
		}
	}
	if len(istoIme) == 1 {
		return &k[istoIme[0]]
	}
	if len(istoIme) == 0 && len(k) == 1 {
		return &k[0]
	}
	return nil
}

func poljaKontakta(u models.User, k posta.Kontakt) []RazlikaKontakta {
	stanje := func(gocop, exch string, isto bool) string {
		switch {
		case exch == "":
			return "nema"
		case strings.TrimSpace(gocop) == "":
			return "novo"
		case isto:
			return "isto"
		}
		return "drugacije"
	}
	return []RazlikaKontakta{
		{"email", "E-pošta", u.Email, k.Email, stanje(u.Email, k.Email, strings.EqualFold(strings.TrimSpace(u.Email), k.Email))},
		{"mobile_phone", "Mobitel", u.MobilePhone, k.Mobitel, stanje(u.MobilePhone, k.Mobitel, posta.SamoZnamenke(u.MobilePhone) == posta.SamoZnamenke(k.Mobitel))},
		{"phone", "Fiksni telefon", u.Phone, k.Telefon, stanje(u.Phone, k.Telefon, posta.SamoZnamenke(u.Phone) == posta.SamoZnamenke(k.Telefon))},
	}
}

// PrimijeniKontakt upisuje odabrana polja iz adresara u djelatnika
func (s *AktService) PrimijeniKontakt(ctx context.Context, perms *models.UserPermissions, userID string, polja map[string]string) error {
	id, err := uuid.Parse(userID)
	if err != nil {
		return fmt.Errorf("nepoznat djelatnik")
	}
	x, err := s.users.GetUserByID(id)
	if err != nil || x == nil {
		return fmt.Errorf("nepoznat djelatnik")
	}
	req := UpdateUserRequest{ID: x.ID, Username: x.Username, FullName: x.FullName, Title: x.Title, IsGlobalAdmin: x.IsGlobalAdmin, OrgType: x.OrgType, OrgName: x.OrgName,
		Phone: x.Phone, MobilePhone: x.MobilePhone, ShortPhone: x.ShortPhone, ShortMobile: x.ShortMobile, Email: x.Email, IsActive: x.IsActive}
	for polje, v := range polja {
		v = strings.TrimSpace(v)
		switch polje {
		case "email":
			req.Email = strings.ToLower(v)
		case "mobile_phone":
			req.MobilePhone = posta.FormatirajTelefon(v)
		case "phone":
			req.Phone = posta.FormatirajTelefon(v)
		}
	}
	_, err = s.users.UpdateUser(perms, req)
	return err
}

// PredlozenaAdresa je adresa koju obrazac pisma nudi dok korisnik tipka
type PredlozenaAdresa struct {
	Ime   string `json:"ime"`
	Email string `json:"email"`
	Izvor string `json:"izvor"` // djelatnik, primatelj, služba, adresar
}

// AdreseZaPismo traži adrese po dijelu imena ili adrese: među djelatnicima,
// u registru primatelja i službi, pa u adresaru tvrtke (Exchange) kad je
// korisnik povezan. Najviše 15 pogodaka, bez ponavljanja adrese.
func (s *AktService) AdreseZaPismo(ctx context.Context, u *models.User, upit string) []PredlozenaAdresa {
	upit = strings.ToLower(strings.TrimSpace(upit))
	if len([]rune(upit)) < 2 {
		return nil
	}
	var out []PredlozenaAdresa
	vidjeno := map[string]bool{}
	dodaj := func(ime, email, izvor string) {
		email = strings.ToLower(strings.TrimSpace(email))
		if email == "" || vidjeno[email] || len(out) >= 15 {
			return
		}
		if !strings.Contains(strings.ToLower(ime), upit) && !strings.Contains(email, upit) {
			return
		}
		vidjeno[email] = true
		out = append(out, PredlozenaAdresa{Ime: strings.TrimSpace(ime), Email: email, Izvor: izvor})
	}
	if svi, err := s.users.ListUsers("", 0, "", "", "active"); err == nil {
		for _, x := range svi {
			dodaj(x.FullName, x.Email, "djelatnik")
		}
	}
	if pr, err := s.repo.ListPrimatelji(ctx, ""); err == nil {
		for _, p := range pr {
			for _, a := range posta.Adrese(p.Email) {
				dodaj(p.Naziv, a, "primatelj")
			}
		}
	}
	if s.territories != nil {
		if sl, err := s.territories.ListSluzbe(ctx, 0); err == nil {
			for _, x := range sl {
				for _, a := range posta.Adrese(x.Email) {
					dodaj(x.Naziv, a, "služba")
				}
			}
		}
	}
	if len(out) < 15 && u != nil {
		if k, err := s.Imenik(ctx, u, upit); err == nil {
			for _, x := range k {
				dodaj(x.Ime, x.Email, "adresar")
			}
		}
	}
	return out
}

// ---- potpis e-pošte ----

// Potpis vraća korisnikov potpis e-pošte (HTML); prazno kad ga nije spremio
func (s *AktService) Potpis(ctx context.Context, userID string) string {
	h, _ := s.repo.GetPotpis(ctx, userID)
	return h
}

// SpremiPotpis sprema korisnikov potpis; prazan potpis ga briše
func (s *AktService) SpremiPotpis(ctx context.Context, u *models.User, html string) error {
	if u == nil {
		return ErrUnauthorized
	}
	return s.repo.SavePotpis(ctx, &repository.PotpisPoste{UserID: u.ID.String(), HTML: posta.OcistiHTML(html)})
}

// LogoZaPotpis je adresa s koje se logo organizacije prikazuje u potpisu;
// pri slanju se zamijeni ugrađenom slikom
const LogoZaPotpis = "/posta/logo.png"

// ZadaniPotpis slaže potpis po uzoru na potpis iz Outlooka: logo lijevo,
// desno organizacija, VGO i centar, funkcija, ime s titulom, telefoni i
// adresa
func (s *AktService) ZadaniPotpis(u *models.User) string {
	esc := html.EscapeString
	t := models.Terms()
	org := t.OrgName
	if org == "" {
		org = "Hrvatske vode"
	}
	var jedinice []string
	telCOP := ""
	d := najvisaDuznost(u)
	if d != nil && d.SectorID != nil {
		if sektori, err := s.users.ListSectors(); err == nil {
			for _, sek := range sektori {
				if sek.ID == *d.SectorID {
					if sek.VgoName != "" {
						jedinice = append(jedinice, strings.ToUpper(sek.VgoName))
					}
					telCOP = sek.Phone
				}
			}
		}
	}
	if len(jedinice) == 0 && u.OrgName != "" && !strings.EqualFold(u.OrgName, org) {
		jedinice = append(jedinice, strings.ToUpper(u.OrgName))
	}
	if t.Center != "" {
		jedinice = append(jedinice, strings.ToUpper(t.Center))
	}
	plava := "color:#003366"
	var b strings.Builder
	b.WriteString(`<p>Srdačan pozdrav,</p><table cellspacing="0" cellpadding="0" style="border-collapse:collapse"><tr>`)
	if t.HasLogo() {
		b.WriteString(`<td style="padding:6px 14px 0 0;vertical-align:top"><img src="` + LogoZaPotpis + `" alt="` + esc(org) + `" width="64" style="width:64px;height:auto"></td>`)
	}
	b.WriteString(`<td style="padding:6px 0 0 0;vertical-align:top;font-family:Arial,sans-serif;font-size:10.5pt">`)
	b.WriteString(`<div style="` + plava + `"><b>` + strings.ToUpper(esc(org)) + `</b></div>`)
	if len(jedinice) > 0 {
		b.WriteString(`<div style="` + plava + `;font-size:10pt">` + esc(strings.Join(jedinice, "<br>")) + `</div>`)
	}
	if d != nil && d.Title != "" {
		b.WriteString(`<div><b>` + esc(d.Title) + `</b></div>`)
	}
	b.WriteString(`<div><b>` + esc(u.FullName) + `</b>`)
	if u.Title != "" {
		b.WriteString(`, ` + esc(u.Title))
	}
	b.WriteString(`</div>`)
	if u.Phone != "" {
		b.WriteString(`<div>Tel: <span style="` + plava + `">` + esc(u.Phone) + `</span></div>`)
	}
	if telCOP != "" && posta.SamoZnamenke(telCOP) != posta.SamoZnamenke(u.Phone) {
		b.WriteString(`<div>Tel (` + esc(t.Center) + `): <span style="` + plava + `">` + esc(telCOP) + `</span></div>`)
	}
	if u.MobilePhone != "" {
		b.WriteString(`<div>Gsm: <span style="` + plava + `">` + esc(u.MobilePhone) + `</span></div>`)
	}
	if u.Email != "" {
		b.WriteString(`<div><a href="mailto:` + esc(u.Email) + `" style="` + plava + `">` + esc(u.Email) + `</a></div>`)
	}
	b.WriteString(`</td></tr></table>`)
	return strings.ReplaceAll(b.String(), esc("<br>"), "<br>")
}

// ugradiSlike zamjenjuje u HTML-u pisma logo organizacije i slike zapisane
// kao data: URI ugrađenim slikama (cid:), jer ih klijenti tako prikazuju
func ugradiSlike(m *posta.Poruka) {
	if m.HTML == "" {
		return
	}
	t := models.Terms()
	if t.HasLogo() && strings.Contains(m.HTML, LogoZaPotpis) {
		m.HTML = strings.ReplaceAll(m.HTML, `"`+LogoZaPotpis+`"`, `"cid:logo@gocop"`)
		m.Ugradjene = append(m.Ugradjene, posta.Privitak{Ime: "logo" + nastavakSlike(t.LogoMime), Vrsta: t.LogoMime, Podaci: t.Logo, ContentID: "logo@gocop"})
	}
	n := 0
	m.HTML = reDataSlika.ReplaceAllStringFunc(m.HTML, func(x string) string {
		g := reDataSlika.FindStringSubmatch(x)
		podaci, err := base64.StdEncoding.DecodeString(g[2])
		if err != nil {
			return x
		}
		n++
		id := fmt.Sprintf("slika%d@gocop", n)
		m.Ugradjene = append(m.Ugradjene, posta.Privitak{Ime: fmt.Sprintf("slika%d%s", n, nastavakSlike("image/"+g[1])), Vrsta: "image/" + g[1], Podaci: podaci, ContentID: id})
		return `src="cid:` + id + `"`
	})
}

var reDataSlika = regexp.MustCompile(`src="data:image/(png|jpeg|gif|webp);base64,([A-Za-z0-9+/=\s]+)"`)

func nastavakSlike(mime string) string {
	switch strings.ToLower(mime) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/svg+xml":
		return ".svg"
	case "image/webp":
		return ".webp"
	}
	return ""
}

// poredakUloga je red od najviše prema nižoj unutar iste razine: voditelj
// prije zamjenika, sektor prije područja, područje prije dionice
var poredakUloga = []models.Role{
	models.RoleGlobalAdmin, models.RoleNationalLeader, models.RoleNationalDeputy, models.RoleMainCenterLeader, models.RoleMainCenterDeputy,
	models.RoleSectorMainDeputy, models.RoleSectorLeader, models.RoleSectorDeputy, models.RoleSectorAreaDeputy, models.RoleCopLeader, models.RoleCopDeputy,
	models.RoleAreaAdmin, models.RoleAreaLeader, models.RoleAreaDeputy, models.RoleSectionLeader, models.RoleSectionDeputy,
}

func mjestoUloge(r models.Role) int {
	for i, x := range poredakUloga {
		if x == r {
			return i
		}
	}
	return len(poredakUloga) + 1
}

// najvisaDuznost bira najvišu aktivnu dužnost osobe: po razini uloge, pa po
// redu unutar razine; potpis nosi najvišu funkciju, ne nužno primarnu
func najvisaDuznost(u *models.User) *models.Duty {
	var naj *models.Duty
	for i := range u.Duties {
		d := &u.Duties[i]
		if !d.IsActive {
			continue
		}
		if naj == nil || d.Role.Rank() < naj.Role.Rank() || (d.Role.Rank() == naj.Role.Rank() && mjestoUloge(d.Role) < mjestoUloge(naj.Role)) {
			naj = d
		}
	}
	if naj == nil {
		return u.PrimaryDuty()
	}
	return naj
}
