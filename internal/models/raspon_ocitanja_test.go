package models

import (
	"math"
	"strings"
	"testing"
	"time"
)

// Odluke (8. 10. 2026.), pitanje 21: iste granice kao ručni upis za sve
// putove, uz raspon kote za letve akumulacija HEP-a.

func cmR(v int) *int          { return &v }
func broj(v float64) *float64 { return &v }

func TestRasponVodostajaIKote(t *testing.T) {
	if od, do := RasponVodostaja(false); od != -500 || do != 3000 {
		t.Errorf("vodostaj %d..%d", od, do)
	}
	if od, do := RasponVodostaja(true); od != 5000 || do != 40000 {
		t.Errorf("kota %d..%d", od, do)
	}
	for _, s := range []struct {
		v    float64
		kota bool
		ok   bool
	}{
		{-500, false, true}, {3000, false, true}, {-500.5, false, false}, {3001, false, false},
		{5000, true, true}, {40000, true, true}, {4999, true, false}, {300, true, false},
		{1e20, false, false}, {math.NaN(), false, false}, {math.Inf(-1), true, false},
	} {
		if VodostajURasponu(s.v, s.kota) != s.ok {
			t.Errorf("%v (kota %v): očekivano %v", s.v, s.kota, s.ok)
		}
	}
}

func TestVrijemeIzvanRaspona(t *testing.T) {
	sada := time.Date(2026, 10, 8, 12, 0, 0, 0, Zagreb)
	if o := VrijemeIzvanRaspona(sada.Add(59*time.Minute), sada); o != nil {
		t.Errorf("sat unaprijed je razlika satova: %v", o)
	}
	o := VrijemeIzvanRaspona(sada.Add(61*time.Minute), sada)
	if o == nil || o.Polje != PoljeVrijeme || o.Error() != "vrijeme očitanja ne može biti u budućnosti" || o.Opis != "vrijeme 8. 10. 2026. 13:01" {
		t.Errorf("budućnost: %+v", o)
	}
	o = VrijemeIzvanRaspona(time.Date(1899, 12, 31, 7, 0, 0, 0, time.UTC), sada)
	if o == nil || o.Error() != "vrijeme očitanja nije vjerojatno" {
		t.Errorf("1899.: %+v", o)
	}
	if o := VrijemeIzvanRaspona(time.Date(1900, 1, 1, 7, 0, 0, 0, time.UTC), sada); o != nil {
		t.Errorf("1900.: %v", o)
	}
}

func TestVrijednostiIzvanRaspona(t *testing.T) {
	rd := Reading{LevelCm: cmR(-1102), Level2Cm: cmR(3500), TempC: broj(85), FlowM3s: broj(-3)}
	izvan := VrijednostiIzvanRaspona(rd, false)
	ocekivano := []struct{ polje, opis, poruka string }{
		{PoljeVodostaj, "−1102 cm", "vodostaj -1102 cm je izvan razumnog raspona"},
		{PoljeNizvodni, "3500 cm nizvodno", "vodostaj 3500 cm je izvan razumnog raspona"},
		{PoljeTemperatura, "temperatura 85,0 °C", "temperatura vode 85.0 °C je izvan razumnog raspona"},
		{PoljeProtok, "protok −3,0 m³/s", "protok -3.0 m³/s je izvan razumnog raspona"},
	}
	if len(izvan) != len(ocekivano) {
		t.Fatalf("odbačeno %d: %+v", len(izvan), izvan)
	}
	for i, o := range ocekivano {
		if izvan[i].Polje != o.polje || izvan[i].Opis != o.opis || izvan[i].Error() != o.poruka {
			t.Errorf("%d: %+v (%s), očekivano %+v", i, izvan[i], izvan[i].Error(), o)
		}
	}
	// letva akumulacije: kota u cm n. m., vlastiti raspon
	kota := VrijednostiIzvanRaspona(Reading{LevelCm: cmR(300)}, true)
	if len(kota) != 1 || kota[0].Polje != PoljeKota || kota[0].Opis != "300 cm n. m." ||
		kota[0].Error() != "kota 300 cm n. m. je izvan razumnog raspona" {
		t.Errorf("kota: %+v", kota)
	}
	if izvan := VrijednostiIzvanRaspona(Reading{LevelCm: cmR(19089)}, true); len(izvan) != 0 {
		t.Errorf("kota akumulacije Varaždin je moguća: %+v", izvan)
	}
	// rubovi i NaN
	if izvan := VrijednostiIzvanRaspona(Reading{LevelCm: cmR(-500), Level2Cm: cmR(3000), TempC: broj(-5), FlowM3s: broj(100000)}, false); len(izvan) != 0 {
		t.Errorf("rubovi su mogući: %+v", izvan)
	}
	if izvan := VrijednostiIzvanRaspona(Reading{TempC: broj(math.NaN()), FlowM3s: broj(math.NaN())}, false); len(izvan) != 2 {
		t.Errorf("NaN nije broj: %+v", izvan)
	}
	if izvan := VrijednostiIzvanRaspona(Reading{}, false); len(izvan) != 0 {
		t.Errorf("prazno očitanje: %+v", izvan)
	}
}

// Broje se vrijednosti izvan raspona, a ne ostale vrijednosti istog očitanja
func TestIzvanRasponaOcitanja(t *testing.T) {
	sada := time.Now()
	prije := sada.Add(-time.Hour)

	if izvan := IzvanRasponaOcitanja(Reading{MeasuredAt: prije, LevelCm: cmR(120)}, false, sada); izvan != nil {
		t.Errorf("ispravno očitanje: %+v", izvan)
	}
	// zaglavljeno mjerilo s protokom: broji se samo vodostaj
	izvan := IzvanRasponaOcitanja(Reading{MeasuredAt: prije, LevelCm: cmR(-1104), FlowM3s: broj(0)}, false, sada)
	if len(izvan) != 1 || izvan[0].Opis != "−1104 cm" {
		t.Errorf("zaglavljeno: %+v", izvan)
	}
	// letva akumulacije: kota prolazi, vodostaj ne
	if izvan := IzvanRasponaOcitanja(Reading{MeasuredAt: prije, LevelCm: cmR(19089)}, true, sada); izvan != nil {
		t.Errorf("kota: %+v", izvan)
	}
	if izvan := IzvanRasponaOcitanja(Reading{MeasuredAt: prije, LevelCm: cmR(300)}, true, sada); len(izvan) != 1 || izvan[0].Polje != PoljeKota {
		t.Errorf("vodostaj na akumulaciji: %+v", izvan)
	}
	// vrijeme izvan raspona: ostalo se ne gleda
	izvan = IzvanRasponaOcitanja(Reading{MeasuredAt: sada.Add(3 * time.Hour), LevelCm: cmR(120), TempC: broj(85)}, false, sada)
	if len(izvan) != 1 || izvan[0].Polje != PoljeVrijeme {
		t.Errorf("buduće: %+v", izvan)
	}
	if izvan := IzvanRasponaOcitanja(Reading{LevelCm: cmR(120)}, false, sada); len(izvan) != 1 || izvan[0].Polje != PoljeVrijeme {
		t.Errorf("bez vremena: %+v", izvan)
	}
}

// Putovi bez čovjeka koji bi grešku ispravio (preuzimanje, uvoz) ne
// upisuju vrijednost izvan raspona (Odluke (8. 10. 2026.), pitanje 21).
//
// Dorada: vodostaj (uzvodni ili nizvodni) ili kota izvan raspona odbacuje
// cijelo očitanje, kao ručni upis. Protok koji izvor računa iz vodostaja
// (vizugy.hu krivuljom, Popovac 0,0 m³/s uz zaglavljenih −1104 cm), sati
// rada agregata i stanje iz istog zapisa BP16 pripadaju krivom zapisu, a
// sat mora ostati slobodan za rezervni izvor i ispravak. Kad je izvan
// raspona samo temperatura ili protok, otpada samo ta vrijednost: pokvaren
// termometar ne odnosi dobar vodostaj.
func TestOcistiOcitanje(t *testing.T) {
	sada := time.Now()
	prije := sada.Add(-time.Hour)

	// sve u rasponu: ništa se ne dira
	rd := Reading{MeasuredAt: prije, LevelCm: cmR(120)}
	if izvan, ok := OcistiOcitanje(&rd, false, sada); izvan != nil || !ok || rd.LevelCm == nil {
		t.Errorf("ispravno očitanje: %+v, %v", izvan, ok)
	}
	// prazno očitanje otprije ostaje kakvo je (to ne odlučuje raspon)
	prazno := Reading{MeasuredAt: prije}
	if izvan, ok := OcistiOcitanje(&prazno, false, sada); izvan != nil || !ok {
		t.Errorf("prazno: %+v, %v", izvan, ok)
	}

	// zaglavljeno mjerilo bez ičega drugog: očitanje otpada
	zaglavljeno := Reading{MeasuredAt: prije, LevelCm: cmR(-1102)}
	izvan, ok := OcistiOcitanje(&zaglavljeno, false, sada)
	if ok || len(izvan) != 1 || izvan[0].Opis != "−1102 cm" {
		t.Errorf("zaglavljeno: %+v, %v", izvan, ok)
	}
	// zaglavljeno mjerilo s protokom izvora: protok je izračunat iz krivog
	// vodostaja, pa otpada s njim; broji se samo vodostaj
	sProtokom := Reading{MeasuredAt: prije, LevelCm: cmR(-1104), FlowM3s: broj(0)}
	if izvan, ok := OcistiOcitanje(&sProtokom, false, sada); ok || len(izvan) != 1 || izvan[0].Polje != PoljeVodostaj {
		t.Errorf("s protokom: %+v, %v, %+v", izvan, ok, sProtokom)
	}
	// vizugy.hu: protok preračunat krivuljom iz vodostaja izvan raspona
	krivulja := Reading{MeasuredAt: prije, LevelCm: cmR(-9999), FlowM3s: broj(10),
		FlowMethod: FlowMethodKrivulja, FlowNote: "preračunat iz vodostaja krivuljom"}
	if izvan, ok := OcistiOcitanje(&krivulja, false, sada); ok || len(izvan) != 1 {
		t.Errorf("protok iz krivulje: %+v, %v, %+v", izvan, ok, krivulja)
	}

	// pokvaren termometar ne odnosi dobar vodostaj; s temperaturom ide i njezina bilješka
	rd = Reading{MeasuredAt: prije, LevelCm: cmR(215), TempC: broj(85), TempNote: "senzor",
		FlowM3s: broj(-1), FlowMethod: FlowMethodKrivulja, FlowNote: "krivulja"}
	izvan, ok = OcistiOcitanje(&rd, false, sada)
	if !ok || len(izvan) != 2 || rd.LevelCm == nil || *rd.LevelCm != 215 || rd.TempC != nil || rd.TempNote != "" ||
		rd.FlowM3s != nil || rd.FlowMethod != "" || rd.FlowNote != "" {
		t.Errorf("djelomično: %+v, %v, %+v", izvan, ok, rd)
	}
	// protok izvan raspona bez vodostaja: ostaje temperatura
	samoTemperatura := Reading{MeasuredAt: prije, TempC: broj(12), FlowM3s: broj(-3)}
	if izvan, ok := OcistiOcitanje(&samoTemperatura, false, sada); !ok || len(izvan) != 1 || samoTemperatura.FlowM3s != nil || samoTemperatura.TempC == nil {
		t.Errorf("samo temperatura: %+v, %v, %+v", izvan, ok, samoTemperatura)
	}
	// samo protok izvan raspona: ništa ne ostaje
	samoProtok := Reading{MeasuredAt: prije, FlowM3s: broj(-3)}
	if _, ok := OcistiOcitanje(&samoProtok, false, sada); ok {
		t.Error("očitanje bez ijedne vrijednosti u rasponu upisalo bi se")
	}

	// nizvodni vodostaj izvan raspona: zapis ustave otpada cijeli, s
	// uzvodnim vodostajem i zapornicom, da ga uvoz upiše kad se ispravi
	ustava := Reading{MeasuredAt: prije, LevelCm: cmR(100), Level2Cm: cmR(-900), Gate: GateClosed}
	if izvan, ok := OcistiOcitanje(&ustava, false, sada); ok || len(izvan) != 1 || izvan[0].Polje != PoljeNizvodni {
		t.Errorf("ustava: %+v, %v, %+v", izvan, ok, ustava)
	}

	// tipfeler u vodostaju crpne stanice: zapis otpada cijeli, sa satima agregata
	sati := 7
	crpna := Reading{MeasuredAt: prije, LevelCm: cmR(21500), AgHours1: &sati, Observer: "Pero Perić"}
	if izvan, ok := OcistiOcitanje(&crpna, false, sada); ok || len(izvan) != 1 {
		t.Errorf("crpna stanica: %+v, %v, %+v", izvan, ok, crpna)
	}
	// ni napomena ne zadržava zapis s vodostajem izvan raspona
	sNapomenom := Reading{MeasuredAt: prije, LevelCm: cmR(21500), Note: "letva pod ledom"}
	if izvan, ok := OcistiOcitanje(&sNapomenom, false, sada); ok || len(izvan) != 1 {
		t.Errorf("s napomenom: %+v, %v", izvan, ok)
	}
	// vodostaj i temperatura izvan raspona: broje se obje vrijednosti
	obje := Reading{MeasuredAt: prije, LevelCm: cmR(-1102), TempC: broj(85)}
	if izvan, ok := OcistiOcitanje(&obje, false, sada); ok || len(izvan) != 2 {
		t.Errorf("vodostaj i temperatura: %+v, %v", izvan, ok)
	}

	// letva akumulacije: kota prolazi, vodostaj ne
	akumulacija := Reading{MeasuredAt: prije, LevelCm: cmR(19089)}
	if izvan, ok := OcistiOcitanje(&akumulacija, true, sada); izvan != nil || !ok {
		t.Errorf("kota: %+v, %v", izvan, ok)
	}
	akumulacija.LevelCm, akumulacija.TempC = cmR(300), broj(12)
	if izvan, ok := OcistiOcitanje(&akumulacija, true, sada); ok || len(izvan) != 1 || izvan[0].Polje != PoljeKota {
		t.Errorf("vodostaj na akumulaciji: %+v, %v", izvan, ok)
	}

	// vrijeme izvan raspona odbacuje cijelo očitanje, i ispravne vrijednosti:
	// sve pripadaju nemogućem trenutku
	buduce := Reading{MeasuredAt: sada.Add(3 * time.Hour), LevelCm: cmR(120), TempC: broj(85)}
	izvan, ok = OcistiOcitanje(&buduce, false, sada)
	if ok || len(izvan) != 1 || izvan[0].Polje != PoljeVrijeme || buduce.LevelCm == nil {
		t.Errorf("buduće: %+v, %v", izvan, ok)
	}
}

func TestImaSadrzaj(t *testing.T) {
	for _, s := range []struct {
		rd  Reading
		ima bool
	}{
		{Reading{}, false},
		{Reading{Note: "   "}, false},
		{Reading{Note: "led"}, true},
		{Reading{StructureState: StructureStateIdle}, true},
		{Reading{Gate: GateOpen}, true},
		{Reading{TempC: broj(4)}, true},
	} {
		if s.rd.ImaSadrzaj() != s.ima {
			t.Errorf("%+v: očekivano %v", s.rd, s.ima)
		}
	}
}

func TestOdbaceneVrijednostiZaStranicuLetve(t *testing.T) {
	kad := time.Date(2026, 10, 6, 1, 0, 0, 0, time.UTC)
	rd := Reading{StationID: "postaja-1", MeasuredAt: kad, Origin: "hdv.voda.hr", LevelCm: cmR(-1102)}
	izvan := VrijednostiIzvanRaspona(rd, false)
	z := OdbaceneVrijednosti(rd, izvan)
	if len(z) != 1 || z[0].Letva != "station:postaja-1" || !z[0].Kad.Equal(kad) || z[0].Polje != PoljeVodostaj ||
		z[0].Opis != "−1102 cm" || z[0].Izvor != "hdv.voda.hr" {
		t.Errorf("zapis: %+v", z)
	}
	if z := OdbaceneVrijednosti(rd, nil); len(z) != 0 {
		t.Errorf("ništa odbačeno: %+v", z)
	}
	s := SazetakOdbacenih{Broj: 50, Zadnja: z[0]}
	if s.IzvorLabel() != "hdv.voda.hr" {
		t.Errorf("izvor: %q", s.IzvorLabel())
	}
	s.Zadnja.Izvor = ReadingOriginBP16
	if !strings.Contains(s.IzvorLabel(), "VGI Baranja") {
		t.Errorf("izvor BP16: %q", s.IzvorLabel())
	}
}

func TestTekstOdbacenihZaStranicuLetve(t *testing.T) {
	sada := time.Date(2026, 10, 6, 14, 0, 0, 0, Zagreb)
	s := SazetakOdbacenih{Broj: 50, Zadnja: OdbacenaVrijednost{Polje: PoljeVodostaj, Opis: "−1102 cm",
		Kad: time.Date(2026, 10, 6, 3, 0, 0, 0, Zagreb)}}
	if got := s.tekst(sada); got != "Odbačeno izvan raspona: 50, zadnje −1102 cm u 03 h" {
		t.Errorf("danas: %q", got)
	}
	s.Zadnja.Kad = time.Date(2026, 10, 5, 23, 0, 0, 0, Zagreb)
	if got := s.tekst(sada); got != "Odbačeno izvan raspona: 50, zadnje −1102 cm 5. 10. 2026. u 23 h" {
		t.Errorf("jučer: %q", got)
	}
	s.Zadnja.Polje, s.Zadnja.Opis = PoljeVrijeme, "vrijeme 3. 1. 2062. 07:00"
	if got := s.tekst(sada); got != "Odbačeno izvan raspona: 50, zadnje vrijeme 3. 1. 2062. 07:00" {
		t.Errorf("vrijeme: %q", got)
	}
	if !strings.HasPrefix(s.Tekst(), "Odbačeno izvan raspona: 50") {
		t.Errorf("tekst: %q", s.Tekst())
	}
}

// Oznaku kote po odluci imaju tri letve akumulacija HEP-a, po šifri
func TestJeLetvaAkumulacijeHEP(t *testing.T) {
	for _, sifra := range []string{"gvb-he-varazdin", "gvb-he-cakovec", "gvb-he-dubrava"} {
		if !JeLetvaAkumulacijeHEP(sifra) {
			t.Errorf("%s nije prepoznata kao letva akumulacije", sifra)
		}
	}
	for _, sifra := range []string{"", "botovo", "gvb-he"} {
		if JeLetvaAkumulacijeHEP(sifra) {
			t.Errorf("%q je prepoznata kao letva akumulacije", sifra)
		}
	}
}
