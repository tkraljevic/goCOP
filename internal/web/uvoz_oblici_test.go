package web

import (
	"testing"
)

// Skupni unos očitanja prima ispis s letva.voda.hr i ono što ljudi sami
// slože u Excelu: CSV s točkom-zarezom i hrvatskim datumom (i s datumom i
// satom u odvojenim stupcima), ćelije u navodnicima, sat sa sekundama.
// Svaki oblik mora dati isti sat i istu vrijednost kao ispis s letve.
func TestUvozPrimaOblikeIzExcelaILetve(t *testing.T) {
	const ocekivano = "2026-11-25 06:00" // 07:00 po zagrebačkom vremenu
	for _, r := range []struct{ ime, redak string }{
		{"letva.voda.hr, zaslon", "25.11.2026. 07 h    512"},
		{"letva.voda.hr, Excel", "25.11.2026. 07:00\t512"},
		{"CSV s točkom-zarezom", "25.11.2026. 07:00;512"},
		{"CSV bez točke iza godine", "25.11.2026 07:00;512"},
		{"CSV, datum i sat u odvojenim stupcima", "25.11.2026.;07:00;512"},
		{"CSV s navodnicima", `"25.11.2026. 07:00";"512"`},
		{"CSV sa sekundama", "25.11.2026. 07:00:00;512"},
		{"CSV s praznim stupcem na kraju", "25.11.2026. 07:00;512;"},
		{"ISO s točkom-zarezom", "2026-11-25 07:00;512"},
	} {
		redci, satni, err := citajZalijepljeno(r.redak)
		if err != nil || len(redci) != 1 || redci[0].Greska != "" {
			t.Errorf("%s: %q nije pročitan (%v %+v)", r.ime, r.redak, err, redci)
			continue
		}
		if got := redci[0].Kad.Format("2006-01-02 15:04"); got != ocekivano || redci[0].Vrijedi != 512 || !satni {
			t.Errorf("%s: %q → %s %v (satni %v)", r.ime, r.redak, got, redci[0].Vrijedi, satni)
		}
	}
}

// Decimalni zarez i minus u CSV-u s točkom-zarezom ostaju dio broja.
func TestUvozCSVDecimalniZarezIMinus(t *testing.T) {
	redci, _, err := citajZalijepljeno("Vrijeme;Vodostaj (cm)\n25.11.2026. 07:00;512,5\n25.11.2026. 08:00;-35\n")
	if err != nil || len(redci) != 2 {
		t.Fatalf("%v %+v", err, redci)
	}
	if redci[0].Vrijedi != 512.5 || redci[1].Vrijedi != -35 {
		t.Errorf("vrijednosti %v i %v", redci[0].Vrijedi, redci[1].Vrijedi)
	}
}

// Excel: izvoz s letva.voda.hr ima redni broj u drugom stupcu i vodostaj u
// trećem; vlastita tablica ima vrijeme i vodostaj. Vrijeme smije biti tekst
// ili pravi Excelov datum (broj dana od 30. 12. 1899.).
func TestUvozExcelaLetveIVlastiteTablice(t *testing.T) {
	for _, s := range []struct {
		ime    string
		redci  [][]string
		ocekuj string
	}{
		{"izvoz s letva.voda.hr", [][]string{
			{"Vrijeme očitanja", "Rbr.", "Dunav - Batina (DHMZ)", "", "", "Vodostaj manji od minimuma"},
			{"25.11.2026. 07:00", "1", "512"}}, "25.11.2026. 07:00\t512\n"},
		{"vlastita tablica, vrijeme kao tekst", [][]string{
			{"Vrijeme", "Vodostaj (cm)"},
			{"25.11.2026. 07:00", "512"}}, "25.11.2026. 07:00\t512\n"},
		{"vlastita tablica, pravi Excelov datum", [][]string{
			{"Datum i vrijeme", "Vodostaj (cm)"},
			{"46351.291666666664", "512"}}, "25.11.2026. 07:00\t512\n"},
		{"vlastita tablica, samo datum", [][]string{
			{"Datum", "Vodostaj (cm)"},
			{"46351", "512"}}, "25.11.2026.\t512\n"},
	} {
		tekst, _, err := xlsxUOcitanja(s.redci)
		if err != nil || tekst != s.ocekuj {
			t.Errorf("%s: %q, %v; očekivano %q", s.ime, tekst, err, s.ocekuj)
			continue
		}
		if redci, _, err := citajZalijepljeno(tekst); err != nil || len(redci) != 1 || redci[0].Greska != "" || redci[0].Vrijedi != 512 {
			t.Errorf("%s: čitač ne prima %q (%v %+v)", s.ime, tekst, err, redci)
		}
	}
}

// Tablica bez naslova vremena ili bez stupca vodostaja javlja što fali.
func TestUvozExcelaKazeStoFali(t *testing.T) {
	if _, _, err := xlsxUOcitanja([][]string{{"25.11.2026. 07:00", "512"}}); err == nil {
		t.Error("tablica bez zaglavlja prošla je bez poruke")
	}
	if _, _, err := xlsxUOcitanja([][]string{{"Vrijeme", "Rbr."}, {"25.11.2026. 07:00", "1"}}); err == nil {
		t.Error("tablica bez stupca vodostaja prošla je bez poruke")
	}
}

// Prazni redci, redci bez vodostaja i sati koje preuzimanje nije stiglo
// preskaču se; tablica u kojoj nema nijedne vrijednosti javlja to.
func TestUvozExcelaPreskaceRupe(t *testing.T) {
	tekst, _, err := xlsxUOcitanja([][]string{
		{},
		{"Vrijeme", "Vodostaj (cm)"},
		{},
		{"25.11.2026. 07:00"},
		{"25.11.2026. 08:00", ""},
		{"", "514"},
		{"25.11.2026. 09:00", "517"},
	})
	if err != nil || tekst != "25.11.2026. 09:00\t517\n" {
		t.Errorf("%q, %v", tekst, err)
	}
	if _, _, err := xlsxUOcitanja([][]string{{"Vrijeme", "Vodostaj (cm)"}, {"25.11.2026. 07:00", ""}}); err == nil {
		t.Error("tablica bez ijedne vrijednosti prošla je bez poruke")
	}
}
