// proba-hidroview gleda što Geolux HydroView (hdv.voda.hr) nudi: koje
// postaje račun vidi, koje veličine mjere i kakvi su pragovi upisani uz njih.
// Ne mijenja ništa ni ondje ni kod nas — služi da se prije uvoza vidi stanje.
//
// Vjerodajnice se ne upisuju u naredbeni redak. Bez ičega u okolini uzima se
// račun čvora upisan u aplikaciju (upis-hidroview-racuna) — isti koji
// poslužitelj koristi za preuzimanje, zaključan ključem čvora pa vrijedi
// samo na ovom računalu. Okolina ima prednost, za probu tuđim računom:
//
//	read "?Korisnik: " HDV_KORISNIK
//	read -s "?Lozinka: " HDV_LOZINKA
//	export HDV_KORISNIK HDV_LOZINKA
//	go run ./cmd/proba-hidroview -postaja TIKVE
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gocop/internal/hidroview"
	"gocop/internal/hidroview/racun"
)

func main() {
	adresa := flag.String("adresa", hidroview.ZadanaAdresa, "adresa sustava")
	trazi := flag.String("postaja", "", "dio naziva postaje; prazno ispisuje sve")
	pregled := flag.Bool("pregled", false, "prođi sve postaje i prebroji koje veličine mjere")
	jsonIspis := flag.Bool("json", false, "ispiši svaki zapisivač kao redak JSON-a, sa stanjem srednjeg vodostaja u zadnjih -dana dana")
	dana := flag.Int("dana", 2, "koliko dana podataka dohvatiti za prikaz")
	dbPath := flag.String("db", "data/gocop.db", "baza čvora, zbog računa upisanog u aplikaciju")
	flag.Parse()

	korisnik, lozinka := os.Getenv("HDV_KORISNIK"), os.Getenv("HDV_LOZINKA")
	if korisnik == "" || lozinka == "" {
		r, err := racun.Cvora(*dbPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "račun čvora:", err)
			fmt.Fprintln(os.Stderr, "Upiši ga naredbom upis-hidroview-racuna, ili postavi u okolini:")
			fmt.Fprintln(os.Stderr, `  read "?Korisnik: " HDV_KORISNIK`)
			fmt.Fprintln(os.Stderr, `  read -s "?Lozinka: " HDV_LOZINKA`)
			fmt.Fprintln(os.Stderr, "  export HDV_KORISNIK HDV_LOZINKA")
			os.Exit(2)
		}
		korisnik, lozinka = r.Korisnik, r.Lozinka
		if r.Adresa != "" && *adresa == hidroview.ZadanaAdresa {
			*adresa = r.Adresa
		}
		fmt.Fprintf(os.Stderr, "račun čvora: %s (%s)\n", korisnik, *adresa)
	}

	ctx, otkazi := context.WithTimeout(context.Background(), 20*time.Minute)
	defer otkazi()
	k := &hidroview.Klijent{Adresa: *adresa}
	if err := k.Prijava(ctx, korisnik, lozinka); err != nil {
		fmt.Fprintln(os.Stderr, "prijava:", err)
		os.Exit(1)
	}
	postaje, err := k.Postaje(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "postaja: %d\n", len(postaje))

	if *pregled {
		prebroji(ctx, k, postaje)
		return
	}
	if *jsonIspis {
		ispisiJSON(ctx, k, postaje, *dana)
		return
	}

	nadjene := postaje
	if *trazi != "" {
		nadjene = nil
		for _, p := range postaje {
			if strings.Contains(strings.ToUpper(p.Site.Naziv), strings.ToUpper(*trazi)) {
				nadjene = append(nadjene, p)
			}
		}
		if len(nadjene) == 0 {
			fmt.Printf("postaja s „%s“ u nazivu nije nađena\n", *trazi)
			return
		}
		for _, p := range nadjene {
			opisiPostaju(ctx, k, p, *dana)
		}
		return
	}
	for _, p := range nadjene {
		fmt.Printf("  %-26s %-14s %-18s %s\n", p.Site.Naziv, p.Site.SifraPostaje,
			p.Site.SifraProjekta, vrijemeIliNikad(p.Zadnje))
	}
}

func opisiPostaju(ctx context.Context, k *hidroview.Klijent, p hidroview.Postaja, dana int) {
	fmt.Printf("\n%s (%s) — grupa %s\n  site_id    %s\n  zapisivač  %s, sken %d min\n"+
		"  postaja    %s   projekt %s\n  koordinate %.6f, %.6f\n  zadnje     %s\n",
		p.Site.Naziv, p.Site.Opis, p.Grupa, p.SiteID, p.LoggerID, p.Site.Sken,
		p.Site.SifraPostaje, p.Site.SifraProjekta, p.Site.Sirina, p.Site.Duzina,
		vrijemeIliNikad(p.Zadnje))

	mjerenja, alarmi, err := k.Oprema(ctx, p.SiteID)
	if err != nil {
		fmt.Println("  oprema:", err)
		return
	}
	for _, m := range mjerenja {
		if naziviVelicina[m.Velicina] == "" {
			continue
		}
		fmt.Printf("  mjeri      %-16s %-24s %s\n", m.Velicina, naziviVelicina[m.Velicina], m.Odakle)
		if len(m.Postavke) > 0 {
			kljucevi := make([]string, 0, len(m.Postavke))
			for k := range m.Postavke {
				kljucevi = append(kljucevi, k)
			}
			sort.Strings(kljucevi)
			for _, k := range kljucevi {
				fmt.Printf("             postavka %s = %s\n", k, m.Postavke[k])
			}
		}
	}
	for _, a := range alarmi {
		fmt.Printf("  prag       %-22s %s %.2f\n", a.Opis, a.Odnos, a.Prag)
	}

	do := time.Now()
	od := do.AddDate(0, 0, -dana)
	for _, m := range mjerenja {
		if m.Velicina != hidroview.VelicinaSrednjiVodostaj && m.Velicina != hidroview.VelicinaTempVode &&
			m.Velicina != hidroview.VelicinaProtok {
			continue
		}
		v, err := k.Vrijednosti(ctx, m.ID, od, do)
		if err != nil {
			fmt.Printf("  %s: %v\n", m.Velicina, err)
			continue
		}
		if len(v) == 0 {
			fmt.Printf("  %-16s nema vrijednosti u zadnjih %d dana\n", naziviVelicina[m.Velicina], dana)
			continue
		}
		prvi, zadnji := v[0], v[len(v)-1]
		fmt.Printf("  %-16s %d vrijednosti, %s … %s, zadnja %.5f\n",
			naziviVelicina[m.Velicina], len(v),
			prvi.Kad.Local().Format("02.01.2006. 15:04"), zadnji.Kad.Local().Format("02.01.2006. 15:04"),
			zadnji.Vrijednost)
	}
}

// prebroji prolazi sve postaje i javlja koja veličina gdje postoji. Traži
// jedan poziv po postaji, pa zna potrajati.
func prebroji(ctx context.Context, k *hidroview.Klijent, postaje []hidroview.Postaja) {
	broj := map[string]int{}
	sPragovima := 0
	popisi := map[string][]string{}
	for i, p := range postaje {
		mjerenja, alarmi, err := k.Oprema(ctx, p.SiteID)
		if err != nil {
			fmt.Printf("  %-26s oprema: %v\n", p.Site.Naziv, err)
			continue
		}
		if len(alarmi) > 0 {
			sPragovima++
		}
		vidjeno := map[string]bool{}
		for _, m := range mjerenja {
			if vidjeno[m.Velicina] {
				continue
			}
			vidjeno[m.Velicina] = true
			broj[m.Velicina]++
			switch m.Velicina {
			case hidroview.VelicinaProtok, hidroview.VelicinaTempVode, hidroview.VelicinaOborina:
				popisi[m.Velicina] = append(popisi[m.Velicina], p.Site.Naziv)
			}
		}
		if (i+1)%25 == 0 {
			fmt.Printf("  … pregledano %d/%d\n", i+1, len(postaje))
		}
	}
	fmt.Println("\nveličine po postajama:")
	kljucevi := make([]string, 0, len(broj))
	for v := range broj {
		kljucevi = append(kljucevi, v)
	}
	sort.Slice(kljucevi, func(a, b int) bool { return broj[kljucevi[a]] > broj[kljucevi[b]] })
	for _, v := range kljucevi {
		fmt.Printf("  %-16s %3d  %s\n", v, broj[v], naziviVelicina[v])
	}
	fmt.Printf("\npostaja s upisanim pragovima obrane: %d\n", sPragovima)
	for _, v := range []string{hidroview.VelicinaProtok, hidroview.VelicinaTempVode, hidroview.VelicinaOborina} {
		ispisiPopis(naziviVelicina[v], popisi[v])
	}
}

func ispisiPopis(sto string, popis []string) {
	if len(popis) == 0 {
		fmt.Printf("%s: nijedna postaja\n", sto)
		return
	}
	sort.Strings(popis)
	fmt.Printf("%s (%d): %s\n", sto, len(popis), strings.Join(popis, ", "))
}

func vrijemeIliNikad(sek int64) string {
	if sek <= 0 {
		return "nikad"
	}
	return time.Unix(sek, 0).Format("2006-01-02 15:04:05")
}

var naziviVelicina = map[string]string{
	hidroview.VelicinaSrednjiVodostaj: "srednji vodostaj",
	hidroview.VelicinaVodostaj:        "vodostaj instrumenta",
	hidroview.VelicinaTempVode:        "temperatura vode",
	hidroview.VelicinaProtok:          "protok",
	hidroview.VelicinaOborina:         "oborina",
	hidroview.VelicinaBrzina:          "površinska brzina",
}

// ispisiJSON ispisuje svaki zapisivač kao jedan redak JSON-a: gdje stoji,
// kad se zadnji javio i koliko je srednjeg vodostaja dao u zadnjih dana
// dana. Služi da se letve iz plana spare sa zapisivačima strojno, a ne
// čitanjem ispisa — i da se među dva zapisivača iste postaje vidi koji radi.
func ispisiJSON(ctx context.Context, k *hidroview.Klijent, postaje []hidroview.Postaja, dana int) {
	do := time.Now()
	od := do.AddDate(0, 0, -dana)
	enc := json.NewEncoder(os.Stdout)
	for _, p := range postaje {
		red := map[string]any{
			"naziv": p.Site.Naziv, "opis": p.Site.Opis, "site_id": p.SiteID,
			"zapisivac": p.LoggerID, "sifra": p.Site.SifraPostaje, "projekt": p.Site.SifraProjekta,
			"sirina": p.Site.Sirina, "duzina": p.Site.Duzina, "zadnje": vrijemeIliNikad(p.Zadnje),
		}
		mjerenja, _, err := k.Oprema(ctx, p.SiteID)
		if err != nil {
			red["greska"] = err.Error()
			_ = enc.Encode(red)
			continue
		}
		// Isti izbor kao uvoznik: obrađeni vodostaj je sveden na nulu letve,
		// sirovi s instrumenta nije.
		var izabrano *hidroview.Mjerenje
		for i := range mjerenja {
			if mjerenja[i].Velicina != hidroview.VelicinaSrednjiVodostaj {
				continue
			}
			if izabrano == nil || (strings.HasPrefix(mjerenja[i].Odakle, "obrada:") && !strings.HasPrefix(izabrano.Odakle, "obrada:")) {
				izabrano = &mjerenja[i]
			}
		}
		for _, m := range mjerenja {
			if izabrano == nil || m.ID != izabrano.ID {
				continue
			}
			red["instrument"] = m.Odakle
			v, err := k.Vrijednosti(ctx, m.ID, od, do)
			if err != nil {
				red["greska"] = err.Error()
				break
			}
			red["vrijednosti"] = len(v)
			if len(v) > 0 {
				red["zadnja_m"] = v[len(v)-1].Vrijednost
			}
			break
		}
		_ = enc.Encode(red)
	}
}
