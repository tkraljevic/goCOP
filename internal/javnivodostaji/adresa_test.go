package javnivodostaji

import "testing"

// Adresa postaje mora biti ona koju otvara preglednik: pregled po postaji
// postoji samo na mobilnoj stranici i traži sektor. Bez sektora ostaje
// stara adresa, iz koje preuzimanje i dalje čita broj postaje.
func TestAdresaPostaje(t *testing.T) {
	sa := AdresaPostaje(Postaja{ID: 425, Sektor: 2})
	zeli := "https://mvodostaji.voda.hr/Home/PregledVodostajaPostaje?sektorID=2&bpID=0&postajaID=425"
	if sa != zeli {
		t.Errorf("sa sektorom: %q, očekivano %q", sa, zeli)
	}
	bez := AdresaPostaje(Postaja{ID: 425})
	if PostajaIzAdrese(bez) != 425 {
		t.Errorf("iz adrese bez sektora ne čita se broj postaje: %q", bez)
	}
	if PostajaIzAdrese(sa) != 425 {
		t.Errorf("iz adrese sa sektorom ne čita se broj postaje: %q", sa)
	}
}
