package main

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gocop/internal/disk"
	"gocop/internal/peers"
	"gocop/internal/web"
)

// mjestaDiska su mape koje čvor puni: mapa baze (gocop.db, sadrzaj.db,
// prognoze.db, oborine.db), mapa arhive vodostaja, stablo izvornih datoteka
// arhive i mapa paketa. Prazno stablo ili mapa paketa ne prate se.
func mjestaDiska(dbPath, arhivaPut, podaci, paketi string) []disk.Mjesto {
	return []disk.Mjesto{
		{Put: apsolutna(filepath.Dir(dbPath)), Naziv: "bazom"},
		{Put: apsolutna(filepath.Dir(arhivaPut)), Naziv: "arhivom", Premjesti: "arhivu"},
		{Put: apsolutna(podaci), Naziv: "izvornim datotekama arhive", Premjesti: "izvorne datoteke arhive"},
		{Put: apsolutna(paketi), Naziv: "paketima", Premjesti: "pakete"},
	}
}

// apsolutna: Postava pokreće čvor s apsolutnom bazom, a -podaci i -pakete
// ostaju relativni. Windows relativnu putanju mjeri na disku tekuće mape, a
// svezak joj nađe na C:\, pa bi se baza i paketi na disku D: brojali kao
// dva diska. Prazno ostaje prazno: to se ne prati.
func apsolutna(put string) string {
	if a, err := filepath.Abs(put); put != "" && err == nil {
		return a
	}
	return put
}

// imeCvora je ime čvora za upozorenje, a bez imena njegova oznaka
func imeCvora(cvor *peers.Node) string {
	if cvor.Name == "" {
		return cvor.ID
	}
	return cvor.Name
}

// javiAdministratoru stoji iza savjeta na pločici i u e-pošti: upozorenje
// vidi osoba za računalom, a ona javi administratoru kad ne zna što učiniti
// (Odluke (15. 10. 2026.))
const javiAdministratoru = "Ako ne znate kako, javite administratoru."

// upozorenjaDiska su diskovi čvora na kojima je mjesta malo, za pločicu
// razmjene. Pločicu gledaju i s drugih računala, pa poruka nosi ime čvora.
func upozorenjaDiska(nd *disk.Nadzor, cvor *peers.Node) []web.DiskRazmjene {
	var out []web.DiskRazmjene
	for _, d := range maloMjesta(nd.Stanje()) {
		out = append(out, web.DiskRazmjene{Poruka: d.Poruka(imeCvora(cvor)), Savjet: d.Savjet() + " " + javiAdministratoru})
	}
	return out
}

// maloMjesta su diskovi na kojima je mjesta malo
func maloMjesta(diskovi []disk.Disk) []disk.Disk {
	var out []disk.Disk
	for _, d := range diskovi {
		if d.Malo() {
			out = append(out, d)
		}
	}
	return out
}

const (
	// razmakObavijestiDiska: dok je mjesta malo, e-pošta stiže jednom na dan
	razmakObavijestiDiska = 24 * time.Hour
	// istekObavijestiDiska je najdulje slanje e-pošte o disku
	istekObavijestiDiska = 2 * time.Minute
	// biljegObavijestiDiska je datoteka uz bazu s vremenom zadnje e-pošte o
	// disku, da je ponovno pokretanje čvora ne pošalje opet isti dan
	biljegObavijestiDiska  = "obavijest-o-disku.txt"
	predmetObavijestiDiska = "goCOP: na disku računala ostaje malo mjesta"
)

// obavijestDiska šalje e-poštu kad na disku čvora ostane malo mjesta.
// Primaju je osobe koje su na ovom računalu spremile lozinku sandučića,
// svaka sa svog računa na svoju adresu (Odluke (15. 10. 2026.)).
type obavijestDiska struct {
	ctx     context.Context
	ime     func() string // ime čvora; može se promijeniti dok čvor radi
	posalji func(ctx context.Context, predmet, tekst string) (int, error)
	biljeg  string
	sad     func() time.Time
	javi    func(format string, args ...any)
	zadnja  time.Time // i kad se biljeg ne da zapisati (pun disk)
}

// nakonProvjere šalje e-poštu kad je mjesta malo, najviše jednom na dan.
// Kad mjesta opet ima, biljeg se briše, pa sljedeći put e-pošta ide odmah.
func (o *obavijestDiska) nakonProvjere(diskovi []disk.Disk) {
	malo := maloMjesta(diskovi)
	if len(malo) == 0 {
		o.zadnja = time.Time{}
		if err := os.Remove(o.biljeg); err != nil && !errors.Is(err, fs.ErrNotExist) {
			o.javi("disk: zapis o zadnjoj e-pošti o disku ne da se obrisati: %v", err)
		}
		return
	}
	if o.nedavno() {
		return
	}
	// prvo zapis, pa slanje: neuspjelo slanje ne ponavlja se svakih deset
	// minuta, da poslužitelj ne zaključa račun zbog krive lozinke
	o.zapisi()
	ctx, cancel := context.WithTimeout(o.ctx, istekObavijestiDiska)
	defer cancel()
	n, err := o.posalji(ctx, predmetObavijestiDiska, tekstObavijestiDiska(o.ime(), malo))
	switch {
	case err != nil:
		o.javi("disk: e-pošta o disku nije poslana svima (poslano %d): %v", n, err)
	case n == 0:
		o.javi("disk: nitko na ovom računalu nije spremio lozinku sandučića, pa e-pošta o disku nije poslana; upozorenje stoji na pločici razmjene")
	default:
		o.javi("disk: e-pošta o disku poslana je (primatelja: %d)", n)
	}
}

// nedavno: e-pošta o disku poslana je u zadnja 24 sata. Biljeg iz
// budućnosti (pomaknut sat) ne vrijedi.
func (o *obavijestDiska) nedavno() bool {
	t := o.zadnja
	if b, err := os.ReadFile(o.biljeg); err == nil {
		if u, err := time.Parse(time.RFC3339, strings.TrimSpace(string(b))); err == nil && u.After(t) {
			t = u
		}
	}
	d := o.sad().Sub(t)
	return d >= 0 && d < razmakObavijestiDiska
}

func (o *obavijestDiska) zapisi() {
	o.zadnja = o.sad()
	if err := os.WriteFile(o.biljeg, []byte(o.zadnja.UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
		o.javi("disk: zapis o e-pošti o disku nije spremljen, pa je nakon ponovnog pokretanja čvora e-pošta može stići opet: %v", err)
	}
}

// tekstObavijestiDiska: što piše u e-pošti o disku
func tekstObavijestiDiska(cvor string, malo []disk.Disk) string {
	var b strings.Builder
	for _, d := range malo {
		b.WriteString(d.Poruka(cvor) + " " + d.Savjet() + "\n\n")
	}
	b.WriteString(javiAdministratoru + " Proslijedite mu ovu poruku.\n\n")
	b.WriteString("Poruku je goCOP poslao s vašeg računa e-pošte jer ste na ovom računalu spremili lozinku sandučića. " +
		"Dok mjesta bude malo, stiže jednom na dan.\n")
	return b.String()
}

// pokreniNadzorDiska provjerava slobodno mjesto odmah i zatim svakih deset
// minuta, dok čvor radi. Upozorenje se vidi na pločici razmjene, a e-poštom
// stiže osobama koje su na ovom računalu spremile lozinku sandučića.
func pokreniNadzorDiska(ctx context.Context, dbPath, arhivaPut, podaci, paketi string, ime func() string,
	posalji func(ctx context.Context, predmet, tekst string) (int, error)) *disk.Nadzor {
	n := disk.NoviNadzor(disk.Sustav{}, log.Printf, mjestaDiska(dbPath, arhivaPut, podaci, paketi)...)
	o := &obavijestDiska{ctx: ctx, ime: ime, posalji: posalji, sad: time.Now, javi: log.Printf,
		biljeg: filepath.Join(filepath.Dir(dbPath), biljegObavijestiDiska)}
	n.NakonProvjere(o.nakonProvjere)
	go n.Vrti(ctx, disk.Svakih)
	return n
}
