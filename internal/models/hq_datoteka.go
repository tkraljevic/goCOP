package models

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Datoteku krivulja protoka letve (…_hq_krivulje.csv) čitaju gradnja arhive
// i uvoz izvoza HIS-2000, koji izvoz spaja s datotekom koju letva već ima.
// Datoteku koju gradnja odbije spajanje ne smije tiho prepisati, a onu koju
// gradnja pročita mora pročitati i spajanje. Zato provjere zaglavlja i
// odsječka stoje ovdje, jednom za oba.

// ZaglavljeDatotekeKrivulja je prvi redak datoteke krivulja, s imenima svih
// stupaca koje datoteka smije imati.
const ZaglavljeDatotekeKrivulja = "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena"

// obavezniStupciKrivulja su stupci bez kojih se krivulja ne da pročitati.
// Stupac p4 došao je naknadno, s potencijskim odsječcima, pa ga starija
// datoteka nema. Bez stupca oblik odsječak je polinom.
var obavezniStupciKrivulja = []string{"vrijedi_od", "vrijedi_do", "od_cm", "do_cm", "p1", "p2", "p3"}

// najviseZnakovaRetka je koliko se znakova prvog retka navodi u poruci.
// Datoteka koja retke dijeli samo znakom CR (stari „Macintosh CSV”) čita se
// kao jedan redak, pa bi poruka inače navela cijelu datoteku.
const najviseZnakovaRetka = 80

// ImeStupcaKrivulje je ime stupca iz zaglavlja datoteke krivulja onako kako
// se traži: bez oznake BOM koju na početak datoteke stavlja npr. Excel, bez
// razmaka oko imena i malim slovima.
func ImeStupcaKrivulje(polje string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(polje, "\ufeff")))
}

// ZastoNijeZaglavljeKrivulja kaže zašto prvi redak datoteke krivulja nije
// njezino zaglavlje. Za zaglavlje vraća prazno.
//
// Prije se zaglavlje nije provjeravalo. Datoteka koja retke dijeli samo
// znakom CR čitala se kao samo zaglavlje, pa je gradnja letvi maknula sve
// krivulje. Datoteci bez zaglavlja prvi se odsječak gubio, a ostali su se
// čitali po krivim stupcima. Znak CR u imenu stupca zato znači da to nije
// zaglavlje, i kad je ime bez njega ispravno, npr. „napomena” iza koje su
// samo prazni redci odvojeni znakom CR.
func ZastoNijeZaglavljeKrivulja(polja []string) string {
	if jeZaglavljeKrivulja(polja) && !slices.ContainsFunc(polja, imaCR) {
		return ""
	}
	return fmt.Sprintf("nije zaglavlje kakvo treba („%s”). Prvi redak mora imati imena stupaca, npr. %s, "+
		"a svaki redak mora biti u svom retku datoteke", skracenRedak(polja), ZaglavljeDatotekeKrivulja)
}

// jeZaglavljeKrivulja javlja je li redak zaglavlje datoteke krivulja: svako
// ime je ime stupca krivulje ili prazno, a obavezni stupci su tu. Prva dva
// stupca su vrijedi_od i vrijedi_do, jer se razdoblje valjanosti čita iz
// njih, a ne po zaglavlju. Prazno ime dopušteno je jer Excel i LibreOffice
// svaki redak, i zaglavlje, završe točkom-zarezom kad je tablica šira od
// podataka, npr. zbog zalutale ćelije u stupcu L.
func jeZaglavljeKrivulja(polja []string) bool {
	imena := make([]string, len(polja))
	for i, p := range polja {
		imena[i] = ImeStupcaKrivulje(p)
	}
	poznata := strings.Split(ZaglavljeDatotekeKrivulja, ";")
	nepoznato := func(ime string) bool { return ime != "" && !slices.Contains(poznata, ime) }
	nedostaje := func(ime string) bool { return !slices.Contains(imena, ime) }
	return len(imena) >= 2 && imena[0] == "vrijedi_od" && imena[1] == "vrijedi_do" &&
		!slices.ContainsFunc(imena, nepoznato) && !slices.ContainsFunc(obavezniStupciKrivulja, nedostaje)
}

// imaCR javlja ima li polje znak CR. Čitač ga ostavi u polju kad redak
// završi samim CR-om, bez LF-a, pa je u polju i početak sljedećeg retka.
func imaCR(polje string) bool {
	return strings.Contains(polje, "\r")
}

// RedakKrivuljeZaPoruku je redak datoteke krivulja za poruku: polja odvojena
// točkom-zarezom, a CR i LF kao razmak, da poruka ostane u jednom retku i da
// se vidi kad je u jednom retku više redaka.
func RedakKrivuljeZaPoruku(polja []string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(strings.Join(polja, ";"))
}

// skracenRedak je prvi redak datoteke za poruku (RedakKrivuljeZaPoruku), bez
// nevidljive oznake BOM i skraćen na najviseZnakovaRetka znakova.
func skracenRedak(polja []string) string {
	redak := strings.TrimPrefix(RedakKrivuljeZaPoruku(polja), "\ufeff")
	if z := []rune(redak); len(z) > najviseZnakovaRetka {
		return string(z[:najviseZnakovaRetka]) + "…"
	}
	return redak
}

// BrojKrivulje čita broj iz polja datoteke krivulja, s decimalnim zarezom ili
// točkom. Isto čitanje vrijedi i za provjeru retka i za gradnju, pa provjera
// propušta točno one brojeve koje gradnja zna pročitati.
func BrojKrivulje(polje string) (float64, error) {
	return strconv.ParseFloat(strings.ReplaceAll(polje, ",", "."), 64)
}

// ZastoNijeOdsjecakKrivulje kaže zašto polja odsječka u retku datoteke
// krivulja ne valjaju. Za ispravan odsječak vraća prazno. Stupci su mjesta
// stupaca po imenu iz zaglavlja. Granice od_cm i do_cm moraju biti cijeli
// brojevi, p1 do p4 brojevi (BrojKrivulje) ili prazni, a oblik polinom,
// potencija ili prazan. Razlog kaže prvi takav stupac.
//
// Prije svega redak ne smije imati znak CR. Redak koji završi samim CR-om
// čitač spoji sa sljedećim, pa bi se odsječak sljedećeg retka tiho izgubio.
//
// Tako se hvata zarez upisan umjesto točke-zareza u retku od deset ili
// jedanaest stupaca, kakve imaju sve datoteke: polja iza zareza pomaknu se
// ulijevo, pa je npr. od_cm „-100,600” ili u stupcu p4 stoji izvor. Prije je
// takav redak dao drukčiju krivulju, a gradnja je njome zamijenila ispravnu
// krivulju istog početka.
//
// Ne hvata se sve. Zarez umjesto točke-zareza među p-stupcima prođe kad je
// izvor prazan: u zadnji p-stupac dođe prazan izvor, a broj sa zarezom čita
// se kao decimalni. Npr. „4,5;6;7;;” umjesto „4;5;6;7;;” daje p1 4,5, p2 6,
// p3 7 i prazan p4. Redci koje upiše uvoz izvoza HIS-2000 izvor uvijek imaju.
func ZastoNijeOdsjecakKrivulje(stupci map[string]int, r []string) string {
	polje := func(ime string) string {
		i, ima := stupci[ime]
		if !ima || i >= len(r) {
			return ""
		}
		return strings.TrimSpace(r[i])
	}
	for _, razlog := range []string{
		zastoImaCR(r),
		zastoNijeCijeli("od_cm", polje("od_cm")), zastoNijeCijeli("do_cm", polje("do_cm")),
		zastoNijeOblik(polje("oblik")),
		zastoNijeBroj("p1", polje("p1")), zastoNijeBroj("p2", polje("p2")),
		zastoNijeBroj("p3", polje("p3")), zastoNijeBroj("p4", polje("p4")),
	} {
		if razlog != "" {
			return razlog
		}
	}
	return ""
}

// zbogZareza je najčešći razlog kad u stupcu s brojem nema broja.
const zbogZareza = ", npr. zbog zareza umjesto točke-zareza"

func zastoImaCR(r []string) string {
	if slices.ContainsFunc(r, imaCR) {
		return "u sebi ima znak CR, pa je u jednom retku datoteke više redaka krivulje. Svaki redak mora biti u svom retku"
	}
	return ""
}

func zastoNijeCijeli(stupac, v string) string {
	if _, err := strconv.Atoi(v); err == nil {
		return ""
	}
	nego := uNavodnicima(v)
	if v == "" {
		nego = "je stupac prazan"
	}
	return "u stupcu " + stupac + " nema cijelog broja, nego " + nego + zbogZareza
}

func zastoNijeBroj(stupac, v string) string {
	if v == "" {
		return ""
	}
	if _, err := BrojKrivulje(v); err == nil {
		return ""
	}
	return "u stupcu " + stupac + " nema broja, nego " + uNavodnicima(v) + zbogZareza
}

func zastoNijeOblik(v string) string {
	switch v {
	case "", OblikPolinom, OblikPotencija:
		return ""
	}
	return "u stupcu oblik ima " + uNavodnicima(v) + ", a smije imati samo polinom ili potencija, malim slovima"
}

func uNavodnicima(v string) string {
	return "„" + v + "”"
}
