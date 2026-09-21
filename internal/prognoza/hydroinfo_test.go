package prognoza

import (
	"testing"
	"time"
)

// Isječak njihove tablice: svaki dan je zaseban okvir s dvije vrijednosti,
// prognozom i rasponom pogreške, a naša slova pišu kao entitete po kodnoj
// stranici 1250.
const ispis = `<table>
<tr><td>Water level forecast [cm]</td></tr>
<tr><td>River</td><td>Station</td><td>Issued at</td><td>Today <br>morning</td>
    <td>22.09. 07h</td><td>23.09. 07h</td></tr>
<tr><td><div>Drava<br><b>Beli&#154;&aelig;e</b><br><i>(21.09.2026 08:59)</i></div></td>
    <td><b>36</b></td>
    <td><table><tr><td><b>26</b></td><td><b> &plusmn; 6</b></td></tr></table></td>
    <td><table><tr><td><b>21</b></td><td><b> &plusmn; 11</b></td></tr></table></td></tr>
<tr><td><div>Drava<br><b>Osijek</b><br><i>(21.09.2026 08:59)</i></div></td>
    <td><b>-151</b></td>
    <td><table><tr><td><b>-166</b></td><td><b> &plusmn; 6</b></td></tr></table></td>
    <td><table><tr><td><b>-170</b></td><td><b> &plusmn; 12</b></td></tr></table></td></tr>
</table>`

// Prognoza se čita s nazivom, vremenom izdanja, jutrošnjom vrijednošću i
// danima; naša slova moraju ispasti ispravno, inače se letva ne prepozna.
func TestCitaTablicuPrognoze(t *testing.T) {
	l, err := Citaj(ispis)
	if err != nil {
		t.Fatal(err)
	}
	if len(l) != 2 {
		t.Fatalf("letvi %d, očekivano 2: %+v", len(l), l)
	}
	if l[0].Naziv != "Belišće" {
		t.Errorf("naziv %q — entiteti su po kodnoj stranici 1250, ne po Unicodeu", l[0].Naziv)
	}
	if Sifra(l[0].Naziv) != "belisce" || Sifra(l[1].Naziv) != "osijek" {
		t.Errorf("naše letve se ne prepoznaju: %q, %q", l[0].Naziv, l[1].Naziv)
	}
	if !l[0].ImaDanas || l[0].Danas != 36 {
		t.Errorf("jutrošnja vrijednost: %+v", l[0])
	}
	if len(l[0].Dani) != 2 {
		t.Fatalf("dana %d, očekivano 2: %+v", len(l[0].Dani), l[0].Dani)
	}
	if l[0].Dani[0].Cm != 26 || l[0].Dani[0].PlusMin != 6 {
		t.Errorf("prvi dan: %+v", l[0].Dani[0])
	}
	if l[1].Dani[1].Cm != -170 || l[1].Dani[1].PlusMin != 12 {
		t.Errorf("Osijek, drugi dan: %+v", l[1].Dani[1])
	}
	// prognoza vrijedi za 7 h po mađarskom, a to je naše vrijeme
	kad := l[0].Dani[0].Kad
	if kad.Hour() != 5 && kad.Hour() != 6 {
		t.Errorf("sat prognoze u UTC-u: %s — 07 h ljeti je 05 h UTC", kad)
	}
	if l[0].Izdano.Day() != 21 || l[0].Izdano.Hour() != 8 {
		t.Errorf("vrijeme izdanja: %s", l[0].Izdano.Format(time.RFC3339))
	}
}

// Prazna ili promijenjena stranica mora javiti grešku.
func TestPraznaTablicaJavljaGresku(t *testing.T) {
	if _, err := Citaj("<html><body>ništa</body></html>"); err == nil {
		t.Error("stranica bez tablice mora javiti grešku")
	}
}
