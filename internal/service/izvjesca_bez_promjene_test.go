package service

import (
	"context"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Odluke (8. 10. 2026.), pitanje 12: u nacrt predano izvješće vraća
// spremljena izmjena. Spremanje bez ikakve promjene ništa ne mijenja:
// izvješće ostaje predano s istim vremenom predaje, nova verzija se ne
// piše, pa se ni izvješće dionice ne može praznim spremanjem povući bez
// potvrde. Pero Perić sastavlja i predaje, Pero Perić (zamjenik) sprema bez
// promjene.
func TestSpremanjePredanogBezPromjeneOstajePredano(t *testing.T) {
	o := novaOkolinaIzvjesca(t)
	ctx := context.Background()
	verzija := func(entitet, id string) int {
		t.Helper()
		var n int
		if err := o.baza.QueryRow(`SELECT count(*) FROM record_versions WHERE entity = ? AND entity_id = ?`, entitet, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	t.Run("izvješće dionice", func(t *testing.T) {
		iz := punoIzvjesce("P.1.1", danas())
		if err := o.svc.Spremi(ctx, o.pero, o.pisePero, o.d111, iz); err != nil {
			t.Fatal(err)
		}
		if err := o.svc.Predaj(ctx, o.pero, o.pisePero, o.d111, iz.ID); err != nil {
			t.Fatal(err)
		}
		predano, _ := o.svc.Get(ctx, iz.ID)
		n := verzija(repository.EntityDnevnaIzvjesca, iz.ID)
		isto := punoIzvjesce("P.1.1", danas())
		isto.ID = iz.ID
		if err := o.svc.Spremi(ctx, o.urednik, o.upravaP, o.d111, isto); err != nil {
			t.Fatal(err)
		}
		g, _ := o.svc.Get(ctx, iz.ID)
		if !g.Predano() || !g.PredanoAt.Equal(*predano.PredanoAt) || g.PonovnoPredano() || g.TkoJeMijenjao() != "" || !g.IzradenoAt.Equal(predano.IzradenoAt) {
			t.Errorf("predano izvješće dionice nakon spremanja bez promjene: %+v", g)
		}
		if m := verzija(repository.EntityDnevnaIzvjesca, iz.ID); m != n {
			t.Errorf("spremanje bez promjene upisalo je verziju: %d → %d", n, m)
		}
		if !isto.Predano() || !isto.PredanoAt.Equal(*predano.PredanoAt) {
			t.Errorf("pozivatelj ne vidi da je izvješće ostalo predano: %+v", isto)
		}
		// izmjena ga i dalje vraća u nacrt
		isto.Sadrzaj.Pregled = "Procjeđivanje kod Primjerova."
		if err := o.svc.Spremi(ctx, o.urednik, o.upravaP, o.d111, isto); err != nil {
			t.Fatal(err)
		}
		if g, _ := o.svc.Get(ctx, iz.ID); g.Predano() || !g.BiloPredano() || g.TkoJeMijenjao() != "Pero Perić (zamjenik)" {
			t.Errorf("izmjena predanog izvješća dionice: %+v", g)
		}
	})

	t.Run("izvješće sektora", func(t *testing.T) {
		obrazac := func(id string) *models.SektorskoIzvjesce {
			return &models.SektorskoIzvjesce{ID: id, Sektor: "P", Dan: danas(), Sadrzaj: models.SektorskiSadrzaj{Hidrometeo: "Vodostaji rastu."}}
		}
		iz := obrazac("")
		if err := o.svc.SpremiSektorsko(ctx, o.pero, o.upravaP, iz, nil, nil); err != nil {
			t.Fatal(err)
		}
		if err := o.svc.PredajSektorsko(ctx, o.pero, o.upravaP, iz.ID); err != nil {
			t.Fatal(err)
		}
		predano, _ := o.svc.GetSektorsko(ctx, iz.ID)
		n := verzija(repository.EntitySektorskaIzvjesca, iz.ID)
		isto := obrazac(iz.ID)
		if err := o.svc.SpremiSektorsko(ctx, o.urednik, o.upravaP, isto, nil, nil); err != nil {
			t.Fatal(err)
		}
		g, _ := o.svc.GetSektorsko(ctx, iz.ID)
		if !g.Predano() || !g.PredanoAt.Equal(*predano.PredanoAt) || g.PonovnoPredano() || g.TkoJeMijenjao() != "" {
			t.Errorf("predano izvješće sektora nakon spremanja bez promjene: %+v", g)
		}
		if m := verzija(repository.EntitySektorskaIzvjesca, iz.ID); m != n {
			t.Errorf("spremanje bez promjene upisalo je verziju: %d → %d", n, m)
		}
		if !isto.Predano() {
			t.Errorf("pozivatelj ne vidi da je izvješće ostalo predano: %+v", isto)
		}
	})
}

// Odluke (8. 10. 2026.), pitanje 12: predano izvješće sektora u nacrt vraća
// spremljena izmjena onoga tko ga sprema — teksta ili odabira izvješća
// dionica i zapisa dnevnika. Izvješće dionice popravljeno poslije predaje i
// novi nacrt dionice za isti dan nisu njegova izmjena: spremanje bez
// promjene ostavlja izvješće sektora predanim, s tablicama kakve su
// predane, i ne navodi onoga tko sprema među onima koji su ga mijenjali.
// Pero Perić piše izvješće dionice, Pero Perić (zamjenik) sastavlja i
// predaje izvješće sektora, a Pero Perić ga poslije sprema.
func TestPredanoSektorskoNeMijenjajuIzvjescaDionica(t *testing.T) {
	o := novaOkolinaIzvjesca(t)
	ctx := context.Background()
	dionica := punoIzvjesce("P.1.1", danas())
	dionica.Sadrzaj.Pravne.Ljudi = 5
	if err := o.svc.Spremi(ctx, o.pero, o.pisePero, o.d111, dionica); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.Predaj(ctx, o.pero, o.pisePero, o.d111, dionica.ID); err != nil {
		t.Fatal(err)
	}
	obrazac := func(id, hidrometeo string) *models.SektorskoIzvjesce {
		return &models.SektorskoIzvjesce{ID: id, Sektor: "P", Dan: danas(), Sadrzaj: models.SektorskiSadrzaj{Hidrometeo: hidrometeo}}
	}
	sektorsko := obrazac("", "Vodostaji rastu.")
	if err := o.svc.SpremiSektorsko(ctx, o.urednik, o.upravaP, sektorsko, []string{dionica.ID}, nil); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.PredajSektorsko(ctx, o.urednik, o.upravaP, sektorsko.ID); err != nil {
		t.Fatal(err)
	}
	predano, _ := o.svc.GetSektorsko(ctx, sektorsko.ID)
	if predano.Sadrzaj.Pregled.Ukupno.Pravne.Ljudi != 5 || predano.Sadrzaj.Pregled.Nacrta != 0 {
		t.Fatalf("priprema: %+v", predano.Sadrzaj.Pregled)
	}

	// poslije predaje dionica P.1.1 popravi i ponovo preda svoje izvješće, a
	// za P.1.2 nastane nacrt za isti dan
	ispravak := punoIzvjesce("P.1.1", danas())
	ispravak.ID = dionica.ID
	ispravak.Sadrzaj.Pravne.Ljudi = 12
	if err := o.svc.Spremi(ctx, o.pero, o.pisePero, o.d111, ispravak); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.Predaj(ctx, o.pero, o.pisePero, o.d111, dionica.ID); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.Spremi(ctx, o.urednik, o.upravaP, o.d112, punoIzvjesce("P.1.2", danas())); err != nil {
		t.Fatal(err)
	}

	// isti tekst i isti odabir: izvješće sektora ostaje predano, kakvo je predano
	isto := obrazac(sektorsko.ID, "Vodostaji rastu.")
	if err := o.svc.SpremiSektorsko(ctx, o.pero, o.upravaP, isto, []string{dionica.ID}, nil); err != nil {
		t.Fatal(err)
	}
	g, _ := o.svc.GetSektorsko(ctx, sektorsko.ID)
	if !g.Predano() || !g.PredanoAt.Equal(*predano.PredanoAt) || g.TkoJeMijenjao() != "" {
		t.Errorf("predano izvješće sektora nakon spremanja bez promjene: predano %v, mijenjali %q", g.Predano(), g.TkoJeMijenjao())
	}
	if p := g.Sadrzaj.Pregled; p.Ukupno.Pravne.Ljudi != 5 || p.Nacrta != 0 {
		t.Errorf("tablice predanog izvješća snimljene iznova bez promjene: %d ljudi, %d nacrta", p.Ukupno.Pravne.Ljudi, p.Nacrta)
	}
	if !isto.Predano() {
		t.Errorf("pozivatelj ne vidi da je izvješće ostalo predano: %+v", isto)
	}

	// izmjena teksta vraća ga u nacrt, tablice se snimaju iznova, a uz
	// izvješće piše tko ga je mijenjao
	izmjena := obrazac(sektorsko.ID, "Vodostaji stagniraju.")
	if err := o.svc.SpremiSektorsko(ctx, o.pero, o.upravaP, izmjena, []string{dionica.ID}, nil); err != nil {
		t.Fatal(err)
	}
	g, _ = o.svc.GetSektorsko(ctx, sektorsko.ID)
	if g.Predano() || !g.BiloPredano() || g.TkoJeMijenjao() != "Pero Perić" || g.Sadrzaj.Pregled.Ukupno.Pravne.Ljudi != 12 || g.Sadrzaj.Pregled.Nacrta != 1 {
		t.Errorf("izmjena predanog izvješća sektora: predano %v, mijenjali %q, pregled %+v", g.Predano(), g.TkoJeMijenjao(), g.Sadrzaj.Pregled)
	}

	// promjena samo odabira izvješća dionica također je izmjena
	if err := o.svc.PredajSektorsko(ctx, o.urednik, o.upravaP, sektorsko.ID); err != nil {
		t.Fatal(err)
	}
	bezDionice := obrazac(sektorsko.ID, "Vodostaji stagniraju.")
	if err := o.svc.SpremiSektorsko(ctx, o.urednik, o.upravaP, bezDionice, nil, nil); err != nil {
		t.Fatal(err)
	}
	g, _ = o.svc.GetSektorsko(ctx, sektorsko.ID)
	if g.Predano() || g.Sadrzaj.Pregled.Izvjesca != 0 || g.TkoJeMijenjao() != "Pero Perić, Pero Perić (zamjenik)" {
		t.Errorf("promjena odabira na predanom izvješću sektora: predano %v, mijenjali %q, pregled %+v", g.Predano(), g.TkoJeMijenjao(), g.Sadrzaj.Pregled)
	}
}

// Odluke (8. 10. 2026.), pitanje 12: što je ušlo u predano izvješće sektora,
// a poslije ga više nema — storniran zapis dnevnika COP-a, obrisan nacrt
// izvješća dionice — obrazac ne nudi, pa ga onaj tko sprema ne može ni
// zadržati. Spremanje bez izmjene teksta i odabira zato izvješće ostavlja
// predanim, kakvo je predano, i ne navodi onoga tko sprema među onima koji
// su ga mijenjali — kao i kad se izvješće dionice popravi. Pero Perić piše
// nacrt dionice, Pero Perić (zamjenik) sastavlja i predaje izvješće sektora.
func TestPredanoSektorskoNestaloIzOdabiraNijeIzmjena(t *testing.T) {
	o := novaOkolinaIzvjesca(t)
	ctx := context.Background()
	pocetak := danas().AddDate(0, 0, -3)
	obrana := &models.Journal{Kind: models.JournalKindDefense, CentarSektor: "P", Title: "Dnevnik COP-a Primjerovo", Year: 2026, StartedAt: &pocetak}
	if err := o.dnevnici.SaveJournal(ctx, obrana); err != nil {
		t.Fatal(err)
	}
	h := danas().Add(7 * time.Hour)
	dojava := &models.JournalEntry{JournalID: obrana.ID, Date: danas(), Kind: models.EntryKindReport, HappenedAt: &h, Text: "Procjeđivanje kod Primjerova.", UserName: "Ana Anić"}
	if err := o.dnevnici.SaveEntry(ctx, dojava); err != nil {
		t.Fatal(err)
	}
	nacrt := punoIzvjesce("P.1.1", danas())
	nacrt.Sadrzaj.Pravne.Ljudi = 7
	if err := o.svc.Spremi(ctx, o.pero, o.pisePero, o.d111, nacrt); err != nil {
		t.Fatal(err)
	}
	obrazac := func(id, napomena string) *models.SektorskoIzvjesce {
		return &models.SektorskoIzvjesce{ID: id, Sektor: "P", Dan: danas(), Sadrzaj: models.SektorskiSadrzaj{Hidrometeo: "Vodostaji rastu.", Napomena: napomena}}
	}
	sektorsko := obrazac("", "")
	if err := o.svc.SpremiSektorsko(ctx, o.urednik, o.upravaP, sektorsko, []string{nacrt.ID}, []string{dojava.ID}); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.PredajSektorsko(ctx, o.urednik, o.upravaP, sektorsko.ID); err != nil {
		t.Fatal(err)
	}
	predano, _ := o.svc.GetSektorsko(ctx, sektorsko.ID)
	if predano.Sadrzaj.Pregled.Izvjesca != 1 || len(predano.Sadrzaj.Zapisi) != 1 {
		t.Fatalf("priprema: %+v", predano.Sadrzaj)
	}

	// zapis se stornira, nacrt dionice briše; obrazac ih više ne nudi
	dojava.Voided, dojava.VoidReason = true, "Krivi dan."
	if err := o.dnevnici.SaveEntry(ctx, dojava); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.Obrisi(ctx, o.pero, o.pisePero, o.d111, nacrt.ID); err != nil {
		t.Fatal(err)
	}

	isto := obrazac(sektorsko.ID, "")
	if err := o.svc.SpremiSektorsko(ctx, o.pero, o.upravaP, isto, nil, nil); err != nil {
		t.Fatal(err)
	}
	g, _ := o.svc.GetSektorsko(ctx, sektorsko.ID)
	if !g.Predano() || !g.PredanoAt.Equal(*predano.PredanoAt) || g.TkoJeMijenjao() != "" {
		t.Errorf("predano izvješće sektora nakon spremanja bez izmjene: predano %v, mijenjali %q", g.Predano(), g.TkoJeMijenjao())
	}
	if g.Sadrzaj.Pregled.Izvjesca != 1 || g.Sadrzaj.Pregled.Ukupno.Pravne.Ljudi != 7 || len(g.Sadrzaj.Zapisi) != 1 {
		t.Errorf("predano izvješće nije ostalo kakvo je predano: %+v", g.Sadrzaj)
	}

	// izmjena teksta ga vraća u nacrt, a nestalo više nije u njemu
	izmjena := obrazac(sektorsko.ID, "Najava za sutra.")
	if err := o.svc.SpremiSektorsko(ctx, o.pero, o.upravaP, izmjena, nil, nil); err != nil {
		t.Fatal(err)
	}
	g, _ = o.svc.GetSektorsko(ctx, sektorsko.ID)
	if g.Predano() || g.TkoJeMijenjao() != "Pero Perić" || g.Sadrzaj.Pregled.Izvjesca != 0 || len(g.Sadrzaj.Zapisi) != 0 {
		t.Errorf("izmjena predanog izvješća sektora: predano %v, mijenjali %q, %+v", g.Predano(), g.TkoJeMijenjao(), g.Sadrzaj)
	}
}

// Snimka izvješća sektora čita izvješća dionica i zapise dnevnika dana;
// kad se jedno od toga ne da pročitati, izvješće se ne sprema.
func TestSnimkaSektorskogBezIzvora(t *testing.T) {
	for _, tablica := range []string{"journal_entries", "dnevna_izvjesca"} {
		t.Run(tablica, func(t *testing.T) {
			o := novaOkolinaIzvjesca(t)
			ctx := context.Background()
			pocetak := danas().AddDate(0, 0, -3)
			obrana := &models.Journal{Kind: models.JournalKindDefense, CentarSektor: "P", Title: "Dnevnik COP-a Primjerovo", Year: 2026, StartedAt: &pocetak}
			if err := o.dnevnici.SaveJournal(ctx, obrana); err != nil {
				t.Fatal(err)
			}
			if _, err := o.baza.Exec(`ALTER TABLE ` + tablica + ` RENAME TO nema_izvora`); err != nil {
				t.Fatal(err)
			}
			iz := &models.SektorskoIzvjesce{Sektor: "P", Dan: danas(), Sadrzaj: models.SektorskiSadrzaj{Hidrometeo: "Vodostaji rastu."}}
			if err := o.svc.SpremiSektorsko(ctx, o.pero, o.upravaP, iz, nil, nil); err == nil {
				t.Error("izvješće sektora spremljeno bez snimke")
			}
			if postoji, _ := o.svc.sektorska.ZaDan(ctx, "P", danas()); postoji != nil {
				t.Errorf("spremljeno: %+v", postoji)
			}
		})
	}
}
