package models

import (
	"strconv"
	"testing"
	"time"
)

func TestSkupineIObliciSredstava(t *testing.T) {
	for _, s := range []struct{ id, naziv, rimski string }{
		{GrupaOprema, "Oprema", "I"},
		{GrupaAlat, "Alat", "II"},
		{GrupaMaterijal, "Materijal", "III"},
		{GrupaPribor, "Pribor i osobna zaštitna sredstva", "IV"},
		{"NOVA", "NOVA", ""},
	} {
		if got := GrupaNaziv(s.id); got != s.naziv {
			t.Errorf("GrupaNaziv(%q) = %q, očekivano %q", s.id, got, s.naziv)
		}
		if got := GrupaRimski(s.id); got != s.rimski {
			t.Errorf("GrupaRimski(%q) = %q, očekivano %q", s.id, got, s.rimski)
		}
		if got := PostojiGrupa(s.id); got != (s.rimski != "") {
			t.Errorf("PostojiGrupa(%q) = %v", s.id, got)
		}
	}
	for o, want := range map[string]string{
		OblikPrazno: "prazno", OblikPunjeno: "napunjeno", OblikPostavljeno: "postavljeno", OblikNeispravno: "neispravno",
		OblikOsnovni: "osnovno", "NOVO": "osnovno",
	} {
		if got := OblikNaziv(o); got != want {
			t.Errorf("OblikNaziv(%q) = %q, očekivano %q", o, got, want)
		}
	}
}

// Sredstvo bez oblika broji se kao jedan prazan oblik.
func TestObliciVrsteSredstva(t *testing.T) {
	bez := VrstaSredstva{}
	if o := bez.SviOblici(); len(o) != 1 || o[0] != OblikOsnovni || bez.ImaOblike() {
		t.Errorf("vrsta bez oblika: %q, %v", o, bez.ImaOblike())
	}
	vrece := VrstaSredstva{Oblici: []string{OblikPrazno, OblikPunjeno}}
	if o := vrece.SviOblici(); len(o) != 2 || o[1] != OblikPunjeno || !vrece.ImaOblike() {
		t.Errorf("vreće: %q", o)
	}
}

func TestPunNazivSkladista(t *testing.T) {
	if got := (Skladiste{Naziv: "Nemetin"}).PunNaziv(); got != "Nemetin" {
		t.Errorf("bez područja %q", got)
	}
	if got := (Skladiste{Naziv: "Nemetin", AreaID: 15, AreaName: "Mali sliv Vuka"}).PunNaziv(); got != "BP 15 – Mali sliv Vuka · Nemetin" {
		t.Errorf("s područjem %q", got)
	}
}

// itoa piše brojeve područja i godine; provjerava se do 12 znamenki, koliko
// stane u njegov međuspremnik.
func TestItoaKaoStrconv(t *testing.T) {
	for _, n := range []int{0, 1, 7, 10, 15, 1234, 2026, -1, -56, 999999999999, -99999999999} {
		if got := itoa(n); got != strconv.Itoa(n) {
			t.Errorf("itoa(%d) = %q", n, got)
		}
	}
}

func TestNazivPrometaIStrane(t *testing.T) {
	for _, s := range []struct{ id, naziv, strana string }{
		{PrometPrimka, "Primka", "Dopremio"},
		{PrometIzdano, "Izdano na teren", "Preuzeo"},
		{PrometPrijenos, "Prijenos", "Preuzeo"},
		{PrometPovrat, "Povrat s terena", "Vratio"},
		{PrometUtrosak, "Utrošak na terenu", "Ugradio"},
		{PrometOtpis, "Otpis", "Rashodovao"},
		{PrometPunjenje, "Punjenje", "Osoba"},
		{PrometPopis, "Usklađenje po inventuri", "Osoba"},
		{PrometPocetno, "Početno stanje", "Osoba"},
		{"NOVO", "NOVO", "Osoba"},
	} {
		if got := PrometNaziv(s.id); got != s.naziv {
			t.Errorf("PrometNaziv(%q) = %q, očekivano %q", s.id, got, s.naziv)
		}
		if got := StranaOznaka(s.id); got != s.strana {
			t.Errorf("StranaOznaka(%q) = %q, očekivano %q", s.id, got, s.strana)
		}
	}
}

func TestMjestoTerena(t *testing.T) {
	m := MjestoTerena{AreaID: 15, SectionCode: "B.15.1", StructureID: "cs-1", Mjesto: "kuća iza groblja"}
	if got := m.Kljuc(); got != "15|B.15.1|cs-1|kuća iza groblja" {
		t.Errorf("ključ %q", got)
	}
	if got := (MjestoTerena{}).Kljuc(); got != "0|||" {
		t.Errorf("ključ praznog mjesta %q", got)
	}
	if m.Prazno() || !(MjestoTerena{}).Prazno() {
		t.Error("Prazno ne prati opis mjesta")
	}
	for _, d := range []MjestoTerena{{AreaID: 1}, {SectionCode: "B.1"}, {StructureID: "s"}, {Mjesto: "m"}} {
		if d.Prazno() {
			t.Errorf("mjesto %+v je opisano", d)
		}
	}

	p := Promet{AreaID: 15, SectionCode: "B.15.1", StructureID: "cs-1", Mjesto: "kuća iza groblja"}
	if p.Teren() != m || !p.NaTerenu() {
		t.Errorf("teren prometa %+v", p.Teren())
	}
	st := Stanje{AreaID: 15, SectionCode: "B.15.1", StructureID: "cs-1", Mjesto: "kuća iza groblja"}
	if st.Teren() != m {
		t.Errorf("teren stanja %+v", st.Teren())
	}
}

func TestMjestoNazivPrometaIStanja(t *testing.T) {
	for _, s := range []struct {
		ime  string
		p    Promet
		want string
	}{
		{"skladište s nazivom", Promet{SkladisteID: "sk-1", SkladisteNaziv: "Nemetin", AreaID: 15}, "Nemetin"},
		{"skladište bez naziva", Promet{SkladisteID: "sk-1"}, "skladište"},
		{"cijeli teren", Promet{AreaID: 15, AreaName: "Mali sliv Vuka", SectionCode: "B.15.1", StructureName: "CS Nemetin", Mjesto: "kuća iza groblja"},
			"teren · BP 15 Mali sliv Vuka · B.15.1 · CS Nemetin · kuća iza groblja"},
		{"područje bez naziva", Promet{AreaID: 15}, "teren · BP 15"},
		{"samo opis", Promet{Mjesto: "Kopačevo"}, "teren · Kopačevo"},
	} {
		if got := s.p.MjestoNaziv(); got != s.want {
			t.Errorf("%s: %q, očekivano %q", s.ime, got, s.want)
		}
	}
	if (Promet{SkladisteID: "sk-1"}).NaTerenu() {
		t.Error("promet skladišta je na terenu")
	}
	if got := (Stanje{}).MjestoNaziv(); got != "teren, nije razvrstano" {
		t.Errorf("neopisano stanje %q", got)
	}
	if got := (Stanje{AreaID: 15, AreaName: "Mali sliv Vuka", SectionCode: "B.15.1"}).MjestoNaziv(); got != "BP 15 Mali sliv Vuka · B.15.1" {
		t.Errorf("stanje na dionici %q", got)
	}
}

func TestStanjeVrste(t *testing.T) {
	s := StanjeVrste{PoOblicima: map[string]float64{OblikPrazno: 300, OblikPunjeno: 120}}
	if s.Oblik(OblikPrazno) != 300 || s.Oblik(OblikNeispravno) != 0 {
		t.Error("količina po obliku")
	}
	if (StanjeVrste{}).Oblik(OblikPrazno) != 0 {
		t.Error("stanje bez oblika")
	}
	for _, x := range []struct {
		ukupno, potrebno float64
		want             bool
	}{{0, 0, false}, {5, 0, true}, {0, 10, true}, {-2, 0, true}} {
		if got := (StanjeVrste{Ukupno: x.ukupno, Potrebno: x.potrebno}).Ima(); got != x.want {
			t.Errorf("Ima(%v, %v) = %v", x.ukupno, x.potrebno, got)
		}
	}
}

// Popis: razlika višak/manjak po stavci, broj stavki koje odstupaju, a
// prazan je popis bez ijedne prebrojane količine i potrebe.
func TestPopisSkladista(t *testing.T) {
	p := Popis{
		Dan: time.Date(2026, 12, 30, 23, 0, 0, 0, time.UTC),
		Stavke: []PopisnaStavka{
			{VrstaID: "vrece", Oblik: OblikPrazno, Utvrdjeno: 280, Knjizno: 300},
			{VrstaID: "vrece", Oblik: OblikPunjeno, Utvrdjeno: 120, Knjizno: 120},
			{VrstaID: "lopate", Utvrdjeno: 12, Knjizno: 10, Potrebno: 5},
		},
	}
	if got := p.Stavke[0].Razlika(); got != -20 {
		t.Errorf("manjak %v", got)
	}
	if got := p.Stavke[2].Razlika(); got != 2 {
		t.Errorf("višak %v", got)
	}
	if got := p.Razlika(); got != 2 {
		t.Errorf("stavki s razlikom %d, očekivano 2", got)
	}
	if got := p.DanKey(); got != "2026-12-31" {
		t.Errorf("dan popisa po zagrebačkom vremenu %q", got)
	}
	if p.Zakljucen() || p.Prazan() {
		t.Error("otvoren popis s količinama")
	}
	kad := time.Date(2027, 1, 5, 10, 0, 0, 0, Zagreb)
	p.ZakljucenoAt = &kad
	if !p.Zakljucen() {
		t.Error("zaključen popis")
	}

	if s := p.StavkaZa("vrece", OblikPunjeno); s.Utvrdjeno != 120 || s.Knjizno != 120 {
		t.Errorf("nađena stavka %+v", s)
	}
	if s := p.StavkaZa("vrece", OblikNeispravno); s.VrstaID != "vrece" || s.Oblik != OblikNeispravno || s.Utvrdjeno != 0 {
		t.Errorf("stavka koje nema %+v", s)
	}

	if !(Popis{}).Prazan() || (Popis{}).Razlika() != 0 {
		t.Error("popis bez stavki")
	}
	samoKnjiga := Popis{Stavke: []PopisnaStavka{{VrstaID: "vrece", Knjizno: 300}}}
	if !samoKnjiga.Prazan() || samoKnjiga.Razlika() != 1 {
		t.Error("popis bez prebrojanih količina je prazan, a odstupa od knjige")
	}
	if (Popis{Stavke: []PopisnaStavka{{Potrebno: 1}}}).Prazan() {
		t.Error("popis s potrebom nije prazan")
	}
}

func TestSortirajVrste(t *testing.T) {
	v := []VrstaSredstva{
		{ID: "lopata", Grupa: GrupaAlat, Redoslijed: 2, Naziv: "Lopata"},
		{ID: "cizme", Grupa: GrupaPribor, Redoslijed: 1, Naziv: "Čizme"},
		{ID: "pumpa", Grupa: GrupaOprema, Redoslijed: 1, Naziv: "Pumpa"},
		{ID: "vrece-b", Grupa: GrupaMaterijal, Redoslijed: 1, Naziv: "Vreće 60x100"},
		{ID: "vrece-a", Grupa: GrupaMaterijal, Redoslijed: 1, Naziv: "vreće 50x80"},
		{ID: "kramp", Grupa: GrupaAlat, Redoslijed: 1, Naziv: "Kramp"},
	}
	SortirajVrste(v)
	want := []string{"pumpa", "kramp", "lopata", "vrece-a", "vrece-b", "cizme"}
	for i, id := range want {
		if v[i].ID != id {
			t.Fatalf("redoslijed %v, očekivano %v", ids(v), want)
		}
	}
	SortirajVrste(nil)
}

func ids(v []VrstaSredstva) []string {
	out := make([]string, len(v))
	for i := range v {
		out[i] = v[i].ID
	}
	return out
}

func TestOznakaSredstva(t *testing.T) {
	for _, s := range []struct{ naziv, want string }{
		{"Vreće 50x80 cm", "vrece-50x80-cm"},
		{"  Jumbo vreće (1 m³)  ", "jumbo-vrece-1-m"},
		{"ŠTAP, ĐON i ŽICA", "stap-don-i-zica"},
		{"Čizme  gumene", "cizme-gumene"},
		{"Ćuskija", "cuskija"},
		{"---", ""},
		{"", ""},
		{"Box barijera 1,2 m", "box-barijera-1-2-m"},
	} {
		if got := OznakaSredstva(s.naziv); got != s.want {
			t.Errorf("OznakaSredstva(%q) = %q, očekivano %q", s.naziv, got, s.want)
		}
	}
}

// Stanje na 31. prosinca nosi potrebe za iduću godinu, inače za tekuću.
func TestPotrebeNabave(t *testing.T) {
	if got := PotrebaID("sk-1", 2027, "vrece-50x80"); got != "sk-1/2027/vrece-50x80" {
		t.Errorf("ključ potrebe %q", got)
	}
	for _, s := range []struct {
		dan  time.Time
		want int
	}{
		{time.Date(2026, 12, 31, 0, 0, 0, 0, Zagreb), 2027},
		{time.Date(2026, 12, 31, 23, 59, 0, 0, Zagreb), 2027},
		{time.Date(2026, 12, 30, 0, 0, 0, 0, Zagreb), 2026},
		{time.Date(2027, 1, 1, 0, 0, 0, 0, Zagreb), 2027},
		{time.Date(2026, 6, 15, 0, 0, 0, 0, Zagreb), 2026},
	} {
		if got := GodinaPotreba(s.dan); got != s.want {
			t.Errorf("GodinaPotreba(%s) = %d, očekivano %d", s.dan.Format("2006-01-02"), got, s.want)
		}
	}
}
