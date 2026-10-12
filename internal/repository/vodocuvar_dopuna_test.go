package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"gocop/internal/models"
)

// upisOsobe je bilješka izmišljene osobe na listu
func upisOsobe(ime string, kad time.Time) models.UpisRukovoditelja {
	return models.UpisRukovoditelja{UserID: "id-" + ime, Ime: ime, Kad: kad, Tekst: "upisao " + ime}
}

// Kopija predanog lista pročitana prije nego što je druga kartica na list
// dodala bilješku, parafu, ovjeru i prijavu: njezin upis sve to zadržava,
// i na površini i u verziji u knjizi.
func TestSpremanjeZadrzavaDodatkeIzMedjuvremena(t *testing.T) {
	o := novaOkolinaBrisanja(t, true)
	ctx := context.Background()
	kopija, err := o.repo.Get(ctx, o.nacrt.ID)
	if err != nil {
		t.Fatal(err)
	}
	sad := time.Now()
	druga, err := o.repo.Get(ctx, o.nacrt.ID)
	if err != nil {
		t.Fatal(err)
	}
	druga.Upisi = append(druga.Upisi, upisOsobe("Iva Ivić", sad))
	druga.Parafe = append(druga.Parafe, models.Parafa{UserID: "id-Ana Anić", Ime: "Ana Anić", Kad: sad})
	druga.PotvrdioID, druga.Potvrdio, druga.PotvrdenoAt = "id-Ana Anić", "Ana Anić", &sad
	druga.Prijave = append(druga.Prijave, models.PrijavaNaListu{ID: "prijava-1"})
	if err := o.repo.Save(ctx, druga); err != nil {
		t.Fatal(err)
	}

	kopija.Upisi = append(kopija.Upisi, upisOsobe("Marko Markić", sad.Add(time.Second)))
	if err := o.repo.Save(ctx, kopija); err != nil {
		t.Fatal(err)
	}
	l, err := o.repo.Get(ctx, o.nacrt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Upisi) != 2 || l.Upisi[0].Ime != "Iva Ivić" || l.Upisi[1].Ime != "Marko Markić" {
		t.Errorf("upisi nakon preklopljenog spremanja: %+v", l.Upisi)
	}
	if len(l.Parafe) != 1 || !l.Potvrden() || l.Potvrdio != "Ana Anić" || len(l.Prijave) != 1 {
		t.Errorf("parafa, ovjera ili prijava izgubljena: %+v %q %+v", l.Parafe, l.Potvrdio, l.Prijave)
	}
	var zadnja models.VodocuvarskiList
	if err := zadnjaVerzijaLista(ctx, o, &zadnja); err != nil {
		t.Fatal(err)
	}
	if len(zadnja.Upisi) != 2 || !zadnja.Potvrden() {
		t.Errorf("verzija u knjizi nema dodatke iz međuvremena: %+v", zadnja.Upisi)
	}
}

// zadnjaVerzijaLista čita zadnju verziju lista iz knjige
func zadnjaVerzijaLista(ctx context.Context, o *okolinaBrisanja, l *models.VodocuvarskiList) error {
	var payload string
	err := o.baza.QueryRowContext(ctx, `SELECT payload FROM record_versions WHERE entity = ? AND entity_id = ? ORDER BY version_id DESC LIMIT 1`,
		EntityVodocuvarski, o.nacrt.ID).Scan(&payload)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(payload), l)
}

// Vodočuvarov obrazac pročitan prije bilješke rukovoditelja: ni spremanje
// nacrta ni predaja ne brišu bilješku upisanu u međuvremenu.
func TestNacrtIPredajaZadrzavajuBiljeskuIzMedjuvremena(t *testing.T) {
	for _, predaja := range []bool{false, true} {
		t.Run(fmt.Sprint("predaja=", predaja), func(t *testing.T) {
			o := novaOkolinaBrisanja(t, false)
			ctx := context.Background()
			obrazac, err := o.repo.Get(ctx, o.nacrt.ID)
			if err != nil {
				t.Fatal(err)
			}
			rukovoditelj, err := o.repo.Get(ctx, o.nacrt.ID)
			if err != nil {
				t.Fatal(err)
			}
			rukovoditelj.Upisi = append(rukovoditelj.Upisi, upisOsobe("Iva Ivić", time.Now()))
			if err := o.repo.Save(ctx, rukovoditelj); err != nil {
				t.Fatal(err)
			}

			obrazac.Opis = "obilazak nasipa i ustave"
			if predaja {
				kad := time.Now()
				obrazac.PredanoAt = &kad
				err = o.repo.Predaj(ctx, obrazac, nil)
			} else {
				err = o.repo.Save(ctx, obrazac)
			}
			if err != nil {
				t.Fatal(err)
			}
			l, err := o.repo.Get(ctx, o.nacrt.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(l.Upisi) != 1 || l.Opis != "obilazak nasipa i ustave" || l.Predan() != predaja {
				t.Errorf("list nakon vodočuvarova spremanja: upisi %+v, opis %q, predan %v", l.Upisi, l.Opis, l.Predan())
			}
		})
	}
}

// Istodobni upisi na isti predani list, svaki iz svoje kopije: sačuvani su
// svi, bez obzira na raspored.
func TestIstodobniUpisiNaPredaniList(t *testing.T) {
	o := novaOkolinaBrisanja(t, true)
	ctx := context.Background()
	const n = 8
	pocetak := time.Now()
	var wg sync.WaitGroup
	greske := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			l, err := o.repo.Get(ctx, o.nacrt.ID)
			if err == nil {
				l.Upisi = append(l.Upisi, upisOsobe(fmt.Sprint("osoba ", i), pocetak.Add(time.Duration(i)*time.Millisecond)))
				err = o.repo.Save(ctx, l)
			}
			greske <- err
		}(i)
	}
	wg.Wait()
	close(greske)
	for err := range greske {
		if err != nil {
			t.Fatal(err)
		}
	}
	l, err := o.repo.Get(ctx, o.nacrt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Upisi) != n {
		t.Errorf("sačuvano %d od %d istodobnih upisa", len(l.Upisi), n)
	}
}
