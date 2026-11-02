package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Stanje sektora po branjenim područjima: područja u obrani prva, s plohom
// faze, ostala u preventivnoj obrani
func TestKarticaStanjaSektora(t *testing.T) {
	od := time.Date(2026, 10, 30, 6, 15, 0, 0, models.Zagreb)
	podrucja := []models.Area{{ID: 15, Name: "Mali sliv Vuka"}, {ID: 34, Name: "Drava i Dunav"}, {ID: 16}}
	dionice := []models.Section{{Code: "B.34.1", AreaID: 34}, {Code: "B.15.1", AreaID: 15}}
	stanja := map[string]models.StanjeObrane{"B.34.1": {Aktivni: []models.AktivniStadij{{Stupanj: models.PhasePrep, Od: od}}}}
	k := karticaStanjaSektora("B", podrucja, stanja, dionice)
	if k.Naslov != "Stanje obrane · Sektor B" || len(k.Redovi) != 3 {
		t.Fatalf("kartica: %+v", k)
	}
	if r := k.Redovi[0]; r.Glavno != "BP 34 · Drava i Dunav" || r.Istaknuto != "prep" || r.Sporedno != "od 30. 10." || !r.Hitno {
		t.Errorf("područje u obrani: %+v", r)
	}
	if r := k.Redovi[1]; r.Glavno != "BP 15 · Mali sliv Vuka" || r.Vrijednost != "preventivna obrana" || r.Istaknuto != "" {
		t.Errorf("mirno područje: %+v", r)
	}
	if k.Redovi[2].Glavno != "BP 16" {
		t.Errorf("područje bez imena: %+v", k.Redovi[2])
	}
	if p := karticaStanjaSektora("B", nil, nil, nil); p.Prazno == nil || p.Prazno.Vrsta != PraznoNedostupno {
		t.Errorf("bez područja: %+v", p)
	}
}

// Moje dežurstvo: do kada dežuram i tko preuzima; inače tko dežura ili da
// nitko ne dežura; u preventivnoj obrani dežurstva nema
func TestKarticaMojegDezurstva(t *testing.T) {
	sad := time.Date(2026, 11, 2, 10, 0, 0, 0, models.Zagreb)
	ja := &models.User{ID: uuid.New(), FullName: "Pero Proba"}
	dnevnik := &models.Journal{ID: "dn"}
	st := &service.StanjeObrane{Sektor: "B", Centar: "COP Osijek", Dnevnik: dnevnik}
	plan := []models.Dezurstvo{
		{UserID: ja.ID.String(), Opis: opisDezurstva, Od: sad.Add(-3 * time.Hour), Do: sad.Add(9 * time.Hour)},
		{UserID: "druga", UserName: "Iva Proba", Opis: opisDezurstva, Od: sad.Add(9 * time.Hour), Do: sad.Add(21 * time.Hour)},
		{UserID: "treca", UserName: "Kasnije", Opis: opisDezurstva, Od: sad.Add(21 * time.Hour), Do: sad.Add(33 * time.Hour)},
		{UserID: "teren", UserName: "Teren", Opis: models.OpisiRada[2].Opis, Od: sad.Add(9 * time.Hour), Do: sad.Add(12 * time.Hour)},
	}
	k := karticaMojegDezurstva("B", st, plan, ja, sad)
	if k.URL != "/dnevnici/dn/dezurstva" || len(k.Redovi) != 1 || k.Redovi[0].Glavno != "Dežurstvo do 19:00" || k.Redovi[0].Sporedno != "COP Osijek · smjenu preuzima Iva Proba" {
		t.Errorf("moja smjena: %+v", k)
	}
	if k := karticaMojegDezurstva("B", st, plan[:1], ja, sad); k.Redovi[0].Sporedno != "COP Osijek · smjenu nakon toga plan nema" {
		t.Errorf("bez iduće smjene: %+v", k.Redovi)
	}
	od := sad.Add(-time.Hour)
	st.Dnevnik = &models.Journal{ID: "dn", DezurniID: "druga", DezurniIme: "Iva Proba", DezurniOd: &od}
	if k := karticaMojegDezurstva("B", st, nil, ja, sad); k.Redovi[0].Glavno != "Dežura Iva Proba" || k.Redovi[0].Sporedno != "od danas 09:00" {
		t.Errorf("dežura drugi: %+v", k.Redovi)
	}
	st.Dnevnik = dnevnik
	if k := karticaMojegDezurstva("B", st, nil, ja, sad); k.Redovi[0].Glavno != "Nitko ne dežura" || !k.Redovi[0].Hitno {
		t.Errorf("nitko ne dežura: %+v", k.Redovi)
	}
	if k := karticaMojegDezurstva("B", nil, nil, ja, sad); k.Prazno == nil || !strings.Contains(k.Prazno.Tekst, "preventivnoj obrani") {
		t.Errorf("preventivna obrana: %+v", k)
	}
}

// Dnevnik COP-a: zadnja tri upisa, najnoviji prvi, bez poništenih
func TestKarticaDnevnikaCOP(t *testing.T) {
	sad := time.Date(2026, 11, 2, 11, 0, 0, 0, models.Zagreb)
	st := &service.StanjeObrane{Dnevnik: &models.Journal{ID: "dn"}}
	kad := func(h int) time.Time { return time.Date(2026, 11, 2, h, 0, 0, 0, models.Zagreb) }
	dogodilo := kad(5)
	upisi := []models.JournalEntry{
		{Text: "najstariji", UserName: "A", CreatedAt: kad(6)},
		{Text: "drugi", UserName: "B", CreatedAt: kad(7), HappenedAt: &dogodilo},
		{Text: "treći", UserName: "C", CreatedAt: kad(8)},
		{Text: "poništen", UserName: "D", CreatedAt: kad(9), Voided: true},
		{Text: "zadnji", CreatedAt: kad(10)},
	}
	k := karticaDnevnikaCOP(st, upisi, sad)
	if len(k.Redovi) != 3 || k.Redovi[0].Glavno != "zadnji" || k.Redovi[0].Sporedno != "danas 10:00" || k.Redovi[2].Sporedno != "danas 05:00 · B" ||
		k.URL != "/dnevnici/dn" || k.Gumbi[0].URL != "/dnevnici/dn#novi-zapis" {
		t.Errorf("upisi: %+v", k)
	}
	if k := karticaDnevnikaCOP(st, nil, sad); k.Tekst != "U dnevnik danas još nema upisa." || len(k.Gumbi) != 1 {
		t.Errorf("bez upisa: %+v", k)
	}
	if k := karticaDnevnikaCOP(nil, nil, sad); k.Prazno == nil {
		t.Errorf("bez dnevnika: %+v", k)
	}
}

// Naslovna dežurnog kroz rukovatelja: kartice njegova sektora, vodostaji
// samo sektora, za riješiti bez prava i djelatnika
func TestNaslovnaDezurnog(t *testing.T) {
	o := novaOkolinaNaslovne(t)
	dez := dozvoleNaslovne(false, models.RoleOperator)
	dez.User.Duties[0].SectorID = nizNaslovne("B")
	if karticeUloge(dez)[0] != kMojeDezurstvo {
		t.Fatalf("kartice dežurnog: %v", karticeUloge(dez))
	}
	s := o.zovi("/", dez).Body.String()
	for _, zeli := range []string{"Moje dežurstvo", "preventivnoj obrani", "Dnevnik COP-a nije otvoren", "Stanje sektora trenutačno nije dostupno."} {
		if !strings.Contains(s, zeli) {
			t.Errorf("Naslovna dežurnog nema %q", zeli)
		}
	}
	prava := 0
	o.h.SetIzvori(IzvoriNaslovne{
		StanjaSektora: func(context.Context, string) (map[string]models.StanjeObrane, []models.Section, error) {
			return nil, nil, nil
		},
		Podrucja: func(string) ([]models.Area, error) { return []models.Area{{ID: 16, Name: "Baranja"}}, nil },
		Letve: func(context.Context) ([]models.GaugeSummary, error) {
			return []models.GaugeSummary{
				{Name: "Batina", SectorID: "B", Phase: models.PhasePrep, Latest: &models.Reading{LevelCm: cmNaslovne(300), MeasuredAt: time.Now()}},
				{Name: "Sisak", SectorID: "C", Phase: models.PhasePrep, Latest: &models.Reading{LevelCm: cmNaslovne(600), MeasuredAt: time.Now()}},
				{Name: "Dalj", SectorIDs: []string{"B"}, Phase: models.PhaseRegular, Latest: &models.Reading{LevelCm: cmNaslovne(500), MeasuredAt: time.Now()}},
			}, nil
		},
		Prava: func(context.Context) (int, string, error) { prava++; return 3, "", nil },
	})
	if s := o.zovi("/", dez).Body.String(); !strings.Contains(s, "BP 16 · Baranja") {
		t.Error("stanje sektora bez područja")
	}
	w := o.zovi("/naslovna/kartica/vodostaji", dez).Body.String()
	if !strings.Contains(w, "Batina") || !strings.Contains(w, "Dalj") || strings.Contains(w, "Sisak") {
		t.Errorf("vodostaji dežurnog: %s", w)
	}
	if w := o.zovi("/naslovna/kartica/za-rijesiti", dez).Body.String(); prava != 0 || !strings.Contains(w, "Nema ništa za riješiti.") {
		t.Errorf("za riješiti dežurnog: %d %s", prava, w)
	}
	o.h.SetIzvori(IzvoriNaslovne{
		StanjaSektora: func(context.Context, string) (map[string]models.StanjeObrane, []models.Section, error) {
			return nil, nil, errors.New("baza zauzeta")
		},
		Podrucja: func(string) ([]models.Area, error) { return nil, nil },
	})
	if s := o.zovi("/", dez).Body.String(); !strings.Contains(s, "Stanje obrane · Sektor B") || !strings.Contains(s, "Stanje sektora trenutačno nije dostupno.") {
		t.Error("stanje sektora s greškom")
	}
}

// Dežurni s otvorenim dnevnikom: plan i upisi dolaze iz izvora
func TestKarticeDezurnogSDnevnikom(t *testing.T) {
	dez := dozvoleNaslovne(false, models.RoleOperator)
	dez.User.Duties[0].SectorID = nizNaslovne("B")
	sad := time.Now()
	data := &DashboardViewData{Stanja: []service.StanjeObrane{{Sektor: "B", Centar: "COP Osijek", Dnevnik: &models.Journal{ID: "dn"}}}}
	h := &DashboardHandler{izvori: IzvoriNaslovne{
		Dezurstva: func(context.Context, string) ([]models.Dezurstvo, error) {
			return []models.Dezurstvo{{UserID: dez.User.ID.String(), Opis: opisDezurstva, Od: sad.Add(-time.Hour), Do: sad.Add(time.Hour)}}, nil
		},
		Zapisi: func(context.Context, string) ([]models.JournalEntry, error) {
			return []models.JournalEntry{{Text: "Proba upisa", CreatedAt: sad}}, nil
		},
	}}
	r := zahtjevSDozvolama(dez)
	if k := h.karticaDezurnog(r, dez, data, kMojeDezurstvo); len(k.Redovi) != 1 || !strings.HasPrefix(k.Redovi[0].Glavno, "Dežurstvo do ") {
		t.Errorf("moje dežurstvo: %+v", k)
	}
	if k := h.karticaDezurnog(r, dez, data, kDnevnikCOP); len(k.Redovi) != 1 || k.Redovi[0].Glavno != "Proba upisa" {
		t.Errorf("dnevnik COP-a: %+v", k)
	}
	if stanjeSektora(data.Stanja, "C") != nil {
		t.Error("stanje tuđeg sektora")
	}
}

// zahtjevSDozvolama je zahtjev Naslovne osobe s danim pravima
func zahtjevSDozvolama(p *models.UserPermissions) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(r.Context(), contextKeyUser, &p.User)
	return r.WithContext(context.WithValue(ctx, contextKeyPerms, p))
}
