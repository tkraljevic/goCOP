package web

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"gocop/internal/hydro"
	"gocop/internal/models"
)

// objektiRedaka vraća nazive objekata po retcima kartice; redak „Bez nasipa”
// ima ključ "".
func objektiRedaka(rows []EmbankmentRow) map[string][]string {
	out := map[string][]string{}
	for _, r := range rows {
		k := ""
		if r.Embankment != nil {
			k = r.Embankment.Name
		}
		for _, o := range r.Objects {
			out[k] = append(out[k], o.Name)
		}
	}
	return out
}

func istiPopis(t *testing.T, opis string, got, want []string) {
	t.Helper()
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Errorf("%s: %v, očekivano %v", opis, got, want)
	}
}

const (
	loBobotski = "Nasip uz l.o. Bobotskog kanala"
	doBobotski = "Nasip uz d.o. Bobotskog kanala"
)

// B.15.11, poddionica 1: BP 15 nasip upisuje posebno za lijevu i posebno za
// desnu obalu, oba kkm 24+000–40+000, a objekti — mostovi, propusti, ušća —
// nemaju ni nasip ni obalu. Svaki takav objekt stoji uz oba nasipa, istim
// redom; objekt izvan oba raspona ide u redak „Bez nasipa”.
func TestObjektBezObaleIdeNaObaJednakaNasipa(t *testing.T) {
	p := models.SectionPart{
		Bank: "LD",
		Embankments: []models.PartEmbankment{
			{Name: loBobotski, WaterKind: "kkm", WaterFrom: km(24), WaterTo: km(40)},
			{Name: doBobotski, WaterKind: "kkm", WaterFrom: km(24), WaterTo: km(40)},
		},
		Objects: []models.PartObject{
			{Name: "c.m. Tenja - Silaš", StationingKind: "kkm", StationingText: "kkm 24+635"},
			{Name: "ušće k. Rit (aut.č. Ø 80 cm)", StationingKind: "kkm", Stationing: km(25.015)},
			{Name: "HŽ.m. Osijek-Vinkovci", StationingKind: "kkm", StationingText: "kkm 33+435"},
			{Name: "propust Pomoćin-Pustara", StationingKind: "kkm", StationingText: "kkm 41+122"},
		},
	}
	rows := embankmentRows(p)
	if len(rows) != 3 || rows[2].Embankment != nil {
		t.Fatalf("redaka %d, očekivano dva nasipa i redak bez nasipa", len(rows))
	}
	po := objektiRedaka(rows)
	mostovi := []string{"c.m. Tenja - Silaš", "ušće k. Rit (aut.č. Ø 80 cm)", "HŽ.m. Osijek-Vinkovci"}
	istiPopis(t, "l.o.", po[loBobotski], mostovi)
	istiPopis(t, "d.o.", po[doBobotski], mostovi)
	istiPopis(t, "bez nasipa", po[""], []string{"propust Pomoćin-Pustara"})
}

// Objekt s obalom na paru nasipa l.o./d.o. ide samo uz nasip svoje obale;
// objekt obiju obala ide uz oba. Obala objekta piše se kako god je upisana.
func TestObjektObaleIdeNaNasipSvojeObale(t *testing.T) {
	p := models.SectionPart{
		Embankments: []models.PartEmbankment{
			{Name: loBobotski, WaterKind: "kkm", WaterFrom: km(24), WaterTo: km(40)},
			{Name: doBobotski, WaterKind: "kkm", WaterFrom: km(24), WaterTo: km(40)},
		},
		Objects: []models.PartObject{
			{Name: "čep lijevo", Bank: "L", StationingKind: "kkm", Stationing: km(26)},
			{Name: "čep desno", Bank: " d ", StationingKind: "kkm", Stationing: km(27)},
			{Name: "sifon", Bank: "LD", StationingKind: "kkm", Stationing: km(28)},
		},
	}
	po := objektiRedaka(embankmentRows(p))
	istiPopis(t, "l.o.", po[loBobotski], []string{"čep lijevo", "sifon"})
	istiPopis(t, "d.o.", po[doBobotski], []string{"čep desno", "sifon"})
	istiPopis(t, "bez nasipa", po[""], nil)
}

// Objekt s upisanim nasipom ostaje samo na njemu, i kad drugi nasip jednakog
// raspona pokriva njegovu stacionažu.
func TestObjektSaSvojimNasipomOstajeNaNjemu(t *testing.T) {
	p := models.SectionPart{
		Embankments: []models.PartEmbankment{
			{Name: loBobotski, WaterKind: "kkm", WaterFrom: km(24), WaterTo: km(40)},
			{Name: doBobotski, WaterKind: "kkm", WaterFrom: km(24), WaterTo: km(40)},
		},
		Objects: []models.PartObject{
			{Name: "CS Seleš", StationingKind: "kkm", Stationing: km(30), OnEmbankment: "nasip uz d.o. bobotskog kanala"},
			{Name: "čep", Bank: "L", StationingKind: "kkm", Stationing: km(31), OnEmbankment: doBobotski},
		},
	}
	po := objektiRedaka(embankmentRows(p))
	istiPopis(t, "l.o.", po[loBobotski], nil)
	istiPopis(t, "d.o.", po[doBobotski], []string{"CS Seleš", "čep"})
}

// Nasip bez obale u nazivu i nasip „uz l.o. i d.o.” (jedan nasip za obje
// obale, kao B.16.1) brane obje obale i primaju objekte obiju.
func TestNasipObijuObalaPrimaSve(t *testing.T) {
	for _, naziv := range []string{"Nasip Gomboš", "Usporni nasip uz l.o. i d.o. K. Travnik", "Lijevi i desni nasip spojnog kanala CS Draž-p. Karašica"} {
		p := models.SectionPart{
			Embankments: []models.PartEmbankment{{Name: naziv, WaterKind: "rkm", WaterFrom: km(10), WaterTo: km(0)}},
			Objects: []models.PartObject{
				{Name: "lijevi", Bank: "L", StationingKind: "rkm", Stationing: km(2)},
				{Name: "desni", Bank: "D", StationingKind: "rkm", Stationing: km(3)},
				{Name: "most", StationingKind: "rkm", Stationing: km(4)},
			},
		}
		rows := embankmentRows(p)
		if len(rows) != 1 {
			t.Errorf("%s: %d redaka, očekivan samo nasip", naziv, len(rows))
			continue
		}
		istiPopis(t, naziv, objektiRedaka(rows)[naziv], []string{"lijevi", "desni", "most"})
	}
}

// Nejednaki rasponi: pobjeđuje najuži, kao i dosad. Rasponi koji se
// razlikuju za manje od metra jednaki su, pa objekt ide uz oba.
func TestNejednakiRasponiNajuziPobjeduje(t *testing.T) {
	p := models.SectionPart{
		Embankments: []models.PartEmbankment{
			{Name: "Nasip uz l.o. r. Vuke", WaterKind: "rkm", WaterFrom: km(51.67), WaterTo: km(43.183)},
			{Name: "Nasip uz d.o. r. Vuke", WaterKind: "rkm", WaterFrom: km(48.408), WaterTo: km(41.977)},
		},
		Objects: []models.PartObject{{Name: "most Petrova Slatina", StationingKind: "rkm", StationingText: "rkm 44+273"}},
	}
	po := objektiRedaka(embankmentRows(p))
	istiPopis(t, "l.o.", po["Nasip uz l.o. r. Vuke"], nil)
	istiPopis(t, "d.o.", po["Nasip uz d.o. r. Vuke"], []string{"most Petrova Slatina"})

	p.Embankments = []models.PartEmbankment{
		{Name: loBobotski, WaterKind: "kkm", WaterFrom: km(24), WaterTo: km(40.0005)},
		{Name: doBobotski, WaterKind: "kkm", WaterFrom: km(24), WaterTo: km(40)},
		{Name: "Nasip uz d.o. k. Velika Osatina", WaterKind: "kkm", WaterFrom: km(20), WaterTo: km(40.0015)},
	}
	p.Objects = []models.PartObject{{Name: "propust", StationingKind: "kkm", Stationing: km(30)}}
	po = objektiRedaka(embankmentRows(p))
	istiPopis(t, "l.o. pola metra dulji", po[loBobotski], []string{"propust"})
	istiPopis(t, "d.o.", po[doBobotski], []string{"propust"})
	istiPopis(t, "širi nasip", po["Nasip uz d.o. k. Velika Osatina"], nil)
}

// B.15.2, poddionica 2: nasip l.o. Vuke ide rkm 51+670–43+183, nasip d.o.
// rkm 48+408–41+977. Objekt l.o. na rkm 42+780 pokriva samo nasip d.o.; nije
// na njemu, nego ostaje bez nasipa. Objekt l.o. na rkm 43+240 ide na nasip
// l.o. iako je nasip d.o. uži, a objekt d.o. na rkm 50+080 ostaje bez nasipa.
func TestObjektObaleBezSvogNasipaOstajeBezNasipa(t *testing.T) {
	lo, do := "Nasip uz l.o. r. Vuke", "Nasip uz d.o. r. Vuke"
	p := models.SectionPart{
		Embankments: []models.PartEmbankment{
			{Name: lo, WaterKind: "rkm", WaterFrom: km(51.67), WaterTo: km(43.183)},
			{Name: do, WaterKind: "rkm", WaterFrom: km(48.408), WaterTo: km(41.977)},
		},
		Objects: []models.PartObject{
			{Name: "k. Šomoret (a.č.Ø 80)", Bank: "L", StationingKind: "rkm", StationingText: "rkm 42+780"},
			{Name: "k. Kiš Mihalj (a.č.)", Bank: "L", StationingKind: "rkm", StationingText: "rkm 43+240"},
			{Name: "k. Bajević (a.č. Ø 80)", Bank: "D", StationingKind: "rkm", StationingText: "rkm 42+824"},
			{Name: "k. Ileš (a.č. Ø 80)", Bank: "D", StationingKind: "rkm", StationingText: "rkm 50+080"},
		},
	}
	rows := embankmentRows(p)
	if len(rows) != 3 || rows[2].Embankment != nil {
		t.Fatalf("redaka %d, očekivano dva nasipa i redak bez nasipa", len(rows))
	}
	po := objektiRedaka(rows)
	istiPopis(t, "l.o.", po[lo], []string{"k. Kiš Mihalj (a.č.)"})
	istiPopis(t, "d.o.", po[do], []string{"k. Bajević (a.č. Ø 80)"})
	istiPopis(t, "bez nasipa", po[""], []string{"k. Šomoret (a.č.Ø 80)", "k. Ileš (a.č. Ø 80)"})
}

// Obala nasipa čita se iz naziva; l.o. i d.o. moraju biti zasebne oznake, a
// ne slova u drugoj riječi.
func TestObalaNasipaIzNaziva(t *testing.T) {
	for _, c := range []struct{ naziv, obala string }{
		{"Nasip uz l.o. Bobotskog kanala", hydro.BankLeft},
		{"Nasip uz L.O. Bobotskog kanala", hydro.BankLeft},
		{"Nasip uz l. o. r. Vuke", hydro.BankLeft},
		{"Nasip uz l.o r. Vuke", hydro.BankLeft},
		{"l.o.", hydro.BankLeft},
		{"l.o. Bobotskog kanala Nasip Budžak", hydro.BankLeft},
		{"Gradska obaloutvrda l.o. r. Drave", hydro.BankLeft},
		{"Lijevi sekundarni nasip Sece", hydro.BankLeft},
		{"Lijevoobalni nasip Batina – Zmajevac", hydro.BankLeft},
		{"Nasip uz d.o. r. Vuke", hydro.BankRight},
		{"Usporni nasip uz D. O. Županijskog kanala", hydro.BankRight},
		{"d.o", hydro.BankRight},
		{"Desni sekundarni nasip Kovačke livade", hydro.BankRight},
		{"desnoobalni nasip", hydro.BankRight},
		{"Usporni nasip uz l.o. i d.o. K. Travnik", hydro.BankBoth},
		{"Lijevi i desni nasip Lateralnog kanala", hydro.BankBoth},
		// bez obale: slova l, o i d stoje u drugim riječima
		{"Nasip Gomboš", hydro.BankBoth},
		{"Nasip Donji Miholjac-Sveti Đurađ", hydro.BankBoth},
		{"Nasip Lonja do Lužca", hydro.BankBoth},
		{"Nasip lo", hydro.BankBoth},
		{"Nasip Sl. Orahovica", hydro.BankBoth},
		{"Nasip Vel.Obreška", hydro.BankBoth},
		{"Nasip Kod.Ostrovo", hydro.BankBoth},
		{"Nasip žl.o", hydro.BankBoth},
		{"Brana akumulacije Borovik", hydro.BankBoth},
		{"Nasip s obaloutvrdom za zaštitu Vukovara", hydro.BankBoth},
		{"", hydro.BankBoth},
	} {
		if got := obalaNasipa(c.naziv); got != c.obala {
			t.Errorf("obalaNasipa(%q) = %q, očekivano %q", c.naziv, got, c.obala)
		}
	}
}

func TestObalaPrima(t *testing.T) {
	for _, c := range []struct {
		nasip, objekt string
		prima         bool
	}{
		{hydro.BankLeft, "L", true}, {hydro.BankLeft, "D", false}, {hydro.BankLeft, "LD", true}, {hydro.BankLeft, "", true},
		{hydro.BankRight, "L", false}, {hydro.BankRight, "d", true}, {hydro.BankRight, "LD", true}, {hydro.BankRight, "", true},
		{hydro.BankBoth, "L", true}, {hydro.BankBoth, "D", true}, {hydro.BankBoth, "LD", true}, {hydro.BankBoth, "", true},
	} {
		if got := obalaPrima(c.nasip, c.objekt); got != c.prima {
			t.Errorf("obalaPrima(%q, %q) = %v", c.nasip, c.objekt, got)
		}
	}
}

// Isti objekt uz dva nasipa piše se u oba retka i na kartici i u Excelu, a
// broj objekata u zaglavlju ostaje broj objekata poddionice.
func TestObjektUzDvaNasipaNaKarticiIIzvozu(t *testing.T) {
	part := models.SectionPart{Seq: 1, Bank: "LD", StationingKind: "kkm",
		Embankments: []models.PartEmbankment{
			{Name: loBobotski, WaterKind: "kkm", WaterFrom: km(24), WaterTo: km(40)},
			{Name: doBobotski, WaterKind: "kkm", WaterFrom: km(24), WaterTo: km(40)},
		},
		Objects: []models.PartObject{
			{Name: "HŽ.m. Osijek-Vinkovci", StationingKind: "kkm", StationingText: "kkm 33+435"},
			{Name: "c.m. Osijek-Vinkovci", StationingKind: "kkm", StationingText: "kkm 34+606"},
		},
	}
	d := SectionPageData{
		CurrentUser: &models.User{FullName: "Pero Perić"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Section: models.Section{Code: "B.15.11", AreaID: 15, SectorID: "B"},
		Parts:   []PartView{{SectionPart: part, Rows: embankmentRows(part)}},
	}

	html := iscrtaj(t, "section_detail.html", d)
	if n := strings.Count(html, "HŽ.m. Osijek-Vinkovci"); n != 2 {
		t.Errorf("most je na kartici %d puta, očekivano uz oba nasipa", n)
	}
	if !strings.Contains(html, ">2 objekta<") {
		t.Error("zaglavlje ne broji objekte poddionice")
	}

	var b bytes.Buffer
	if err := KnjigaDionice(d, ZaglavljeIzvoza{Datum: time.Now()}).Zapisi(&b); err != nil {
		t.Fatal(err)
	}
	redci, err := procitajXLSX(b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	var sve []string
	for _, r := range redci {
		sve = append(sve, strings.Join(r, "|"))
	}
	list := strings.Join(sve, "\n")
	if n := strings.Count(list, "HŽ.m. Osijek-Vinkovci"); n != 2 {
		t.Errorf("most je u Excelu %d puta, očekivano uz oba nasipa\n%s", n, list)
	}
	if !strings.Contains(list, "2 nasipa, 2 objekta") {
		t.Errorf("zaglavlje u Excelu ne broji objekte poddionice\n%s", list)
	}
}

// Stacionaža druge vrste ne pada na nasip: nkm po kruni nasipa nije kkm uz
// kanal, i kad je broj unutar raspona.
func TestRasponPokrivaSamoIstuVrstu(t *testing.T) {
	e := models.PartEmbankment{Name: loBobotski, WaterKind: "kkm", WaterFrom: km(24), WaterTo: km(40)}
	if _, ok := rasponPokriva(e, "nkm", 30); ok {
		t.Error("nkm je pao na raspon kkm")
	}
	if span, ok := rasponPokriva(e, "KKM", 30); !ok || span != 16 {
		t.Errorf("kkm 30+000: %v %v", span, ok)
	}
	if _, ok := rasponPokriva(models.PartEmbankment{WaterFrom: km(24)}, "kkm", 30); ok {
		t.Error("nasip bez kraja raspona ne pokriva ništa")
	}
}
