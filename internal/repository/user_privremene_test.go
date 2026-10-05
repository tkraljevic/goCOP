package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// privremena je privremeno imenovanje u sektoru B, području 16
func privremena(rok *time.Time, sObranom bool, ovisiO *uuid.UUID) *models.Duty {
	sektor, podrucje := "B", 16
	return &models.Duty{UserID: osnovaKorisnik, Title: "Privremeni rukovoditelj dionice", Role: models.RoleSectionLeader,
		ScopeType: models.ScopeSection, SectorID: &sektor, AreaID: &podrucje, SectionCodes: osnovaDionica,
		IsTemporary: true, Reason: "nedostaje osoblja", Rok: rok, ExpiresAt: rok, IsticeSObranom: sObranom, OvisiO: ovisiO}
}

// Zadani datum, istek s obranom i ovisnost putuju kroz upis, izmjenu i
// čitanje (jedno zaduženje i popis osobe)
func TestPrivremenaDuznostPoljaUBazi(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	rok := time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)
	izvor := uuid.New()
	d := privremena(&rok, true, &izvor)
	nuzno(t, repo.AddDuty(d))

	procitana, err := repo.GetDuty(d.ID)
	nuzno(t, err)
	if procitana.Rok == nil || !procitana.Rok.Equal(rok) || !procitana.IsticeSObranom || procitana.OvisiO == nil || *procitana.OvisiO != izvor {
		t.Fatalf("pročitano: rok %v, s obranom %v, ovisi o %v", procitana.Rok, procitana.IsticeSObranom, procitana.OvisiO)
	}
	osobe, err := repo.GetDutiesForUser(osnovaKorisnik)
	nuzno(t, err)
	if len(osobe) != 1 || osobe[0].Rok == nil || !osobe[0].IsticeSObranom || osobe[0].OvisiO == nil {
		t.Fatalf("popis osobe: %+v", osobe)
	}

	// izmjena: bez datuma, bez obrane i bez ovisnosti
	procitana.Rok, procitana.ExpiresAt, procitana.IsticeSObranom, procitana.OvisiO = nil, nil, false, nil
	nuzno(t, repo.UpdateDuty(procitana))
	poslije, err := repo.GetDuty(d.ID)
	nuzno(t, err)
	if poslije.Rok != nil || poslije.IsticeSObranom || poslije.OvisiO != nil {
		t.Errorf("poslije izmjene: rok %v, s obranom %v, ovisi o %v", poslije.Rok, poslije.IsticeSObranom, poslije.OvisiO)
	}

	// početna dužnost pri otvaranju računa nosi ista polja
	novi := &models.User{Username: "privremeni", PasswordHash: "x", FullName: "Pero Perić", OrgType: models.OrgHrvatskeVode, IsActive: true}
	pocetna := privremena(&rok, true, &izvor)
	nuzno(t, repo.CreateUser(novi, pocetna))
	if p, err := repo.GetDuty(pocetna.ID); err != nil || p.Rok == nil || !p.IsticeSObranom || p.OvisiO == nil {
		t.Errorf("početna dužnost: %+v %v", p, err)
	}
}

// Za preračun isteka: aktivne privremene s istekom ovisnim o obrani ili
// drugoj dužnosti, i već istekle; ne stalne, ne one samo s datumom, ne opozvane
func TestPrivremeneSIstekom(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	jucer := time.Now().Add(-24 * time.Hour)
	izvor := uuid.New()
	sObranom := privremena(nil, true, nil)
	ovisna := privremena(nil, false, &izvor)
	istekla := privremena(&jucer, true, nil)
	samoDatum := privremena(&jucer, false, nil)
	opozvana := privremena(nil, true, nil)
	stalna := privremena(nil, false, nil)
	stalna.IsTemporary = false
	for _, d := range []*models.Duty{sObranom, ovisna, istekla, samoDatum, opozvana, stalna} {
		nuzno(t, repo.AddDuty(d))
	}
	nuzno(t, repo.RevokeDuty(opozvana.ID))

	popis, err := repo.PrivremeneSIstekom()
	nuzno(t, err)
	ima := map[uuid.UUID]bool{}
	for _, d := range popis {
		ima[d.ID] = true
	}
	if len(popis) != 3 || !ima[sObranom.ID] || !ima[ovisna.ID] || !ima[istekla.ID] {
		t.Errorf("popis: %d %v", len(popis), ima)
	}
}

// Upis isteka mijenja samo istek i bilježi verziju, koja ga nosi drugim čvorovima
func TestPostaviIstek(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	d := privremena(nil, true, nil)
	nuzno(t, repo.AddDuty(d))
	kraj := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	nuzno(t, repo.PostaviIstek(d.ID, &kraj))

	p, err := repo.GetDuty(d.ID)
	nuzno(t, err)
	if p.ExpiresAt == nil || !p.ExpiresAt.Equal(kraj) || !p.IsticeSObranom || p.Rok != nil {
		t.Fatalf("poslije upisa isteka: istek %v, s obranom %v, rok %v", p.ExpiresAt, p.IsticeSObranom, p.Rok)
	}
	top, err := n.rec.Latest(ctxRaz, EntityDuties, d.ID.String())
	nuzno(t, err)
	var uVerziji models.Duty
	nuzno(t, json.Unmarshal(top.Payload, &uVerziji))
	if uVerziji.ExpiresAt == nil || !uVerziji.ExpiresAt.Equal(kraj) {
		t.Errorf("verzija ne nosi istek: %v", uVerziji.ExpiresAt)
	}

	// poništen prekid obrane vraća imenovanje: istek se briše
	nuzno(t, repo.PostaviIstek(d.ID, nil))
	if p, _ := repo.GetDuty(d.ID); p.ExpiresAt != nil {
		t.Errorf("istek nije obrisan: %v", p.ExpiresAt)
	}
	// nepostojeća dužnost: nema verzije
	if err := repo.PostaviIstek(uuid.New(), &kraj); err == nil {
		t.Errorf("nepostojeća dužnost bez greške")
	}
}

// Stupac ovisi_o: prazno i neispravno je bez ovisnosti
func TestOvisiOZapis(t *testing.T) {
	id := uuid.New()
	if ovisiOZapis(nil) != "" || ovisiOZapis(&id) != id.String() {
		t.Errorf("zapis: %q %q", ovisiOZapis(nil), ovisiOZapis(&id))
	}
	if ovisiOIzZapisa("") != nil || ovisiOIzZapisa("nije-uuid") != nil {
		t.Errorf("prazno ili neispravno mora biti nil")
	}
	if p := ovisiOIzZapisa(id.String()); p == nil || *p != id {
		t.Errorf("čitanje: %v", p)
	}
}

// Greška baze ne prolazi tiho: zatvorena baza, tablica koje nema i zapis
// koji se ne da pročitati vraćaju grešku, a ne prazan popis ili tihi upis
func TestPrivremeneGreskeBaze(t *testing.T) {
	zatvorena := noviCvor(t, "ured")
	repo := NewUserRepository(zatvorena.db, zatvorena.rec)
	zatvorena.db.Close()
	if _, err := repo.PrivremeneSIstekom(); err == nil {
		t.Errorf("popis iz zatvorene baze bez greške")
	}
	if err := repo.PostaviIstek(uuid.New(), nil); err == nil {
		t.Errorf("upis u zatvorenu bazu bez greške")
	}

	bezTablice := noviCvor(t, "ured")
	repo = NewUserRepository(bezTablice.db, bezTablice.rec)
	if _, err := bezTablice.db.Exec(`ALTER TABLE duties RENAME TO duties_negdje`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PrivremeneSIstekom(); err == nil {
		t.Errorf("popis bez tablice dužnosti bez greške")
	}
	if err := repo.PostaviIstek(uuid.New(), nil); err == nil {
		t.Errorf("upis bez tablice dužnosti bez greške")
	}

	necitljiv := noviCvor(t, "ured")
	repo = NewUserRepository(necitljiv.db, necitljiv.rec)
	d := privremena(nil, true, nil)
	nuzno(t, repo.AddDuty(d))
	if _, err := necitljiv.db.Exec(`UPDATE duties SET created_at = 'nije vrijeme' WHERE id = ?`, d.ID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PrivremeneSIstekom(); err == nil {
		t.Errorf("necitljiv zapis bez greške")
	}

	// knjiga se ne da upisati: istek se ne sprema bez verzije
	bezKnjige := noviCvor(t, "ured")
	repo = NewUserRepository(bezKnjige.db, bezKnjige.rec)
	d = privremena(nil, true, nil)
	nuzno(t, repo.AddDuty(d))
	if _, err := bezKnjige.db.Exec(`ALTER TABLE record_versions RENAME TO record_versions_negdje`); err != nil {
		t.Fatal(err)
	}
	kraj := time.Now()
	if err := repo.PostaviIstek(d.ID, &kraj); err == nil {
		t.Errorf("upis isteka bez knjige bez greške")
	}
	if _, err := bezKnjige.db.Exec(`ALTER TABLE record_versions_negdje RENAME TO record_versions`); err != nil {
		t.Fatal(err)
	}
	if p, _ := repo.GetDuty(d.ID); p.ExpiresAt != nil {
		t.Errorf("istek spremljen bez verzije: %v", p.ExpiresAt)
	}
}
