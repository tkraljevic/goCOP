package razmjena

// Poruke signala (plan WEB synca, F2b). Svaka poruka je jedan okvir
// (okvir.go) s JSON-om {vrsta, tijelo}. Računalo se prijavi (prijava),
// sastajalište ga primi (prijavljen) ili odbije (odbijeno), a obje strane
// šalju puls. Potpisane poruke (poziv, odziv, sinkro) idu istim okvirom, a
// sastajalište ih prosljeđuje bajt po bajt ili javi da cilja nema (nema).
// Nepoznata vrsta se zanemaruje, pa novija inačica smije dodati svoju.

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

// Vrste poruka signala; potpisane su VrstaPoziv, VrstaOdziv i VrstaSinkro
const (
	SignalPrijava    = "prijava"
	SignalPrijavljen = "prijavljen"
	SignalPuls       = "puls"
	SignalNema       = "nema"
	SignalOdbijeno   = "odbijeno"
	SignalUpozorenje = "upozorenje"
)

// oznakaSastajalista nosi poruka prijavljen: druga strana je sastajalište
// ove inačice
const oznakaSastajalista = "sastajaliste/1"

// Rokovi signala; var, da ih test skrati
var (
	razmakPulsa       = 25 * time.Second
	rokCitanjaSignala = 75 * time.Second
	rokPisanjaSignala = 10 * time.Second
	rokPrijave        = 10 * time.Second
)

// porukaSignala je jedan okvir signala
type porukaSignala struct {
	Vrsta  string          `json:"vrsta"`
	Tijelo json.RawMessage `json:"tijelo,omitempty"`
}

// prijavaSignala šalje računalo kad se spoji. Proces je nasumična oznaka
// ovog pokretanja: po njoj sastajalište razlikuje ponovno spajanje istog
// programa od kopije na drugom računalu.
type prijavaSignala struct {
	Ver     int    `json:"ver"`
	Proces  string `json:"proces"`
	Program string `json:"program,omitempty"`
}

// prijavljenSignala šalje sastajalište kad primi računalo; Vrijeme je
// njegov sat u milisekundama
type prijavljenSignala struct {
	Ver     int    `json:"ver"`
	Oznaka  string `json:"oznaka"`
	Vrijeme int64  `json:"vrijeme"`
	PulsSek int    `json:"pulsSek"`
}

// pulsSignala: uz Odgovori druga strana odmah vrati puls
type pulsSignala struct {
	Odgovori bool `json:"odgovori,omitempty"`
}

// nemaSignala: sastajalište poruku nije proslijedilo
type nemaSignala struct {
	Sesija string `json:"sesija"`
	Razlog string `json:"razlog"`
}

// Razlozi poruke nema
const (
	NemaNijePrijavljen = "nije-prijavljen"
	NemaPrevisePoziva  = "previse-poziva"
	NemaPreviseSesija  = "previse-sesija"
)

// odbijenoSignala: sastajalište ne prima računalo; PonovnoZaSek je kad
// smije ponovno (0: uobičajena odgoda)
type odbijenoSignala struct {
	Kod          KodSignala `json:"kod"`
	Razlog       string     `json:"razlog,omitempty"`
	PonovnoZaSek int        `json:"ponovnoZaSek,omitempty"`
}

// upozorenjeSignala: sastajalište javlja nešto što prijavu ne prekida
// (kopija istog ključa na drugom računalu)
type upozorenjeSignala struct {
	Kod KodSignala `json:"kod"`
}

// vezaSignala je TLS veza signala: poruke u okvirima, jedan pisač
// odjednom, rok za svako čitanje i pisanje
type vezaSignala struct {
	c       net.Conn
	kljuc   ed25519.PublicKey // druga strana
	vj      []byte            // vjerodajnice druge strane (na sastajalištu)
	pisanje sync.Mutex
}

// posalji zapiše poruku vrste s tijelom
func (v *vezaSignala) posalji(vrsta string, tijelo any) error {
	return v.pisi(okvirSignala(vrsta, tijelo))
}

// okvirSignala je zapis poruke vrste s tijelom
func okvirSignala(vrsta string, tijelo any) []byte {
	b, _ := json.Marshal(tijelo)                                     // tijela su strukture i brojevi, koji se uvijek daju zapisati
	okvir, _ := json.Marshal(porukaSignala{Vrsta: vrsta, Tijelo: b}) // RawMessage je već ispravan JSON
	return okvir
}

// proslijedi zapiše poruku s tijelom kakvo je stiglo, bajt po bajt
func (v *vezaSignala) proslijedi(p porukaSignala) error {
	okvir, _ := json.Marshal(p) // tijelo je JSON koji je već pročitan
	return v.pisi(okvir)
}

func (v *vezaSignala) pisi(okvir []byte) error {
	v.pisanje.Lock()
	defer v.pisanje.Unlock()
	return v.pisiBezBrave(okvir)
}

// pisiBezBrave piše okvir kad pozivatelj već drži bravu pisanja
func (v *vezaSignala) pisiBezBrave(okvir []byte) error {
	_ = v.c.SetWriteDeadline(time.Now().Add(rokPisanjaSignala)) // na zatvorenoj vezi padne i pisanje
	return PisiOkvir(v.c, okvir)
}

// primi pročita sljedeću poruku u roku
func (v *vezaSignala) primi(rok time.Duration) (porukaSignala, error) {
	_ = v.c.SetReadDeadline(time.Now().Add(rok)) // na zatvorenoj vezi padne i čitanje
	b, err := CitajOkvir(v.c)
	if err != nil {
		return porukaSignala{}, err
	}
	var p porukaSignala
	if err := json.Unmarshal(b, &p); err != nil || p.Vrsta == "" {
		return porukaSignala{}, fmt.Errorf("%w: okvir signala", ErrOblikPoruke)
	}
	return p, nil
}

func (v *vezaSignala) zatvori() { v.c.Close() }

// potpisanaIz čita potpisanu poruku iz okvira signala
func potpisanaIz(p porukaSignala) (PotpisanaPoruka, error) {
	pp := PotpisanaPoruka{Vrsta: p.Vrsta}
	if err := json.Unmarshal(p.Tijelo, &pp); err != nil {
		return PotpisanaPoruka{}, fmt.Errorf("%w: %v", ErrOblikPoruke, err)
	}
	return pp, nil
}

// jePotpisana: vrsta potpisane poruke
func jePotpisana(vrsta string) bool {
	return vrsta == VrstaPoziv || vrsta == VrstaOdziv || vrsta == VrstaSinkro
}

// tijeloIz čita tijelo poruke u v; greška kad nije zadanog oblika
func tijeloIz(p porukaSignala, v any) error {
	if err := json.Unmarshal(p.Tijelo, v); err != nil {
		return fmt.Errorf("%w: %s", ErrOblikPoruke, p.Vrsta)
	}
	return nil
}

// klijentKljuc je ključ konteksta s oznakom klijenta weba (SKlijentom)
type klijentKljuc struct{}

// SKlijentom stavlja u kontekst zahtjeva oznaku klijenta kako je vidi web
// sučelje (adresa iza posrednika, mreža /48 za IPv6). Sastajalište po njoj
// ograđuje rukovanja.
func SKlijentom(ctx context.Context, klijent string) context.Context {
	return context.WithValue(ctx, klijentKljuc{}, klijent)
}

func klijentIz(ctx context.Context) string {
	k, _ := ctx.Value(klijentKljuc{}).(string)
	return k
}
