package main

// Satni krug čvora koji izdaje prognozu: ide nakon svakog preuzimanja
// vodostaja s izvora. Strane prognoze (mađarska, austrijska, srpska),
// oborine, stvarni kišomjeri (i jednom dnevno njihovo ulaganje u arhivu),
// izračun i izdavanje prognoze. Čvor koji prognozu ne izdaje ništa od toga
// ne radi: izdanje mu stiže razmjenom.

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"

	"gocop/internal/javnivodostaji"
	"gocop/internal/kisomjeri"
	"gocop/internal/models"
	"gocop/internal/oborine"
	"gocop/internal/peers"
	"gocop/internal/prognoza"
	"gocop/internal/web"
)

// Dohvati stranih prognoza; testovi ih zamjenjuju, jer pravi idu na internet
var (
	dohvatiMadjarsku  = prognoza.Dohvati
	dohvatiAustrijsku = prognoza.DohvatiNOEL
	dohvatiSrpsku     = prognoza.DohvatiHidmet
)

// stranaPrognoza je tuđa služba čija se prognoza čita svaki krug
type stranaPrognoza struct {
	korak      string // faza kruga na ekranu
	postotak   int
	podrijetlo string
	dohvati    func(context.Context, *http.Client) ([]prognoza.Letva, error)
	sifra      func(string) string
}

// stranePrognoze redom kojim se čitaju:
//   - mađarska izlazi jednom dnevno, ali ne uvijek u isti sat; treba je i za
//     usporedbu i kao ulaz tamo gdje nam lanac nema ništa uzvodno;
//   - austrijska (Donja Austrija) daje vrhu Dunava, Wildungsmaueru, 48 sati
//     unaprijed; izlazi više puta dnevno;
//   - srpska izlazi u 12 h; uz naše letve stoji drugom bojom.
func stranePrognoze() []stranaPrognoza {
	return []stranaPrognoza{
		{"mađarska prognoza (hydroinfo.hu)", 91, prognoza.Podrijetlo, dohvatiMadjarsku, prognoza.Sifra},
		{"austrijska prognoza (noel.gv.at)", 92, prognoza.PodrijetloNOEL, dohvatiAustrijsku, prognoza.SifraNOEL},
		{"srpska prognoza (hidmet.gov.rs)", 94, prognoza.PodrijetloHidmet, dohvatiSrpsku, prognoza.SifraSrpske},
	}
}

// preuzmiStranuPrognozu dohvati jednu tuđu prognozu i zapiše što je novo
func preuzmiStranuPrognozu(ctx context.Context, uvoznik *javnivodostaji.Uvoznik, pb *sql.DB, iz stranaPrognoza) {
	letve, err := iz.dohvati(ctx, nil)
	if err != nil {
		log.Printf("%s: %v", iz.podrijetlo, err)
		uvoznik.Redak("%s: %v", iz.podrijetlo, err)
		return
	}
	n, err := prognoza.SpremiTude(pb, iz.podrijetlo, letve, iz.sifra)
	if err != nil {
		log.Printf("%s: zapis: %v", iz.podrijetlo, err)
		uvoznik.Redak("%s: zapis: %v", iz.podrijetlo, err)
		return
	}
	if n > 0 {
		log.Printf("%s: zapisano %d novih vrijednosti", iz.podrijetlo, n)
	}
	uvoznik.Redak("%s: %d letvi, %d novih vrijednosti", iz.podrijetlo, len(letve), n)
}

// satniKrug drži ono o čemu krug ovisi i dan zadnjeg ulaganja kišomjera
type satniKrug struct {
	uvoznik     *javnivodostaji.Uvoznik
	mreza       *peers.Service
	baza        *sql.DB // glavna baza (registar oborinskih točaka)
	prognoze    *sql.DB
	osvjezivac  *prognoza.Osvjezivac
	oborine     *oborine.Uvoznik   // nil kad oborine nisu uključene
	kisomjeri   *kisomjeri.Uvoznik // nil kad stvarni kišomjeri nisu uključeni
	posluzitelj *web.Server
	razmjenaArh *razmjenaArhive // nil bez arhive
	podaci      string          // stablo izvornih datoteka arhive; prazno bez njega
	objavi      func(context.Context, *prognoza.Ishod)

	kisUlozeno string // dan zadnjeg ulaganja mjerenja u arhivu
}

// vrti je jedan krug. Tijelo je premješteno iz main bez izmjena, pa
// ovisnosti nose ista imena kao ondje.
func (k *satniKrug) vrti(ctx context.Context) {
	javniUvoznik, peersService, pb := k.uvoznik, k.mreza, k.prognoze
	oborineUvoznik, kisUvoznik := k.oborine, k.kisomjeri
	if !peersService.TrenutneUloge().Izdaje {
		// Tuđe prognoze, kišu i izračun radi čvor koji izdaje
		// prognozu; njegovo izdanje stiže razmjenom.
		javniUvoznik.Redak("prognozu izdaje drugi čvor; izdanje stiže razmjenom")
		return
	}
	// Tuđe prognoze: svaka se čita svaki krug, a zapisuje samo novo; greška
	// jedne ne zaustavlja ostale
	hu, otkazi := context.WithTimeout(ctx, time.Minute)
	for _, iz := range stranePrognoze() {
		javniUvoznik.Korak(iz.korak, iz.postotak)
		preuzmiStranuPrognozu(hu, javniUvoznik, pb, iz)
	}
	otkazi()
	if oborineUvoznik != nil {
		k.preuzmiOborine(ctx)
	}
	if kisUvoznik != nil {
		k.preuzmiKisomjere(ctx)
	}
	k.izracunajIIzdaj(ctx)
}

// preuzmiOborine preuzme satne oborine za kišomjere (Open-Meteo)
func (k *satniKrug) preuzmiOborine(ctx context.Context) {
	javniUvoznik, database, osvjezivac, oborineUvoznik := k.uvoznik, k.baza, k.osvjezivac, k.oborine
	javniUvoznik.Korak("oborine (Open-Meteo)", 95)
	ob, otkazi := context.WithTimeout(ctx, 2*time.Minute)
	if n, err := oborineUvoznik.Preuzmi(ob); err != nil {
		log.Printf("oborine: %v", err)
		javniUvoznik.Redak("oborine: %v", err)
	} else {
		javniUvoznik.Redak("oborine: %d sati za kišomjere", n)
	}
	otkazi()
	// registar se mogao promijeniti (nova ili premještena točka)
	if tocke, err := prognoza.OborinskeTocke(database); err == nil && len(tocke) > 0 {
		osvjezivac.Oborine = &prognoza.OborinskiIzvor{Tocke: tocke, Satne: oborineUvoznik.Satne}
	}
}

// preuzmiKisomjere preuzme mjerenja stvarnih kišomjera i jednom dnevno
// završene dane uloži u arhivu i izda pakete
func (k *satniKrug) preuzmiKisomjere(ctx context.Context) {
	javniUvoznik, kisUvoznik, server, razmjenaArh := k.uvoznik, k.kisomjeri, k.posluzitelj, k.razmjenaArh
	javniUvoznik.Korak("stvarni kišomjeri", 95)
	kc, otkazi := context.WithTimeout(ctx, 2*time.Minute)
	n, err := kisUvoznik.Preuzmi(kc)
	otkazi()
	if err != nil {
		log.Printf("stvarni kišomjeri: %v", err)
		javniUvoznik.Redak("stvarni kišomjeri: %v", err)
	}
	javniUvoznik.Redak("stvarni kišomjeri: %d mjerenja", n)
	// Jednom dnevno, nakon ponoći, završeni dani idu u arhivu:
	// zadnja četiri dana, da krug koji je ispao ne ostavi rupu.
	sada := time.Now()
	if dan := sada.In(kisomjeri.Zagreb).Format("2006-01-02"); dan != k.kisUlozeno && k.podaci != "" && !imaStablo(k.podaci) {
		// Gradnja letvu slaže iz stabla, pa bi čvoru koji je arhivu
		// dobio paketima povijest kišomjera svela na zadnje dane.
		javniUvoznik.Redak("stvarni kišomjeri: na ovom čvoru nema stabla izvornih datoteka (%s), pa se ne ulažu u arhivu", k.podaci)
		k.kisUlozeno = dan
	}
	if dan := sada.In(kisomjeri.Zagreb).Format("2006-01-02"); dan != k.kisUlozeno && k.podaci != "" {
		if postaje, err := kisUvoznik.Postaje(); err == nil {
			letve, err := kisomjeri.Ulozi(k.podaci, kisUvoznik.Spremiste, postaje, sada.Add(-96*time.Hour), sada)
			if err != nil {
				log.Printf("stvarni kišomjeri, ulaganje: %v", err)
			}
			izdano := 0
			for _, l := range letve {
				if _, err := server.IzgradiLetvu(l, nil); err != nil {
					log.Printf("stvarni kišomjeri, gradnja %s: %v", l, err)
					continue
				}
				// Paket izlazi odmah, pa arhiva kiše raste i na
				// ostalim čvorovima bez ručnog izdavanja.
				if iz, err := server.IzdajArhivu(l, false, nil); err != nil {
					log.Printf("stvarni kišomjeri, izdavanje %s: %v", l, err)
				} else {
					izdano += iz.Promijenjenih
				}
			}
			if izdano > 0 && razmjenaArh != nil {
				razmjenaArh.potakniKrug() // kazalo novih paketa ide u razmjenu odmah
			}
			javniUvoznik.Redak("stvarni kišomjeri: uloženo u arhivu za %d postaja, izdano %d paketa", len(letve), izdano)
			k.kisUlozeno = dan
		}
	}
}

// izracunajIIzdaj izračuna prognozu za ovaj sat (ili iznova, kad je
// tražena rukom), zapiše je i objavi; kad vrh lanca kasni, to javi
func (k *satniKrug) izracunajIIzdaj(ctx context.Context) {
	javniUvoznik, osvjezivac, objaviIzdanje := k.uvoznik, k.osvjezivac, k.objavi
	javniUvoznik.Korak("izračun prognoze", 96)
	racun := osvjezivac
	if prognoza.TraziIznova(ctx) {
		// Generiraj rukom: novi račun i za već izdani sat.
		iznova := *osvjezivac
		iznova.Iznova = true
		racun = &iznova
	}
	ishod, err := racun.Osvjezi(ctx)
	if err != nil {
		log.Printf("prognoza: %v", err)
		javniUvoznik.Redak("prognoza: %v", err)
		return
	}
	if ishod.Preskoceno {
		if ceka := ishod.KoCeka(); ceka != "" {
			// Prognoza stoji jer vrh lanca kasni: to se mora vidjeti, a ne
			// izgledati kao da je sve u redu.
			poruka := fmt.Sprintf("prognoza stoji na %s jer kasni vrh lanca: %s",
				time.Unix(ishod.Sada*3600, 0).In(models.Zagreb).Format("2.1. u 15:04"), ceka)
			log.Printf("prognoza: %s", poruka)
			javniUvoznik.Redak("%s", poruka)
			return
		}
		javniUvoznik.Redak("prognoza: za ovaj sat već je izdana, ništa novo")
		return
	}
	javniUvoznik.Korak("zapis prognoze", 98)
	if err := osvjezivac.Zapisi(ishod); err != nil {
		log.Printf("prognoza: zapis: %v", err)
		javniUvoznik.Redak("prognoza: zapis: %v", err)
		return
	}
	objaviIzdanje(ctx, ishod)
	log.Printf("prognoza: izdana za %s UTC (%.0f h unatrag), %d letvi, %d vrijednosti",
		time.Unix(ishod.Sada*3600, 0).UTC().Format("2006-01-02 15:04"),
		ishod.Zaostatak(time.Now()).Hours(), ishod.Letvi(), len(ishod.Izdane))
	javniUvoznik.Redak("prognoza izdana za %s, %d letvi, %d vrijednosti",
		time.Unix(ishod.Sada*3600, 0).In(models.Zagreb).Format("2.1. u 15:04"), ishod.Letvi(), len(ishod.Izdane))
	for letva, zasto := range ishod.BezPrognoze {
		javniUvoznik.Redak("bez prognoze %s: %v", letva, zasto)
	}
}
