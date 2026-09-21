package javnivodostaji

import (
	"testing"
	"time"
)

// Isječak stranice srpskog zavoda, s razmacima i prijelomima kakvi ondje
// stvarno stoje: datum i sat u jednoj ćeliji, vrijednost u sljedećoj.
const ispisHidmet = `<tfoot>
 <tr><td>Napomena: Vreme u tabeli odgovara univerzalnom koordiniranom vremenu
 uvećanom za jedan čas: UTC+1 (srednjoevropsko zonsko vreme).</td></tr>
 </tfoot>
                             <tr>
				<td class="bela75 levo">&nbsp;20.09.2026 21:00</td>
				<td class="bela75 ">&nbsp;
				-115</td>
			  </tr>
                            <tr>
				<td class="siva75 levo">&nbsp;20.09.2026 20:00</td>
				<td class="siva75 ">&nbsp;
				-116</td>
			  </tr>
                            <tr>
				<td class="bela75 levo">&nbsp;20.06.2026 03:00</td>
				<td class="bela75 ">&nbsp;
				212</td>
			  </tr>`

// Vrijeme na srpskoj stranici je UTC+1 cijele godine, bez ljetnog pomaka.
// Čitano kao naše lokalno, ljetni bi podaci ispali sat u krivo.
func TestHidmetCitaUStalnomPomaku(t *testing.T) {
	r, err := CitajHidmet(ispisHidmet)
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 3 {
		t.Fatalf("redaka %d, očekivano 3", len(r))
	}
	// najstariji prvi
	if !r[0].Kad.Equal(time.Date(2026, 6, 20, 2, 0, 0, 0, time.UTC)) || r[0].LevelCm == nil || *r[0].LevelCm != 212 {
		t.Errorf("ljetni redak: %+v — 03:00 UTC+1 je 02:00 UTC, ne 01:00", r[0])
	}
	if !r[2].Kad.Equal(time.Date(2026, 9, 20, 20, 0, 0, 0, time.UTC)) || r[2].LevelCm == nil || *r[2].LevelCm != -115 {
		t.Errorf("zadnji redak: %+v", r[2])
	}
}

// Adresa nosi broj postaje i po njemu se izvor prepoznaje; hrvatska adresa
// ne smije završiti kod srpskog čitača ni obrnuto.
func TestHidmetPrepoznajeSvojeAdrese(t *testing.T) {
	sr := AdresaHidmet(42010)
	if PostajaHidmetIzAdrese(sr) != 42010 {
		t.Errorf("iz %q ne čita se broj postaje", sr)
	}
	h := Hidmet{}
	if !h.Prepoznaje(sr) {
		t.Errorf("srpski čitač ne prepoznaje svoju adresu %q", sr)
	}
	hr := AdresaPostaje(Postaja{ID: 425, Sektor: 2})
	if h.Prepoznaje(hr) {
		t.Errorf("srpski čitač prepoznaje hrvatsku adresu %q", hr)
	}
	if (&Client{}).Prepoznaje(sr) {
		t.Errorf("hrvatski čitač prepoznaje srpsku adresu %q", sr)
	}
}

// Prazna ili promijenjena stranica mora javiti grešku, a ne tiho vratiti
// prazan niz: letva bi inače izgledala kao da nema novih podataka.
func TestHidmetJavljaPraznuStranicu(t *testing.T) {
	if _, err := CitajHidmet("<html><body>Nema ničega</body></html>"); err == nil {
		t.Error("stranica bez tablice mora javiti grešku")
	}
}
