package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// verzijeListova broji verzije listova u knjizi
func (o *okolinaVodocuvara) verzijeListova(t *testing.T) int {
	t.Helper()
	var n int
	if err := o.baza.QueryRow(`SELECT COUNT(*) FROM record_versions WHERE entity = ?`, repository.EntityVodocuvarski).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Isti list predaje se istodobno iz dvije kartice: obje su pročitale list
// nepredan, a druga ga preda nakon što je predaja prve već pročitala list,
// a prije njezina upisa. Predaja prve se odbija kao predaja predanog lista i
// ne upisuje ništa: ostaju vrijeme predaje, opis i zadaci druge kartice, a u
// knjizi nema nove verzije. Isto vrijedi kad lista još nema ni kao nacrta,
// pa bi svaka kartica upisala svoj list za isti dan.
func TestIstodobnaPredajaIstogLista(t *testing.T) {
	for _, slucaj := range []struct {
		naziv string
		nacrt bool
	}{
		{"nacrt već spremljen", true},
		{"bez nacrta", false},
	} {
		t.Run(slucaj.naziv, func(t *testing.T) {
			o := novaOkolinaVodocuvara(t)
			ctx := context.Background()
			u := vdVodocuvar()
			dan := vdDan(time.March, 10)
			z := o.vdZadatak(t, u, "pregledati ustavu", dan)
			if slucaj.nacrt {
				if _, err := o.vs.Spremi(ctx, u, dan, vdUnos("nacrt"), false); err != nil {
					t.Fatal(err)
				}
			}

			drugaKartica := vdUnos("obilazak iz druge kartice")
			drugaKartica.Zadaci = map[string]UnosZadatka{z.ID: {Status: models.ZadatakOdbacen, Obavljeno: "ustava srušena"}}
			var druga *models.VodocuvarskiList
			pozvano, verzije := 0, 0
			o.vs.prijeUpisaPredaje = func(ctx context.Context) {
				pozvano++
				if pozvano > 1 {
					return
				}
				var err error
				if druga, err = o.vs.Spremi(ctx, u, dan, drugaKartica, true); err != nil {
					t.Fatal(err)
				}
				verzije = o.verzijeListova(t)
			}
			prvaKartica := vdUnos("obilazak iz prve kartice")
			prvaKartica.Zadaci = map[string]UnosZadatka{z.ID: {Status: models.ZadatakObavljen, Obavljeno: "pregledana"}}
			l, err := o.vs.Spremi(ctx, u, dan, prvaKartica, true)
			vdGreska(t, err, "list od "+dan.Format("02.01.2006.")+" je već predan i više se ne mijenja")
			if !errors.Is(err, repository.ErrListPredan) {
				t.Errorf("greška nije ErrListPredan: %v", err)
			}
			if l != nil {
				t.Errorf("odbijena predaja vratila je list: %+v", l)
			}
			if druga == nil || !druga.Predan() {
				t.Fatal("druga kartica nije predala list")
			}

			uBazi := o.vdListIzBaze(t, u, dan)
			if uBazi.ID != druga.ID || uBazi.Opis != "obilazak iz druge kartice" || !uBazi.PredanoAt.Equal(*druga.PredanoAt) {
				t.Errorf("predan list prepisan: %s %q %v (druga kartica %s %v)", uBazi.ID, uBazi.Opis, uBazi.PredanoAt, druga.ID, druga.PredanoAt)
			}
			if nl := vdNaListu(uBazi, z.ID); nl == nil || nl.Status != models.ZadatakOdbacen || nl.Obavljeno != "ustava srušena" {
				t.Errorf("zadatak na listu: %+v", uBazi.Zadaci)
			}
			if ev := o.vdZadatakIzBaze(t, z.ID); ev.Status != models.ZadatakOdbacen || ev.ListID != druga.ID {
				t.Errorf("evidencija: %+v", ev)
			}
			var listova int
			if err := o.baza.QueryRow(`SELECT COUNT(*) FROM vodocuvarski_listovi WHERE user_id = ?`, u.ID.String()).Scan(&listova); err != nil {
				t.Fatal(err)
			}
			if listova != 1 {
				t.Errorf("listova za dan: %d, očekivan jedan", listova)
			}
			// odbijena predaja ne upisuje verziju u knjigu
			if n := o.verzijeListova(t); n != verzije {
				t.Errorf("odbijena predaja upisala je verziju u knjigu: %d → %d", verzije, n)
			}
		})
	}
}
