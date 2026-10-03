package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/gogpu/systray"

	"gocop/internal/ikona"
	"gocop/internal/postava"
)

// traka je ikona u traci s izbornikom; sva logika je u internal/postava
type traka struct {
	p          *postava.Postava
	t          *systray.SystemTray
	instaliran bool // Postava stoji u mapi instalacije (ne proba iz mape gradnje)
	ctx        context.Context
	izadji     func()

	status, otvori, prekidac, nadogradi, provjeri, priPrijavi *systray.MenuItem

	mu       sync.Mutex
	ikonaZa  string // stanje za koje je ikona zadnji put nacrtana
	javljeno string // izdanje o kojem je već stigla obavijest
}

func pokreniTraku(p *postava.Postava) {
	otkljucaj, vec, err := postava.Zakljucaj(p.M)
	if err != nil {
		p.Pisi("Zaključavanje: %v", err)
	}
	if vec {
		postava.Poruka(naslov, "goCOP Postava već radi: ikona je u traci uz sat.", false)
		return
	}
	p.Pisi("goCOP Postava %s pokrenuta (%s)", verzija, p.Exe)

	ctx, cancel := context.WithCancel(context.Background())
	tr := &traka{p: p, ctx: ctx, instaliran: filepath.Base(filepath.Dir(p.Exe)) == postava.ImeMapePostave}
	var jednom sync.Once
	tr.izadji = func() {
		jednom.Do(func() {
			p.Pisi("Izlaz: gasim čvor")
			_ = p.Cvor.Zaustavi(30 * time.Second)
			cancel()
			otkljucaj()
			tr.t.Remove()
			os.Exit(0)
		})
	}

	tr.t = systray.New()
	tr.t.SetMenu(tr.izbornik()).SetTooltip("goCOP").Show()
	tr.t.OnDoubleClick(tr.otvoriPlocu)
	tr.osvjezi()
	p.NaPromjenu(tr.osvjezi)

	// macOS i Linux: -zaustavi i deinstalacija šalju SIGTERM
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, os.Interrupt)
	go func() { <-sig; tr.izadji() }()

	go p.Prati(ctx)
	go tr.pocetak()
	if err := tr.t.Run(); err != nil {
		p.Pisi("Traka: %v", err)
	}
	tr.izadji()
}

func (tr *traka) izbornik() *systray.Menu {
	m := systray.NewMenu()
	tr.status = m.Add("goCOP", nil)
	tr.status.SetDisabled(true)
	m.AddSeparator()
	tr.otvori = m.Add("Otvori goCOP", tr.otvoriPlocu)
	tr.prekidac = m.Add("Zaustavi", func() { go tr.prekidacKlik() })
	m.AddSeparator()
	tr.nadogradi = m.Add("Nema novijeg izdanja", func() { go tr.nadogradiKlik() })
	tr.provjeri = m.Add("Provjeri nadogradnje", func() { go tr.provjeriKlik() })
	m.AddSeparator()
	m.Add("Otvori mapu s podacima", func() { _ = postava.Otvori(tr.p.M.Podaci) })
	m.Add("Dnevnik čvora", func() { _ = postava.Otvori(tr.p.M.DnevnikCvora()) })
	ukljuceno, _ := postava.PriPrijavi(tr.p.Exe)
	tr.priPrijavi = m.AddCheckbox("Pokreni pri prijavi", ukljuceno, func() { go tr.priPrijaviKlik() })
	m.Add("O programu", func() { go tr.oProgramu() })
	m.AddSeparator()
	m.Add("Izlaz", func() { go tr.izadji() })
	return m
}

// pocetak: popravi unose instalacije, preuzmi goCOP ako ga nema, pa ga pokreni
func (tr *traka) pocetak() {
	p := tr.p
	if tr.instaliran {
		if ukljuceno, ispravno := postava.PriPrijavi(p.Exe); ukljuceno && !ispravno {
			// unos pokazuje na staru putanju (npr. premještena instalacija)
			if err := postava.UkljuciPriPrijavi(p.Exe); err == nil {
				p.Pisi("Pokretanje pri prijavi popravljeno na %s", p.Exe)
			}
		}
		if !postava.UPutu(p.M.Program) {
			if err := postava.DodajUPut(p.M.Program); err != nil {
				p.Pisi("PATH: %v", err)
			}
		}
	}
	if !p.Cvor.Instaliran() {
		tr.preuzmi()
		return
	}
	if err := p.Cvor.Pokreni(); err != nil {
		p.Pisi("Pokretanje čvora: %v", err)
		postava.Poruka(naslov, "goCOP se ne može pokrenuti: "+err.Error(), true)
	}
}

// preuzmi dohvaća prvo izdanje goCOP-a kad ga još nema
func (tr *traka) preuzmi() {
	p := tr.p
	if err := p.Provjeri(tr.ctx); err != nil {
		postava.Poruka(naslov, "goCOP još nije preuzet, a GitHub nije dostupan:\n"+err.Error()+
			"\n\nPokušajte kasnije: desni klik na ikonu → Preuzmi goCOP.", true)
		return
	}
	s := p.Stanje()
	if s.Novije == nil {
		postava.Poruka(naslov, "Na GitHubu još nema potpisanog izdanja goCOP-a za ovaj sustav.", true)
		return
	}
	if !postava.Pitanje(naslov, fmt.Sprintf("goCOP još nije preuzet. Preuzeti izdanje %s sada?", s.Novije.Verzija)) {
		return
	}
	if _, err := p.Nadogradi(tr.ctx); err != nil {
		postava.Poruka(naslov, "Preuzimanje nije uspjelo: "+err.Error(), true)
		return
	}
	if err := p.Cvor.Pokreni(); err != nil {
		postava.Poruka(naslov, "goCOP se ne može pokrenuti: "+err.Error(), true)
	}
}

func (tr *traka) otvoriPlocu() {
	s := tr.p.Stanje()
	if s.Cvor != postava.Radi {
		return
	}
	_ = postava.Otvori(postava.AdresaPloce(s.Port))
}

func (tr *traka) prekidacKlik() {
	p := tr.p
	switch p.Stanje().Cvor {
	case postava.NijeInstaliran:
		tr.preuzmi()
	case postava.Zaustavljen, postava.Pao:
		if err := p.Cvor.Pokreni(); err != nil {
			postava.Poruka(naslov, "goCOP se ne može pokrenuti: "+err.Error(), true)
		}
	default:
		if err := p.Cvor.Zaustavi(30 * time.Second); err != nil {
			postava.Poruka(naslov, err.Error(), true)
		}
	}
}

func (tr *traka) nadogradiKlik() {
	p := tr.p
	s := p.Stanje()
	if s.Novije == nil || s.Zauzeto != "" {
		return
	}
	staro := s.Izdanje
	if staro == "" {
		staro = "trenutnog"
	}
	pitanje := fmt.Sprintf("Nadograditi goCOP s %s na %s?\n\nČvor se nakratko zaustavlja, a baza se prije toga kopira u mapu data\\kopije. Ako novo izdanje ne krene, vraća se prethodno.", staro, s.Novije.Verzija)
	if !postava.Pitanje(naslov, pitanje) {
		return
	}
	izd, err := p.Nadogradi(tr.ctx)
	if err != nil {
		postava.Poruka(naslov, "Nadogradnja nije uspjela:\n"+err.Error(), true)
		return
	}
	tr.t.ShowNotification("goCOP", "Nadograđeno na "+izd)
}

func (tr *traka) provjeriKlik() {
	p := tr.p
	if err := p.Provjeri(tr.ctx); err != nil {
		postava.Poruka(naslov, "Provjera nije uspjela:\n"+err.Error(), true)
		return
	}
	s := p.Stanje()
	if s.Novije != nil {
		tr.nadogradiKlik()
		return
	}
	poruka := "goCOP je najnovije izdanje."
	if s.Izdanje != "" {
		poruka = "goCOP " + s.Izdanje + " je najnovije izdanje."
	}
	if s.NovijaPostava != "" {
		poruka += "\n\nDostupna je nova Postava (" + s.NovijaPostava + "): " + s.StranicaPostave
	}
	postava.Poruka(naslov, poruka, false)
}

func (tr *traka) priPrijaviKlik() {
	ukljuceno, _ := postava.PriPrijavi(tr.p.Exe)
	var err error
	if ukljuceno {
		err = postava.IskljuciPriPrijavi()
	} else {
		err = postava.UkljuciPriPrijavi(tr.p.Exe)
	}
	if err != nil {
		postava.Poruka(naslov, err.Error(), true)
	}
	sad, _ := postava.PriPrijavi(tr.p.Exe)
	tr.priPrijavi.SetChecked(sad)
}

func (tr *traka) oProgramu() {
	s := tr.p.Stanje()
	cvor := "nije preuzet"
	switch {
	case s.Izdanje != "":
		cvor = s.Izdanje + ", radi"
	case s.Cvor != postava.NijeInstaliran:
		if izd, err := postava.IzdanjeDatoteke(tr.ctx, tr.p.M.Gocop()); err == nil {
			cvor = izd + ", " + opisStanja(s)
		}
	}
	tekst := fmt.Sprintf("goCOP Postava %s\ngoCOP: %s\n\nMapa: %s", verzija, cvor, tr.p.M.Baza)
	if !s.ZadnjaProvjera.IsZero() {
		tekst += "\nZadnja provjera izdanja: " + s.ZadnjaProvjera.Format("2. 1. 2006. 15:04")
	}
	if s.NovijaPostava != "" {
		tekst += "\n\nDostupna je nova Postava (" + s.NovijaPostava + "): " + s.StranicaPostave
	}
	tekst += "\n\nIzdanja: https://github.com/tkraljevic/goCOP/releases\nLicenca: EUPL-1.2"
	postava.Poruka(naslov, tekst, false)
}

func opisStanja(s postava.Stanje) string {
	switch s.Cvor {
	case postava.Zaustavljen:
		return "zaustavljen"
	case postava.Pokrece:
		return "pokreće se"
	case postava.NeOdgovara:
		return "ne odgovara"
	case postava.Pao:
		return "pao je; vidi dnevnik čvora"
	case postava.Radi:
		return "radi"
	}
	return "nije preuzet"
}

// osvjezi slaže izbornik, opis i ikonu prema stanju
func (tr *traka) osvjezi() {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	s := tr.p.Stanje()

	status := "goCOP " + opisStanja(s)
	if s.Izdanje != "" {
		status = "goCOP " + s.Izdanje + " " + opisStanja(s)
	}
	if s.Zauzeto != "" {
		status = s.Zauzeto + "…"
	}
	tr.status.SetLabel(status)
	tr.t.SetTooltip(status)
	tr.otvori.SetDisabled(s.Cvor != postava.Radi)

	switch s.Cvor {
	case postava.NijeInstaliran:
		tr.prekidac.SetLabel("Preuzmi goCOP")
	case postava.Zaustavljen, postava.Pao:
		tr.prekidac.SetLabel("Pokreni")
	default:
		tr.prekidac.SetLabel("Zaustavi")
	}
	tr.prekidac.SetDisabled(s.Zauzeto != "")
	tr.provjeri.SetDisabled(s.Zauzeto != "")

	if s.Novije != nil {
		tr.nadogradi.SetLabel("Nadogradi na " + s.Novije.Verzija.String())
		tr.nadogradi.SetDisabled(s.Zauzeto != "")
		if tr.javljeno != s.Novije.Oznaka && s.Cvor != postava.NijeInstaliran {
			tr.javljeno = s.Novije.Oznaka
			tr.t.ShowNotification("goCOP", "Dostupno je izdanje "+s.Novije.Verzija.String()+". Nadogradnja: desni klik na ikonu.")
		}
	} else {
		tr.nadogradi.SetLabel("Nema novijeg izdanja")
		tr.nadogradi.SetDisabled(true)
	}

	st := ikona.Stoji
	switch s.Cvor {
	case postava.Radi, postava.Pokrece:
		st = ikona.Radi
		if s.Novije != nil {
			st = ikona.Nadogradnja
		}
	case postava.NeOdgovara, postava.Pao:
		st = ikona.Greska
	}
	kljuc := fmt.Sprint(st)
	if kljuc != tr.ikonaZa {
		tr.ikonaZa = kljuc
		postaviIkonu(tr.t, st)
	}
}
