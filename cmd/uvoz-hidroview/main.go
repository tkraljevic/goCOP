// uvoz-hidroview preuzima telemetriju iz Geolux HydroViewa (hdv.voda.hr) i
// dopisuje je u arhivske nizove. Time niz koji se dosad održavao ručnim
// izvozom CSV-a nastavlja sam.
//
// Vodostaj letve uzima se iz veličine „srednji vodostaj“ (#hydro-$7), ne iz
// sirovog očitanja instrumenta: na njoj stoje i pragovi obrane, pa je to ona
// koja je svedena na nulu letve. Vrijednosti stižu u metrima i ovdje se
// pretvaraju u centimetre, kako ih arhiva vodi.
//
// Vjerodajnice se čitaju iz okoline, da ne ostanu u povijesti ljuske:
//
//	read "?Korisnik: " HDV_KORISNIK
//	read -s "?Lozinka: " HDV_LOZINKA
//	export HDV_KORISNIK HDV_LOZINKA
//	go run ./cmd/uvoz-hidroview -letva tikves -sliv dunav -naziv TIKVEŠ -probno=false
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/hidroview"
)

// veličine koje uzimamo i kako se zovu u arhivi
var uzimamo = []struct {
	Sifra    string
	Velicina string
	UCm      bool // vrijednost stiže u metrima
}{
	{hidroview.VelicinaSrednjiVodostaj, "vodostaj", true},
	{hidroview.VelicinaTempVode, "temperatura", false},
	{hidroview.VelicinaProtok, "protok", false},
}

func main() {
	adresa := flag.String("adresa", hidroview.ZadanaAdresa, "adresa sustava")
	koren := flag.String("izlaz", "vodostaji", "korijenska mapa arhivskih nizova")
	sliv := flag.String("sliv", "", "mapa sliva, npr. dunav")
	letva := flag.String("letva", "", "naša šifra letve, npr. tikves")
	naziv := flag.String("naziv", "", "naziv postaje u HydroViewu; prazno znači isto što i -letva")
	izvor := flag.String("izvor", "", "oznaka izvora u arhivi; prazno znači prepoznaj po uređaju")
	vrsta := flag.String("vrsta", "satni", "vrsta niza")
	odS := flag.String("od", "", "od kojeg datuma (YYYY-MM-DD); prazno znači zadnjih 30 dana")
	doS := flag.String("do", "", "do kojeg datuma (YYYY-MM-DD); prazno znači do sada")
	komad := flag.Int("komad", 30, "koliko dana po zahtjevu")
	zaokruzi := flag.Int("zaokruzi", -1, "na koliko minuta svesti vrijeme; -1 znači po mjernom koraku postaje, 0 ne zaokružuje")
	probno := flag.Bool("probno", true, "samo ispiši što bi se upisalo")
	flag.Parse()
	if *sliv == "" || *letva == "" {
		log.Fatal("trebaju -sliv i -letva")
	}
	trazi := *naziv
	if trazi == "" {
		trazi = *letva
	}

	od, do := time.Now().AddDate(0, 0, -30), time.Now()
	if *odS != "" {
		var err error
		if od, err = time.ParseInLocation("2006-01-02", *odS, time.Local); err != nil {
			log.Fatalf("-od: %v", err)
		}
	}
	if *doS != "" {
		var err error
		if do, err = time.ParseInLocation("2006-01-02", *doS, time.Local); err != nil {
			log.Fatalf("-do: %v", err)
		}
		do = do.AddDate(0, 0, 1) // do kraja tog dana
	}

	korisnik, lozinka := os.Getenv("HDV_KORISNIK"), os.Getenv("HDV_LOZINKA")
	if korisnik == "" || lozinka == "" {
		log.Fatal("nedostaju HDV_KORISNIK i HDV_LOZINKA u okolini")
	}
	ctx, otkazi := context.WithTimeout(context.Background(), 30*time.Minute)
	defer otkazi()
	k := &hidroview.Klijent{Adresa: *adresa}
	if err := k.Prijava(ctx, korisnik, lozinka); err != nil {
		log.Fatal("prijava: ", err)
	}

	postaje, err := nadjiPostaje(ctx, k, trazi)
	if err != nil {
		log.Fatal(err)
	}
	// Postaja je gotovo uvijek par zapisivača — tlačni i radarski — pa se
	// uzimaju oba. Svaki ide u svoj izvor, a arhiva pri spajanju sama uzme
	// bolji i rezervnim popuni ono što bolji nije javio.
	fmt.Printf("nađeno zapisivača: %d\n", len(postaje))
	// Oprema se čita unaprijed, jer o njoj ovisi i ime izvora: kad postaja
	// ima dva uređaja iste vrste — Podravska Moslavina ima dva tlačna — ime
	// izvora dobiva i broj zapisivača, da se dva niza ne sliju u jedan.
	oprema := make([][]hidroview.Mjerenje, len(postaje))
	alarmiPo := make([][]hidroview.Alarm, len(postaje))
	imena := make([]string, len(postaje))
	koliko := map[string]int{}
	for i, postaja := range postaje {
		m, a, err := k.Oprema(ctx, postaja.SiteID)
		if err != nil {
			log.Fatal(err)
		}
		oprema[i], alarmiPo[i] = m, a
		imena[i] = *izvor
		if imena[i] == "" {
			imena[i] = prepoznajIzvor(m)
		}
		koliko[imena[i]]++
	}
	for i := range imena {
		if koliko[imena[i]] > 1 {
			imena[i] += "-" + postaje[i].LoggerID
		}
	}
	for i, postaja := range postaje {
		mjerenja, alarmi := oprema[i], alarmiPo[i]
		oznaka := imena[i]
		// Zapisivač javlja koju sekundu prije ili poslije punog koraka —
		// 17:45:56 umjesto 17:45:00 — pa bi svako preuzimanje dodalo novi
		// trenutak umjesto da dopuni postojeći. Vrijeme se zato svodi na
		// mjerni korak, kako je i dosadašnji izvoz radio.
		korak := time.Duration(*zaokruzi) * time.Minute
		if *zaokruzi < 0 {
			korak = time.Duration(postaja.Site.Sken) * time.Minute
		}
		fmt.Printf("\n%s (%s), zapisivač %s, zadnje javljanje %s -> izvor %s\n",
			postaja.Site.Naziv, postaja.Site.SifraPostaje, postaja.LoggerID,
			vrijemeIliNikad(postaja.Zadnje), oznaka)
		for _, a := range alarmi {
			o := strings.ToLower(a.Opis)
			if strings.Contains(o, "stanje") || strings.Contains(o, "mjere") {
				fmt.Printf("  njihov prag: %-22s %s %.2f m = %.0f cm\n", a.Opis, a.Odnos, a.Prag, a.Prag*100)
			}
		}
		for _, u := range uzimamo {
			m := nadjiMjerenje(mjerenja, u.Sifra)
			if m == nil {
				fmt.Printf("  %-12s ovaj zapisivač tu veličinu ne mjeri\n", u.Velicina)
				continue
			}
			redci, err := dohvati(ctx, k, m.ID, od, do, *komad, u.UCm, korak)
			if err != nil {
				fmt.Printf("  %-12s %v\n", u.Velicina, err)
				continue
			}
			if len(redci) == 0 {
				fmt.Printf("  %-12s nema vrijednosti u zadanom razdoblju\n", u.Velicina)
				continue
			}
			fmt.Printf("  %-12s %d vrijednosti, %s … %s, zadnja %.3f\n", u.Velicina, len(redci),
				redci[0].Vrijeme.Local().Format("02.01.2006. 15:04"),
				redci[len(redci)-1].Vrijeme.Local().Format("02.01.2006. 15:04"),
				redci[len(redci)-1].Vrijednost)
			if *probno {
				continue
			}
			put, err := arhiva.Dopuni(*koren, *sliv, *letva, oznaka, u.Velicina, *vrsta, redci)
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println("    ->", put)
		}
	}
	if *probno {
		fmt.Println("\nprobni prolaz — ništa nije zapisano; ponovite s -probno=false")
	}
}

func vrijemeIliNikad(sek int64) string {
	if sek <= 0 {
		return "nikad"
	}
	return time.Unix(sek, 0).Format("2006-01-02 15:04")
}

// prepoznajIzvor imenuje izvor po tome što na zapisivaču visi: Geoluxov LX
// je radar razine, Sebin bubbler tlačni cjevovod. Kad se ne vidi ni jedno ni
// drugo — a događa se, jer instrumenti ondje znaju biti bez naziva — izvor
// ostaje samo „geolux“, da mu se ne pripiše uređaj koji možda nije ondje.
func prepoznajIzvor(mjerenja []hidroview.Mjerenje) string {
	for _, m := range mjerenja {
		o := strings.ToUpper(m.Odakle)
		if strings.Contains(o, "LX-") || strings.Contains(o, "RADAR") {
			return "geolux-radar"
		}
		if strings.Contains(o, "BUBBLER") || strings.Contains(o, "SEBA") {
			return "geolux-seba"
		}
	}
	return "geolux"
}

// nadjiPostaje vraća sve zapisivače koji nose traženi naziv, onaj koji se
// javlja prvi. Jedna letva ondje je najčešće dva zapisa, po jedan za svaki
// uređaj.
func nadjiPostaje(ctx context.Context, k *hidroview.Klijent, trazi string) ([]hidroview.Postaja, error) {
	sve, err := k.Postaje(ctx)
	if err != nil {
		return nil, err
	}
	var nadjene []hidroview.Postaja
	for _, p := range sve {
		if strings.Contains(strings.ToUpper(p.Site.Naziv), strings.ToUpper(trazi)) {
			nadjene = append(nadjene, p)
		}
	}
	if len(nadjene) == 0 {
		return nil, fmt.Errorf("postaja „%s“ nije nađena među %d postaja", trazi, len(sve))
	}
	sort.SliceStable(nadjene, func(a, b int) bool { return nadjene[a].Zadnje > nadjene[b].Zadnje })
	return nadjene, nil
}

func nadjiMjerenje(mjerenja []hidroview.Mjerenje, sifra string) *hidroview.Mjerenje {
	// Kad istu veličinu daje više izvora, prednost ima naknadna obrada:
	// ondje je vrijednost svedena na nulu letve.
	var prvi *hidroview.Mjerenje
	for i := range mjerenja {
		if mjerenja[i].Velicina != sifra {
			continue
		}
		if strings.HasPrefix(mjerenja[i].Odakle, "obrada:") {
			return &mjerenja[i]
		}
		if prvi == nil {
			prvi = &mjerenja[i]
		}
	}
	return prvi
}

// dohvati vadi razdoblje u komadima, jer dugačak raspon poslužitelj ne mora
// dati odjednom.
func dohvati(ctx context.Context, k *hidroview.Klijent, mjerenjeID string,
	od, do time.Time, danaPoKomadu int, uCm bool, korak time.Duration) ([]arhiva.Redak, error) {
	if danaPoKomadu < 1 {
		danaPoKomadu = 30
	}
	var out []arhiva.Redak
	for poc := od; poc.Before(do); poc = poc.AddDate(0, 0, danaPoKomadu) {
		kraj := poc.AddDate(0, 0, danaPoKomadu)
		if kraj.After(do) {
			kraj = do
		}
		v, err := k.Vrijednosti(ctx, mjerenjeID, poc, kraj)
		if err != nil {
			return nil, err
		}
		for _, x := range v {
			kad := x.Kad
			if korak > 0 {
				kad = kad.Round(korak)
			}
			vrijednost := x.Vrijednost
			if uCm {
				vrijednost *= 100
			}
			out = append(out, arhiva.Redak{Vrijeme: kad, Vrijednost: vrijednost})
		}
	}
	return out, nil
}
