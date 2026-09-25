package javnivodostaji

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"gocop/internal/hidroview"
)

// Adresa letve je poveznica na postaju u njihovu sučelju; iz nje se mora
// pročitati šifra postaje, a tuđe adrese ne smiju završiti kod ovog čitača.
func TestHidroViewPrepoznajeSvojeAdrese(t *testing.T) {
	const site = "5QpcTwBap96QWw7puV8xCFipnPXWN7KenjoaLBFb1WCF"
	h := &HidroView{}
	for _, a := range []string{
		AdresaHidroView(site),
		"https://hdv.voda.hr/#/site/" + site,
		"https://hdv.voda.hr/api/v1/installed_equipment/get?site_id=" + site,
	} {
		if PostajaHidroViewIzAdrese(a) != site {
			t.Errorf("iz %q ne čita se šifra postaje", a)
		}
		if !h.Prepoznaje(a) {
			t.Errorf("čitač ne prepoznaje svoju adresu %q", a)
		}
	}
	for _, tudja := range []string{
		AdresaPostaje(Postaja{ID: 425, Sektor: 2}),
		AdresaHidmet(42010),
		AdresaSHMU(5140),
		"https://hdv.voda.hr/", // bez šifre postaje
	} {
		if h.Prepoznaje(tudja) {
			t.Errorf("čitač prepoznaje tuđu adresu %q", tudja)
		}
	}
}

// Bez upisanog računa letva mora jasno reći što nedostaje, a ne šutjeti.
func TestHidroViewBezRacunaJavljaSto(t *testing.T) {
	h := &HidroView{}
	_, err := h.Ocitanja(t.Context(), AdresaHidroView("5QpcTwBap96QWw7puV8xCFipnPXWN7Kenjoa"))
	if err == nil {
		t.Fatal("bez računa mora javiti grešku")
	}
	if got := err.Error(); got == "" {
		t.Error("greška nema poruku")
	}
}

// Tlačni zapisivač javlja samo promjenu, pa puni sat često nema svoju
// vrijednost. Tada vrijedi zadnja javljena — ali ne dovijeka: nakon šest sati
// šutnje sat ostaje prazan, da se prekid dojave ne pretvori u ravnu crtu.
func TestPuniSatiDrziZadnjuJavljenu(t *testing.T) {
	t0 := time.Date(2000, 1, 3, 8, 0, 0, 0, time.UTC)
	u := func(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }
	v := []hidroview.Vrijednost{
		{Kad: u(75), Vrijednost: 1.00},  // 09:15
		{Kad: u(150), Vrijednost: 1.20}, // 10:30
		{Kad: u(360), Vrijednost: 0.90}, // 14:00, točno na sat
		{Kad: u(902), Vrijednost: 0.50}, // 23:02, unutar pet minuta
	}
	got := puniSati(v, u(30), u(930)) // 08:30 … 23:30
	zelim := map[int]float64{
		10: 1.00, 11: 1.20, 12: 1.20, 13: 1.20, // zadržano
		14: 0.90, 15: 0.90, 16: 0.90, 17: 0.90, 18: 0.90, 19: 0.90, 20: 0.90, // 20:00 je točno šest sati
		23: 0.50,
	}
	for sat := 8; sat <= 23; sat++ {
		kad := time.Date(2000, 1, 3, sat, 0, 0, 0, time.UTC).Unix()
		v, ima := got[kad]
		z, treba := zelim[sat]
		switch {
		case treba && !ima:
			t.Errorf("%02d:00 nema vrijednosti, očekivano %.2f", sat, z)
		case !treba && ima:
			t.Errorf("%02d:00 ima %.2f, a sat mora ostati prazan", sat, v)
		case treba && v != z:
			t.Errorf("%02d:00 = %.2f, očekivano %.2f", sat, v, z)
		}
	}
	if len(puniSati(nil, u(0), u(60))) != 0 {
		t.Error("bez vrijednosti mora vratiti prazno")
	}
}

// Kad se sustav ne da dosegnuti, druga letva istog sustava ne čeka nego
// odmah dobije odgovor da je sustav nedostupan; kriva lozinka to ne radi.
func TestHidroViewOsiguracNakonMrezneGreske(t *testing.T) {
	// zatvorena vrata: veza se odbija odmah
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	adresa := "http://" + l.Addr().String()
	l.Close()
	h := &HidroView{Base: adresa, Racun: func(string) (string, string, bool) { return "k", "l", true }}
	ctx := context.Background()
	_, err = h.Ocitanja(ctx, "https://hdv.voda.hr/#/site/AAAAAAAAAAAAAAAAAAAA/latest")
	if err == nil || strings.Contains(err.Error(), "nedostupan od") {
		t.Fatalf("prva letva: %v", err)
	}
	_, err = h.Ocitanja(ctx, "https://hdv.voda.hr/#/site/BBBBBBBBBBBBBBBBBBBB/latest")
	if err == nil || !strings.Contains(err.Error(), "nedostupan od") {
		t.Fatalf("druga letva mora dobiti osigurač: %v", err)
	}
	// istekao predah: pokušava iznova
	for k := range h.nedostupan {
		h.nedostupan[k] = time.Now().Add(-Predah - time.Second)
	}
	_, err = h.Ocitanja(ctx, "https://hdv.voda.hr/#/site/BBBBBBBBBBBBBBBBBBBB/latest")
	if err == nil || strings.Contains(err.Error(), "nedostupan od") {
		t.Fatalf("nakon predaha opet se pokušava: %v", err)
	}
}
