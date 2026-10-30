package models

import (
	"math"
	"testing"
)

func TestHasWatercourse(t *testing.T) {
	if (Station{Watercourse: "  "}).HasWatercourse() || (Station{}).HasWatercourse() {
		t.Error("prazna voda je utvrđena")
	}
	if !(Station{Watercourse: "Dunav"}).HasWatercourse() {
		t.Error("Dunav nije utvrđen")
	}
}

// Država iz zagrade u nazivu ima prednost pred domenom javne adrese; bez
// oboje letva je domaća.
func TestZemljaIzNazivaIAdrese(t *testing.T) {
	for _, s := range []struct {
		ime  string
		st   Station
		want string
	}{
		{"zagrada pobjeđuje adresu", Station{Name: "Bezdan (Srbija)", JavniURL: "https://www.vizugy.hu/x"}, "Srbija"},
		{"izvorni naziv", Station{Name: "Bezdan", SourceName: "Bezdan (Serbia)"}, "Srbija"},
		{"mađarska domena", Station{JavniURL: "https://ddvizig.hu"}, "Mađarska"},
		{"srpska domena", Station{JavniURL: "https://primjer.rs"}, "Srbija"},
		{"arso", Station{JavniURL: "https://www.arso.gov.si/vode/"}, "Slovenija"},
		{"austrijska domena", Station{JavniURL: "https://www.noel.gv.at/wasserstand/"}, "Austrija"},
		{"austrijska domena na kraju", Station{JavniURL: "https://pegel.at"}, "Austrija"},
		{"bosanska domena", Station{JavniURL: "https://voda.ba/"}, "Bosna i Hercegovina"},
		{"crnogorska domena", Station{JavniURL: "https://meteo.co.me/"}, "Crna Gora"},
		{"njemačka domena", Station{JavniURL: "https://www.gkd.bayern.de/de/fluesse"}, "Njemačka"},
		{"velika slova", Station{JavniURL: "HTTPS://WWW.SHMU.SK/"}, "Slovačka"},
		{"domaća adresa", Station{JavniURL: "https://vodostaji.voda.hr/"}, "Hrvatska"},
		{"bez ičega", Station{Name: "Batina"}, "Hrvatska"},
	} {
		if got := s.st.Zemlja(); got != s.want {
			t.Errorf("%s: %q, očekivano %q", s.ime, got, s.want)
		}
		if got := s.st.JeInozemna(); got != (s.want != "Hrvatska") {
			t.Errorf("%s: JeInozemna %v", s.ime, got)
		}
	}
}

func TestPrepoznajZemljuIzTeksta(t *testing.T) {
	for _, s := range []struct{ ulaz, want string }{
		{"Mohács (Hungary)", "Mađarska"},
		{"x (Magyarország)", "Mađarska"},
		{"x (magyarorszag)", "Mađarska"},
		{"x (HU)", "Mađarska"},
		{"x (Slovensko)", "Slovačka"},
		{"x (Slovenia)", "Slovenija"},
		{"x (Österreich)", "Austrija"},
		{"x (osterreich)", "Austrija"},
		{"x (BiH)", "Bosna i Hercegovina"},
		{"x (bosnia and herzegovina)", "Bosna i Hercegovina"},
		{"x (Montenegro)", "Crna Gora"},
		{"x (Deutschland)", "Njemačka"},
		{"x (Italia)", "Italija"},
		{"x ( Croatia )", "Hrvatska"},
		{"Mohács (rkm 1446) (Mađarska)", "Mađarska"},
		{"Dunav (Dunav / Mađarska)", "Mađarska"},
		{"x (HR-SRB)", "Hrvatska"},
		{"x (rkm 12-14)", ""},
		{"Batina", ""},
		{"Bezdan (Srbija", ""},
		{"x (nepoznato)", ""},
	} {
		if got := prepoznajZemljuIzTeksta(s.ulaz); got != s.want {
			t.Errorf("prepoznajZemljuIzTeksta(%q) = %q, očekivano %q", s.ulaz, got, s.want)
		}
	}
}

// Novi sustav dolazi prvi; bez upisanog sustava vrijede zadani nazivi.
func TestKoteVodnePlohe(t *testing.T) {
	if k := (Station{}).Kote(250); k != nil {
		t.Errorf("letva bez kote nule: %v", k)
	}
	if (Station{}).ImaKotuNule() {
		t.Error("letva bez kote nule ima kotu")
	}
	st := Station{ZeroDatumNew: broj(82.0), ZeroDatum: broj(81.739)}
	k := st.Kote(250)
	if len(k) != 2 || !st.ImaKotuNule() {
		t.Fatalf("kote %v", k)
	}
	if k[0].Sustav != "HVRS71" || !k[0].Nova || math.Abs(k[0].Kota-84.5) > 1e-9 {
		t.Errorf("novi sustav %+v", k[0])
	}
	if k[1].Sustav != "TRST" || k[1].Nova || math.Abs(k[1].Kota-84.239) > 1e-9 {
		t.Errorf("stari sustav %+v", k[1])
	}
	if got := st.RazlikaVisinskihSustava(); math.Abs(got-(-0.261)) > 1e-9 {
		t.Errorf("razlika sustava %v, očekivano -0,261", got)
	}

	samoStara := Station{ZeroDatum: broj(81.739), ZeroDatumSystem: "BALT"}
	if k := samoStara.Kote(-20); len(k) != 1 || k[0].Sustav != "BALT" || math.Abs(k[0].Kota-81.539) > 1e-9 {
		t.Errorf("samo stara kota: %v", k)
	}
	if samoStara.RazlikaVisinskihSustava() != 0 {
		t.Error("razlika s jednim sustavom nije nula")
	}
	if k := (Station{ZeroDatumNew: broj(82), ZeroDatumNewSystem: "HVRS71-2"}).Kote(0); len(k) != 1 || k[0].Sustav != "HVRS71-2" {
		t.Errorf("vlastiti naziv novog sustava: %v", k)
	}
}

func TestOznakeVrijednostiIKvalitete(t *testing.T) {
	if got := (StationReturnLevel{}).Label(); got != "—" {
		t.Errorf("povratni bez vrijednosti: %q", got)
	}
	if got := (StationReturnLevel{LevelCm: ptr(350)}).Label(); got != "+350 cm" {
		t.Errorf("povratni: %q", got)
	}
	if got := (StationExtreme{}).Label(); got != "—" {
		t.Errorf("ekstrem bez vrijednosti: %q", got)
	}
	if got := (StationExtreme{LevelCm: ptr(-127)}).Label(); got != "-127 cm" {
		t.Errorf("ekstrem: %q", got)
	}
	for q, want := range map[string]string{
		QualityReconstructed: "rekonstruirano", QualityUncertain: "sumnjivo", QualityMeasured: "izmjereno", "": "izmjereno",
	} {
		if got := QualityLabel(q); got != want {
			t.Errorf("QualityLabel(%q) = %q, očekivano %q", q, got, want)
		}
	}
}

// Batina: izmjereno i preračunato vode se odvojeno; sumnjiva vrijednost
// nije mjerenje, a zapis bez broja ne odlučuje ništa.
func TestEkstremiIzmjereniIRekonstruirani(t *testing.T) {
	st := Station{Extremes: []StationExtreme{
		{Kind: ExtremeMax, LevelCm: ptr(775), OnDate: "2013-06-14"},
		{Kind: ExtremeMax, LevelCm: ptr(795), OnDate: "1965-06-24", Quality: QualityReconstructed},
		{Kind: ExtremeMax, OnDate: "1926"},
		{Kind: ExtremeMax, LevelCm: ptr(700), Quality: QualityMeasured},
		{Kind: ExtremeMin, LevelCm: ptr(-100), OnDate: "2026-10-01", Quality: QualityMeasured},
		{Kind: ExtremeMin, LevelCm: ptr(-127), OnDate: "1909-01-07", Quality: QualityReconstructed},
		{Kind: ExtremeMin, LevelCm: ptr(-140), Quality: QualityUncertain},
		{Kind: ExtremeMin},
	}}
	provjeri := func(ime string, e *StationExtreme, cm int, dan string) {
		t.Helper()
		if e == nil || *e.LevelCm != cm || e.OnDate != dan {
			t.Errorf("%s: %+v, očekivano %d (%s)", ime, e, cm, dan)
		}
	}
	provjeri("najviše izmjereno", st.NajviseIzmjereno(), 775, "2013-06-14")
	provjeri("najviše zabilježeno", st.NajviseZabiljezeno(), 795, "1965-06-24")
	provjeri("najniže izmjereno", st.NajnizeIzmjereno(), -100, "2026-10-01")
	provjeri("najniže rekonstruirano", st.NajnizeRekonstruirano(), -140, "")
	if !st.ImaNajnize() || !st.RekordSeRazlikuje() {
		t.Error("Batina ima najniže i rekord koji se razlikuje")
	}
	if e := st.NajviseIzmjereno(); e != &st.Extremes[0] {
		t.Error("vraćen je primjerak, ne zapis letve")
	}

	samoIzmjereno := Station{Extremes: []StationExtreme{{Kind: ExtremeMax, LevelCm: ptr(775)}}}
	if samoIzmjereno.RekordSeRazlikuje() || samoIzmjereno.ImaNajnize() {
		t.Error("letva s jednim izmjerenim maksimumom")
	}
	samoRekonstruirano := Station{Extremes: []StationExtreme{
		{Kind: ExtremeMax, LevelCm: ptr(795), Quality: QualityReconstructed},
		{Kind: ExtremeMin, LevelCm: ptr(-127), Quality: QualityReconstructed},
	}}
	if samoRekonstruirano.NajviseIzmjereno() != nil || samoRekonstruirano.RekordSeRazlikuje() {
		t.Error("bez izmjerenog maksimuma nema usporedbe rekorda")
	}
	if samoRekonstruirano.NajnizeIzmjereno() != nil || !samoRekonstruirano.ImaNajnize() {
		t.Error("rekonstruirani minimum je ipak najniži zabilježeni")
	}
	var prazna Station
	if prazna.NajviseIzmjereno() != nil || prazna.NajviseZabiljezeno() != nil || prazna.NajnizeIzmjereno() != nil ||
		prazna.NajnizeRekonstruirano() != nil || prazna.ImaNajnize() || prazna.RekordSeRazlikuje() {
		t.Error("letva bez ekstrema")
	}
}

func TestKoordinateLetveIKarta(t *testing.T) {
	var bez Station
	if bez.ImaKoordinate() || bez.KoordinateHR() != "" || bez.KartaURL() != "" {
		t.Error("letva bez koordinata")
	}
	if (Station{Latitude: broj(45.8)}).ImaKoordinate() {
		t.Error("samo širina nije položaj")
	}
	st := Station{Latitude: broj(45.854722), Longitude: broj(18.840278)}
	if got := st.KoordinateHR(); got != "45° 51′ 17″ S  18° 50′ 25″ I" {
		t.Errorf("koordinate %q", got)
	}
	if got := st.KartaURL(); got != "https://www.openstreetmap.org/?mlat=45.854722&mlon=18.840278#map=16/45.854722/18.840278" {
		t.Errorf("karta %q", got)
	}
	jug := Station{Latitude: broj(-33.5), Longitude: broj(-70.25)}
	if got := jug.KoordinateHR(); got != "33° 30′ 0″ J  70° 15′ 0″ Z" {
		t.Errorf("južna i zapadna polutka %q", got)
	}
}

// Ograda vrijedi za svoj izvor i veličinu, i samo za vrijednosti unutar
// svojih granica (granice nisu uključene).
func TestOgradaNizaVrijediZa(t *testing.T) {
	o := OgradaNiza{Izvor: "letva-dhmz", Velicina: "vodostaj"}
	for _, s := range []struct {
		izvor, velicina string
		want            bool
	}{
		{"letva-dhmz", "vodostaj", true}, {"LETVA-DHMZ", "Vodostaj", true}, {"letva-hv", "vodostaj", false}, {"letva-dhmz", "protok", false},
	} {
		if got := o.VrijediZa(s.izvor, s.velicina); got != s.want {
			t.Errorf("VrijediZa(%q, %q) = %v", s.izvor, s.velicina, got)
		}
	}
	if !(OgradaNiza{Izvor: "his2000"}).VrijediZa("his2000", "protok") {
		t.Error("ograda bez veličine vrijedi za sve veličine izvora")
	}

	for _, s := range []struct {
		ime    string
		o      OgradaNiza
		v      float64
		want   bool
		raspon string
	}{
		{"bez granica", OgradaNiza{}, 1e6, true, ""},
		{"ispod", OgradaNiza{Ispod: broj(100)}, 99, true, "ispod 100"},
		{"na donjoj granici", OgradaNiza{Ispod: broj(100)}, 100, false, "ispod 100"},
		{"iznad", OgradaNiza{Iznad: broj(500)}, 501, true, "iznad 500"},
		{"na gornjoj granici", OgradaNiza{Iznad: broj(500)}, 500, false, "iznad 500"},
		{"u rasponu", OgradaNiza{Iznad: broj(50), Ispod: broj(100.5)}, 75, true, "50 – 100.5"},
		{"izvan raspona", OgradaNiza{Iznad: broj(50), Ispod: broj(100.5)}, 40, false, "50 – 100.5"},
	} {
		if got := s.o.VrijediZaVrijednost(s.v); got != s.want {
			t.Errorf("%s: VrijediZaVrijednost(%v) = %v", s.ime, s.v, got)
		}
		if got := s.o.Raspon(); got != s.raspon {
			t.Errorf("%s: raspon %q, očekivano %q", s.ime, got, s.raspon)
		}
		if got := s.o.ImaGranice(); got != (s.raspon != "") {
			t.Errorf("%s: ImaGranice %v", s.ime, got)
		}
	}
}

func TestRazdobljeOgradeIKrajnosti(t *testing.T) {
	for _, s := range []struct{ od, do, want string }{
		{"2017-01-01", "2017-02-15", "2017-01-01 – 2017-02-15"},
		{"2017-01-01", "", "od 2017-01-01"},
		{"", "2017-02-15", "do 2017-02-15"},
		{"", "", ""},
	} {
		if got := (OgradaNiza{Od: s.od, Do: s.do}).Razdoblje(); got != s.want {
			t.Errorf("Razdoblje(%q, %q) = %q", s.od, s.do, got)
		}
	}
	if (Ograda{}).ImaGranice() || !(Ograda{Raspon: "ispod 100"}).ImaGranice() {
		t.Error("Ograda.ImaGranice ne prati raspon")
	}
	if (KrajnostIzNiza{Izvor: "preracun-mohacs"}).JeMjerena() || !(KrajnostIzNiza{Izvor: "his2000"}).JeMjerena() {
		t.Error("JeMjerena ne razlikuje preračun")
	}
	if (KrajnostIzNiza{Kind: ExtremeMin}).Naslov() != "najniži" || (KrajnostIzNiza{Kind: ExtremeMax}).Naslov() != "najviši" {
		t.Error("naslov krajnosti")
	}
}

func TestSazetakINapomenaEkstrema(t *testing.T) {
	st := Station{Extremes: []StationExtreme{
		{Kind: ExtremeMax, OnDate: "1926", Note: "provjeriti"},
		{Kind: ExtremeMax, LevelCm: ptr(775), OnDate: "2013-06-14", Note: "provjeriti"},
		{Kind: ExtremeMin, LevelCm: ptr(-100), Note: "provjeriti"},
	}}
	if got := st.SazetakEkstrema(); got != "najviši +775 cm (2013-06-14) · najniži -100 cm" {
		t.Errorf("sažetak %q", got)
	}
	if got := st.ZajednickaNapomenaEkstrema(); got != "provjeriti" {
		t.Errorf("zajednička napomena %q", got)
	}
	st.Extremes[2].Note = "drugo"
	if got := st.ZajednickaNapomenaEkstrema(); got != "" {
		t.Errorf("različite napomene %q", got)
	}
	if got := (Station{Extremes: []StationExtreme{{Kind: ExtremeMax}}}).SazetakEkstrema(); got != "" {
		t.Errorf("ekstremi bez broja %q", got)
	}
}
