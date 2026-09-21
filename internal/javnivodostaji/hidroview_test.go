package javnivodostaji

import "testing"

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
