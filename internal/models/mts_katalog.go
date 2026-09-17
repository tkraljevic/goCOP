package models

// Propisani popis sredstava za obranu od poplava, redom kojim stoji u
// obrascu koji sektori jednom godišnje šalju Glavnom centru. Isti je za sve
// sektore — zato i stoji u programu, a ne u postavkama svakog centra: da se
// stanje jednog sektora može zbrojiti sa stanjem drugoga i da se vidi gdje
// čega ima viška kad negdje ponestane.
//
// Vrsta se ne briše nego gasi, jer stari promet i popisi na nju pokazuju.
// Organizacija smije dodati svoju vrstu; onda više nije iz ovog popisa i
// zbraja se samo unutar organizacije.

// oblici2 skraćuje popis oblika u tablici kataloga
func oblici2(a, b string) []string { return []string{a, b} }

type katalogRedak struct {
	id       string
	naziv    string
	jedinica string
	oblici   []string
}

var katalogOprema = []katalogRedak{
	{"agregat-rasvjeta", "Agregat za rasvjetu", "kom", nil},
	{"reflektor-stalak", "Reflektor sa stalkom", "kom", nil},
	{"camac-oprema", "Čamac s opremom", "kom", nil},
	{"motor-vanbrodski", "Motor vanbrodski za čamac", "kom", nil},
	{"pila-motorna", "Pila motorna", "kom", nil},
	{"pobijac-zmurja", "Pobijač žmurja", "kom", nil},
	{"pumpa-diesel-350", "Pumpa diesel mobilna 350 l/s", "kom", nil},
	{"pumpa-traktorska-350", "Pumpa traktorska 350 l/s", "kom", nil},
	{"pumpa-traktorska-800", "Pumpa traktorska 800 l/s", "kom", nil},
	{"pumpa-elektricna", "Pumpa električna", "kom", nil},
	{"prikolica-camac", "Prikolica za čamac", "kom", nil},
	{"radio-rucna", "Radio stanica ručna", "kom", nil},
	{"radio-prijenosna", "Radio stanica prijenosna", "kom", nil},
	{"stroj-punjenje-vreca", "Stroj za punjenje vreća", "kom", nil},
}

var katalogAlat = []katalogRedak{
	{"bat-zeljezni", "Bat željezni (5 - 10 kg)", "kom", nil},
	{"klijesta-kombinirana", "Kliješta (kombinirana)", "kom", nil},
	{"kolica-rucna", "Kolica ručna", "kom", nil},
	{"kosir", "Kosir", "kom", nil},
	{"kramp", "Kramp (pijuk)", "kom", nil},
	{"caklja", "Čaklja (kuka)", "kom", nil},
	{"lopata", "Lopata", "kom", nil},
	{"stihaca", "Štihača", "kom", nil},
	{"motika-kopacica", "Motika kopačica", "kom", nil},
	{"pila-s-lukom", "Pila s lukom", "kom", nil},
	{"pajser", "Pajser", "kom", nil},
	{"sjekira-velika", "Sjekira velika", "kom", nil},
	{"sjekirica-mala", "Sjekirica mala", "kom", nil},
	{"vile-za-kamen", "Vile za kamen", "kom", nil},
	{"vile-obicne", "Vile obične", "kom", nil},
	{"cekic-tesarski", "Čekić tesarski", "kom", nil},
}

// Vreće se vode prazne i napunjene: u skladištu čekaju prazne, na nasip idu
// pune, a između je punjenje — isti komad, druga upotrebljivost.
var katalogMaterijal = []katalogRedak{
	{"cavli", "Čavli", "kg", nil},
	{"daske", "Daske", "m³", nil},
	{"folija-pvc", "Folija PVC", "m²", nil},
	{"gredice-drvene", "Gredice drvene", "m³", nil},
	{"kamen-lomljeni", "Kamen lomljeni", "m³", nil},
	{"kamen-tucanik", "Kamen tucanik ili batuda", "m³", nil},
	{"pijesak", "Pijesak", "m³", nil},
	{"uze-50m", "Uže (50 m)", "kom", nil},
	{"vrece-50x80", "Vreće 50x80 cm", "kom", oblici2(OblikPrazno, OblikPunjeno)},
	{"jumbo-vrece", "Jumbo vreće 90x90x120 cm", "kom", oblici2(OblikPrazno, OblikPunjeno)},
	{"zica-paljena", "Žica paljena", "kg", nil},
	{"zmurje-celicno-4m", "Žmurje čelično - 4m", "kom", nil},
	{"gabioni", "Gabioni", "m'", nil},
	{"geomreza", "Geomreža", "m²", nil},
	{"geotekstil", "Geotekstil", "m²", nil},
	{"vodena-barijera", "Vodena barijera", "m'", nil},
	{"vodena-cijev", "Vodena cijev", "kom", nil},
	{"geomembrana-4x6", "Zašt. geomembrana 4x6 m", "kom", nil},
	{"geomembrana-4x8", "Zašt. geomembrana 4x8 m", "kom", nil},
	{"geomembrana-4x10", "Zašt. geomemb. 4x10 m", "kom", nil},
	{"geomembrana-4x12", "Zašt. geomemb. 4x12 m", "kom", nil},
	{"sandorove-grede", "Šandorove grede", "kom", nil},
	{"box-barijera-1x1x1", "Box barijere 1x1x1", "m'", nil},
	{"box-barijera-5x1x1", "Box barijere 5x1x1", "m'", nil},
	{"box-barijera-3x1x1", "Box barijere 3x1x1", "m'", nil},
	{"box-barijera-3x1x05", "Box barijere 3x1x0.5", "m'", nil},
}

var katalogPribor = []katalogRedak{
	{"cizme-gumene", "Čizme (gumene)", "par", nil},
	{"cizme-ribarske", "Čizme (ribarske)", "par", nil},
	{"kabanica-kisna", "Kabanica kišna", "kom", nil},
	{"kutija-prve-pomoci", "Kutija prve pomoći", "kom", nil},
	{"prsluk-spasavanje", "Prsluk za spašavanje", "kom", nil},
	{"reflektor-rucni", "Reflektor ručni", "kom", nil},
	{"rukavice-zastitne", "Rukavice zaštitne", "kom", nil},
	{"svjetiljka-rucna", "Svjetiljka ručna", "kom", nil},
	{"dalekozor", "Dalekozor", "kom", nil},
	{"baterije-mobitel", "Baterije za mobitel", "kom", nil},
}

// KatalogSredstava vraća propisani popis kao vrste, s rednim brojevima
// unutar skupine kakve obrazac ima
func KatalogSredstava() []VrstaSredstva {
	var out []VrstaSredstva
	for _, g := range []struct {
		grupa string
		redci []katalogRedak
	}{
		{GrupaOprema, katalogOprema},
		{GrupaAlat, katalogAlat},
		{GrupaMaterijal, katalogMaterijal},
		{GrupaPribor, katalogPribor},
	} {
		for i, r := range g.redci {
			out = append(out, VrstaSredstva{ID: r.id, Grupa: g.grupa, Redoslijed: i + 1, Naziv: r.naziv,
				Jedinica: r.jedinica, Oblici: r.oblici, Aktivna: true})
		}
	}
	return out
}
