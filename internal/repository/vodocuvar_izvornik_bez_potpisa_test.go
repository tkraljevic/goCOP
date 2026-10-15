package repository

import (
	"testing"
	"time"

	"gocop/internal/models"
)

// Izvornik bez potpisa lista (Odluke (8. 10. 2026.), pitanje 7): čvor 0.0.34
// u izvornik ne upisuje potpis (predano_at), a ni izvornici upisani prije
// ove dorade ga nemaju. Izvornik se sprema odmah nakon predaje ili ovjere
// na istom čvoru, pa pripada potpisu zadnje predane verzije lista koju je
// taj čvor upisao prije njega. Osobe su izmišljene: vodočuvar Ivo Ivić,
// rukovoditeljica Ana Anić.

// bezPotpisaUIzvornicima briše potpis iz izvornika koje je čvor upisao u
// knjigu, kakve ih piše čvor 0.0.34
func bezPotpisaUIzvornicima(t *testing.T, n *cvor) {
	t.Helper()
	_, err := n.db.Exec(`UPDATE record_versions SET payload = json_remove(payload, '$.predano_at')
		WHERE entity = ? AND node_id = ?`, EntityVodocuvarskiIzvornici, n.ime)
	nuzno(t, err)
}

// Ivo preda list na novom čvoru, a kasnije bez veze i na čvoru 0.0.34,
// čiji izvornik nema potpis. Nakon razmjene list prvog potpisa ima izvornik
// novog čvora, a list drugog potpisa izvornik čvora 0.0.34, na svakom
// čvoru s novim programom.
func TestIzvornikBezPotpisaIdeUzListSvogCvora(t *testing.T) {
	spremisteZaProbu(t)
	a, stari, c := noviCvor(t, "novi"), noviCvor(t, "stari"), noviCvor(t, "treci")
	x := models.IDListaVodocuvara(ivoIvic, danIve)
	predajIvu(t, a, "predan na novom")
	izvornikIve(t, a, x, "%PDF novi")
	drugi := predajIvu(t, stari, "predan na starom")
	izvornikIve(t, stari, x, "%PDF stari")
	bezPotpisaUIzvornicima(t, stari)

	posalji(t, stari, a)
	posalji(t, a, c)
	posalji(t, stari, c)
	y := models.IDDvostrukogLista(x, *drugi.PredanoAt)
	for _, n := range []*cvor{a, c} {
		prvi, dvojnik := listoviPoOznaci(t, n, x, y)
		if prvi.Opis != "predan na novom" || dvojnik.Opis != "predan na starom" {
			t.Fatalf("čvor %s: prvi %+v\ndrugi %+v", n.ime, prvi, dvojnik)
		}
		if got := pdfIve(t, n, x); got != "%PDF novi" {
			t.Errorf("čvor %s: izvornik prvog lista %q", n.ime, got)
		}
		if got := pdfIve(t, n, y); got != "%PDF stari" {
			t.Errorf("čvor %s: izvornik drugog lista %q", n.ime, got)
		}
	}
}

// List s oznakom otprije (UUIDv7) predan na dva čvora prije ove dorade:
// nijedan izvornik nema potpis. Obnova površine iz knjige pri nadogradnji
// daje svakom listu izvornik čvora koji ga je predao.
func TestIzvornikBezPotpisaPriObnoviPovrsine(t *testing.T) {
	spremisteZaProbu(t)
	a, b := noviCvor(t, "posluzitelj"), noviCvor(t, "laptop")
	stariID := "01900000-0000-7000-8000-0000000000d1"
	predajStari := func(n *cvor, opis string) *models.VodocuvarskiList {
		t.Helper()
		l := listIve(opis)
		l.ID = stariID
		kad := time.Now()
		l.PredanoAt = &kad
		nuzno(t, NewVodocuvarRepository(n.db, n.rec).Predaj(ctxRaz, l, nil))
		izvornikIve(t, n, stariID, "%PDF "+opis)
		bezPotpisaUIzvornicima(t, n)
		return l
	}
	predajStari(a, "predan na poslužitelju")
	drugi := predajStari(b, "predan na laptopu")
	razmijeniSve(t, a, b)
	y := models.IDDvostrukogLista(stariID, *drugi.PredanoAt)
	for _, n := range []*cvor{a, b} {
		if _, err := ReplaySurface(ctxRaz, n.db, n.rec); err != nil {
			t.Fatal(err)
		}
		prvi, dvojnik := listoviPoOznaci(t, n, stariID, y)
		if prvi.Opis != "predan na poslužitelju" || dvojnik.Opis != "predan na laptopu" {
			t.Fatalf("čvor %s: prvi %+v\ndrugi %+v", n.ime, prvi, dvojnik)
		}
		if pdfIve(t, n, stariID) != "%PDF predan na poslužitelju" || pdfIve(t, n, y) != "%PDF predan na laptopu" {
			t.Errorf("čvor %s: izvornici %q, %q", n.ime, pdfIve(t, n, stariID), pdfIve(t, n, y))
		}
	}
}

// Potpis izvornika bez potpisa je potpis zadnje predane verzije lista koju
// je isti čvor upisao prije izvornika: verzija drugog čvora, kasnija verzija
// i nacrt ne daju ga
func TestPotpisIzvornika(t *testing.T) {
	prvi, drugi := time.Date(2026, time.October, 14, 15, 0, 0, 0, models.Zagreb), time.Date(2026, time.October, 14, 16, 0, 0, 0, models.Zagreb)
	verzija := func(cvor, oznaka string, potpis *time.Time) verzijaLista {
		return verzijaLista{cvor: cvor, verzija: oznaka, list: models.VodocuvarskiList{PredanoAt: potpis}}
	}
	iz := verzijaIzvornika{cvor: "stari", verzija: "05"}
	for _, s := range []struct {
		ime     string
		listovi []verzijaLista
		want    *time.Time
	}{
		{"bez verzija lista", nil, nil},
		{"verzija drugog čvora", []verzijaLista{verzija("novi", "01", &prvi)}, nil},
		{"kasnija verzija", []verzijaLista{verzija("stari", "06", &prvi)}, nil},
		{"nacrt", []verzijaLista{verzija("stari", "01", nil)}, nil},
		{"zadnja predana prije izvornika", []verzijaLista{verzija("stari", "01", &prvi), verzija("novi", "02", &prvi), verzija("stari", "03", &drugi), verzija("stari", "07", &prvi)}, &drugi},
	} {
		got := potpisIzvornika(iz, s.listovi)
		if (got == nil) != (s.want == nil) || (got != nil && !got.Equal(*s.want)) {
			t.Errorf("%s: potpis %v, očekivan %v", s.ime, got, s.want)
		}
	}
	izvornici := []verzijaIzvornika{iz, {cvor: "stari", verzija: "08", iz: models.IzvornikLista{PredanoAt: &prvi}}}
	dopuniPotpise(izvornici, []verzijaLista{verzija("stari", "03", &drugi)})
	if !izvornici[0].iz.PredanoAt.Equal(drugi) || !izvornici[1].iz.PredanoAt.Equal(prvi) {
		t.Errorf("dopunjeni potpisi: %v, %v", izvornici[0].iz.PredanoAt, izvornici[1].iz.PredanoAt)
	}
}
