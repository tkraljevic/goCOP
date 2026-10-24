package web

import (
	"encoding/json"
	stdhtml "html"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
	"gocop/internal/xlsxw"
)

// izmisljenePostaje su registar izmišljenih letvi za tekstove upozorenja.
func izmisljenePostaje() map[string]models.Station {
	return map[string]models.Station{
		"gornja":  {Code: "gornja", Name: "Gornji Brod (Mađarska)"},
		"srednja": {Code: "srednja", Name: "Srednji Brod (Mađarska)", JavniURL: "https://www.primjer.hu/vodostaji?x=1"},
		"donja":   {Code: "donja", Name: "Donji Grad"},
		"treca":   {Code: "treca", Name: "Treće Selo"},
		"cetvrta": {Code: "cetvrta", Name: "Četvrti Most"},
		"preko":   {Code: "preko", Name: "Preko (Srbija)"},
	}
}

var daniUpozorenja = []string{"pet 9.10.", "sub 10.10.", "ned 11.10.", "pon 12.10.", "uto 13.10.", "sri 14.10."}

// letveBezDnevne su kartice Donjeg Grada i Preka u izdanju bez dnevne:
// od drugog dana satna s oznakom, peti i šesti dan prazni.
func letveBezDnevne() []LetvaPrognoze {
	dani := []CelijaDana{{}, {IzSatne: true}, {IzSatne: true}, {IzSatne: true}, {BezDnevne: true}, {BezDnevne: true}}
	return []LetvaPrognoze{{Kod: "donja", Dani: dani}, {Kod: "preko", Dani: dani}, {Kod: "cetvrta"}}
}

func satUTC(dan, sat int) int64 {
	return time.Date(2026, 10, dan, sat, 0, 0, 0, time.UTC).Unix() / 3600
}

// Letva bez mjerenja zbog koje dnevni model nije računao: ime s izvorom,
// od kada do kada, ciljevi bez dnevne, srpska letva s prenesenom, i koji
// dani u tablici stoje iz satne, a koji su prazni. Vremena su mjesna.
func TestTekstUpozorenjaKarike(t *testing.T) {
	izmisljeniCiljevi(t)
	p := pisacUpozorenja{postaje: izmisljenePostaje(), letve: letveBezDnevne(), dani: daniUpozorenja}
	u := prognoza.Upozorenje{Vrsta: prognoza.VrstaUlaz, Tezina: prognoza.TezinaUpozorenje, Letva: "srednja",
		Zadnje: satUTC(7, 9), Sljedece: satUTC(8, 7),
		Upitno: []prognoza.Upitno{{Letva: "donja", OdSata: -1, Dnevno: true}, {Letva: "preko", OdSata: -1, Dnevno: true},
			{Letva: "treca", OdSata: -1, Dnevno: true}}}
	otvorena, napomene := p.slozi([]prognoza.Upozorenje{u})
	if len(otvorena) != 1 || len(napomene) != 0 {
		t.Fatalf("otvorena %+v, napomene %+v", otvorena, napomene)
	}
	if got, want := otvorena[0].Sto, "Srednji Brod (primjer.hu): nema mjerenja od zadnjeg očitanja 7. 10. u 11 h do 8. 10. u 9 h, "+
		"pa dnevni model nije računao za Donji Grad i Treće Selo."; got != want {
		t.Errorf("što:\n %q\nželim\n %q", got, want)
	}
	if got, want := otvorena[0].Upitno, "Upitno: dnevna prognoza Donji Grad i Treće Selo, prenesena dnevna Preko — u tablici "+
		"sub 10.10., ned 11.10. i pon 12.10. stoji satna (oznaka s), uto 13.10. i sri 14.10. bez vrijednosti."; got != want {
		t.Errorf("upitno:\n %q\nželim\n %q", got, want)
	}

	// Rupa koja još traje: karika je izračunata i satna niz lanac upitna.
	u.Sljedece = 0
	u.Upitno = append([]prognoza.Upitno{{Letva: "srednja", OdSata: 0}, {Letva: "cetvrta", OdSata: 7}}, u.Upitno...)
	u.Upitno[2].OdSata = 1 // donja
	otvorena, _ = p.slozi([]prognoza.Upozorenje{u})
	if got, want := otvorena[0].Sto, "Srednji Brod (primjer.hu): nema mjerenja od 7. 10. u 11 h, pa dnevni model nije računao za "+
		"Donji Grad i Treće Selo; vrijednost u satu izdanja izračunata je iz uzvodnih letvi."; got != want {
		t.Errorf("što:\n %q\nželim\n %q", got, want)
	}
	if !strings.HasPrefix(otvorena[0].Upitno, "Upitno: satna prognoza Srednji Brod od izdanja; Donji Grad najranije od +1 h; "+
		"Četvrti Most najranije od +7 h; dnevna prognoza Donji Grad i Treće Selo") {
		t.Errorf("upitno: %q", otvorena[0].Upitno)
	}
}

// Dani u tablici pišu se uz letve koje ih tako pokazuju: srpska letva s
// prenesenom dnevnom ima satnu 2.–4. dan i inače, bez oznake s, a cilj pri
// visokoj vodi (dnevna tek od 5. dana) ima samo prazne dane. Dani koje sve
// upitne letve pokazuju jednako pišu se bez imena.
func TestDaniBezDnevnePoLetvama(t *testing.T) {
	izmisljeniCiljevi(t)
	sa := []CelijaDana{{}, {IzSatne: true}, {IzSatne: true}, {IzSatne: true}, {BezDnevne: true}, {BezDnevne: true}}
	odTreceg := []CelijaDana{{}, {}, {IzSatne: true}, {IzSatne: true}, {BezDnevne: true}, {BezDnevne: true}}
	prazni := []CelijaDana{{}, {}, {}, {}, {BezDnevne: true}, {BezDnevne: true}}
	upitno := []prognoza.Upitno{{Letva: "donja", OdSata: -1, Dnevno: true}, {Letva: "treca", OdSata: -1, Dnevno: true},
		{Letva: "preko", OdSata: -1, Dnevno: true}}
	for _, tc := range []struct {
		ime              string
		donja, treca, pr []CelijaDana
		want             string
	}{
		{"satna samo uz jednu letvu", sa, prazni, prazni,
			" — u tablici sub 10.10., ned 11.10. i pon 12.10. stoji satna (oznaka s) za letvu Donji Grad, " +
				"uto 13.10. i sri 14.10. bez vrijednosti"},
		{"satna uz dvije od tri", sa, sa, prazni,
			" — u tablici sub 10.10., ned 11.10. i pon 12.10. stoji satna (oznaka s) za letve Donji Grad i Treće Selo, " +
				"uto 13.10. i sri 14.10. bez vrijednosti"},
		{"različiti dani", sa, odTreceg, prazni,
			" — u tablici sub 10.10., ned 11.10. i pon 12.10. stoji satna (oznaka s) za letvu Donji Grad, " +
				"ned 11.10. i pon 12.10. stoji satna (oznaka s) za letvu Treće Selo, uto 13.10. i sri 14.10. bez vrijednosti"},
		{"pri visokoj vodi bez oznake s", prazni, prazni, prazni, " — u tablici uto 13.10. i sri 14.10. bez vrijednosti"},
		{"svima isto", sa, sa, sa,
			" — u tablici sub 10.10., ned 11.10. i pon 12.10. stoji satna (oznaka s), uto 13.10. i sri 14.10. bez vrijednosti"},
	} {
		t.Run(tc.ime, func(t *testing.T) {
			p := pisacUpozorenja{postaje: izmisljenePostaje(), dani: daniUpozorenja, letve: []LetvaPrognoze{
				{Kod: "donja", Dani: tc.donja}, {Kod: "treca", Dani: tc.treca}, {Kod: "preko", Dani: tc.pr}, {Kod: "cetvrta", Dani: sa}}}
			if got := p.daniBezDnevne(upitno); got != tc.want {
				t.Errorf("\n %q\nželim\n %q", got, tc.want)
			}
		})
	}
}

// Satna upitna po dosezima: ista grupa zajedno, dosezi redom, bez dosega
// (samo dnevna) izostaje.
func TestTekstSatnihDosega(t *testing.T) {
	p := pisacUpozorenja{postaje: izmisljenePostaje()}
	got := p.satnaUpitna([]prognoza.Upitno{{Letva: "cetvrta", OdSata: 13}, {Letva: "srednja", OdSata: 0},
		{Letva: "donja", OdSata: 1}, {Letva: "preko", OdSata: 1}, {Letva: "treca", OdSata: -1, Dnevno: true}, {Letva: "nepoznata", OdSata: 13}})
	if want := "Srednji Brod od izdanja; Donji Grad i Preko najranije od +1 h; Četvrti Most i nepoznata najranije od +13 h"; got != want {
		t.Errorf("%q\nželim\n%q", got, want)
	}
}

// Svaka vrsta upozorenja ima svoj tekst i svoj uzrok na kartici.
func TestTekstSvihVrsta(t *testing.T) {
	izmisljeniCiljevi(t)
	p := pisacUpozorenja{postaje: izmisljenePostaje(), letve: letveBezDnevne(), dani: daniUpozorenja}
	jedan := []prognoza.Upitno{{Letva: "donja", OdSata: 3}}
	for _, tc := range []struct {
		u            prognoza.Upozorenje
		sto, kartica string
	}{
		{prognoza.Upozorenje{Vrsta: prognoza.VrstaDnevni, Opis: "nema arhive za srednja", Upitno: []prognoza.Upitno{{Letva: "donja", OdSata: -1, Dnevno: true}}},
			"Dnevni model nije računao za Donji Grad: nema arhive za Srednji Brod.", "Dnevna nije izdana (dnevni model nije računao)."},
		{prognoza.Upozorenje{Vrsta: prognoza.VrstaRezerva, Letva: "gornja", Opis: "rezerva: treca umjesto srednja", Upitno: jedan},
			"Gornji Brod: računa se iz rezerve: Treće Selo umjesto Srednji Brod.", "Satna upitna najranije od +3 h (Gornji Brod iz rezerve)."},
		{prognoza.Upozorenje{Vrsta: prognoza.VrstaDrzi, Letva: "gornja", Opis: "bez svježeg ulaza (srednja): drži se zadnje mjerenje", Upitno: jedan},
			"Gornji Brod: bez svježeg ulaza (Srednji Brod): drži se zadnje mjerenje.", "Satna upitna najranije od +3 h (Gornji Brod bez svježeg ulaza)."},
		{prognoza.Upozorenje{Vrsta: prognoza.VrstaVrh, Letva: "gornja", Opis: "nema budućnosti vrha (dnevni model); drži se zadnje mjerenje", Upitno: jedan},
			"Gornji Brod: vrh lanca, nema budućnosti vrha (dnevni model); drži se zadnje mjerenje.", "Satna upitna najranije od +3 h (Gornji Brod bez budućnosti vrha)."},
		{prognoza.Upozorenje{Vrsta: prognoza.VrstaKisa, Opis: "dnevni model računa bez kiše: oborine nisu uključene", Upitno: []prognoza.Upitno{{Letva: "donja", OdSata: -1, Dnevno: true}}},
			"Dnevni model računa bez kiše: oborine nisu uključene.", "Dnevna upitna (dnevni model bez kiše)."},
		{prognoza.Upozorenje{Vrsta: prognoza.VrstaDnevnaRezerva, Tezina: prognoza.TezinaNapomena, Letva: "donja", Opis: "dnevni model: treca umjesto srednja"},
			"Donji Grad: dnevna prognoza iz rezerve: Treće Selo umjesto Srednji Brod.", ""},
		{prognoza.Upozorenje{Vrsta: prognoza.VrstaZamjena, Tezina: prognoza.TezinaNapomena, Letva: "gornja", Opis: "zamjena: mjerenje kasni 2 h"},
			"Gornji Brod: mjerenje kasni 2 h.", ""},
		{prognoza.Upozorenje{Vrsta: prognoza.VrstaRekonstrukcija, Tezina: prognoza.TezinaNapomena, Letva: "gornja",
			Opis: "rekonstrukcija: nema mjerenja od 11.10. u 04:00; 10 h procijenjeno iz kretanja letve treca 4 h uzvodno; na arhivi 95 % unutar ±10 cm"},
			"Gornji Brod: rekonstruirano iz susjeda: nema mjerenja od 11.10. u 04:00; 10 h procijenjeno iz kretanja letve Treće Selo 4 h uzvodno; na arhivi 95 % unutar ±10 cm.", ""},
		{prognoza.Upozorenje{Vrsta: prognoza.VrstaRekonstrukcija, Tezina: prognoza.TezinaUpozorenje, Letva: "srednja",
			Opis:   "rekonstrukcija: nema mjerenja od 11.10. u 04:00; 10 h procijenjeno iz kretanja letve gornja 2 h uzvodno; razina izvan raspona na kojem je provjereno (od -107 do 569 cm), pa točnost nije provjerena",
			Upitno: []prognoza.Upitno{{Letva: "donja", OdSata: 3, Dnevno: true}}},
			"Srednji Brod: rekonstruirano iz susjeda: nema mjerenja od 11.10. u 04:00; 10 h procijenjeno iz kretanja letve Gornji Brod 2 h uzvodno; razina izvan raspona na kojem je provjereno (od -107 do 569 cm), pa točnost nije provjerena.",
			"Satna upitna najranije od +3 h, dnevna upitna (Srednji Brod rekonstruirana izvan provjerenog raspona)."},
		{prognoza.Upozorenje{Vrsta: prognoza.VrstaBezPrognoze, Tezina: prognoza.TezinaNapomena, Letva: "cetvrta", Opis: "nema ulaza za nijedan sat unaprijed"},
			"Četvrti Most: nema prognoze (nema ulaza za nijedan sat unaprijed).", ""},
		{prognoza.Upozorenje{Vrsta: prognoza.VrstaCeka, Tezina: prognoza.TezinaNapomena, Letva: "gornja", Zadnje: satUTC(8, 3)},
			"Gornji Brod: zadnje mjerenje 8. 10. u 5 h; čeka se, pa sat izdanja stoji iza najsvježijih očitanja ostalih letvi.", ""},
		{prognoza.Upozorenje{Vrsta: prognoza.VrstaKrivulja, Tezina: prognoza.TezinaNapomena, Upitno: []prognoza.Upitno{
			{Letva: "donja", Velicina: "protok", OdSata: 0}, {Letva: "treca", Velicina: "protok", OdSata: 12}, {Letva: "cetvrta", Velicina: "vodostaj"}}},
			"Izvan krivulje protoka (krivulja produljena preko svog ruba): vodostaj Četvrti Most; protok Donji Grad i Treće Selo od +12 h.", ""},
		{prognoza.Upozorenje{Vrsta: "buduca", Tezina: prognoza.TezinaNapomena, Opis: "nešto novo na srednja"},
			"Nešto novo na Srednji Brod.", ""},
		{prognoza.Upozorenje{Vrsta: prognoza.VrstaTudaDatumi, Tezina: prognoza.TezinaUpozorenje,
			Opis: "srpska prognoza (hidmet.gov.rs) od 10. 10.: datumi u tablici ne idu dan za danom (10. 10., 11. 10., 11. 10., 12. 10., 13. 10.); " +
				"uz našu prognozu njihova stoji samo do 11. 10., jer se za ostale stupce ne zna kojem danu pripadaju"},
			"Srpska prognoza (hidmet.gov.rs) od 10. 10.: datumi u tablici ne idu dan za danom (10. 10., 11. 10., 11. 10., 12. 10., 13. 10.); " +
				"uz našu prognozu njihova stoji samo do 11. 10., jer se za ostale stupce ne zna kojem danu pripadaju.", ""},
		// vrste koje ova inačica ne zna: izbačena „bez-tude” i nova bez letve
		{prognoza.Upozorenje{Vrsta: "bez-tude", Tezina: prognoza.TezinaUpozorenje, Letva: "srednja", Upitno: jedan,
			Opis: "mađarska prognoza (hydroinfo.hu) nije stigla, pa ga računa naš lanac, koji pri valu griješi više od nje"},
			"Srednji Brod: mađarska prognoza (hydroinfo.hu) nije stigla, pa ga računa naš lanac, koji pri valu griješi više od nje.",
			"Satna upitna najranije od +3 h (Srednji Brod)."},
		{prognoza.Upozorenje{Vrsta: "buduca", Tezina: prognoza.TezinaUpozorenje, Opis: "nešto novo na srednja", Upitno: jedan},
			"Nešto novo na Srednji Brod.", "Satna upitna najranije od +3 h (vidi upozorenja uz izdanje)."},
	} {
		if got := p.sto(tc.u); got != tc.sto {
			t.Errorf("%s:\n %q\nželim\n %q", tc.u.Vrsta, got, tc.sto)
		}
		if tc.kartica == "" {
			continue
		}
		if got := p.naKartici(tc.u, tc.u.Upitno[0]); got != tc.kartica {
			t.Errorf("%s kartica:\n %q\nželim\n %q", tc.u.Vrsta, got, tc.kartica)
		}
	}
	// krivulja nema rečenicu „Upitno”, kiša nema dane tablice
	if s := p.upitno(prognoza.Upozorenje{Vrsta: prognoza.VrstaKrivulja, Upitno: []prognoza.Upitno{{Letva: "donja", OdSata: 1}}}); s != "" {
		t.Errorf("krivulja: %q", s)
	}
	if s := p.upitno(prognoza.Upozorenje{Vrsta: prognoza.VrstaKisa, Upitno: []prognoza.Upitno{{Letva: "donja", OdSata: -1, Dnevno: true}}}); s != "Upitno: dnevna prognoza Donji Grad." {
		t.Errorf("kiša: %q", s)
	}
	if s := p.upitno(prognoza.Upozorenje{Vrsta: prognoza.VrstaRekonstrukcija, Upitno: []prognoza.Upitno{{Letva: "donja", OdSata: -1, Dnevno: true}}}); s != "Upitno: dnevna prognoza Donji Grad." {
		t.Errorf("rekonstrukcija: %q", s)
	}
	if s := p.upitno(prognoza.Upozorenje{Vrsta: prognoza.VrstaCeka}); s != "" {
		t.Errorf("bez upitnih: %q", s)
	}
	// dnevna bez kartica u tablici: bez rečenice o danima
	if s := (pisacUpozorenja{postaje: izmisljenePostaje()}).dnevnaUpitna(prognoza.Upozorenje{Upitno: []prognoza.Upitno{{Letva: "treca", Dnevno: true}}}); s != "dnevna prognoza Treće Selo" {
		t.Errorf("bez kartica: %q", s)
	}
}

// Kartica letve u otvorenom upozorenju dobiva redak; napomene ga ne daju.
func TestUpozorenjeNaKartici(t *testing.T) {
	izmisljeniCiljevi(t)
	letve := letveBezDnevne()
	p := pisacUpozorenja{postaje: izmisljenePostaje(), letve: letve}
	p.oznaciLetve([]prognoza.Upozorenje{
		{Vrsta: prognoza.VrstaUlaz, Tezina: prognoza.TezinaUpozorenje, Letva: "srednja",
			Upitno: []prognoza.Upitno{{Letva: "donja", OdSata: 1, Dnevno: true}}},
		{Vrsta: prognoza.VrstaBezPrognoze, Tezina: prognoza.TezinaNapomena, Letva: "cetvrta", Upitno: []prognoza.Upitno{{Letva: "cetvrta", OdSata: 0}}},
	})
	if got, want := letve[0].Upozorenje, "Satna upitna najranije od +1 h, dnevna nije izdana (Srednji Brod bez mjerenja)."; got != want {
		t.Errorf("Donji Grad: %q, želim %q", got, want)
	}
	if letve[1].Upozorenje != "" || letve[2].Upozorenje != "" {
		t.Errorf("Preko %q, Četvrti Most %q: bez retka", letve[1].Upozorenje, letve[2].Upozorenje)
	}
}

// Izdanje starije od tri sata: stranica to kaže sama, na svakom čvoru.
func TestZastarjeloIzdanje(t *testing.T) {
	izdano := time.Date(2026, 10, 8, 19, 0, 0, 0, time.UTC)
	if s := zastarjelo(izdano, izdano.Add(2*time.Hour)); s != "" {
		t.Errorf("dva sata: %q", s)
	}
	if s, want := zastarjelo(izdano, izdano.Add(5*time.Hour+20*time.Minute)),
		"Zadnja prognoza izdana je za 8. 10. u 21 h — prije 5 h; novije izdanje nije stiglo ni izračunato. Upitno: sve vrijednosti."; s != want {
		t.Errorf("%q\nželim\n%q", s, want)
	}
}

// Izdavač koji upozorenja ne zapisuje: napomena samo uz izdanje drugog čvora.
func TestIzdavacBezZapisa(t *testing.T) {
	if n := bezZapisa(false, "cvor-izdavac"); len(n) != 1 || !strings.Contains(n[0].Sto, "ne zapisuje upozorenja") {
		t.Errorf("stari izdavač: %+v", n)
	}
	if n := bezZapisa(true, "cvor-izdavac"); n != nil {
		t.Errorf("zapisano: %+v", n)
	}
	if n := bezZapisa(false, ""); n != nil {
		t.Errorf("vlastito izdanje: %+v", n)
	}
	var nema *CitacPrognoza
	if u, zapisano := nema.Upozorenja(time.Now()); u != nil || zapisano {
		t.Errorf("bez baze: %v %v", u, zapisano)
	}
}

// izdanjeKodPrimatelja zapisuje izdanje kod izdavača i prenosi ga u bazu
// primatelja kako ide razmjenom; s bezUpozorenja paket je kakav šalje
// izdavač na starijoj inačici. Vraća put do baze primatelja.
func izdanjeKodPrimatelja(t *testing.T, izdano int64, u []prognoza.Upozorenje, bezUpozorenja bool) string {
	t.Helper()
	dir := t.TempDir()
	izdavac, err := prognoza.Otvori(filepath.Join(dir, "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer izdavac.Close()
	ishod := &prognoza.Ishod{Sada: izdano, Upozorenja: u, Izdane: []prognoza.Izdana{
		{Letva: "donja", Velicina: "vodostaj", Izdano: izdano, Ciljni: izdano, Vrijednost: 100, Dolje: 100, Gore: 100, Model: prognoza.ModelLanac},
		{Letva: "donja", Velicina: "vodostaj", Izdano: izdano, Ciljni: izdano + 6, Vrijednost: 104, Dolje: 100, Gore: 108, Model: prognoza.ModelLanac}}}
	o := &prognoza.Osvjezivac{Baza: izdavac, Cvor: "cvor-izdavac"}
	if err := o.Zapisi(ishod); err != nil {
		t.Fatal(err)
	}
	paket, err := prognoza.SastaviIzdanje(izdavac, ishod)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]map[string]any
	sirovo, _ := json.Marshal(paket)
	if err := json.Unmarshal(sirovo, &m); err != nil {
		t.Fatal(err)
	}
	if bezUpozorenja {
		delete(m["zapis"], "Upozorenja")
	}
	om, err := prognoza.Zamotaj(prognoza.Omotnica{Izdano: izdano, Cvor: o.Cvor}, m)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(om)
	var stigao prognoza.PaketIzdanja
	if _, err := prognoza.Odmotaj(payload, &stigao); err != nil {
		t.Fatal(err)
	}
	put := filepath.Join(dir, "b.db")
	primatelj, err := prognoza.Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	defer primatelj.Close()
	if ok, err := prognoza.PrimiIzdanje(primatelj, "v1", stigao); err != nil || !ok {
		t.Fatalf("primanje: %v %v", ok, err)
	}
	return put
}

// Primatelj pokazuje upozorenje izdavača, ne svoje: izdanje stiže
// razmjenom, a stranica ga piše imenima iz svog registra.
func TestPrimateljPokazujeUpozorenjeIzdavaca(t *testing.T) {
	izmisljeniCiljevi(t)
	izdano := satUTC(8, 19)
	u := []prognoza.Upozorenje{{Vrsta: prognoza.VrstaUlaz, Tezina: prognoza.TezinaUpozorenje, Letva: "srednja", Zadnje: izdano - 11,
		Upitno: []prognoza.Upitno{{Letva: "srednja", OdSata: 0}, {Letva: "donja", OdSata: 1, Dnevno: true}}},
		{Vrsta: prognoza.VrstaBezPrognoze, Tezina: prognoza.TezinaNapomena, Letva: "cetvrta", Opis: "nema ulaza za nijedan sat unaprijed"}}
	c, err := OtvoriPrognoze(izdanjeKodPrimatelja(t, izdano, u, false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	data := PrognozePageData{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		ActiveNav: "prognoze", Izdano: "8.10.2026. u 21:00", Udio: 70, Dani: daniUpozorenja, IzdaoCvor: c.Izdavac(),
		Letve: []LetvaPrognoze{{Kod: "donja", Naziv: "Donji Grad", Racuna: "vodostaj"}}}
	izd := time.Unix(izdano*3600, 0)
	upozorenjaStranice(&data, c, izd, izd.Add(time.Hour), izmisljenePostaje())
	data.Tablice = []TablicaPrognoza{{Naslov: "Rijeka", Letve: data.Letve}}
	if data.IzdaoCvor != "cvor-izdavac" || len(data.Upozorenja) != 1 || len(data.Napomene) != 1 || data.Zastarjelo != "" {
		t.Fatalf("izdavač %q, upozorenja %+v, napomene %+v, zastarjelo %q", data.IzdaoCvor, data.Upozorenja, data.Napomene, data.Zastarjelo)
	}
	html := stdhtml.UnescapeString(iscrtaj(t, "prognoze.html", data))
	for _, want := range []string{"Upozorenja uz ovo izdanje", "Srednji Brod (primjer.hu): nema mjerenja od 8. 10. u 10 h",
		"Donji Grad najranije od +1 h", "Napomene uz ovo izdanje (1)", "Četvrti Most: nema prognoze",
		"prog-upozorenje", "Satna upitna najranije od +1 h, dnevna nije izdana (Srednji Brod bez mjerenja)."} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica nema %q", want)
		}
	}
}

// Izdavač na starijoj inačici šalje upozorenje vrste koje više nema
// („bez-tude”, Komárom bez mađarske prognoze). Primatelj ga piše opisom
// izdavača, a na kartici stoji ime letve, ne nagađani razlog.
func TestPrimateljPokazujeIzbacenuVrstuUpozorenja(t *testing.T) {
	izdano := satUTC(8, 19)
	u := []prognoza.Upozorenje{{Vrsta: "bez-tude", Tezina: prognoza.TezinaUpozorenje, Letva: "srednja",
		Opis:   "mađarska prognoza (hydroinfo.hu) nije stigla, pa ga računa naš lanac, koji pri valu griješi više od nje",
		Upitno: []prognoza.Upitno{{Letva: "srednja", OdSata: 1}, {Letva: "donja", OdSata: 4}}}}
	c, err := OtvoriPrognoze(izdanjeKodPrimatelja(t, izdano, u, false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	data := PrognozePageData{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		ActiveNav: "prognoze", Izdano: "8.10.2026. u 21:00", Udio: 70, Dani: daniUpozorenja, IzdaoCvor: c.Izdavac(),
		Letve: []LetvaPrognoze{{Kod: "donja", Naziv: "Donji Grad", Racuna: "vodostaj"}}}
	izd := time.Unix(izdano*3600, 0)
	upozorenjaStranice(&data, c, izd, izd.Add(time.Hour), izmisljenePostaje())
	data.Tablice = []TablicaPrognoza{{Naslov: "Rijeka", Letve: data.Letve}}
	html := stdhtml.UnescapeString(iscrtaj(t, "prognoze.html", data))
	for _, want := range []string{"Upozorenja uz ovo izdanje",
		"Srednji Brod: mađarska prognoza (hydroinfo.hu) nije stigla, pa ga računa naš lanac, koji pri valu griješi više od nje.",
		"Upitno: satna prognoza Srednji Brod najranije od +1 h; Donji Grad najranije od +4 h.",
		"Satna upitna najranije od +4 h (Srednji Brod)."} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica nema %q", want)
		}
	}
}

// Izdanje od izdavača koji upozorenja ne bilježi: primatelj ne kaže da je
// sve u redu, nego da izdavač upozorenja ne zapisuje.
func TestPrimateljStarijegIzdavaca(t *testing.T) {
	izdano := satUTC(8, 19)
	c, err := OtvoriPrognoze(izdanjeKodPrimatelja(t, izdano, []prognoza.Upozorenje{}, true))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	data := PrognozePageData{IzdaoCvor: c.Izdavac()}
	upozorenjaStranice(&data, c, time.Unix(izdano*3600, 0), time.Unix(izdano*3600, 0), nil)
	if len(data.Upozorenja) != 0 || len(data.Napomene) != 1 || !strings.Contains(data.Napomene[0].Sto, "ne zapisuje upozorenja") {
		t.Errorf("upozorenja %+v, napomene %+v", data.Upozorenja, data.Napomene)
	}
}

// Stranica: zastarjelost, otvorena upozorenja, sklopljene napomene i redak
// na kartici; bez ičega od toga nema ni okvira.
func TestStranicaPrognozaUpozorenja(t *testing.T) {
	osnova := PrognozePageData{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		ActiveNav: "prognoze", Izdano: "8.10.2026. u 21:00", Udio: 70}
	html := iscrtaj(t, "prognoze.html", osnova)
	for _, nema := range []string{"Upozorenja uz ovo izdanje", "Napomene uz ovo izdanje", "prog-zastarjelo"} {
		if strings.Contains(html, nema) {
			t.Errorf("stranica bez upozorenja ima %q", nema)
		}
	}
	d := osnova
	d.Zastarjelo = "Zadnja prognoza izdana je za 8. 10. u 21 h — prije 5 h."
	d.Napomene = []UpozorenjeStranice{{Sto: "Četvrti Most: nema prognoze."}}
	html = iscrtaj(t, "prognoze.html", d)
	for _, want := range []string{"prog-zastarjelo", "prije 5 h", "Napomene uz ovo izdanje (1)", "Četvrti Most: nema prognoze."} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica nema %q", want)
		}
	}
	if strings.Contains(html, "Upozorenja uz ovo izdanje") {
		t.Error("samo napomene, a stoji naslov upozorenja")
	}
}

// Izvor uz ime stoji samo kad ga registar ima i kad se adresa da pročitati.
func TestImeSIzvorom(t *testing.T) {
	p := pisacUpozorenja{postaje: izmisljenePostaje()}
	for kod, want := range map[string]string{"srednja": "Srednji Brod (primjer.hu)", "donja": "Donji Grad", "nepoznata": "nepoznata"} {
		if got := p.imeSIzvorom(kod); got != want {
			t.Errorf("%s: %q, želim %q", kod, got, want)
		}
	}
	if h := izvorMjerenja("http://[::1"); h != "" {
		t.Errorf("pokvarena adresa: %q", h)
	}
	if s := velikoSlovo(""); s != "" {
		t.Errorf("prazno: %q", s)
	}
	if s := velikoSlovo("čeka se"); s != "Čeka se" {
		t.Errorf("%q", s)
	}
}

// Napomena lista izvoza počinje upozorenjima koja imenuju letve tog lista,
// i zastarjelošću; upozorenje o letvama drugog lista tu ne stoji.
func TestListPrognozeNapomenaUpozorenja(t *testing.T) {
	v := func(x float64) *float64 { return &x }
	// List nosi i redak vode (pritoka na listu rijeke), protok uz satni
	// termin i dan koji letva nema, da se vidi da napomena ne smeta ostalom.
	data := PrognozePageData{Udio: 70, Izdano: "8.10.2026. u 21:00", Dani: []string{"pet 9.10.", "sub 10.10."}, Bliski: []int{6},
		Zastarjelo: "Zadnja prognoza izdana je za 8. 10. u 21 h — prije 5 h.",
		Upozorenja: []UpozorenjeStranice{
			{Sto: "Srednji Brod: nema mjerenja.", Upitno: "Upitno: satna prognoza Donji Grad najranije od +1 h.", letve: []string{"srednja", "donja"}},
			{Sto: "Daleki Vrh: nema mjerenja.", letve: []string{"daleko"}},
		}}
	tab := TablicaPrognoza{Naslov: "Rijeka", Letve: []LetvaPrognoze{{Kod: "donja", Naziv: "Donji Grad", Voda: "Pritoka", Racuna: "vodostaj", SadaCmV: v(100),
		Vrijednosti: []VrijednostPrognoze{{DosegH: 6, Ima: true, CmV: v(102), QV: v(40)}},
		Dani:        []CelijaDana{{CmV: v(101)}}}}}
	k := &xlsxw.Knjiga{}
	listPrognoze(k, ZaglavljeIzvoza{Potpisnici: []PotpisnikIzvoza{{Funkcija: "voditelj Centra obrane od poplava", Ime: "Ana Anić"}}}, data, tab)
	txt := tekstKnjige(t, k)
	for _, want := range []string{">Pritoka<", ">40<", "sub 10.10. 07 h", "Zadnja prognoza izdana je za 8. 10. u 21 h", "Upozorenje: Srednji Brod: nema mjerenja. Upitno: satna prognoza Donji Grad najranije od +1 h."} {
		if !strings.Contains(txt, want) {
			t.Errorf("u napomeni nema %q", want)
		}
	}
	if strings.Contains(txt, "Daleki Vrh") {
		t.Error("upozorenje drugog lista u napomeni")
	}
	if s := upozorenjaIzvoza(PrognozePageData{}, tab.Letve); s != "" {
		t.Errorf("bez upozorenja: %q", s)
	}
}

// Sažetak počinje svim otvorenim upozorenjima; postaja nosi svoj redak u
// napomeni.
func TestSazetakUpozorenja(t *testing.T) {
	data := PrognozePageData{Zastarjelo: "Zadnja prognoza izdana je prije 5 h.",
		Upozorenja: []UpozorenjeStranice{{Sto: "Srednji Brod: nema mjerenja.", Upitno: "Upitno: dnevna prognoza Donji Grad."}, {Sto: "Daleki Vrh: nema mjerenja."}}}
	if got, want := upozorenjaSazetka(data), "Upozorenja uz izdanje: Zadnja prognoza izdana je prije 5 h. Srednji Brod: nema mjerenja. "+
		"Upitno: dnevna prognoza Donji Grad. Daleki Vrh: nema mjerenja."; got != want {
		t.Errorf("%q\nželim\n%q", got, want)
	}
	if s := upozorenjaSazetka(PrognozePageData{}); s != "" {
		t.Errorf("bez upozorenja: %q", s)
	}
	if n := neprazno("", "a", ""); len(n) != 1 || n[0] != "a" {
		t.Errorf("neprazno: %q", n)
	}
	sve := postajeSazetka(PrognozePageData{Letve: []LetvaPrognoze{{Kod: sazetakSkupine[0].Letve[0], Naziv: "X", Upozorenje: "Dnevna nije izdana (Srednji Brod bez mjerenja)."}}})
	if p := sve[sazetakSkupine[0].Voda]; len(p) != 1 || p[0].Upozorenje != "Dnevna nije izdana (Srednji Brod bez mjerenja)." {
		t.Errorf("postaja sažetka: %+v", p)
	}
}

// Natpis grafa letve kaže, bez imena, od kojeg je dosega satna upitna i je
// li dnevna izdana; prognoza se čita iz baze kakva je stigla razmjenom.
func TestNatpisGrafaUpitno(t *testing.T) {
	izdano := satUTC(8, 19)
	u := []prognoza.Upozorenje{
		{Vrsta: prognoza.VrstaUlaz, Tezina: prognoza.TezinaUpozorenje, Letva: "srednja",
			Upitno: []prognoza.Upitno{{Letva: "donja", OdSata: 7, Dnevno: true}}},
		{Vrsta: prognoza.VrstaRezerva, Tezina: prognoza.TezinaUpozorenje, Letva: "gornja",
			Upitno: []prognoza.Upitno{{Letva: "donja", OdSata: 4}, {Letva: "treca", OdSata: -1, Dnevno: true}}},
		{Vrsta: prognoza.VrstaKisa, Tezina: prognoza.TezinaUpozorenje, Upitno: []prognoza.Upitno{{Letva: "cetvrta", OdSata: -1, Dnevno: true}}},
		{Vrsta: prognoza.VrstaKrivulja, Tezina: prognoza.TezinaNapomena, Upitno: []prognoza.Upitno{{Letva: "cetvrta", Velicina: "protok", OdSata: 0}}},
	}
	for letva, want := range map[string]string{"donja": "Satna upitna najranije od +4 h; dnevna nije izdana",
		"treca": "Dnevna nije izdana", "cetvrta": "Dnevna upitna", "peta": ""} {
		if got := upitnoLetve(u, letva); got != want {
			t.Errorf("%s: %q, želim %q", letva, got, want)
		}
	}
	c, err := OtvoriPrognoze(izdanjeKodPrimatelja(t, izdano, u, false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	p := c.ZaLetvu("donja", "vodostaj")
	if p == nil || p.Upitno != "Satna upitna najranije od +4 h; dnevna nije izdana" {
		t.Fatalf("prognoza niza: %+v", p)
	}
	g := crtajNizSPrognozom(nizOcitanja(p.Izdano, 24), "vodostaj", nil, nil, p)
	if !strings.HasSuffix(g.PrognozaNatpis, "slučajeva. Satna upitna najranije od +4 h; dnevna nije izdana.") {
		t.Errorf("natpis: %q", g.PrognozaNatpis)
	}
	if g := crtajNizSPrognozom(nizOcitanja(p.Izdano, 24), "vodostaj", nil, nil, nizPrognoze(p.Izdano, 24)); !strings.HasSuffix(g.PrognozaNatpis, "slučajeva.") {
		t.Errorf("natpis bez upozorenja: %q", g.PrognozaNatpis)
	}
}

// Pločica kiše bez dnevne prognoze to kaže, umjesto da porast tiho izostane.
func TestKisaSlivovaBezDnevne(t *testing.T) {
	sliv := SlivUKisi{StanjeSliva: prognoza.StanjeSliva{Sliv: "A", Razina: 1}, Naziv: "Gornji sliv", Klasa: "kisa-zuto",
		Letve: []PorastLetve{{Naziv: "Donji Grad"}}}
	var b strings.Builder
	if err := kisaSlivovaTmpl.Execute(&b, kisaSlivovaPodaci{Slivovi: []SlivUKisi{sliv}, Upozorenja: []SlivUKisi{sliv}}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"(dnevne prognoze nema)", "zadnje izdanje nema dnevne prognoze"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("pločica nema %q", want)
		}
	}
	b.Reset()
	sliv.Letve[0].Ima, sliv.Letve[0].Cm, sliv.Letve[0].Dan = true, 12, 2
	if err := kisaSlivovaTmpl.Execute(&b, kisaSlivovaPodaci{Slivovi: []SlivUKisi{sliv}, Upozorenja: []SlivUKisi{sliv}, Izdano: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "dnevne prognoze nema") || !strings.Contains(b.String(), "Porast vode iz dnevne prognoze izdane") {
		t.Errorf("pločica s dnevnom: %s", b.String())
	}
}

// Kad dnevne nema samo za neke letve, a za druge u izdanju ima (Mohács bez
// mjerenja: dunavske letve bez dnevne, dravske s njom), pločica mora reći za
// koje, umjesto da im porast tiho izostane.
func TestKisaSlivovaBezDnevneZaDioLetvi(t *testing.T) {
	iscrtajSliv := func(letve ...PorastLetve) string {
		t.Helper()
		sliv := SlivUKisi{StanjeSliva: prognoza.StanjeSliva{Sliv: "H", Razina: 1}, Naziv: "Donji sliv", Klasa: "kisa-zuto", Letve: letve}
		var b strings.Builder
		if err := kisaSlivovaTmpl.Execute(&b, kisaSlivovaPodaci{Slivovi: []SlivUKisi{sliv}, Upozorenja: []SlivUKisi{sliv},
			Izdano: time.Now()}); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	s := iscrtajSliv(PorastLetve{Naziv: "Donji Grad"}, PorastLetve{Naziv: "Preko"}, PorastLetve{Naziv: "Belo Selo", Ima: true, Cm: 12, Dan: 2})
	for _, want := range []string{"(bez dnevne prognoze: Donji Grad, Preko)", "+12 cm (2. dan)", "Porast vode iz dnevne prognoze izdane"} {
		if !strings.Contains(s, want) {
			t.Errorf("pločica nema %q:\n%s", want, s)
		}
	}
	if s := iscrtajSliv(PorastLetve{Naziv: "Donji Grad"}, PorastLetve{Naziv: "Preko"}); !strings.Contains(s, "(dnevne prognoze nema)") {
		t.Errorf("nijedna letva sliva nema dnevnu, a pločica to ne kaže:\n%s", s)
	}
}

// Prag od 0 mm znači da praga nema (Odluke (13. 10. 2026.)), pa tablica
// kiše po međuslivovima tada pokazuje crticu, a ne prag od 0,0 mm.
func TestKisaSlivovaBezPragaTrijuDana(t *testing.T) {
	prag := func(dan3 float64) string {
		t.Helper()
		sliv := SlivUKisi{StanjeSliva: prognoza.StanjeSliva{Sliv: "A", Ima: true, Palo24: 2,
			Pragovi: prognoza.PragoviKise{Dan1: [3]float64{20, 40, 80}, Dan3: [3]float64{dan3}}}, Naziv: "Gornji sliv"}
		var b strings.Builder
		if err := kisaSlivovaTmpl.Execute(&b, kisaSlivovaPodaci{Slivovi: []SlivUKisi{sliv}}); err != nil {
			t.Fatal(err)
		}
		const celija = `<td class="mono reg-card-sub" style="text-align:right;">`
		s := b.String()
		i := strings.Index(s, celija)
		if i < 0 {
			t.Fatalf("tablica nema stupca s pragom:\n%s", s)
		}
		s = s[i+len(celija):]
		return s[:strings.Index(s, "</td>")]
	}
	if got := prag(0); got != "—" {
		t.Errorf("bez praga triju dana stupac kaže %q, želim crticu", got)
	}
	if got := prag(31.5); got != "31,5" {
		t.Errorf("prag triju dana %q, želim 31,5", got)
	}
}

// Izdanje iz vremena kad je Komárom vodila mađarska prognoza (do 22. 10.
// 2026.), ili izdanje izdavača na starijoj inačici, u izboru nosi vrh po
// tuđoj prognozi. Kartica takvog vrha i dalje kaže da je u tom izdanju išao
// po tuđoj prognozi, s izvorom iz zapisa, a ne da ga vodi naš dnevni model.
// Mađarska prognoza stoji u tablici dana uz našu, radi usporedbe.
func TestIzdanjeSVrhomPoTudojPrognozi(t *testing.T) {
	put := bazaPrognoza(t, satStarog)
	pb, err := prognoza.Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	opis := "dok je svježa, vodi je tuđa prognoza (hydroinfo.hu), jer je na valovima bolja od našeg lanca; bez nje računa se iz: moslavina"
	if err := prognoza.SpremiIzbor(pb, satStarog, map[string]prognoza.Izbor{"vrh:donji-miholjac": {Inacica: 2, Opis: opis}}); err != nil {
		t.Fatal(err)
	}
	_, ciljevi := daniPregleda(satOdEpohe(satStarog))
	if _, err := pb.Exec(`INSERT INTO tude (izvor, letva, izdano, ciljni, vrijednost, raspon) VALUES (?, 'belisce', ?, ?, 222, 0)`,
		prognoza.Podrijetlo, satStarog-2, ciljevi[0]); err != nil {
		t.Fatal(err)
	}
	if err := pb.Close(); err != nil {
		t.Fatal(err)
	}
	data := podaciRanijeg(rukovateljBaze(t, put), satStarog)
	letva := func(kod string) LetvaPrognoze {
		t.Helper()
		i := slices.IndexFunc(data.Letve, func(l LetvaPrognoze) bool { return l.Kod == kod })
		if i < 0 {
			t.Fatalf("nema kartice %s: %+v", kod, data.Letve)
		}
		return data.Letve[i]
	}
	if dm := letva("donji-miholjac"); !dm.TudiVrh || dm.NasVrh || dm.Racuna != "" || !strings.Contains(dm.Rezerva, opis) {
		t.Errorf("vrh po tuđoj prognozi: tuđi %v, naš %v, računa %q, redak %q", dm.TudiVrh, dm.NasVrh, dm.Racuna, dm.Rezerva)
	}
	if b := letva("belisce"); len(b.Dani) == 0 ||
		!slices.ContainsFunc(b.Dani[0].Tude, func(x TudaCelija) bool { return x.Oznaka == "HU" && x.CmV == 222 }) {
		t.Errorf("Belišće bez mađarske prognoze u tablici dana: %+v", b.Dani)
	}
	data.CurrentUser, data.Permissions = &models.User{FullName: "P"}, &models.UserPermissions{IsGlobalAdmin: true}
	html := stdhtml.UnescapeString(iscrtaj(t, "prognoze.html", data))
	for _, want := range []string{"vrh lanca; u ovom izdanju dalje od mjerenja ide po tuđoj prognozi", opis} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica nema %q", want)
		}
	}
}

// Letva koja u satu izdanja nije povezana sa živom vodom na kartici kaže iz
// kojeg ulaza i ispod kojeg praga, imenima iz registra.
func TestKarticaNepovezaneLetve(t *testing.T) {
	h := NewPrognozeHandler(nil, nil, nil)
	popis := map[string]models.Station{
		"gornja":  {Code: "gornja", Name: "Gornji Brod (Mađarska)"},
		"rukavac": {Code: "rukavac", Name: "Rukavac"},
	}
	got := h.opisiLetve(popis, []PregledLetve{{Letva: "rukavac", Racuna: "vodostaj", Ulazi: []string{"gornja"},
		Nepovezana: true, PragPovezanosti: 123}})
	want := "Rukavac trenutno nije povezan sa živom vodom (Gornji Brod ispod 123 cm), pa se ne može ni prognozirati."
	if len(got) != 1 || got[0].Nepovezana != want {
		t.Errorf("kartica %+v\nželim %q", got, want)
	}
}
