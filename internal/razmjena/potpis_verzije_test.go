package razmjena

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"
)

func primjerVerzije() PodaciVerzije {
	return PodaciVerzije{
		VersionID: "0190a1b2-0000-7000-8000-000000000002", Entity: "users", EntityID: "u1", NodeID: "racunalo-a",
		Supersedes: "0190a1b2-0000-7000-8000-000000000001", Channel: "", SchemaVersion: 1,
		CreatedAt: time.Date(2026, 11, 26, 7, 15, 0, 123456789, time.UTC),
		Payload:   []byte(`{"full_name":"Ana Horvat","napomena":"a<b>&c"}`),
	}
}

// Potpis vrijedi pod ključem autora i prestaje vrijediti čim se promijeni
// bilo koji potpisani podatak verzije, ključ ili ime protokola.
func TestPotpisVerzije(t *testing.T) {
	autor := ed25519.NewKeyFromSeed(sjeme(0x01))
	v := primjerVerzije()
	potpis, kljuc, err := PotpisiVerziju(autor, "gocop", v)
	if err != nil {
		t.Fatal(err)
	}
	if kljuc != PublicKeyString(autor.Public().(ed25519.PublicKey)) {
		t.Fatalf("ključ autora %q", kljuc)
	}
	if err := ProvjeriPotpisVerzije("gocop", v, potpis, kljuc); err != nil {
		t.Fatalf("ispravan potpis: %v", err)
	}

	izmjene := map[string]func(*PodaciVerzije){
		"version_id":     func(v *PodaciVerzije) { v.VersionID += "x" },
		"entity":         func(v *PodaciVerzije) { v.Entity = "memberships" },
		"entity_id":      func(v *PodaciVerzije) { v.EntityID = "u2" },
		"node_id":        func(v *PodaciVerzije) { v.NodeID = "racunalo-b" },
		"supersedes":     func(v *PodaciVerzije) { v.Supersedes = "" },
		"archived":       func(v *PodaciVerzije) { v.Archived = true },
		"channel":        func(v *PodaciVerzije) { v.Channel = "ocitanja/16/2026" },
		"schema_version": func(v *PodaciVerzije) { v.SchemaVersion = 2 },
		"created_at":     func(v *PodaciVerzije) { v.CreatedAt = v.CreatedAt.Add(time.Second) },
		"sadržaj":        func(v *PodaciVerzije) { v.Payload = []byte(`{"full_name":"Ana Horvat","napomena":"a<b>&d"}`) },
	}
	for ime, izmijeni := range izmjene {
		promijenjena := primjerVerzije()
		izmijeni(&promijenjena)
		if err := ProvjeriPotpisVerzije("gocop", promijenjena, potpis, kljuc); !errors.Is(err, ErrPotpisVerzije) {
			t.Errorf("promijenjen %s: %v", ime, err)
		}
	}
	tudji := PublicKeyString(ed25519.NewKeyFromSeed(sjeme(0x02)).Public().(ed25519.PublicKey))
	if err := ProvjeriPotpisVerzije("gocop", v, potpis, tudji); !errors.Is(err, ErrPotpisVerzije) {
		t.Errorf("tuđi ključ: %v", err)
	}
	if err := ProvjeriPotpisVerzije("gocop-sim", v, potpis, kljuc); !errors.Is(err, ErrPotpisVerzije) {
		t.Errorf("drugo ime protokola: %v", err)
	}
}

// Ono što putovanje mijenja, a potpis ne pokriva, ne kvari potpis: dio
// vremena ispod sekunde, razmaci u sadržaju i <, >, & napisani kao <,
// > i & (tako ih Go šalje žicom).
func TestPotpisVerzijePrezivljavaPut(t *testing.T) {
	autor := ed25519.NewKeyFromSeed(sjeme(0x01))
	v := primjerVerzije()
	potpis, kljuc, err := PotpisiVerziju(autor, "gocop", v)
	if err != nil {
		t.Fatal(err)
	}
	putovi := map[string]func(*PodaciVerzije){
		"sekunde":    func(v *PodaciVerzije) { v.CreatedAt = v.CreatedAt.Truncate(time.Second) },
		"druga zona": func(v *PodaciVerzije) { v.CreatedAt = v.CreatedAt.In(time.FixedZone("CET", 3600)) },
		"razmaci": func(v *PodaciVerzije) {
			v.Payload = []byte("{ \"full_name\": \"Ana Horvat\",\n  \"napomena\": \"a<b>&c\" }")
		},
		"izbjegnuto":   func(v *PodaciVerzije) { v.Payload = []byte(`{"full_name":"Ana Horvat","napomena":"a<b>&c"}`) },
		"bez promjene": func(*PodaciVerzije) {},
	}
	for ime, put := range putovi {
		p := primjerVerzije()
		put(&p)
		if err := ProvjeriPotpisVerzije("gocop", p, potpis, kljuc); err != nil {
			t.Errorf("%s: %v", ime, err)
		}
	}
}

// Sadržaj koji nije JSON ne da se potpisati ni provjeriti; kriv ključ ili
// potpis koji nije base64 ne prolaze.
func TestPotpisVerzijeNeispravno(t *testing.T) {
	autor := ed25519.NewKeyFromSeed(sjeme(0x01))
	v := primjerVerzije()
	potpis, kljuc, err := PotpisiVerziju(autor, "gocop", v)
	if err != nil {
		t.Fatal(err)
	}
	losa := primjerVerzije()
	losa.Payload = []byte(`{"nije":`)
	if _, _, err := PotpisiVerziju(autor, "gocop", losa); !errors.Is(err, ErrOblikVerzije) {
		t.Errorf("potpis sadržaja koji nije JSON: %v", err)
	}
	if err := ProvjeriPotpisVerzije("gocop", losa, potpis, kljuc); !errors.Is(err, ErrOblikVerzije) {
		t.Errorf("provjera sadržaja koji nije JSON: %v", err)
	}
	for _, k := range []string{"", "nije base64!", "AAAA"} {
		if err := ProvjeriPotpisVerzije("gocop", v, potpis, k); !errors.Is(err, ErrKljucAutora) {
			t.Errorf("ključ %q: %v", k, err)
		}
	}
	for _, p := range []string{"", "nije base64!", potpis[:20]} {
		if err := ProvjeriPotpisVerzije("gocop", v, p, kljuc); !errors.Is(err, ErrPotpisVerzije) {
			t.Errorf("potpis %q: %v", p, err)
		}
	}
}
