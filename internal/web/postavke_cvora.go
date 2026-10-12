package web

// Postavke ovog čvora iz gocop.toml u Administraciji (Odluke 12. 10. 2026.).
// Nema zasebne stranice: svaka postavka stoji na stranici kojoj prirodno
// pripada (Čvor i mreža, Pretplate, Održavanje baze, Opcije). Popis postavki,
// objašnjenja i vrste čitaju se iz same strukture config.Config, pa se nova
// postavka pojavi sama; raspored niže kaže samo gdje stoji i smije li se
// mijenjati. Test pada kad Config dobije postavku koje u rasporedu nema.
//
// Mijenja ih samo globalni administrator, izravno s ovog računala ili iz
// lokalne mreže, nikad kroz posrednika (tunel). Program datoteku čita samo
// pri pokretanju, pa promjena vrijedi tek nakon ponovnog pokretanja.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"gocop/internal/config"
	"gocop/internal/models"
)

// nacinPostavke kaže što se s postavkom smije u programu
type nacinPostavke string

const (
	postavkaUrediva     nacinPostavke = "uredivo"
	postavkaSamoCitanje nacinPostavke = "samo-citanje"
	postavkaTajna       nacinPostavke = "tajno"   // vrijednost se nigdje ne prikazuje
	postavkaDrugdje     nacinPostavke = "drugdje" // vodi je druga stranica (E-pošta)
)

// mjestoPostavke je redak rasporeda: blok u kojem postavka stoji, što se s
// njom smije, kratki naziv i, za samo čitanje, zašto
type mjestoPostavke struct {
	dio    string
	nacin  nacinPostavke
	naziv  string
	razlog string
}

// Zašto se postavka ne mijenja u programu
const (
	razlogAdrese     = "Krivom adresom ili portom čvor se više ne bi otvarao ni s ovog računala, pa se mijenja samo u datoteci."
	razlogImena      = "Pod ovim imenom čvor upisuje zapise, a drugi ga čvorovi pamte uz ključ. Ne mijenja se."
	razlogNaziva     = "Mijenja se samo u datoteci, kao i ime čvora; drugima stiže nakon ponovnog pokretanja."
	razlogPosrednika = "O ovome ovisi koji zahtjev program smatra zahtjevom izvana, pa se mijenja samo u datoteci."
	razlogBaze       = "Uz bazu moraju ići ključ čvora (node-key) i ova datoteka, pa se premješta samo ručno, dok čvor ne radi."
	razlogPutanje    = "Mijenja se samo u datoteci, dok čvor ne radi, jer uz novu putanju treba premjestiti i datoteke."
	razlogNove       = "Nova postavka: zasad se mijenja samo u datoteci."
	// [sync] bootstrap program zasad ne čita (docs/INSTALACIJA.md); dok ga ne
	// čita, spremanje bi samo obećalo promjenu koje nema
	razlogBootstrapa = "Program ovu postavku zasad ne čita. Čvorove sa stalnom adresom upišite gore, pod Domenski čvorovi."
)

// redakRasporeda: puni ključ i njegovo mjesto
type redakRasporeda struct {
	kljuc string
	mjestoPostavke
}

// rasporedPostavki je jedina tablica rasporeda: puni ključ → blok, način i
// naziv, redom kako stoje u bloku (urediva prije onih samo za čitanje).
// [posta] vodi stranica E-pošta i ovdje se ne prikazuje.
var rasporedPostavki = []redakRasporeda{
	{"javna_adresa", mjestoPostavke{"cvor", postavkaUrediva, "Javna adresa", ""}},
	{"kontakt.centar", mjestoPostavke{"cvor", postavkaUrediva, "Centar obrane od poplava", ""}},
	{"kontakt.centar_telefon", mjestoPostavke{"cvor", postavkaUrediva, "Telefon centra", ""}},
	{"node.id", mjestoPostavke{"cvor", postavkaSamoCitanje, "Ime čvora u mreži", razlogImena}},
	{"node.name", mjestoPostavke{"cvor", postavkaSamoCitanje, "Naziv čvora", razlogNaziva}},
	{"addr", mjestoPostavke{"cvor", postavkaSamoCitanje, "Adresa web sučelja", razlogAdrese}},
	{"sync.exchange_port", mjestoPostavke{"cvor", postavkaSamoCitanje, "Port razmjene", razlogAdrese}},
	{"sync.pair_port", mjestoPostavke{"cvor", postavkaSamoCitanje, "Port uparivanja", razlogAdrese}},
	{"sync.discovery_port", mjestoPostavke{"cvor", postavkaSamoCitanje, "Port pronalaženja", razlogAdrese}},

	{"sync.auto_sync", mjestoPostavke{"mreza", postavkaUrediva, "Automatska sinkronizacija", ""}},
	{"sync.bootstrap", mjestoPostavke{"mreza", postavkaSamoCitanje, "Stalno dostupni čvorovi", razlogBootstrapa}},
	{"web.pouzdani_posrednici", mjestoPostavke{"mreza", postavkaSamoCitanje, "Pouzdani posrednici", razlogPosrednika}},
	{"web.zaglavlje_klijenta", mjestoPostavke{"mreza", postavkaSamoCitanje, "Zaglavlje s adresom klijenta", razlogPosrednika}},

	{"sync.sve", mjestoPostavke{"pretplate", postavkaUrediva, "Prati sve", ""}},
	{"vodostaji.povijest_mjeseci", mjestoPostavke{"pretplate", postavkaUrediva, "Povijest tuđih očitanja (mjeseci)", ""}},

	{"db", mjestoPostavke{"baza", postavkaSamoCitanje, "Baza čvora", razlogBaze}},
	{"arhiva", mjestoPostavke{"baza", postavkaSamoCitanje, "Arhiva vodostaja", razlogPutanje}},
	{"podaci", mjestoPostavke{"baza", postavkaSamoCitanje, "Izvorne datoteke arhive", razlogPutanje}},
	{"skenovi", mjestoPostavke{"baza", postavkaSamoCitanje, "Skenovi i fotografije s terena", razlogPutanje}},
	{"pakete", mjestoPostavke{"baza", postavkaSamoCitanje, "Mapa .cop paketa", razlogPutanje}},

	{"karta.plocice", mjestoPostavke{"opcije", postavkaUrediva, "Izvor pločica karte", ""}},
	{"karta.zasluge", mjestoPostavke{"opcije", postavkaUrediva, "Natpis o podrijetlu karte", ""}},
	{"karta.najvise_z", mjestoPostavke{"opcije", postavkaUrediva, "Najveće približavanje karte", ""}},

	{"posta.nacin", mjestoPostavke{dio: "posta", nacin: postavkaDrugdje}},
	{"posta.posluzitelj", mjestoPostavke{dio: "posta", nacin: postavkaDrugdje}},
	{"posta.domena", mjestoPostavke{dio: "posta", nacin: postavkaDrugdje}},
	{"posta.port", mjestoPostavke{dio: "posta", nacin: postavkaDrugdje}},
	{"posta.sigurnost", mjestoPostavke{dio: "posta", nacin: postavkaDrugdje}},
}

// redRasporeda je redni broj ključa u rasporedu; ključ bez retka ide na kraj
func redRasporeda(puni string) int {
	for i, r := range rasporedPostavki {
		if r.kljuc == puni {
			return i
		}
	}
	return len(rasporedPostavki)
}

// dioOdjeljka: gdje stoji postavka koje u rasporedu (još) nema, po odjeljku
var dioOdjeljka = map[string]string{
	"": "cvor", "node": "cvor", "kontakt": "cvor", "web": "mreza", "sync": "mreza",
	"vodostaji": "pretplate", "karta": "opcije", "posta": "posta",
}

// mjestoKljuca vraća redak rasporeda; nova postavka bez retka prikazuje se
// u bloku svog odjeljka, samo za čitanje, dok joj se ne odredi mjesto
func mjestoKljuca(k config.Kljuc) mjestoPostavke {
	if i := redRasporeda(k.Puni()); i < len(rasporedPostavki) {
		return rasporedPostavki[i].mjestoPostavke
	}
	dio, ok := dioOdjeljka[k.Odjeljak]
	if !ok {
		dio = "cvor"
	}
	if dio == "posta" {
		return mjestoPostavke{dio: dio, nacin: postavkaDrugdje}
	}
	return mjestoPostavke{dio: dio, nacin: postavkaSamoCitanje, naziv: k.Oznaka(), razlog: razlogNove}
}

// dioPostavki je blok postavki na jednoj stranici
type dioPostavki struct {
	ime      string
	stranica string // putanja stranice; obrazac ide na stranica + "/postavke-cvora"
	sidro    string
	naslov   string // naslov u kartici; prazno kad ga daje stranica
}

var dijeloviPostavki = []dioPostavki{
	{ime: "cvor", stranica: "/settings", sidro: "postavke-cvor", naslov: "Postavke ovog čvora (gocop.toml)"},
	{ime: "mreza", stranica: "/settings", sidro: "postavke-mreza", naslov: "Postavke mreže ovog čvora (gocop.toml)"},
	{ime: "pretplate", stranica: "/pretplate", sidro: "postavke-cvora"},
	{ime: "baza", stranica: "/administracija/baza", sidro: "postavke-cvora"},
	{ime: "opcije", stranica: "/administracija/opcije", sidro: "postavke-cvora"},
}

func nadjiDio(ime string) (dioPostavki, bool) {
	for _, d := range dijeloviPostavki {
		if d.ime == ime {
			return d, true
		}
	}
	return dioPostavki{}, false
}

// PostavkeCvora je ono što main zna o gocop.toml ovog čvora
type PostavkeCvora struct {
	Put       string            // datoteka koju čvor čita, ili bi je čitao kad nastane
	Vrijedi   config.Config     // s čime čvor radi: datoteka, zadano i zastavice pokretanja
	Zastavice map[string]string // puni ključ → zastavica pokretanja jača od datoteke ("-addr")
	Pocetna   config.Config     // od čega nastaje nova datoteka: zadano i ime čvora
}

// postavkeCvora čuva PostavkeCvora; mu drži i jedno spremanje u isto vrijeme
type postavkeCvora struct {
	mu sync.Mutex
	p  PostavkeCvora
}

// SetPostavkeCvora daje poslužitelju gocop.toml ovog čvora; bez toga se
// blokovi postavki ne prikazuju
func (s *Server) SetPostavkeCvora(p PostavkeCvora) {
	s.postavke = &postavkeCvora{p: p}
}

// postaviAddr: čvor je s porta 80 sam prešao na 8080
func (pc *postavkeCvora) postaviAddr(addr string) {
	pc.mu.Lock()
	pc.p.Vrijedi.Addr = addr
	pc.mu.Unlock()
}

func (pc *postavkeCvora) snimka() PostavkeCvora {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	return pc.p
}

// stanjeDatoteke je gocop.toml kakav je sada na disku
type stanjeDatoteke struct {
	tekst     []byte        // sadržaj; kad datoteke nema, primjer koji bi se zapisao
	cfg       config.Config // pročitano; kad datoteke nema, početne postavke
	postoji   bool
	otisak    string
	mijenjano time.Time
	greska    error // datoteka se ne da pročitati
}

// procitajGocopToml čita gocop.toml istim čitačem kao pri pokretanju
func procitajGocopToml(p PostavkeCvora) stanjeDatoteke {
	st := stanjeDatoteke{cfg: p.Pocetna, otisak: "nema"}
	tekst, err := os.ReadFile(p.Put)
	if errors.Is(err, os.ErrNotExist) {
		st.tekst, st.greska = config.Primjer(p.Pocetna)
		return st
	}
	if err != nil {
		st.greska = err
		return st
	}
	st.tekst, st.postoji, st.otisak = tekst, true, otisakTeksta(tekst)
	if fi, err := os.Stat(p.Put); err == nil {
		st.mijenjano = fi.ModTime()
	}
	cfg, err := config.ProcitajTekst(tekst)
	if err != nil {
		st.greska = err
		return st
	}
	// ono što program pri pokretanju sam dopuni nije razlika
	cfg.ZamijeniZatvoreneIzvore()
	if cfg.Node.ID == "" {
		cfg.Node.ID = p.Vrijedi.Node.ID
	}
	st.cfg = cfg
	return st
}

func otisakTeksta(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}

// PostavkeStranice su blokovi postavki čvora na jednoj stranici
type PostavkeStranice struct {
	blokovi     map[string]*BlokPostavki
	CekaPonovno bool                     // spremljena promjena čeka ponovno pokretanje
	Ponovno     *PonovnoPokretanjePrikaz // samo na stranici Čvor i mreža
}

// Dio je blok s tim imenom (cvor, mreza…); nil kad ga na stranici nema
func (p *PostavkeStranice) Dio(ime string) *BlokPostavki {
	if p == nil {
		return nil
	}
	return p.blokovi[ime]
}

// BlokPostavki je pregled, i za urediv dio obrazac, postavki jednog bloka
type BlokPostavki struct {
	Dio         string
	Naslov      string
	Sidro       string
	Akcija      string // kamo ide obrazac
	Stavke      []StavkaPostavke
	Uredivo     bool   // obrazac: administrator, izravno iz lokalne mreže
	ImaUredivih bool   // ima li u bloku išta što se mijenja ovdje
	Zabrana     string // zašto je samo pregled
	Otisak      string // sadržaj datoteke kad je obrazac otvoren
	Greska      string // zadnje spremanje nije uspjelo
	Datoteka    *DatotekaPostavki
}

// Promjenjive su postavke bloka koje se mijenjaju u programu
func (b *BlokPostavki) Promjenjive() []StavkaPostavke {
	return slices.DeleteFunc(slices.Clone(b.Stavke), func(s StavkaPostavke) bool { return !s.Promjenjiva })
}

// Ostale su postavke bloka koje se mijenjaju samo u datoteci
func (b *BlokPostavki) Ostale() []StavkaPostavke {
	return slices.DeleteFunc(slices.Clone(b.Stavke), func(s StavkaPostavke) bool { return s.Promjenjiva })
}

// DatotekaPostavki je gdje je gocop.toml, kad je mijenjan i njegove kopije
type DatotekaPostavki struct {
	Put       string
	Postoji   bool
	Mijenjano time.Time
	Kopije    []config.KopijaPostavki
	Greska    string
}

// StavkaPostavke je jedna postavka u bloku
type StavkaPostavke struct {
	Kljuc        string // puni ključ, ime polja u obrascu
	Oznaka       string // kako stoji u datoteci: [sync] auto_sync
	Naziv        string
	Opis         string
	Primjeri     []string // retci s primjerima iz objašnjenja
	OpisOdjeljka string   // objašnjenje odjeljka, uz prvu postavku odjeljka u bloku
	Vrsta        string   // tekst, broj, da-ne, popis
	Promjenjiva  bool     // mijenja se u programu (s ovog zahtjeva ili ne)
	Uredivo      bool     // polje obrasca na ovom zahtjevu
	Tajno        bool
	Prikaz       string // vrijednost s kojom čvor radi
	UDatoteci    string // vrijednost u datoteci, kad je drukčija
	Zastavica    string // zastavica pokretanja koja vrijedi umjesto datoteke
	CekaPonovno  bool   // spremljeno se razlikuje od onoga s čime čvor radi
	Polje        string // vrijednost u obrascu
	Ukljuceno    bool   // za da-ne
	Greska       string
	Razlog       string
}

// izvorPostavki daje rukovatelju stranice blokove postavki čvora
type izvorPostavki func(r *http.Request, stranica string) *PostavkeStranice

func (f izvorPostavki) za(r *http.Request, stranica string) *PostavkeStranice {
	if f == nil {
		return nil
	}
	return f(r, stranica)
}

// pokusajPostavki je spremanje koje nije prošlo: upisano i greške uz polja
type pokusajPostavki struct {
	dio     string
	upisano map[string]string
	greske  map[string]string
	greska  string
}

type kljucPokusaja struct{}

// smijeMijenjatiCvor: globalni administrator, svojim očima, izravno s ovog
// računala ili iz lokalne mreže. Kroz posrednika (tunel) nikad, ni kad je
// posrednik na istom računalu.
func smijeMijenjatiCvor(r *http.Request) error {
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	if perms == nil || !perms.IsGlobalAdmin {
		return errors.New("Postavke čvora mijenja samo administrator.")
	}
	if gleda, _ := r.Context().Value(contextKeyViewing).(bool); gleda {
		return errors.New("Postavke čvora ne mijenjaju se tuđim očima. Vratite se sebi pa pokušajte ponovno.")
	}
	k := klijentIz(r)
	if k.KrozPosrednika || !k.IzLokalneMreze() {
		return errors.New("Postavke ovog čvora mijenjaju se samo s ovog računala ili iz lokalne mreže, a ne izvana ni kroz tunel.")
	}
	return nil
}

func jeAdministrator(r *http.Request) bool {
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	return perms != nil && perms.IsGlobalAdmin
}

// postavkeStranice slaže blokove postavki za stranicu; nil kad ih nema ili
// ih ovaj korisnik ne vidi
func (s *Server) postavkeStranice(r *http.Request, stranica string) *PostavkeStranice {
	if s.postavke == nil || !jeAdministrator(r) {
		return nil
	}
	p := s.postavke.snimka()
	sl := slagacBlokova{p: p, st: procitajGocopToml(p), zabrana: smijeMijenjatiCvor(r)}
	sl.pokusaj, _ = r.Context().Value(kljucPokusaja{}).(*pokusajPostavki)
	out := &PostavkeStranice{blokovi: map[string]*BlokPostavki{}, CekaPonovno: sl.cekaPonovno()}
	for _, d := range dijeloviPostavki {
		if d.stranica == stranica {
			out.blokovi[d.ime] = sl.blok(d)
		}
	}
	if stranica == "/settings" {
		out.Ponovno = s.prikazPonovnog(sl.zabrana)
	}
	return out
}

// slagacBlokova slaže blokove iz jednog čitanja datoteke
type slagacBlokova struct {
	p       PostavkeCvora
	st      stanjeDatoteke
	zabrana error
	pokusaj *pokusajPostavki
}

func (sl slagacBlokova) blok(d dioPostavki) *BlokPostavki {
	b := &BlokPostavki{Dio: d.ime, Naslov: d.naslov, Sidro: d.sidro, Akcija: d.stranica + "/postavke-cvora", Otisak: sl.st.otisak}
	b.Uredivo = sl.zabrana == nil && sl.st.greska == nil
	b.Zabrana, b.Greska = sl.porukeBloka(d)
	sl.dodajStavke(b, d)
	if d.ime == "baza" {
		b.Datoteka = sl.datoteka()
	}
	return b
}

// porukeBloka: zašto je blok samo pregled i što nije u redu s datotekom ili
// zadnjim spremanjem
func (sl slagacBlokova) porukeBloka(d dioPostavki) (zabrana, greska string) {
	if sl.zabrana != nil {
		zabrana = sl.zabrana.Error()
	}
	if sl.st.greska != nil {
		greska = "gocop.toml se ne da pročitati (" + sl.st.greska.Error() + "). S takvom datotekom čvor se ne bi pokrenuo; popravite je u uređivaču teksta."
	}
	if sl.pokusaj != nil && sl.pokusaj.dio == d.ime && sl.pokusaj.greska != "" {
		greska = sl.pokusaj.greska
	}
	return zabrana, greska
}

// dodajStavke slaže postavke bloka; objašnjenje odjeljka ide uz njegovu
// prvu postavku, a isti razlog za više postavki zaredom piše se jednom
func (sl slagacBlokova) dodajStavke(b *BlokPostavki, d dioPostavki) {
	odjeljci, razlog := map[string]bool{}, ""
	for _, k := range kljuceviBloka(d.ime) {
		m := mjestoKljuca(k)
		st := sl.stavka(k, m, b.Uredivo)
		if !odjeljci[k.Odjeljak] {
			st.OpisOdjeljka, odjeljci[k.Odjeljak] = k.OpisOdjeljka, true
		}
		if st.Razlog == razlog {
			st.Razlog = ""
		} else {
			razlog = st.Razlog
		}
		b.ImaUredivih = b.ImaUredivih || m.nacin == postavkaUrediva
		b.Stavke = append(b.Stavke, st)
	}
}

// kljuceviBloka su postavke bloka redom iz rasporeda; nove bez retka na kraju
func kljuceviBloka(dio string) []config.Kljuc {
	var out []config.Kljuc
	for _, k := range config.Kljucevi() {
		if m := mjestoKljuca(k); m.dio == dio && m.nacin != postavkaDrugdje {
			out = append(out, k)
		}
	}
	slices.SortStableFunc(out, func(a, b config.Kljuc) int { return redRasporeda(a.Puni()) - redRasporeda(b.Puni()) })
	return out
}

func (sl slagacBlokova) datoteka() *DatotekaPostavki {
	d := &DatotekaPostavki{Put: sl.p.Put, Postoji: sl.st.postoji, Mijenjano: sl.st.mijenjano}
	if sl.st.postoji {
		d.Kopije = config.Kopije(sl.p.Put)
	}
	if sl.st.greska != nil {
		d.Greska = sl.st.greska.Error()
	}
	return d
}

// stavka slaže jednu postavku: što vrijedi, što je u datoteci i obrazac
func (sl slagacBlokova) stavka(k config.Kljuc, m mjestoPostavke, blokUrediv bool) StavkaPostavke {
	opis, primjeri := razdijeliOpis(k.Opis)
	vrijedi, uDatoteci := sl.p.Vrijedi.Vrijednost(k), sl.st.cfg.Vrijednost(k)
	st := StavkaPostavke{Kljuc: k.Puni(), Oznaka: k.Oznaka(), Naziv: m.naziv, Opis: opis, Primjeri: primjeri,
		Vrsta: vrstaPolja(k.Tip), Razlog: m.razlog, Zastavica: sl.p.Zastavice[k.Puni()],
		Promjenjiva: m.nacin == postavkaUrediva, Uredivo: blokUrediv && m.nacin == postavkaUrediva, Tajno: m.nacin == postavkaTajna}
	if st.Naziv == "" {
		st.Naziv = k.Oznaka()
	}
	if st.Tajno {
		return st
	}
	st.Prikaz = prikaziVrijednost(vrijedi)
	if !config.IstaVrijednost(vrijedi, uDatoteci) {
		st.UDatoteci = prikaziVrijednost(uDatoteci)
		st.CekaPonovno = st.Zastavica == ""
	}
	st.Polje, st.Ukljuceno = poljeVrijednosti(uDatoteci)
	if sl.pokusaj != nil {
		if upisano, ok := sl.pokusaj.upisano[k.Puni()]; ok {
			st.Polje, st.Ukljuceno = upisano, upisano == "1"
		}
		st.Greska = sl.pokusaj.greske[k.Puni()]
	}
	return st
}

// cekaPonovno: neka postavka koja se mijenja u programu spremljena je, a
// čvor još radi sa starom vrijednošću
func (sl slagacBlokova) cekaPonovno() bool {
	if sl.st.greska != nil {
		return false
	}
	for _, k := range config.Kljucevi() {
		if mjestoKljuca(k).nacin != postavkaUrediva || sl.p.Zastavice[k.Puni()] != "" {
			continue
		}
		if !config.IstaVrijednost(sl.p.Vrijedi.Vrijednost(k), sl.st.cfg.Vrijednost(k)) {
			return true
		}
	}
	return false
}

// razdijeliOpis odvaja objašnjenje od redaka s primjerima (počinju s dva
// razmaka, kao u datoteci: "  addr = ':8080'   # drugi port")
func razdijeliOpis(opis string) (string, []string) {
	var tekst []string
	var primjeri []string
	for _, r := range strings.Split(opis, "\n") {
		if strings.HasPrefix(r, "  ") {
			primjeri = append(primjeri, strings.TrimSpace(r))
		} else if r != "" {
			tekst = append(tekst, r)
		}
	}
	return strings.Join(tekst, " "), primjeri
}

func vrstaPolja(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Bool:
		return "da-ne"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "broj"
	case reflect.Slice:
		return "popis"
	}
	return "tekst"
}

// prikaziVrijednost je vrijednost za čitanje; prazno ostaje prazno, a
// stranica napiše „prazno”
func prikaziVrijednost(v any) string {
	switch x := v.(type) {
	case bool:
		if x {
			return "da"
		}
		return "ne"
	case []string:
		return strings.Join(x, ", ")
	}
	return fmt.Sprint(v)
}

// poljeVrijednosti je vrijednost u obrascu: popis po jedan u retku
func poljeVrijednosti(v any) (string, bool) {
	switch x := v.(type) {
	case bool:
		return "", x
	case []string:
		return strings.Join(x, "\n"), false
	}
	return fmt.Sprint(v), false
}

// procitajPolje pretvara upisano u vrijednost vrste postavke, ili vraća
// poruku uz polje
func procitajPolje(k config.Kljuc, upisano string) (any, string) {
	v := reflect.New(k.Tip).Elem()
	switch vrstaPolja(k.Tip) {
	case "da-ne":
		v.SetBool(upisano == "1")
	case "broj":
		n, err := strconv.ParseInt(strings.TrimSpace(upisano), 10, 64)
		if err != nil || v.OverflowInt(n) {
			return nil, "Upišite cijeli broj, npr. 12."
		}
		v.SetInt(n)
	case "popis":
		if k.Tip.Elem().Kind() != reflect.String {
			return nil, "Ova se postavka ne mijenja u obrascu."
		}
		v.Set(reflect.ValueOf(procitajPopis(upisano)).Convert(k.Tip))
	default:
		s := strings.TrimSpace(upisano)
		if k.Tip.Kind() != reflect.String || strings.ContainsAny(s, "\r\n\t") {
			return nil, "Upišite vrijednost u jednom retku."
		}
		v.SetString(s)
	}
	return v.Interface(), ""
}

// procitajPopis: po jedan u retku ili odvojeno zarezom; prazni se preskaču
func procitajPopis(s string) []string {
	out := []string{}
	for _, dio := range strings.FieldsFunc(s, func(c rune) bool { return c == '\n' || c == '\r' || c == ',' }) {
		if dio = strings.TrimSpace(dio); dio != "" {
			out = append(out, dio)
		}
	}
	return out
}

// spremiPostavkeCvora prima obrazac bloka sa stranice; uz grešku stranicu
// pokaže iznova (prikaz) s upisanim i porukama uz polja
func (s *Server) spremiPostavkeCvora(stranica string, prikaz http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := smijeMijenjatiCvor(r); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		if s.postavke == nil {
			http.Error(w, "Postavke ovog čvora nisu dostupne.", http.StatusNotFound)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Neispravan obrazac.", http.StatusBadRequest)
			return
		}
		d, ok := nadjiDio(r.PostFormValue("dio"))
		if !ok || d.stranica != stranica {
			http.Error(w, "Neispravan obrazac.", http.StatusBadRequest)
			return
		}
		if err := provjeriPoljaObrasca(r, d); err != nil {
			log.Printf("Postavke čvora: odbijen obrazac (%s, %s): %v", korisnikZahtjeva(r), klijentIz(r), err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		pokusaj, poruka := s.spremiBlok(r, d)
		if pokusaj != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnprocessableEntity)
			prikaz.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), kljucPokusaja{}, pokusaj)))
			return
		}
		redirectWith(w, r, d.stranica+"#"+d.sidro, "success", poruka)
	})
}

// provjeriPoljaObrasca odbija obrazac koji šalje postavku koja se na tom
// bloku ne mijenja: samo čitanje, tajna, s druge stranice ili nepoznata
func provjeriPoljaObrasca(r *http.Request, d dioPostavki) error {
	for ime := range r.PostForm {
		puni, ok := strings.CutPrefix(ime, "postavka.")
		if !ok {
			continue
		}
		k, ok := config.NadjiKljuc(puni)
		if !ok {
			return fmt.Errorf("Postavka %s ne postoji.", puni)
		}
		if m := mjestoKljuca(k); m.dio != d.ime || m.nacin != postavkaUrediva {
			return fmt.Errorf("Postavka %s ne mijenja se na ovoj stranici.", k.Oznaka())
		}
	}
	return nil
}

// spremiBlok čita obrazac, provjerava i zapisuje; vraća pokušaj s greškama
// kad nije zapisano, inače poruku za korisnika
func (s *Server) spremiBlok(r *http.Request, d dioPostavki) (*pokusajPostavki, string) {
	s.postavke.mu.Lock()
	defer s.postavke.mu.Unlock()
	p := s.postavke.p
	st := procitajGocopToml(p)
	pokusaj := &pokusajPostavki{dio: d.ime, upisano: map[string]string{}, greske: map[string]string{}}
	switch {
	case st.greska != nil:
		pokusaj.greska = "Postavke nisu spremljene: gocop.toml se ne da pročitati (" + st.greska.Error() + "). Popravite datoteku u uređivaču teksta."
		return pokusaj, ""
	case r.PostFormValue("otisak") != st.otisak:
		pokusaj.greska = "Postavke nisu spremljene: gocop.toml je u međuvremenu promijenjen. Pogledajte vrijednosti koje sad vrijede i spremite ponovno."
		return pokusaj, ""
	}
	izmjene := procitajIzmjene(r, d, st.cfg, pokusaj)
	if len(pokusaj.greske) > 0 {
		pokusaj.greska = "Postavke nisu spremljene. Ispravite označena polja."
		return pokusaj, ""
	}
	if len(izmjene) == 0 {
		return nil, "Ništa nije promijenjeno."
	}
	novi, err := s.provjeriIzmjene(st, izmjene, pokusaj)
	if err != nil {
		return pokusaj, ""
	}
	kopija, err := config.Zapisi(p.Put, novi, time.Now())
	if err != nil {
		pokusaj.greska = "Postavke nisu spremljene: gocop.toml se ne da zapisati (" + err.Error() + "). Program mora smjeti pisati u mapu u kojoj je datoteka."
		return pokusaj, ""
	}
	zapisiUDnevnik(r, p.Put, kopija, izmjene)
	return nil, "Postavke čvora spremljene su u gocop.toml. Vrijede nakon ponovnog pokretanja čvora."
}

// procitajIzmjene: postavke bloka koje je obrazac poslao i koje se
// razlikuju od datoteke; greške uz polja upisuje u pokušaj
func procitajIzmjene(r *http.Request, d dioPostavki, uDatoteci config.Config, pokusaj *pokusajPostavki) map[string]any {
	izmjene := map[string]any{}
	for _, k := range config.Kljucevi() {
		vrijednosti, ok := r.PostForm["postavka."+k.Puni()]
		if !ok || mjestoKljuca(k).dio != d.ime {
			continue
		}
		upisano := vrijednosti[len(vrijednosti)-1] // da-ne: skriveno 0, pa kvačica 1
		pokusaj.upisano[k.Puni()] = upisano
		v, poruka := procitajPolje(k, upisano)
		if poruka != "" {
			pokusaj.greske[k.Puni()] = poruka
			continue
		}
		if !config.IstaVrijednost(v, uDatoteci.Vrijednost(k)) {
			izmjene[k.Puni()] = v
		}
	}
	return izmjene
}

// provjeriIzmjene slaže novi tekst datoteke, čita ga istim čitačem kao pri
// pokretanju i provjerava istim pravilima; ništa ne zapisuje
func (s *Server) provjeriIzmjene(st stanjeDatoteke, izmjene map[string]any, pokusaj *pokusajPostavki) ([]byte, error) {
	novi, cfg, err := config.Izmijeni(st.tekst, izmjene)
	if err != nil {
		pokusaj.greska = "Postavke nisu spremljene: " + err.Error() + ". Uredite datoteku u uređivaču teksta."
		return nil, err
	}
	for kljuc, poruka := range config.Provjeri(cfg) {
		if _, mijenja := izmjene[kljuc]; mijenja {
			pokusaj.greske[kljuc] = poruka
		}
	}
	if _, err := NoviPosrednici(cfg.Web.PouzdaniPosrednici, cfg.Web.ZaglavljeKlijenta); err != nil {
		pokusaj.greska = "Postavke nisu spremljene: u [web] " + err.Error() + ", pa se čvor ne bi pokrenuo. Popravite datoteku u uređivaču teksta."
		return nil, err
	}
	if len(pokusaj.greske) > 0 {
		pokusaj.greska = "Postavke nisu spremljene. Ispravite označena polja."
		return nil, errors.New(pokusaj.greska)
	}
	return novi, nil
}

// zapisiUDnevnik: tko je, kad (vrijeme retka) i koje postavke promijenio
func zapisiUDnevnik(r *http.Request, put, kopija string, izmjene map[string]any) {
	var oznake []string
	for _, k := range config.Kljucevi() {
		if _, ok := izmjene[k.Puni()]; ok {
			oznake = append(oznake, k.Oznaka())
		}
	}
	slices.Sort(oznake)
	prije := "datoteke prije nije bilo"
	if kopija != "" {
		prije = "prethodna je u " + kopija
	}
	log.Printf("Postavke čvora: %s s %s promijenio u %s: %s (%s)", korisnikZahtjeva(r), klijentIz(r), put, strings.Join(oznake, ", "), prije)
}

// korisnikZahtjeva je prijavljeni korisnik za dnevnik: ime i korisničko ime
func korisnikZahtjeva(r *http.Request) string {
	u, _ := r.Context().Value(contextKeyRealUsr).(*models.User)
	if u == nil {
		u, _ = r.Context().Value(contextKeyUser).(*models.User)
	}
	if u == nil {
		return "nepoznat korisnik"
	}
	return u.FullName + " (" + u.Username + ")"
}
