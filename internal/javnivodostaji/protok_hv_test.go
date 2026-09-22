package javnivodostaji

import (
	"testing"
	"time"

	"gocop/internal/models"
)

// Isječak stranice protoka kakav ona stvarno daje: dvoznamenkasta godina,
// vrijednost sa zarezom, pa trend koji se ne čita.
const ispisProtokaHV = `<table><tr><th>Datum</th><th>Vrijeme</th><th>Protok</th><th>Trend</th></tr>
<tr><td>22.09.26.</td><td>09:00</td><td>229,86</td><td>0,00</td></tr>
<tr><td>22.09.26.</td><td>08:45</td><td>227,53</td><td>+2,33</td></tr>
<tr><td>22.09.26.</td><td>08:00</td><td>222,91</td><td>0,00</td></tr></table>`

func TestCitajProtokHV(t *testing.T) {
	r, err := CitajProtokHV(ispisProtokaHV)
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 3 {
		t.Fatalf("redaka %d, očekivano 3", len(r))
	}
	// 09:00 po lokalnom je 07:00 UTC ljeti
	zelim := time.Date(2026, 9, 22, 9, 0, 0, 0, time.FixedZone("CEST", 2*3600)).UTC()
	if !r[0].Kad.Equal(zelim) {
		t.Errorf("vrijeme %v, očekivano %v", r[0].Kad, zelim)
	}
	if r[0].FlowM3s == nil || *r[0].FlowM3s != 229.86 {
		t.Errorf("protok %v, očekivano 229,86", r[0].FlowM3s)
	}
	// trend se ne smije pročitati kao vrijednost
	for _, x := range r {
		if x.FlowM3s != nil && *x.FlowM3s < 100 {
			t.Errorf("pročitan trend kao protok: %v", *x.FlowM3s)
		}
	}
}

// Letva bez protoka ne smije javiti grešku — to nije kvar nego stanje.
func TestCitajProtokHVBezTablice(t *testing.T) {
	r, err := CitajProtokHV("<html><body>Nema protoka za ovu postaju</body></html>")
	if err != nil {
		t.Fatalf("prazna stranica ne smije biti greška: %v", err)
	}
	if len(r) != 0 {
		t.Errorf("dobiveno %d redaka, očekivano nijedan", len(r))
	}
}

// Protok se pridružuje vodostaju samo na trenutku koji vodostaj ima.
func TestDopuniProtokom(t *testing.T) {
	t0 := time.Date(2026, 9, 22, 7, 0, 0, 0, time.UTC)
	vodostaji := []Redak{{Kad: t0, LevelCm: intPtr(120)}, {Kad: t0.Add(time.Hour), LevelCm: intPtr(122)}}
	protoci := []Redak{{Kad: t0, FlowM3s: floatPtr(216.07)}, {Kad: t0.Add(30 * time.Minute), FlowM3s: floatPtr(218.34)}}
	out := dopuniProtokom(vodostaji, protoci)
	if len(out) != 2 {
		t.Fatalf("redaka %d, očekivano 2 — protok ne smije dodavati trenutke", len(out))
	}
	if out[0].FlowM3s == nil || *out[0].FlowM3s != 216.07 {
		t.Errorf("prvom retku nije pridružen protok: %+v", out[0])
	}
	if out[1].FlowM3s != nil {
		t.Errorf("drugom retku pridružen protok kojeg za taj sat nema: %+v", out[1])
	}
	// Preuzeti protok mora reći odakle mu vrijednost, inače se poslije čita
	// kao da ga je netko izmjerio.
	if out[0].FlowMetoda != models.FlowMethodKrivulja {
		t.Errorf("protok s javne stranice nije označen kao izveden: %q", out[0].FlowMetoda)
	}
	if out[0].FlowBiljeska == "" {
		t.Error("uz izvedeni protok ne stoji bilješka odakle je")
	}
	if out[1].FlowMetoda != "" {
		t.Errorf("redak bez protoka dobio je oznaku načina: %q", out[1].FlowMetoda)
	}
}
