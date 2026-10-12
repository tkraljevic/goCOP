package models

import (
	"testing"
	"time"
)

// Zaključava stanje računa za administratora: isključen račun je neaktivan
// bez obzira na prijave, uključen račun kojim se nitko nije prijavio čeka
// preuzimanje, a tek zabilježena prijava čini ga aktivnim. Uz svako stanje
// ide njegov naziv i značka.
func TestStanjeRacuna(t *testing.T) {
	prijava := time.Date(2026, 3, 14, 0, 0, 0, 0, Zagreb)
	for _, s := range []struct {
		ime    string
		racun  User
		stanje AccountState
		naziv  string
		znacka string
	}{
		{"isključen, nikad prijavljen", User{FullName: "Pero Perić"}, AccountDisabled, "Neaktivan", "badge-inactive"},
		{"isključen, prijavljivao se", User{FullName: "Ana Anić", LastLoginAt: &prijava}, AccountDisabled, "Neaktivan", "badge-inactive"},
		{"uključen, još se nije prijavio", User{FullName: "Ivo Ivić", IsActive: true, PasswordHash: "postavljena"}, AccountPending, "Nije se prijavio", "badge-pending"},
		{"uključen i prijavljivao se", User{FullName: "Marko Markić", IsActive: true, LastLoginAt: &prijava}, AccountActive, "Aktivan", "badge-active"},
	} {
		got := s.racun.AccountState()
		if got != s.stanje {
			t.Errorf("%s: stanje %q, očekivano %q", s.ime, got, s.stanje)
		}
		if got.Label() != s.naziv || got.BadgeClass() != s.znacka {
			t.Errorf("%s: naziv %q i značka %q, očekivano %q i %q", s.ime, got.Label(), got.BadgeClass(), s.naziv, s.znacka)
		}
	}
	// nepoznato stanje (npr. iz novijeg izdanja) prikazuje se kao aktivno
	if s := AccountState("NOVO"); s.Label() != "Aktivan" || s.BadgeClass() != "badge-active" {
		t.Errorf("nepoznato stanje: %q, %q", s.Label(), s.BadgeClass())
	}
}

// Zaključava potvrdu adrese za PIN izvana: vrijedi samo dok je potvrđena
// adresa jednaka adresi računa, bez obzira na velika slova i razmake;
// promijenjena adresa i prazna adresa potvrdu nemaju.
func TestPotvrdaAdreseZaPIN(t *testing.T) {
	for _, s := range []struct {
		ime       string
		adresa    string
		potvrdena string
		vrijedi   bool
	}{
		{"ista adresa", "pero.peric@primjer.hr", "pero.peric@primjer.hr", true},
		{"razlika u slovima i razmacima", " Pero.Peric@Primjer.hr ", "pero.peric@primjer.hr  ", true},
		{"adresa promijenjena nakon potvrde", "ana.anic@primjer.hr", "pero.peric@primjer.hr", false},
		{"bez potvrde", "ivo.ivic@primjer.hr", "", false},
		{"račun bez adrese", "", "", false},
		{"račun bez adrese, a potvrda ostala", "  ", "marko.markic@primjer.hr", false},
	} {
		u := User{Email: s.adresa, PINAdresaPotvrdena: s.potvrdena}
		if got := u.PotvrdaAdreseVrijedi(); got != s.vrijedi {
			t.Errorf("%s: vrijedi %v, očekivano %v", s.ime, got, s.vrijedi)
		}
	}
}

// Zaključava razliku u povijesti profila: zaduženje s rokom koje nije
// opozvano prestalo je istekom, a opozvano ili ono bez roka nije isteklo.
func TestPrijasnjeZaduzenjeIsteklo(t *testing.T) {
	rok := time.Date(2026, 10, 15, 0, 0, 0, 0, Zagreb)
	for _, s := range []struct {
		ime     string
		z       PrijasnjeZaduzenje
		isteklo bool
	}{
		{"s rokom, nije opozvano", PrijasnjeZaduzenje{Duty: Duty{IsActive: true, ExpiresAt: &rok}}, true},
		{"s rokom, opozvano", PrijasnjeZaduzenje{Duty: Duty{IsActive: false, ExpiresAt: &rok}, OpozvanoAt: &rok}, false},
		{"bez roka", PrijasnjeZaduzenje{Duty: Duty{IsActive: true}}, false},
	} {
		if got := s.z.Isteklo(); got != s.isteklo {
			t.Errorf("%s: isteklo %v, očekivano %v", s.ime, got, s.isteklo)
		}
	}
}

// Zaključava naziv vrste organizacije: matična organizacija nosi naziv iz
// postavki, ostale vrste imaju stalan opis, a nepoznata vrsta ispisuje se
// svojom šifrom.
func TestNazivVrsteOrganizacije(t *testing.T) {
	postaviNazive(t, OrgTerms{OrgName: "Vodna uprava"})
	for _, s := range []struct {
		vrsta OrgType
		naziv string
	}{
		{OrgHrvatskeVode, "Vodna uprava"},
		{OrgPravnaOsoba, "Ugovorni izvođač (pravna osoba)"},
		{OrgVanjski, "Vanjska služba (CZ / MUP / 112)"},
		{OrgType("UDRUGA"), "UDRUGA"},
	} {
		if got := s.vrsta.Label(); got != s.naziv {
			t.Errorf("%s: naziv %q, očekivano %q", s.vrsta, got, s.naziv)
		}
	}
	postaviNazive(t, OrgTerms{})
	if got := OrgHrvatskeVode.Label(); got != "Hrvatske vode" {
		t.Errorf("bez vlastitog naziva: %q, očekivan zadani", got)
	}
}
