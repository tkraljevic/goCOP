package peers

// Potpis paketa izdanja (.cop): prihvaća se samo potpis izdavača nad
// upravo tim manifestom; nepotpisan, krivotvoren ili izmijenjen paket se
// odbija prije nego se išta pročita iz njega.

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"
)

func potpisaniManifest(t *testing.T) (CopManifest, ed25519.PrivateKey) {
	t.Helper()
	_, kljuc, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	m := CopManifest{
		Inacica: 1, Obuhvat: CopObuhvat{Vrsta: "prijave", AreaID: 16, Od: 2026, Do: 2026},
		Kanali: []string{"prijave/16/2026", "prijave/16/2025"}, Izdanje: 3, Prethodno: "a1b2",
		Izdao: "cop-osijek", Nastalo: time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC),
		Otisak: "c3d4", Zapisa: 12, Sadrzaja: 2, Ukljuceno: 1,
	}
	m.Potpis = &CopPotpis{
		Kljuc:  base64.StdEncoding.EncodeToString(kljuc.Public().(ed25519.PublicKey)),
		Potpis: base64.StdEncoding.EncodeToString(ed25519.Sign(kljuc, kanonskiCop(m))),
	}
	return m, kljuc
}

// Ispravan potpis vraća ključ izdavača; redoslijed kanala u manifestu ne
// mijenja potpis, a ni broj uključenih sadržaja (isti paket smije ići sa
// sadržajem ili bez njega).
func TestPotpisCopaVrijedi(t *testing.T) {
	m, kljuc := potpisaniManifest(t)
	javni, err := ProvjeriCopPotpis(m)
	if err != nil || !javni.Equal(kljuc.Public()) {
		t.Fatalf("ispravan potpis: %v", err)
	}
	m.Kanali = []string{"prijave/16/2025", "prijave/16/2026"}
	m.Ukljuceno = 0
	if _, err := ProvjeriCopPotpis(m); err != nil {
		t.Errorf("isti sadržaj drukčije složen: %v", err)
	}
}

// Nepotpisan paket, neupotrebljiv ključ ili potpis, potpis drugog ključa i
// svaka izmjena potpisanog podatka odbijaju se.
func TestPotpisCopaOdbija(t *testing.T) {
	_, tudji, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for ime, izmjena := range map[string]func(m *CopManifest){
		"bez potpisa":         func(m *CopManifest) { m.Potpis = nil },
		"prazan potpis":       func(m *CopManifest) { m.Potpis.Potpis = "" },
		"ključ nije base64":   func(m *CopManifest) { m.Potpis.Kljuc = "nije ključ!" },
		"ključ krive duljine": func(m *CopManifest) { m.Potpis.Kljuc = base64.StdEncoding.EncodeToString([]byte("prekratko")) },
		"potpis nije base64":  func(m *CopManifest) { m.Potpis.Potpis = "nije potpis!" },
		"potpisao drugi ključ": func(m *CopManifest) {
			m.Potpis.Potpis = base64.StdEncoding.EncodeToString(ed25519.Sign(tudji, kanonskiCop(*m)))
		},
		"tuđi ključ uz potpis": func(m *CopManifest) {
			m.Potpis.Kljuc = base64.StdEncoding.EncodeToString(tudji.Public().(ed25519.PublicKey))
		},
		"izmijenjeno izdanje":    func(m *CopManifest) { m.Izdanje++ },
		"izmijenjen izdavač":     func(m *CopManifest) { m.Izdao = "ured-vukovar" },
		"izmijenjen obuhvat":     func(m *CopManifest) { m.Obuhvat.AreaID = 3 },
		"dodan kanal":            func(m *CopManifest) { m.Kanali = append(m.Kanali, "prijave/3/2026") },
		"izmijenjen otisak":      func(m *CopManifest) { m.Otisak = "e5f6" },
		"izmijenjeno prethodno":  func(m *CopManifest) { m.Prethodno = "" },
		"izmijenjen broj zapisa": func(m *CopManifest) { m.Zapisa = 11 },
	} {
		m, _ := potpisaniManifest(t)
		izmjena(&m)
		if javni, err := ProvjeriCopPotpis(m); err == nil || javni != nil {
			t.Errorf("%s: potpis prihvaćen", ime)
		}
	}
}
