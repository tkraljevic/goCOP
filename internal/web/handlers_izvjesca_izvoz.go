package web

import (
	"fmt"
	"strconv"
	"strings"

	"gocop/internal/models"
	"gocop/internal/xlsxw"
)

// KnjigaIzvjesca slaže dnevno izvješće rukovoditelja dionice kao list u
// obliku propisanog obrasca (Privitak 4): zaglavlje s danom, dionicom i
// stadijem, pa odjeljci redom kako stoje na papiru. Uspravno na A4, s
// logotipom i potpisom onoga tko je izvješće izradio.
func KnjigaIzvjesca(iz *models.DnevnoIzvjesce, sec *models.Section, z ZaglavljeIzvoza) *xlsxw.Knjiga {
	k := &xlsxw.Knjiga{LogoPNG: z.LogoPNG}
	B := xlsxw.T
	const stupaca = 10 // A..J
	s := iz.Sadrzaj

	l := k.NoviList("Dnevno izvješće")
	l.Uspravno = true
	l.Sirine = []float64{13, 9, 9, 9, 9, 9, 9, 9, 9, 11}
	zaglavljeLista(l, z, "DNEVNO IZVJEŠĆE RUKOVODITELJA DIONICE", "obrazac iz Privitka 4 Državnog plana obrane od poplava · za dan "+iz.Dan.Format("02.01.2006."), stupaca)

	// red s obrubom preko svih stupaca: ćelije koje nisu zadane su prazne s obrubom
	red := func(celije map[int]xlsxw.Celija, spojevi [][2]int) int {
		r := l.Redak()
		out := make([]xlsxw.Celija, stupaca)
		for c := range out {
			out[c] = B("", xlsxw.Tablica)
		}
		for c, cel := range celije {
			out[c] = cel
		}
		l.Dodaj(out...)
		for _, sp := range spojevi {
			l.Spoji(sp[0], r, sp[1], r)
		}
		return r
	}
	naslov := func(tekst string) {
		l.Dodaj()
		l.Visina(l.Redak()-1, 6)
		red(map[int]xlsxw.Celija{0: B(tekst, xlsxw.Zaglavlje)}, [][2]int{{0, stupaca - 1}})
	}
	// oznaka lijevo (dva stupca), tekst preko ostatka; visina prema duljini teksta
	polje := func(oznaka, tekst string, najmanje float64) {
		r := red(map[int]xlsxw.Celija{0: B(oznaka, xlsxw.TablicaTekst), 2: B(tekst, xlsxw.TablicaTekst)}, [][2]int{{0, 1}, {2, stupaca - 1}})
		l.Visina(r, visinaTeksta(tekst, 95, najmanje, visinaTeksta(oznaka, 22, 0, 0)))
	}
	kvadratic := func(oznaka string, da bool) string {
		if da {
			return "☒ " + oznaka
		}
		return "☐ " + oznaka
	}

	// Zaglavlje obrasca
	stadiji := make([]string, 0, 4)
	for _, p := range models.StadijiObrane {
		stadiji = append(stadiji, kvadratic(models.StadijKratica(p), p == iz.Stadij))
	}
	r := red(map[int]xlsxw.Celija{
		0: B("ZA DAN", xlsxw.TablicaPod), 1: B(iz.Dan.Format("02.01.2006."), xlsxw.Tablica),
		3: B("DIONICA", xlsxw.TablicaPod), 4: B(sec.Code, xlsxw.Tablica),
		6: B("STADIJ", xlsxw.TablicaPod), 7: B(strings.Join(stadiji, "   "), xlsxw.TablicaSredina),
	}, [][2]int{{1, 2}, {4, 5}, {7, stupaca - 1}})
	l.Visina(r, 20)
	opis := sec.EffectiveDescription()
	if iz.Podrucje != "" {
		opis += " · " + iz.Podrucje
	}
	r = red(map[int]xlsxw.Celija{0: B("Opis dionice", xlsxw.TablicaPod), 1: B(opis, xlsxw.TablicaTekst)}, [][2]int{{1, stupaca - 1}})
	l.Visina(r, visinaTeksta(opis, 100, 18, 0))
	r = red(map[int]xlsxw.Celija{0: B("VODOTOK", xlsxw.TablicaPod), 1: B(s.Vodotok, xlsxw.Tablica)}, [][2]int{{1, stupaca - 1}})
	l.Visina(r, 20)

	// Vodostaji
	naslov("VODOSTAJI (PROTOKE) — u 07:00 na mjerodavnom vodomjeru, i drugi važni tijekom dana")
	red(map[int]xlsxw.Celija{0: B("vodomjer", xlsxw.Zaglavlje), 5: B("sat", xlsxw.Zaglavlje), 6: B("vrijednost", xlsxw.Zaglavlje), 8: B("jedinica", xlsxw.Zaglavlje)},
		[][2]int{{0, 4}, {6, 7}, {8, 9}})
	redova := len(s.Vodostaji)
	if redova < 3 {
		redova = 3
	}
	for i := 0; i < redova; i++ {
		celije := map[int]xlsxw.Celija{}
		if i < len(s.Vodostaji) {
			v := s.Vodostaji[i]
			celije[0] = B(v.Postaja, xlsxw.Tablica)
			celije[5] = B(v.Sat, xlsxw.TablicaSredina)
			if n, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimPrefix(v.Vrijednost, "+"), ",", "."), 64); err == nil && !strings.ContainsAny(v.Vrijednost, " /") {
				celije[6] = xlsxw.N(n, xlsxw.TablicaBroj)
			} else {
				celije[6] = B(v.Vrijednost, xlsxw.TablicaSredina)
			}
			celije[8] = B(v.Jedinica, xlsxw.TablicaSredina)
		}
		r = red(celije, [][2]int{{0, 4}, {6, 7}, {8, 9}})
		l.Visina(r, 18)
	}
	tend := make([]string, 0, 4)
	for _, t := range models.Tendencije {
		tend = append(tend, kvadratic(t.Naziv, t.Kod == s.Tendencija))
	}
	r = red(map[int]xlsxw.Celija{0: B("Stanje – tendencija vodostaja (protoke)", xlsxw.TablicaPod), 4: B(strings.Join(tend, "      "), xlsxw.TablicaSredina)}, [][2]int{{0, 3}, {4, stupaca - 1}})
	l.Visina(r, 20)

	// Mjere
	naslov("MJERE OBRANE OD POPLAVA")
	polje("Pregled i ocjena stanja ispravnosti regulacijskih i zaštitnih vodnih građevina i građevina za osnovnu melioracijsku odvodnju te protočnosti korita (naplavine, čepovi); vrijeme uočavanja oštećenja, stanje i lokacija kritičnih mjesta",
		s.Pregled, 70)
	polje("Provedene mjere i radnje (saniranje oštećenja, nadvišenja i privremeni nasipi, stabilizacija obale, uklanjanje naplavina i čepova, crpljenje mobilnim crpkama i drugi radovi)",
		s.Radnje, 70)
	red(map[int]xlsxw.Celija{0: B("vreće", xlsxw.Zaglavlje), 2: B("materijal", xlsxw.Zaglavlje), 5: B("nasipi (m)", xlsxw.Zaglavlje), 8: B("crpke", xlsxw.Zaglavlje)},
		[][2]int{{0, 1}, {2, 4}, {5, 7}, {8, 9}})
	r = red(map[int]xlsxw.Celija{0: B(s.Vrece, xlsxw.TablicaSredina), 2: B(s.Materijal, xlsxw.TablicaSredina), 5: B(s.Nasipi, xlsxw.TablicaSredina), 8: B(s.Crpke, xlsxw.TablicaSredina)},
		[][2]int{{0, 1}, {2, 4}, {5, 7}, {8, 9}})
	l.Visina(r, 20)
	polje("Vrijeme aktiviranja objekata za rasterećenje velikih voda s tehničkim podacima (oteretni kanali, retencije, akumulacije, ustave, preljevi, CS…)",
		s.Objekti, 40)

	// Sudjelovanje
	naslov("SUDJELOVANJE LJUDI I MATERIJALNIH SREDSTAVA U OBRANI OD POPLAVA")
	broj := func(n int) xlsxw.Celija {
		if n == 0 {
			return B("", xlsxw.TablicaSredina)
		}
		return xlsxw.N(float64(n), xlsxw.TablicaSredina)
	}
	red(map[int]xlsxw.Celija{0: B("Pravne osobe za provedbu preventivne, redovne i izvanredne obrane od poplava", xlsxw.TablicaPod)}, [][2]int{{0, stupaca - 1}})
	r = red(map[int]xlsxw.Celija{0: B("ljudi", xlsxw.Zaglavlje), 1: B("kamioni", xlsxw.Zaglavlje), 2: B("bageri", xlsxw.Zaglavlje), 3: B("komb. strojevi", xlsxw.Zaglavlje),
		4: B("utovarivači", xlsxw.Zaglavlje), 5: B("buldožeri", xlsxw.Zaglavlje), 6: B("traktori", xlsxw.Zaglavlje), 7: B("brodovi", xlsxw.Zaglavlje), 8: B("čamci", xlsxw.Zaglavlje), 9: B("ostalo", xlsxw.Zaglavlje)}, nil)
	l.Visina(r, 28)
	p := s.Pravne
	r = red(map[int]xlsxw.Celija{0: broj(p.Ljudi), 1: broj(p.Kamioni), 2: broj(p.Bageri), 3: broj(p.KombStrojevi), 4: broj(p.Utovarivaci), 5: broj(p.Buldozeri),
		6: broj(p.Traktori), 7: broj(p.Brodovi), 8: broj(p.Camci), 9: B(p.Ostalo, xlsxw.TablicaTekst)}, nil)
	l.Visina(r, visinaTeksta(p.Ostalo, 12, 20, 0))
	red(map[int]xlsxw.Celija{0: B("Ostali sudionici obrane od poplava", xlsxw.TablicaPod)}, [][2]int{{0, stupaca - 1}})
	r = red(map[int]xlsxw.Celija{0: B("policija", xlsxw.Zaglavlje), 1: B("vatrogasci", xlsxw.Zaglavlje), 2: B("Hrvatska vojska", xlsxw.Zaglavlje), 3: B("HGSS", xlsxw.Zaglavlje),
		4: B("Civilna zaštita", xlsxw.Zaglavlje), 5: B("Crveni križ", xlsxw.Zaglavlje), 6: B("druge pravne osobe i građani", xlsxw.Zaglavlje)}, [][2]int{{6, 9}})
	l.Visina(r, 28)
	o := s.Ostali
	r = red(map[int]xlsxw.Celija{0: broj(o.Policija), 1: broj(o.Vatrogasci), 2: broj(o.Vojska), 3: broj(o.HGSS), 4: broj(o.CivilnaZastita), 5: broj(o.CrveniKriz),
		6: B(o.Drugi, xlsxw.TablicaTekst)}, [][2]int{{6, 9}})
	l.Visina(r, visinaTeksta(o.Drugi, 40, 20, 0))

	// Stanje na poplavljenom području
	naslov("STANJE NA POPLAVLJENOM PODRUČJU")
	red(map[int]xlsxw.Celija{0: B("Poplavljena područja i objekti", xlsxw.TablicaPod)}, [][2]int{{0, stupaca - 1}})
	r = red(map[int]xlsxw.Celija{0: B("naselja", xlsxw.Zaglavlje), 2: B("ljudi", xlsxw.Zaglavlje), 3: B("stambeni objekti", xlsxw.Zaglavlje), 4: B("industrijski objekti", xlsxw.Zaglavlje),
		5: B("farme", xlsxw.Zaglavlje), 6: B("infrastrukturni objekti (ceste, mostovi…)", xlsxw.Zaglavlje), 7: B("šumske površine (ha)", xlsxw.Zaglavlje),
		8: B("poljoprivredne površine (ha)", xlsxw.Zaglavlje), 9: B("ostale površine (ha)", xlsxw.Zaglavlje)}, [][2]int{{0, 1}})
	l.Visina(r, 40)
	pp := s.Poplavljeno
	ha := func(v float64) xlsxw.Celija {
		if v == 0 {
			return B("", xlsxw.TablicaSredina)
		}
		return xlsxw.N(v, xlsxw.TablicaBroj)
	}
	r = red(map[int]xlsxw.Celija{0: B(pp.Naselja, xlsxw.TablicaTekst), 2: broj(pp.Ljudi), 3: broj(pp.Stambeni), 4: broj(pp.Industrijski), 5: broj(pp.Farme),
		6: B(pp.Infrastruktura, xlsxw.TablicaTekst), 7: ha(pp.SumskeHa), 8: ha(pp.PoljoprivredneHa), 9: ha(pp.OstaleHa)}, [][2]int{{0, 1}})
	l.Visina(r, visinaTeksta(pp.Naselja+"\n"+pp.Infrastruktura, 18, 20, 0))
	red(map[int]xlsxw.Celija{0: B("Evakuacija", xlsxw.TablicaPod)}, [][2]int{{0, stupaca - 1}})
	r = red(map[int]xlsxw.Celija{0: B("naselja", xlsxw.Zaglavlje), 4: B("broj evakuiranih ljudi", xlsxw.Zaglavlje), 6: B("broj kućanstava", xlsxw.Zaglavlje), 8: B("broj i vrsta evakuiranih životinja", xlsxw.Zaglavlje)},
		[][2]int{{0, 3}, {4, 5}, {6, 7}, {8, 9}})
	l.Visina(r, 28)
	e := s.Evakuacija
	r = red(map[int]xlsxw.Celija{0: B(e.Naselja, xlsxw.TablicaTekst), 4: broj(e.Ljudi), 6: broj(e.Kucanstava), 8: B(e.Zivotinje, xlsxw.TablicaTekst)},
		[][2]int{{0, 3}, {4, 5}, {6, 7}, {8, 9}})
	l.Visina(r, visinaTeksta(e.Naselja, 40, 20, visinaTeksta(e.Zivotinje, 20, 0, 0)))

	// Izrada i potpis
	l.Dodaj()
	izrada := "Datum i sat izrade: " + iz.IzradenoAt.In(models.Zagreb).Format("02.01.2006. 15:04")
	if iz.Predano() {
		izrada += " · predano u podcentar " + iz.PredanoAt.In(models.Zagreb).Format("02.01.2006. 15:04")
	}
	r = l.Redak()
	out := make([]xlsxw.Celija, stupaca)
	out[0] = B(izrada, xlsxw.Obican)
	l.Dodaj(out...)
	l.Spoji(0, r, stupaca-1, r)
	zp := z
	zp.Datum = iz.IzradenoAt.In(models.Zagreb)
	potpisiLista(l, zp, stupaca, []PotpisnikIzvoza{{Funkcija: "rukovoditelj / zamjenik rukovoditelja dionice " + sec.Code, Ime: iz.Izradio}})
	napomenaLista(l, fmt.Sprintf("Izvješće se predaje do 08:00 u podcentar obrane od poplava. Zapisano u programu goCOP; %s.", z.Centar), stupaca, 14)
	return k
}

// visinaTeksta procjenjuje visinu retka u točkama za tekst koji se prelama
// na zadanu širinu u znakovima; ne ide ispod najmanje ni ispod druge
// procjene (za ćeliju uz nju)
func visinaTeksta(tekst string, sirina int, najmanje, drugo float64) float64 {
	redaka := 0
	for _, crta := range strings.Split(tekst, "\n") {
		n := len([]rune(crta))/sirina + 1
		redaka += n
	}
	h := float64(redaka)*13.5 + 5
	if h < najmanje {
		h = najmanje
	}
	if h < drugo {
		h = drugo
	}
	return h
}
