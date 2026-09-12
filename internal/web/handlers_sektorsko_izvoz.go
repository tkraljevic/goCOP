package web

import (
	"fmt"
	"strings"

	"gocop/internal/models"
	"gocop/internal/xlsxw"
)

// KnjigaSektorskog slaže dnevno izvješće rukovoditelja sektora kao list na
// A4 vodoravno: zaglavlje, hidrometeorološki uvjeti, vodotoci i dionice po
// branjenim područjima, tekst stanja i mjera, zbrojevi ljudi i sredstava,
// stanje na poplavljenom području, zapisi iz dnevnika, potpisi.
func KnjigaSektorskog(iz *models.SektorskoIzvjesce, z ZaglavljeIzvoza) *xlsxw.Knjiga {
	k := &xlsxw.Knjiga{LogoPNG: z.LogoPNG}
	B := xlsxw.T
	const stupaca = 12 // A..L
	s := iz.Sadrzaj
	p := s.Pregled

	l := k.NoviList("Izvješće sektora")
	l.Vodoravno = true
	l.Sirine = []float64{16, 12, 9, 12, 9, 9, 9, 9, 9, 9, 9, 18}
	podnaslov := fmt.Sprintf("za dan %s · %s · najviši stadij obrane: %s (%s)", iz.Dan.Format("02.01.2006."), z.Centar, models.StadijKratica(iz.NajvisiStadij()), iz.NajvisiStadij().Label())
	zaglavljeLista(l, z, "DNEVNO IZVJEŠĆE RUKOVODITELJA SEKTORA O PROVEDENIM MJERAMA OBRANE OD POPLAVA", podnaslov, stupaca)

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
	tekst := func(oznaka, t string, najmanje float64) {
		r := red(map[int]xlsxw.Celija{0: B(oznaka, xlsxw.TablicaTekst), 2: B(t, xlsxw.TablicaTekst)}, [][2]int{{0, 1}, {2, stupaca - 1}})
		l.Visina(r, visinaTeksta(t, 130, najmanje, visinaTeksta(oznaka, 28, 0, 0)))
	}
	broj := func(n int) xlsxw.Celija {
		if n == 0 {
			return B("", xlsxw.TablicaSredina)
		}
		return xlsxw.N(float64(n), xlsxw.TablicaSredina)
	}
	brojPod := func(n int) xlsxw.Celija {
		if n == 0 {
			return B("", xlsxw.TablicaPod)
		}
		return xlsxw.N(float64(n), xlsxw.TablicaBrojPod)
	}
	ha := func(v float64, pod bool) xlsxw.Celija {
		stil := xlsxw.TablicaBroj
		if pod {
			stil = xlsxw.TablicaBrojPod
		}
		if v == 0 {
			if pod {
				return B("", xlsxw.TablicaPod)
			}
			return B("", xlsxw.TablicaSredina)
		}
		return xlsxw.N(v, stil)
	}
	nazivPodrucja := func(pod models.PodrucjeUPregledu) string {
		if pod.Naziv != "" {
			return pod.Naziv
		}
		return fmt.Sprintf("BP %d", pod.AreaID)
	}

	// Osnovno
	r := red(map[int]xlsxw.Celija{0: B("ZA DAN", xlsxw.TablicaPod), 1: B(iz.Dan.Format("02.01.2006."), xlsxw.Tablica),
		3: B("SEKTOR", xlsxw.TablicaPod), 4: B(iz.Sektor+" · "+z.Centar, xlsxw.Tablica),
		8: B("IZVJEŠĆA DIONICA", xlsxw.TablicaPod), 10: B(fmt.Sprintf("%d uključeno, %d nepredano", p.Izvjesca, p.Nacrta), xlsxw.Tablica)}, [][2]int{{1, 2}, {4, 7}, {8, 9}, {10, 11}})
	l.Visina(r, 20)
	tekst("Hidrometeorološki uvjeti na sektoru po branjenim područjima", s.Hidrometeo, 40)

	// Vodotoci
	naslov("VODOTOCI I DIONICE NA KOJIMA SE PROVODE MJERE OBRANE OD POPLAVA")
	red(map[int]xlsxw.Celija{0: B("vodotok", xlsxw.Zaglavlje), 1: B("najviši stadij", xlsxw.Zaglavlje), 3: B("tendencija", xlsxw.Zaglavlje), 4: B("dionice", xlsxw.Zaglavlje), 7: B("vodostaji (protoke)", xlsxw.Zaglavlje)},
		[][2]int{{1, 2}, {4, 6}, {7, 11}})
	for _, v := range p.Vodotoci {
		r = red(map[int]xlsxw.Celija{0: B(v.Vodotok, xlsxw.TablicaPod), 1: B(models.StadijKratica(v.Stadij)+" — "+v.Stadij.Label(), xlsxw.Tablica), 3: B(models.TendencijaNaziv(v.Tendencija), xlsxw.TablicaSredina),
			4: B(strings.Join(v.Dionice, ", "), xlsxw.TablicaTekst), 7: B(v.Vodostaji, xlsxw.TablicaTekst)}, [][2]int{{1, 2}, {4, 6}, {7, 11}})
		l.Visina(r, visinaTeksta(v.Vodostaji, 60, 18, visinaTeksta(strings.Join(v.Dionice, ", "), 30, 0, 0)))
	}
	for _, pod := range p.Podrucja {
		red(map[int]xlsxw.Celija{0: B("Branjeno područje: "+nazivPodrucja(pod), xlsxw.TablicaPod)}, [][2]int{{0, stupaca - 1}})
		r = red(map[int]xlsxw.Celija{0: B("dionica", xlsxw.Zaglavlje), 1: B("vodotok", xlsxw.Zaglavlje), 2: B("stadij", xlsxw.Zaglavlje), 3: B("vodostaji", xlsxw.Zaglavlje), 5: B("tendencija", xlsxw.Zaglavlje),
			6: B("vreće", xlsxw.Zaglavlje), 7: B("materijal", xlsxw.Zaglavlje), 8: B("nasipi", xlsxw.Zaglavlje), 9: B("crpke", xlsxw.Zaglavlje), 10: B("izvješće izradio", xlsxw.Zaglavlje)}, [][2]int{{3, 4}, {10, 11}})
		l.Visina(r, 18)
		for _, d := range pod.Dionice {
			r = red(map[int]xlsxw.Celija{0: B(d.Code, xlsxw.Tablica), 1: B(d.Vodotok, xlsxw.Tablica), 2: B(models.StadijKratica(d.Stadij), xlsxw.TablicaSredina), 3: B(d.Vodostaji, xlsxw.TablicaTekst),
				5: B(models.TendencijaNaziv(d.Tendencija), xlsxw.TablicaSredina), 6: B(d.Vrece, xlsxw.TablicaTekst), 7: B(d.Materijal, xlsxw.TablicaTekst), 8: B(d.Nasipi, xlsxw.TablicaTekst), 9: B(d.Crpke, xlsxw.TablicaTekst),
				10: B(d.Izradio, xlsxw.TablicaTekst)}, [][2]int{{3, 4}, {10, 11}})
			l.Visina(r, visinaTeksta(d.Vodostaji, 24, 18, visinaTeksta(d.Materijal, 10, 0, visinaTeksta(d.Nasipi, 10, 0, 0))))
		}
	}

	// Stanje i mjere
	naslov("STANJE VODNIH GRAĐEVINA I PROVEDENE MJERE")
	tekst("Vrijeme uočavanja oštećenja vodnih građevina, stanje i lokacije kritičnih mjesta", s.Ostecenja, 40)
	tekst("Zbirni opis provedenih mjera i radnji po branjenim područjima (vreće, materijal, nasipi, crpke)", s.Mjere, 40)
	tekst("Aktiviranje objekata za rasterećenje velikih voda: vrijeme početka i prestanka, tehnički podaci", s.Objekti, 30)

	// Sudjelovanje
	naslov("SUDJELOVANJE LJUDI I MATERIJALNIH SREDSTAVA — PRAVNE OSOBE ZA PROVEDBU OBRANE OD POPLAVA")
	r = red(map[int]xlsxw.Celija{0: B("branjeno područje", xlsxw.Zaglavlje), 1: B("ljudi", xlsxw.Zaglavlje), 2: B("kamioni", xlsxw.Zaglavlje), 3: B("bageri", xlsxw.Zaglavlje), 4: B("komb. strojevi", xlsxw.Zaglavlje),
		5: B("utovarivači", xlsxw.Zaglavlje), 6: B("buldožeri", xlsxw.Zaglavlje), 7: B("traktori", xlsxw.Zaglavlje), 8: B("brodovi", xlsxw.Zaglavlje), 9: B("čamci", xlsxw.Zaglavlje), 10: B("ostalo", xlsxw.Zaglavlje)}, [][2]int{{10, 11}})
	l.Visina(r, 28)
	for _, pod := range p.Podrucja {
		pr := pod.Zbroj.Pravne
		red(map[int]xlsxw.Celija{0: B(nazivPodrucja(pod), xlsxw.Tablica), 1: broj(pr.Ljudi), 2: broj(pr.Kamioni), 3: broj(pr.Bageri), 4: broj(pr.KombStrojevi), 5: broj(pr.Utovarivaci),
			6: broj(pr.Buldozeri), 7: broj(pr.Traktori), 8: broj(pr.Brodovi), 9: broj(pr.Camci), 10: B(pr.Ostalo, xlsxw.TablicaTekst)}, [][2]int{{10, 11}})
	}
	pr := p.Ukupno.Pravne
	red(map[int]xlsxw.Celija{0: B("UKUPNO SEKTOR", xlsxw.TablicaPod), 1: brojPod(pr.Ljudi), 2: brojPod(pr.Kamioni), 3: brojPod(pr.Bageri), 4: brojPod(pr.KombStrojevi), 5: brojPod(pr.Utovarivaci),
		6: brojPod(pr.Buldozeri), 7: brojPod(pr.Traktori), 8: brojPod(pr.Brodovi), 9: brojPod(pr.Camci), 10: B(pr.Ostalo, xlsxw.TablicaPod)}, [][2]int{{10, 11}})
	naslov("OSTALI SUDIONICI OBRANE OD POPLAVA")
	r = red(map[int]xlsxw.Celija{0: B("branjeno područje", xlsxw.Zaglavlje), 1: B("policija", xlsxw.Zaglavlje), 2: B("vatrogasci", xlsxw.Zaglavlje), 3: B("Hrvatska vojska", xlsxw.Zaglavlje), 4: B("HGSS", xlsxw.Zaglavlje),
		5: B("Civilna zaštita", xlsxw.Zaglavlje), 6: B("Crveni križ", xlsxw.Zaglavlje), 7: B("druge pravne osobe i građani", xlsxw.Zaglavlje)}, [][2]int{{7, 11}})
	l.Visina(r, 28)
	for _, pod := range p.Podrucja {
		o := pod.Zbroj.Ostali
		red(map[int]xlsxw.Celija{0: B(nazivPodrucja(pod), xlsxw.Tablica), 1: broj(o.Policija), 2: broj(o.Vatrogasci), 3: broj(o.Vojska), 4: broj(o.HGSS), 5: broj(o.CivilnaZastita), 6: broj(o.CrveniKriz),
			7: B(o.Drugi, xlsxw.TablicaTekst)}, [][2]int{{7, 11}})
	}
	o := p.Ukupno.Ostali
	red(map[int]xlsxw.Celija{0: B("UKUPNO SEKTOR", xlsxw.TablicaPod), 1: brojPod(o.Policija), 2: brojPod(o.Vatrogasci), 3: brojPod(o.Vojska), 4: brojPod(o.HGSS), 5: brojPod(o.CivilnaZastita), 6: brojPod(o.CrveniKriz),
		7: B(o.Drugi, xlsxw.TablicaPod)}, [][2]int{{7, 11}})

	// Poplavljeno
	naslov("STANJE NA POPLAVLJENOM PODRUČJU")
	r = red(map[int]xlsxw.Celija{0: B("branjeno područje", xlsxw.Zaglavlje), 1: B("naselja", xlsxw.Zaglavlje), 3: B("ljudi", xlsxw.Zaglavlje), 4: B("stambeni objekti", xlsxw.Zaglavlje), 5: B("industrijski objekti", xlsxw.Zaglavlje),
		6: B("farme", xlsxw.Zaglavlje), 7: B("infrastrukturni objekti", xlsxw.Zaglavlje), 9: B("šumske (ha)", xlsxw.Zaglavlje), 10: B("poljoprivredne (ha)", xlsxw.Zaglavlje), 11: B("ostale (ha)", xlsxw.Zaglavlje)}, [][2]int{{1, 2}, {7, 8}})
	l.Visina(r, 28)
	for _, pod := range p.Podrucja {
		pp := pod.Zbroj.Poplavljeno
		r = red(map[int]xlsxw.Celija{0: B(nazivPodrucja(pod), xlsxw.Tablica), 1: B(pp.Naselja, xlsxw.TablicaTekst), 3: broj(pp.Ljudi), 4: broj(pp.Stambeni), 5: broj(pp.Industrijski), 6: broj(pp.Farme),
			7: B(pp.Infrastruktura, xlsxw.TablicaTekst), 9: ha(pp.SumskeHa, false), 10: ha(pp.PoljoprivredneHa, false), 11: ha(pp.OstaleHa, false)}, [][2]int{{1, 2}, {7, 8}})
		l.Visina(r, visinaTeksta(pp.Naselja, 24, 18, visinaTeksta(pp.Infrastruktura, 20, 0, 0)))
	}
	pp := p.Ukupno.Poplavljeno
	red(map[int]xlsxw.Celija{0: B("UKUPNO SEKTOR", xlsxw.TablicaPod), 1: B("", xlsxw.TablicaPod), 3: brojPod(pp.Ljudi), 4: brojPod(pp.Stambeni), 5: brojPod(pp.Industrijski), 6: brojPod(pp.Farme),
		7: B("", xlsxw.TablicaPod), 9: ha(pp.SumskeHa, true), 10: ha(pp.PoljoprivredneHa, true), 11: ha(pp.OstaleHa, true)}, [][2]int{{1, 2}, {7, 8}})
	r = red(map[int]xlsxw.Celija{0: B("evakuacija", xlsxw.Zaglavlje), 1: B("naselja", xlsxw.Zaglavlje), 5: B("evakuiranih ljudi", xlsxw.Zaglavlje), 6: B("kućanstava", xlsxw.Zaglavlje), 7: B("broj i vrsta evakuiranih životinja", xlsxw.Zaglavlje)}, [][2]int{{1, 4}, {7, 11}})
	l.Visina(r, 18)
	for _, pod := range p.Podrucja {
		e := pod.Zbroj.Evakuacija
		if e == (models.Evakuacija{}) {
			continue
		}
		red(map[int]xlsxw.Celija{0: B(nazivPodrucja(pod), xlsxw.Tablica), 1: B(e.Naselja, xlsxw.TablicaTekst), 5: broj(e.Ljudi), 6: broj(e.Kucanstava), 7: B(e.Zivotinje, xlsxw.TablicaTekst)}, [][2]int{{1, 4}, {7, 11}})
	}
	e := p.Ukupno.Evakuacija
	red(map[int]xlsxw.Celija{0: B("UKUPNO SEKTOR", xlsxw.TablicaPod), 1: B("", xlsxw.TablicaPod), 5: brojPod(e.Ljudi), 6: brojPod(e.Kucanstava), 7: B("", xlsxw.TablicaPod)}, [][2]int{{1, 4}, {7, 11}})
	tekst("Opis poplavljenih područja i objekata po branjenim područjima i županijama", s.Poplavljeno, 30)
	tekst("Provedene evakuacije po branjenim područjima i županijama", s.Evakuacija, 24)

	// Dnevnik
	if len(s.Zapisi) > 0 {
		naslov("IZ DNEVNIKA CENTRA OBRANE OD POPLAVA — KRONOLOGIJA DANA")
		r = red(map[int]xlsxw.Celija{0: B("vrijeme", xlsxw.Zaglavlje), 1: B("područje", xlsxw.Zaglavlje), 2: B("vrsta", xlsxw.Zaglavlje), 3: B("javio", xlsxw.Zaglavlje), 5: B("zapis", xlsxw.Zaglavlje)}, [][2]int{{3, 4}, {5, 11}})
		l.Visina(r, 18)
		for _, zp := range s.Zapisi {
			r = red(map[int]xlsxw.Celija{0: B(zp.Vrijeme, xlsxw.TablicaSredina), 1: B(zp.Podrucje, xlsxw.Tablica), 2: B(zp.Vrsta, xlsxw.Tablica), 3: B(zp.Javio, xlsxw.TablicaTekst), 5: B(zp.Tekst, xlsxw.TablicaTekst)}, [][2]int{{3, 4}, {5, 11}})
			l.Visina(r, visinaTeksta(zp.Tekst, 90, 18, visinaTeksta(zp.Javio, 20, 0, 0)))
		}
	}
	if strings.TrimSpace(s.Napomena) != "" {
		naslov("OCJENA STANJA I NAPOMENA")
		tekst("Ocjena stanja, najava, što Glavni centar treba znati", s.Napomena, 30)
	}

	// Izrada i potpisi
	l.Dodaj()
	izrada := "Datum i sat izrade: " + iz.IzradenoAt.In(models.Zagreb).Format("02.01.2006. 15:04") + " · sastavio: " + iz.Izradio
	if iz.Predano() {
		izrada += " · predano Glavnom centru " + iz.PredanoAt.In(models.Zagreb).Format("02.01.2006. 15:04")
	}
	r = l.Redak()
	out := make([]xlsxw.Celija, stupaca)
	out[0] = B(izrada, xlsxw.Obican)
	l.Dodaj(out...)
	l.Spoji(0, r, stupaca-1, r)
	zp := z
	zp.Datum = iz.IzradenoAt.In(models.Zagreb)
	potpisnici := []PotpisnikIzvoza{{Funkcija: "sastavio, voditelj Centra obrane od poplava " + models.Terms().Lower("sektor") + "a " + iz.Sektor, Ime: iz.Izradio}}
	for _, ps := range z.Potpisnici {
		if strings.HasPrefix(ps.Funkcija, "rukovoditelj") {
			potpisnici = append(potpisnici, PotpisnikIzvoza{Funkcija: ps.Funkcija + " / zamjenik", Ime: ps.Ime})
		}
	}
	if len(potpisnici) == 1 {
		potpisnici = append(potpisnici, PotpisnikIzvoza{Funkcija: "rukovoditelj / zamjenik rukovoditelja obrane od poplava " + models.Terms().Lower("sektor") + "a " + iz.Sektor})
	}
	potpisiLista(l, zp, stupaca, potpisnici)
	napomenaLista(l, "Izvješće se dostavlja Glavnom centru obrane od poplava do 10:00. Sastavljeno u programu goCOP iz predanih izvješća rukovoditelja dionica i dnevnika centra; tablice su snimljene u trenutku izrade.", stupaca, 24)
	return k
}
