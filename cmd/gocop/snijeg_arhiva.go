package main

// Snijeg na tlu u arhivi. Čvor koji izdaje prognozu jedini čita SNOWGRID;
// preuzete dane ulaže u arhivsko stablo kao niz točke (izvor snowgrid,
// veličina snijeg-na-tlu, po danu), gradi točku i izdaje njezin paket, pa
// povijest ostalim čvorovima stiže paketima arhive, kao kiša kišomjera.

import (
	"log"
	"sync"

	"gocop/internal/config"
	"gocop/internal/kisomjeri"
	"gocop/internal/oborine"
)

// snijegUArhivu ulaže snijeg na tlu u arhivu i izdaje pakete točaka.
type snijegUArhivu struct {
	naTlu       *oborine.SnijegNaTlu
	podaci      string          // stablo izvornih datoteka arhive; prazno bez njega
	arh         arhivaKisomjera // gradnja i izdavanje; nil bez poslužitelja
	razmjenaArh *razmjenaArhive // nil bez arhive

	javljeno sync.Once // „nema stabla” ide u dnevnik jednom
}

// ulozi uloži nove dane u stablo, izgradi promijenjene točke i izda njihove
// pakete. Čvor bez stabla izvornih datoteka ne ulaže: gradnja točku slaže
// iz stabla, pa bi točki koju je dobio paketom povijest svela na nove dane.
func (u *snijegUArhivu) ulozi() {
	if u.podaci == "" || u.arh == nil {
		return
	}
	if !imaStablo(u.podaci) {
		u.javljeno.Do(func() {
			log.Printf("snijeg na tlu (SNOWGRID): na ovom čvoru nema stabla izvornih datoteka (%s), pa se ne ulaže u arhivu",
				config.PunaPutanja(u.podaci))
		})
		return
	}
	tocke, err := u.naTlu.Ulozi(u.podaci, kisomjeri.SlivStabla)
	if err != nil {
		log.Printf("snijeg na tlu (SNOWGRID), ulaganje u arhivu: %v", err)
	}
	izdano := izgradiIIzdaj(u.arh, tocke, "snijeg na tlu")
	if izdano > 0 && u.razmjenaArh != nil {
		u.razmjenaArh.potakniKrug() // kazalo novih paketa ide u razmjenu odmah
	}
	if len(tocke) > 0 {
		log.Printf("snijeg na tlu (SNOWGRID): u arhivu uloženo %d točaka, izdano %d paketa", len(tocke), izdano)
	}
}
