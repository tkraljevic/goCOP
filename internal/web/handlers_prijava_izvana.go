package web

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Prijava izvana na profilu, kod djelatnika i u administraciji e-pošte:
// zapamćena računala, rezervni kodovi, privremeni kod od administratora,
// račun koji šalje PIN i sklopka. Sve radnje rade sa stvarno prijavljenom
// osobom i odbijaju se dok se gleda tuđim očima: tuđe oči ne smiju ni
// napraviti kodove ni zaboraviti nečija računala. Kodovi se pokazuju samo
// na stranici odmah iza radnje, s Cache-Control: no-store.

var errDrugiKorakNedostupan = errors.New("drugi korak prijave izvana nije dostupan na ovom čvoru")

// dolaziIzvana javlja dolazi li zahtjev izvana (tunel ili javna adresa); isto
// pravilo kao za PIN pri prijavi
func dolaziIzvana(r *http.Request) bool {
	k := klijentIz(r)
	return service.IzvanaAdresa(k.KrozPosrednika, k.Adresa)
}

// stvarnaOsoba vraća stvarno prijavljenu osobu i gleda li ona tuđim očima
func stvarnaOsoba(r *http.Request) (*models.User, bool) {
	viewing, _ := r.Context().Value(contextKeyViewing).(bool)
	if u, ok := r.Context().Value(contextKeyRealUsr).(*models.User); ok && u != nil {
		return u, viewing
	}
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	return u, viewing
}

// SetDrugiKorak spaja rukovatelja s drugim korakom prijave izvana
func (h *UsersHandler) SetDrugiKorak(f func() *service.DrugiKorak) { h.drugiKorak = f }

// SetCvor daje ime ovog čvora, za napomenu da kodovi vrijede samo na njemu
func (h *UsersHandler) SetCvor(f func() string) { h.cvor = f }

// imeCvora vraća ime ovog čvora ili prazno
func (h *UsersHandler) imeCvora() string {
	if h.cvor == nil {
		return ""
	}
	return h.cvor()
}

func (h *UsersHandler) dk() *service.DrugiKorak {
	if h.drugiKorak == nil {
		return nil
	}
	return h.drugiKorak()
}

// ProfilPrijaveIzvana je odjeljak „Prijava izvana” na vlastitom profilu.
type ProfilPrijaveIzvana struct {
	TudjimOcima bool   // gleda se tuđim očima: ništa se ne pokazuje ni ne mijenja
	Ukljuceno   bool   // PIN se traži za prijavu izvana
	Domena      string // domena na koju ide PIN bez potvrde administratora
	Adresa      string // maskirana adresa na koju ide PIN; prazno kad ne ide nikamo
	Razlog      string // zašto PIN nema kamo ići
	// StanjeAdrese: u domeni, potvrđena izvan nje (tko, kada), nepotvrđena,
	// zajednička ili nema je; nil kad se ne da pročitati
	StanjeAdrese *service.StanjeAdresePIN

	Racunala   []service.Racunalo
	Rezervnih  int        // neiskorištenih rezervnih kodova
	RezervniOd *time.Time // kad je niz napravljen; nil = nikad
	Kodovi     []string   // upravo napravljeni rezervni kodovi, samo na ovoj stranici
	Greska     string     // stanje se nije dalo pročitati

	// Cvor je ime ovog čvora: rezervni kodovi vrijede samo na njemu
	Cvor string
	// Izvana: profil je otvoren izvana (kroz tunel), pa kodovi napravljeni
	// ovdje vrijede za prijavu izvana kroz ovaj čvor
	Izvana bool
}

// profilPrijaveIzvana slaže odjeljak profila za osobu
func (h *UsersHandler) profilPrijaveIzvana(r *http.Request, u *models.User, kodovi []string) *ProfilPrijaveIzvana {
	d := h.dk()
	if d == nil || u == nil {
		return nil
	}
	ctx := r.Context()
	o := d.Opcije(ctx)
	p := &ProfilPrijaveIzvana{Ukljuceno: o.PIN, Domena: o.Domena, Kodovi: kodovi, Cvor: h.imeCvora(), Izvana: dolaziIzvana(r)}
	if _, viewing := stvarnaOsoba(r); viewing {
		p.TudjimOcima = true
		return p
	}
	if s, err := d.StanjeAdrese(ctx, u); err != nil {
		p.Razlog = err.Error()
	} else {
		p.StanjeAdrese, p.Adresa = s, s.Adresa
		if s.Razlog != nil {
			p.Razlog = s.Razlog.Error()
		}
	}
	var greske []string
	tok := ""
	if c, err := r.Cookie(service.KolacicRacunala); err == nil {
		tok = c.Value
	}
	racunala, err := d.Racunala(ctx, u.ID, tok)
	if err != nil {
		greske = append(greske, err.Error())
	}
	p.Racunala = racunala
	if p.Rezervnih, p.RezervniOd, err = d.StanjeRezervnih(ctx, u.ID); err != nil {
		greske = append(greske, err.Error())
	}
	p.Greska = strings.Join(greske, "; ")
	return p
}

const natragPrijavaIzvana = "/profile#prijava-izvana"

// putRezervnihKodova je stranica s novim rezervnim kodovima: odgovor na
// POST, bez GET-a. Obrasci profila na njoj (kontakti, lozinka) vraćaju se
// zato na profil, a ne na nju (405).
const putRezervnihKodova = "/profile/rezervni-kodovi"

// povratnaAdresaProfila je sigurnaPovratnaAdresa, ali sa stranice novih
// rezervnih kodova vodi na profil
func povratnaAdresaProfila(r *http.Request, zadano string) string {
	if p := sigurnaPovratnaAdresa(r, zadano); p != putRezervnihKodova {
		return p
	}
	return "/profile"
}

// noviRezervniKodovi provjeri lozinku (uz ograničenje krivih upisa kao pri
// promjeni lozinke) i napravi novi niz rezervnih kodova
func (h *UsersHandler) noviRezervniKodovi(r *http.Request) (*models.User, []string, error) {
	d := h.dk()
	if d == nil {
		return nil, nil, errDrugiKorakNedostupan
	}
	u, viewing := stvarnaOsoba(r)
	if u == nil {
		return nil, nil, service.ErrUserNotFound
	}
	if viewing {
		return nil, nil, service.ErrTudjimOcima
	}
	kljuc := kljucPonovneLozinke("", u.ID.String())
	if err := ponovnaLozinkaDopustena(kljuc); err != nil {
		return nil, nil, err
	}
	kodovi, err := d.NapraviRezervneKodove(r.Context(), u, r.FormValue("lozinka"), viewing)
	ishodPonovneLozinke(kljuc, err)
	return u, kodovi, err
}

// HandleRezervniKodovi napravi novi niz rezervnih kodova i pokaže ga jednom
func (h *UsersHandler) HandleRezervniKodovi(w http.ResponseWriter, r *http.Request) {
	_, kodovi, err := h.noviRezervniKodovi(r)
	if err != nil {
		redirectWith(w, r, natragPrijavaIzvana, "error", err.Error())
		return
	}
	h.prikaziProfil(w, r, kodovi)
}

// HandleRezervniKodoviTxt napravi novi niz rezervnih kodova i da ga kao
// datoteku za ispis; stari niz time prestaje vrijediti
func (h *UsersHandler) HandleRezervniKodoviTxt(w http.ResponseWriter, r *http.Request) {
	u, kodovi, err := h.noviRezervniKodovi(r)
	if err != nil {
		redirectWith(w, r, natragPrijavaIzvana, "error", err.Error())
		return
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "goCOP: rezervni kodovi za prijavu izvana\r\n")
	fmt.Fprintf(&b, "Račun: %s (%s)\r\n", u.Username, u.FullName)
	fmt.Fprintf(&b, "Napravljeni: %s\r\n", time.Now().In(models.Zagreb).Format("02.01.2006. u 15:04"))
	cvor := h.imeCvora()
	if cvor == "" {
		cvor = r.Host
	}
	fmt.Fprintf(&b, "Čvor: %s\r\n\r\n", cvor)
	for _, k := range kodovi {
		fmt.Fprintf(&b, "  [ ] %s\r\n", k)
	}
	b.WriteString("\r\nSvaki kod vrijedi za jednu prijavu, kad PIN ne stigne e-poštom.\r\n" +
		"Kodovi vrijede samo na čvoru " + cvor + " na kojem su napravljeni; na drugom čvoru\r\n" +
		"broje se kao krivi kod. Za prijavu izvana napravite ih na javnom čvoru,\r\n" +
		"kroz koji se prijavljujete izvana (npr. https://cop-osijek.com).\r\n" +
		"Iskorišteni kod prekrižite. Novi niz poništava ovaj.\r\n" +
		"Čuvajte ih kao lozinku: zajedno s lozinkom otvaraju vaš račun izvana.\r\n")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="gocop-rezervni-kodovi-`+sigurnoIme(u.Username)+`.txt"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(b.Bytes())
}

// HandleZaboraviRacunalo zaboravlja jedno zapamćeno računalo stvarne osobe
func (h *UsersHandler) HandleZaboraviRacunalo(w http.ResponseWriter, r *http.Request) {
	d := h.dk()
	u, viewing := stvarnaOsoba(r)
	if d == nil || u == nil {
		redirectWith(w, r, natragPrijavaIzvana, "error", errDrugiKorakNedostupan.Error())
		return
	}
	if err := d.Zaboravi(r.Context(), u.ID, r.PathValue("id"), viewing); err != nil {
		redirectWith(w, r, natragPrijavaIzvana, "error", err.Error())
		return
	}
	redirectWith(w, r, natragPrijavaIzvana, "success", "Računalo je zaboravljeno: s njega se izvana opet traži PIN.")
}

// HandleZaboraviSvaRacunala zaboravlja sva zapamćena računala stvarne osobe
func (h *UsersHandler) HandleZaboraviSvaRacunala(w http.ResponseWriter, r *http.Request) {
	d := h.dk()
	u, viewing := stvarnaOsoba(r)
	if d == nil || u == nil {
		redirectWith(w, r, natragPrijavaIzvana, "error", errDrugiKorakNedostupan.Error())
		return
	}
	n, err := d.ZaboraviSva(r.Context(), u.ID, viewing)
	if err != nil {
		redirectWith(w, r, natragPrijavaIzvana, "error", err.Error())
		return
	}
	redirectWith(w, r, natragPrijavaIzvana, "success", fmt.Sprintf("Zaboravljeno računala: %d. Izvana se sa svakog opet traži PIN.", n))
}

// ---- administracija: račun koji šalje PIN i sklopka ----

// PINStanje je odjeljak „PIN za prijavu izvana” na stranici e-pošte.
type PINStanje struct {
	service.StanjeSlanja
	// Izvana: stranica je otvorena izvana (kroz tunel), pa je ovo čvor kroz
	// koji se ljudi prijavljuju izvana; samo se s takvog PIN uključuje
	Izvana bool
	// BezHTTPS: stranica je otvorena izvana, a ni veza ni posrednik ne
	// javljaju HTTPS (sigurnoIzvana); prijava izvana s PIN-om tada se odbija
	BezHTTPS bool
	// MozeUkljuciti: pošiljatelj je ispravan, probni PIN je prošao i
	// stranica je otvorena izvana preko HTTPS-a
	MozeUkljuciti bool
}

// SetDrugiKorak spaja stranicu e-pošte s drugim korakom prijave izvana
func (h *AktiHandler) SetDrugiKorak(f func() *service.DrugiKorak) { h.drugiKorak = f }

func (h *AktiHandler) dk() *service.DrugiKorak {
	if h.drugiKorak == nil {
		return nil
	}
	return h.drugiKorak()
}

// pinStanje čita stanje pošiljatelja i sklopke za stranicu
func (h *AktiHandler) pinStanje(r *http.Request) (*PINStanje, error) {
	d := h.dk()
	if d == nil {
		return nil, nil
	}
	s, err := d.Stanje(r.Context())
	if err != nil {
		return nil, err
	}
	p := &PINStanje{StanjeSlanja: *s, Izvana: dolaziIzvana(r)}
	p.BezHTTPS = p.Izvana && !sigurnoIzvana(r)
	p.MozeUkljuciti = s.Posiljatelj != nil && s.Posiljatelj.NeispravanOd == nil && s.ProbaUspjela && p.Izvana && !p.BezHTTPS
	return p, nil
}

const natragPIN = "/administracija/posta#pin"

// porukaUkljuciBezHTTPS: posrednik ne javlja HTTPS, pa bi se uz uključen
// PIN svaka prijava izvana odbila
const porukaUkljuciBezHTTPS = "PIN se ne uključuje: ova stranica nije stigla preko HTTPS-a (ni izravno, ni kroz posrednika koji to javlja zaglavljem X-Forwarded-Proto: https), " +
	"pa bi se svaka prijava izvana s PIN-om odbila. Otvorite stranicu preko HTTPS-a (npr. https://cop-osijek.com) ili podesite posrednika."

// pinRadnja vraća servis i ovlasti za radnju nad PIN-om, ili javi grešku:
// tuđim očima ništa (probni PIN išao bi na tuđu adresu, a „iz moje pošte”
// uzeo bi tuđi račun)
func (h *AktiHandler) pinRadnja(w http.ResponseWriter, r *http.Request) (*service.DrugiKorak, *models.User, *models.UserPermissions, bool) {
	u, perms, _ := h.base(r)
	d := h.dk()
	if d == nil || u == nil {
		redirectWith(w, r, natragPIN, "error", errDrugiKorakNedostupan.Error())
		return nil, nil, nil, false
	}
	if _, viewing := stvarnaOsoba(r); viewing {
		redirectWith(w, r, natragPIN, "error", service.ErrTudjimOcima.Error())
		return nil, nil, nil, false
	}
	return d, u, perms, true
}

// HandlePINPosiljatelj sprema ili briše račun koji s ovog čvora šalje PIN.
// Prijava se provjeri na poslužitelju prije spremanja; adresa pošiljatelja
// traži se u adresaru kad nije upisana. Svaki upis lozinke broji se kao
// pokušaj osobe, a svaka prijava na poslužitelj i kao pokušaj računa u
// domeni (naplatiPrijaveAD), jer kriva lozinka zaključava račun u domeni.
func (h *AktiHandler) HandlePINPosiljatelj(w http.ResponseWriter, r *http.Request) {
	d, u, perms, ok := h.pinRadnja(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if r.FormValue("radnja") == "obrisi" {
		if err := d.ObrisiPosiljatelja(ctx, perms); err != nil {
			redirectWith(w, r, natragPIN, "error", err.Error())
			return
		}
		redirectWith(w, r, natragPIN, "success", "Račun za slanje PIN-a obrisan je s ovog čvora. Dok je PIN uključen, izvana se ulazi samo rezervnim ili privremenim kodom.")
		return
	}
	korisnik, lozinka := strings.TrimSpace(r.FormValue("korisnik")), r.FormValue("lozinka")
	if r.FormValue("radnja") == "iz_poste" {
		s := h.akti()
		if s == nil {
			redirectWith(w, r, natragPIN, "error", "E-pošta nije uključena.")
			return
		}
		k, l, err := s.RacunPosteOtkljucan(ctx, u)
		if err != nil {
			redirectWith(w, r, natragPIN, "error", "Račun e-pošte se ne da uzeti: "+err.Error())
			return
		}
		korisnik, lozinka = k, l
	}
	kljuc := kljucPonovneLozinke("posta-pin", u.ID.String())
	if err := ponovnaLozinkaDopustena(kljuc); err != nil {
		redirectWith(w, r, natragPIN, "error", err.Error())
		return
	}
	// prijave računa u domeni broje se zajedno s osobnim sandučićem; ime bez
	// domene troši dvije (goCOP pokuša i DOMENA\ime)
	var kljucAD string
	if lozinka != "" && korisnik != "" {
		n := 1
		if prijavaBezDomene(korisnik) {
			n = 2
		}
		var err error
		if kljucAD, err = naplatiPrijaveAD(u.ID.String(), korisnik, n); err != nil {
			redirectWith(w, r, natragPIN, "error", upperFirst(err.Error())+".")
			return
		}
		ponovnaLozinka.Fail(kljuc)
	}
	p, err := d.SpremiPosiljatelja(ctx, perms, korisnik, lozinka, r.FormValue("adresa"))
	if kljucAD != "" {
		ishodPrijaveAD(err == nil || service.PrijavaProsla(err), kljucAD, kljuc)
	}
	if err != nil {
		redirectWith(w, r, natragPIN, "error", err.Error())
		return
	}
	redirectWith(w, r, natragPIN, "success", "Prijava je provjerena i račun "+p.Korisnik+" <"+p.Adresa+"> spremljen je na ovaj čvor. Pošaljite sebi probni PIN.")
}

// HandlePINProba šalje probni PIN administratoru; tek uspjeh dopušta
// uključiti PIN za prijavu izvana
func (h *AktiHandler) HandlePINProba(w http.ResponseWriter, r *http.Request) {
	d, _, perms, ok := h.pinRadnja(w, r)
	if !ok {
		return
	}
	adresa, err := d.PosaljiProbniPIN(r.Context(), perms)
	if err != nil {
		redirectWith(w, r, natragPIN, "error", "Probni PIN nije poslan: "+err.Error())
		return
	}
	redirectWith(w, r, natragPIN, "success", "Probni PIN poslan je na "+adresa+". Kad stigne, PIN za prijavu izvana može se uključiti.")
}

// HandlePINSklopka uključuje ili isključuje PIN za prijavu izvana i sprema
// dopuštenu domenu; postavka vrijedi na svim čvorovima, a uključiti se može
// samo sa stranice otvorene izvana preko HTTPS-a (kroz tunel, na čvoru koji
// prima prijave izvana) i tek nakon uspješnog probnog PIN-a na tom čvoru.
// Kroz posrednika koji ne javlja HTTPS svaka bi se prijava izvana s PIN-om
// odbila (sigurnoIzvana), pa se ondje PIN ne uključuje.
func (h *AktiHandler) HandlePINSklopka(w http.ResponseWriter, r *http.Request) {
	d, _, perms, ok := h.pinRadnja(w, r)
	if !ok {
		return
	}
	o := service.OpcijePrijaveIzvana{PIN: r.FormValue("ukljuci") == "1", Domena: r.FormValue("domena")}
	if o.PIN && !d.Ukljuceno(r.Context()) && dolaziIzvana(r) && !sigurnoIzvana(r) {
		redirectWith(w, r, natragPIN, "error", porukaUkljuciBezHTTPS)
		return
	}
	if err := d.SpremiOpcije(r.Context(), perms, o, dolaziIzvana(r)); err != nil {
		redirectWith(w, r, natragPIN, "error", err.Error())
		return
	}
	if o.PIN {
		redirectWith(w, r, natragPIN, "success", "PIN za prijavu izvana je uključen. Prijava iz lokalne mreže ostaje bez PIN-a.")
		return
	}
	redirectWith(w, r, natragPIN, "success", "PIN za prijavu izvana je isključen; prijava izvana traži samo lozinku.")
}
