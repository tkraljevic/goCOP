package main

// Satni krug čvora koji izdaje prognozu: ide nakon svakog preuzimanja
// vodostaja s izvora. Strane prognoze (mađarska, austrijska, srpska),
// oborine, stvarni kišomjeri (i jednom dnevno njihovo ulaganje u arhivu),
// izračun i izdavanje prognoze. Čvor koji prognozu ne izdaje ništa od toga
// ne radi: izdanje mu stiže razmjenom.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/config"
	"gocop/internal/javnivodostaji"
	"gocop/internal/kisomjeri"
	"gocop/internal/models"
	"gocop/internal/oborine"
	"gocop/internal/peers"
	"gocop/internal/prognoza"
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
//   - mađarska izlazi jednom dnevno, ali ne uvijek u isti sat; stoji uz našu
//     radi usporedbe i puni pričuvni izračun u Excelu, a u lanac ne ulazi;
//   - austrijska (Donja Austrija) za Wildungsmauer 48 sati unaprijed, više
//     puta dnevno; stoji samo radi usporedbe, u lanac ne ulazi;
//   - srpska izlazi u 12 h; uz naše letve stoji drugom bojom.
func stranePrognoze() []stranaPrognoza {
	return []stranaPrognoza{
		{"mađarska prognoza (hydroinfo.hu)", 91, prognoza.Podrijetlo, dohvatiMadjarsku, prognoza.Sifra},
		{"austrijska prognoza (noel.gv.at)", 92, prognoza.PodrijetloNOEL, dohvatiAustrijsku, prognoza.SifraNOEL},
		{"srpska prognoza (hidmet.gov.rs)", 94, prognoza.PodrijetloHidmet, dohvatiSrpsku, prognoza.SifraSrpske},
	}
}

// preuzmiStranuPrognozu dohvati jednu tuđu prognozu i zapiše što je novo.
// Tablica s neurednim datumima zapiše se do prvog neurednog, a baza
// prognoza to pamti za upozorenje izdanja dok tablica ne bude ispravljena.
func preuzmiStranuPrognozu(ctx context.Context, uvoznik *javnivodostaji.Uvoznik, pb *sql.DB, iz stranaPrognoza) {
	letve, err := iz.dohvati(ctx, nil)
	neuredne := prognoza.NeuredneIz(err)
	if err != nil && len(neuredne) == 0 {
		log.Printf("%s: %v", iz.podrijetlo, err)
		uvoznik.Redak("%s: %v", iz.podrijetlo, err)
		return
	}
	for _, nd := range neuredne {
		log.Printf("%s: %v", iz.podrijetlo, nd)
		uvoznik.Redak("%s: %v", iz.podrijetlo, nd)
	}
	n, greska := prognoza.SpremiTude(pb, iz.podrijetlo, letve, iz.sifra)
	if greska != nil {
		log.Printf("%s: zapis: %v", iz.podrijetlo, greska)
		uvoznik.Redak("%s: zapis: %v", iz.podrijetlo, greska)
		if !errors.As(greska, new(*prognoza.PreskocenePostaje)) {
			return // ništa nije zapisano
		}
	}
	if greska := prognoza.ZapamtiNeuredne(pb, iz.podrijetlo, err); greska != nil {
		log.Printf("%s: zapis neurednih datuma: %v", iz.podrijetlo, greska)
	}
	if n > 0 {
		log.Printf("%s: zapisano %d novih vrijednosti", iz.podrijetlo, n)
	}
	uvoznik.Redak("%s: %s", iz.podrijetlo, opisStranePrognoze(len(letve), n))
}

// uloziKisomjere uloži mjerenja kišomjera u arhivsko stablo; testovi ga
// zamjenjuju
var uloziKisomjere = kisomjeri.Ulozi

// arhivaKisomjera gradi letve kišomjera u arhivi i izdaje njihove pakete; na
// čvoru je to poslužitelj, a u testovima zamjena
type arhivaKisomjera interface {
	IzgradiLetvu(letva string, zapisi io.Writer) (arhiva.Izvjestaj, error)
	IzdajArhivu(letva string, probno bool, zapisi io.Writer) (arhiva.IzvjestajIzdanja, error)
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
	posluzitelj arhivaKisomjera
	razmjenaArh *razmjenaArhive // nil bez arhive
	podaci      string          // stablo izvornih datoteka arhive; prazno bez njega
	objavi      func(context.Context, *prognoza.Ishod)

	kisUlozeno string // dan zadnjeg ulaganja mjerenja u arhivu
}

// biljegPreuzimanja je datoteka uz bazu s vremenom zadnjeg preuzimanja
// letvi, da ponovno pokretanje zna je li krug ovog sata već prošao
const biljegPreuzimanja = "zadnje-preuzimanje.txt"

// minutaPreuzimanja je minuta svakog sata u kojoj počinje krug, iz
// gocop.toml; neispravna se javi u dnevnik i vrijedi zadana
func minutaPreuzimanja(cfg config.Config, javi func(string, ...any)) int {
	minuta, poruka := cfg.MinutaPreuzimanja()
	if poruka != "" {
		javi("gocop.toml [vodostaji] minuta_preuzimanja = %d ne vrijedi (%s); krug ide u :%02d",
			cfg.Readings.MinutaPreuzimanja, poruka, minuta)
	}
	return minuta
}

// bezIzdanja javlja da na čvoru koji izdaje prognozu za zadani sat još nema
// izdanja; tada krug pri pokretanju ide odmah. Čvor koji ne izdaje izdanje
// prima razmjenom, pa za njega krug ne žuri.
func (k *satniKrug) bezIzdanja(sat time.Time) bool {
	if !k.mreza.TrenutneUloge().Izdaje {
		return false
	}
	zadnje, ima, err := prognoza.ZadnjeIzdanje(k.prognoze)
	if err != nil {
		log.Printf("prognoza: zadnje izdanje: %v", err)
		return true
	}
	return !ima || zadnje < sat.Unix()/3600
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
		javniUvoznik.Redak("%s", opisOborina(n))
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
	javniUvoznik, kisUvoznik := k.uvoznik, k.kisomjeri
	javniUvoznik.Korak("stvarni kišomjeri", 95)
	kc, otkazi := context.WithTimeout(ctx, 2*time.Minute)
	n, err := kisUvoznik.Preuzmi(kc)
	otkazi()
	if err != nil {
		log.Printf("stvarni kišomjeri: %v", err)
		javniUvoznik.Redak("stvarni kišomjeri: %v", err)
	}
	javniUvoznik.Redak("%s", opisMjerenjaKisomjera(n))
	k.ulaziKisomjereJednomDnevno(time.Now())
}

// ulaziKisomjereJednomDnevno: jednom dnevno, nakon ponoći, završeni dani
// idu u arhivu. Čvor bez stabla izvornih datoteka ih ne ulaže: gradnja
// letvu slaže iz stabla, pa bi čvoru koji je arhivu dobio paketima
// povijest kišomjera svela na zadnje dane.
func (k *satniKrug) ulaziKisomjereJednomDnevno(sada time.Time) {
	dan := sada.In(kisomjeri.Zagreb).Format("2006-01-02")
	if dan == k.kisUlozeno || k.podaci == "" {
		return
	}
	if !imaStablo(k.podaci) {
		k.uvoznik.Redak("stvarni kišomjeri: na ovom čvoru nema stabla izvornih datoteka (%s), pa se ne ulažu u arhivu",
			config.PunaPutanja(k.podaci))
		k.kisUlozeno = dan
		return
	}
	if k.ulaziKisomjere(sada) {
		k.kisUlozeno = dan
	}
}

// ulaziKisomjere uloži u arhivu zadnja četiri dana mjerenja, da krug koji
// je ispao ne ostavi rupu, i izda pakete promijenjenih letvi. Bez popisa
// postaja ne uloži ništa i vrati netočno, pa ulaganje pokuša idući krug.
func (k *satniKrug) ulaziKisomjere(sada time.Time) bool {
	postaje, err := k.kisomjeri.Postaje()
	if err != nil {
		return false
	}
	letve, err := uloziKisomjere(k.podaci, k.kisomjeri.Spremiste, postaje, sada.Add(-96*time.Hour), sada)
	if err != nil {
		log.Printf("stvarni kišomjeri, ulaganje: %v", err)
	}
	izdano := izgradiIIzdaj(k.posluzitelj, letve)
	if izdano > 0 && k.razmjenaArh != nil {
		k.razmjenaArh.potakniKrug() // kazalo novih paketa ide u razmjenu odmah
	}
	k.uvoznik.Redak("%s", opisUlaganjaKisomjera(len(letve), izdano))
	return true
}

// izgradiIIzdaj izgradi svaku promijenjenu letvu kišomjera iz stabla i
// odmah izda njen paket, pa arhiva kiše raste i na ostalim čvorovima bez
// ručnog izdavanja; vraća koliko je paketa izdano
func izgradiIIzdaj(arh arhivaKisomjera, letve []string) int {
	izdano := 0
	for _, l := range letve {
		if _, err := arh.IzgradiLetvu(l, nil); err != nil {
			log.Printf("stvarni kišomjeri, gradnja %s: %v", l, err)
			continue
		}
		iz, err := arh.IzdajArhivu(l, false, nil)
		if err != nil {
			log.Printf("stvarni kišomjeri, izdavanje %s: %v", l, err)
			continue
		}
		izdano += iz.Promijenjenih
	}
	return izdano
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
		javiPreskoceno(javniUvoznik, ishod)
		return
	}
	javniUvoznik.Korak("zapis prognoze", 98)
	// Zapisuje isti račun kojim je izračunato: „Generiraj” tako gazi staro
	// izdanje istog sata cijelo, kao i na čvorovima koji ga primaju razmjenom.
	if err := racun.Zapisi(ishod); err != nil {
		log.Printf("prognoza: zapis: %v", err)
		javniUvoznik.Redak("prognoza: zapis: %v", err)
		return
	}
	objaviIzdanje(ctx, ishod)
	javiIzdanje(javniUvoznik, ishod)
}

// javiPreskoceno kaže zašto krug nije izdao novu prognozu. Kad stoji jer
// vrh lanca kasni, to se mora vidjeti, a ne izgledati kao da je sve u redu.
func javiPreskoceno(uvoznik *javnivodostaji.Uvoznik, ishod *prognoza.Ishod) {
	ceka := ishod.KoCeka()
	if ceka == "" {
		uvoznik.Redak("prognoza: za ovaj sat već je izdana, ništa novo")
		return
	}
	poruka := fmt.Sprintf("prognoza stoji na %s jer kasni vrh lanca: %s",
		time.Unix(ishod.Sada*3600, 0).In(models.Zagreb).Format("2.1. u 15:04"), ceka)
	log.Printf("prognoza: %s", poruka)
	uvoznik.Redak("%s", poruka)
}

// javiIzdanje upisuje u dnevnik kruga za koji je sat prognoza izdana, s
// koliko letvi i vrijednosti, letve bez prognoze i upozorenja izdanja
func javiIzdanje(uvoznik *javnivodostaji.Uvoznik, ishod *prognoza.Ishod) {
	koliko := letveIVrijednosti(ishod.Letvi(), len(ishod.Izdane))
	log.Printf("prognoza: izdana za %s UTC (%.0f h unatrag), %s",
		time.Unix(ishod.Sada*3600, 0).UTC().Format("2006-01-02 15:04"), ishod.Zaostatak(time.Now()).Hours(), koliko)
	uvoznik.Redak("prognoza izdana za %s, %s",
		time.Unix(ishod.Sada*3600, 0).In(models.Zagreb).Format("2.1. u 15:04"), koliko)
	for letva, zasto := range ishod.BezPrognoze {
		uvoznik.Redak("bez prognoze %s: %v", letva, zasto)
	}
	javiUpozorenja(uvoznik, ishod)
}

// javiUpozorenja upisuje u dnevnik kruga dnevne ciljeve koji u izdanju
// nemaju dnevnu prognozu, s razlogom, i otvorena upozorenja izdanja.
// Cilj kojem je glavni model pao, a rezerva računala, ovdje se ne javlja:
// dnevnu ima.
func javiUpozorenja(uvoznik *javnivodostaji.Uvoznik, ishod *prognoza.Ishod) {
	ima := map[string]bool{}
	for _, d := range ishod.Dnevne {
		ima[d.Letva] = true
	}
	var bez []string
	for letva := range ishod.BezDnevne {
		if !ima[letva] {
			bez = append(bez, letva)
		}
	}
	sort.Strings(bez)
	for _, letva := range bez {
		zasto := strings.TrimPrefix(ishod.BezDnevne[letva].Error(), letva+": ")
		log.Printf("prognoza: bez dnevne prognoze %s: %s", letva, zasto)
		uvoznik.Redak("bez dnevne prognoze %s: %s", letva, zasto)
	}
	for _, u := range ishod.Upozorenja {
		if u.Tezina < prognoza.TezinaUpozorenje {
			continue
		}
		log.Printf("prognoza: upozorenje: %s", u)
		uvoznik.Redak("upozorenje: %s", u)
	}
}
