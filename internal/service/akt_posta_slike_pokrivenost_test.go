package service

import (
	"encoding/base64"
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/posta"
)

// Slike zapisane u pismu kao data: adrese postaju ugrađene slike (cid:),
// redom kojim stoje; neispravan zapis ostaje kakav jest
func TestUgradiSlike(t *testing.T) {
	prazno := posta.Poruka{}
	ugradiSlike(&prazno)
	if prazno.HTML != "" || len(prazno.Ugradjene) != 0 {
		t.Errorf("pismo bez HTML-a: %+v", prazno)
	}

	jpeg := base64.StdEncoding.EncodeToString([]byte("\xff\xd8\xffjpeg"))
	gif := base64.StdEncoding.EncodeToString([]byte("GIF89a"))
	m := posta.Poruka{HTML: `<p>Prva</p><img src="data:image/jpeg;base64,` + jpeg + `">` +
		`<img src="data:image/gif;base64,` + gif + `">` +
		`<img src="data:image/png;base64,===">` +
		`<img src="https://primjer.test/slika.png">`}
	ugradiSlike(&m)

	if len(m.Ugradjene) != 2 {
		t.Fatalf("ugrađeno %d slika, očekivano 2: %+v", len(m.Ugradjene), m.Ugradjene)
	}
	prva, druga := m.Ugradjene[0], m.Ugradjene[1]
	if prva.ContentID != "slika1@gocop" || prva.Ime != "slika1.jpg" || prva.Vrsta != "image/jpeg" || string(prva.Podaci) != "\xff\xd8\xffjpeg" {
		t.Errorf("prva slika: %+v", prva)
	}
	if druga.ContentID != "slika2@gocop" || druga.Ime != "slika2.gif" || string(druga.Podaci) != "GIF89a" {
		t.Errorf("druga slika: %+v", druga)
	}
	for _, ocekivano := range []string{`src="cid:slika1@gocop"`, `src="cid:slika2@gocop"`, `src="data:image/png;base64,==="`, `src="https://primjer.test/slika.png"`} {
		if !strings.Contains(m.HTML, ocekivano) {
			t.Errorf("HTML nema %s: %s", ocekivano, m.HTML)
		}
	}
}

func TestNastavakSlike(t *testing.T) {
	for vrsta, nastavak := range map[string]string{
		"image/png": ".png", "IMAGE/JPEG": ".jpg", "image/gif": ".gif", "image/svg+xml": ".svg", "image/webp": ".webp",
		"image/bmp": "", "": "", "application/pdf": "",
	} {
		if got := nastavakSlike(vrsta); got != nastavak {
			t.Errorf("nastavakSlike(%q) = %q, očekivano %q", vrsta, got, nastavak)
		}
	}
}

// Potpis nosi najvišu funkciju: unutar iste razine voditelj prije
// zamjenika, a uloga izvan poretka dolazi iza svih
func TestMjestoUlogeIPoredak(t *testing.T) {
	if mjestoUloge(models.RoleGlobalAdmin) != 0 {
		t.Error("globalni administrator mora biti prvi")
	}
	if !(mjestoUloge(models.RoleSectorLeader) < mjestoUloge(models.RoleSectorDeputy)) {
		t.Error("voditelj sektora mora biti ispred zamjenika")
	}
	if !(mjestoUloge(models.RoleAreaLeader) < mjestoUloge(models.RoleSectionLeader)) {
		t.Error("rukovoditelj područja mora biti ispred rukovoditelja dionice")
	}
	if n := mjestoUloge(models.Role("NEPOZNATA")); n != len(poredakUloga)+1 {
		t.Errorf("nepoznata uloga: %d", n)
	}
	if n := mjestoUloge(models.RoleWaterGuard); n != len(poredakUloga)+1 {
		t.Errorf("vodočuvar nije u poretku uprave: %d", n)
	}

	sektor, podrucje := "P", 1
	u := &models.User{Duties: []models.Duty{
		{Title: "Zamjenik", Role: models.RoleAreaDeputy, AreaID: &podrucje, SectorID: &sektor, IsActive: true},
		{Title: "Rukovoditelj", Role: models.RoleAreaLeader, AreaID: &podrucje, SectorID: &sektor, IsActive: true},
		{Title: "Bivši voditelj sektora", Role: models.RoleSectorLeader, SectorID: &sektor, IsActive: false},
	}}
	if d := najvisaDuznost(u); d == nil || d.Title != "Rukovoditelj" {
		t.Errorf("najviša dužnost: %+v", d)
	}
}
