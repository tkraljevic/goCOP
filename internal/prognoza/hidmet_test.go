package prognoza

import (
	"testing"
	"time"
)

// Isječak njihove tablice, s izmišljenim brojevima: prvi datum je dan izdanja,
// iza četiri dana prognoze stoje granice redovne i izvanredne obrane.
const ispisHidmet = `<table summary="Prognoza vodostaja"><tr>
<td class="siva75"></td><td class="siva75"></td><td class="siva75">23.09.</td><td class="siva75">24.09.</td>
<td class="siva75">25.09.</td><td class="siva75">26.09.</td><td class="siva75">27.09.</td></tr>
<tr> <td class="bela75 levo">&nbsp;DUNAV&nbsp;</td> <td class="bela75 levo"><a href="x?hm_id=42010" class="bold">BEZDAN</a></td>
<td class="bela75">110</td> <td class="bela75">120</td> <td class="bela75">130</td> <td class="bela75">125</td>
<td class="bela75">115</td> <td class="bela75">500</td> <td class="bela75 bold">700</td> </tr>
<tr> <td class="siva75 levo">&nbsp;DUNAV&nbsp;</td> <td class="siva75 levo"><a href="x?hm_id=42030" class="bold">BAČKA PALANKA </a></td>
<td class="siva75">-10</td> <td class="siva75">-5</td> <td class="siva75">0</td> <td class="siva75">5</td>
<td class="siva75">10</td> <td class="siva75">580</td> <td class="siva75 bold">680</td> </tr></table>`

func TestCitaSrpskuPrognozu(t *testing.T) {
	l, err := CitajHidmet(ispisHidmet, time.Date(2026, 9, 23, 20, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(l) != 2 || SifraSrpske(l[0].Naziv) != "bezdan" || SifraSrpske(l[1].Naziv) != "backa-palanka" {
		t.Fatalf("letve %+v", l)
	}
	b := l[0]
	if !b.ImaDanas || b.Danas != 110 || len(b.Dani) != 4 {
		t.Fatalf("Bezdan %+v — granice obrane ne smiju ući u prognozu", b)
	}
	// 24.9. u 07 h po ljetnom računanju vremena je 05 h UTC
	if got := b.Dani[0]; got.Cm != 120 || got.Kad != time.Date(2026, 9, 24, 5, 0, 0, 0, time.UTC) {
		t.Errorf("prvi dan %+v", got)
	}
	if b.Izdano.UTC().Hour() != 10 {
		t.Errorf("izdano %v, a treba u 12 h po lokalnom", b.Izdano)
	}
}
