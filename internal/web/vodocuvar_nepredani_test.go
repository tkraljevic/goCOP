package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Upozorenje na nepredane listove, Odluke (11. 10. 2026.): vodočuvar koji
// dnevni list nije potpisao i predao, a dan mu je prošao, na početnoj
// stranici i u svom dnevniku dobiva upozorenje s poveznicom na svaki takav
// list. Rukovoditeljica njegovih listova ga ne dobiva. Osobe su izmišljene:
// vodočuvar Pero Perić, rukovoditeljica Ana Anić.

func listoviNaDane(dani ...int) []models.VodocuvarskiList {
	var out []models.VodocuvarskiList
	for _, d := range dani {
		out = append(out, models.VodocuvarskiList{ID: "l" + time.Date(2026, time.October, d, 0, 0, 0, 0, models.Zagreb).Format("0102"),
			Datum: time.Date(2026, time.October, d, 0, 0, 0, 0, models.Zagreb)})
	}
	return out
}

// Dani se nabrajaju zarezom, a zadnji s „i”; iza petog dana stoji „i još
// N dana”. Datum već završava točkom, pa iza njega nema druge točke.
func TestUpozorenjeNepredanih(t *testing.T) {
	if u := upozorenjeNepredanih(nil); u != nil {
		t.Fatalf("bez nepredanih listova: %+v", u)
	}
	tmpl := novaOkolinaDnevnika(t).tmpl("vodocuvar.html")
	tekst := func(u *UpozorenjeNepredanih) string {
		t.Helper()
		var b strings.Builder
		if err := tmpl.ExecuteTemplate(&b, "nepredaniListovi", u); err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(b.String())
	}
	if s := tekst(nil); s != "" {
		t.Errorf("upozorenje bez nepredanih listova: %q", s)
	}
	for _, slucaj := range []struct {
		dani []int
		want string
	}{
		{[]int{8}, `Niste potpisali i predali dnevni list za <a href="/vodocuvar/l1008">8. 10.</a> Otvorite list, provjerite ga i predajte.`},
		{[]int{8, 9}, `Niste potpisali i predali dnevne listove za <a href="/vodocuvar/l1008">8. 10.</a> i <a href="/vodocuvar/l1009">9. 10.</a> Otvorite list, provjerite ga i predajte.`},
		{[]int{1, 2, 3}, `za <a href="/vodocuvar/l1001">1. 10.</a>, <a href="/vodocuvar/l1002">2. 10.</a> i <a href="/vodocuvar/l1003">3. 10.</a> Otvorite`},
		{[]int{1, 2, 3, 4, 5}, `<a href="/vodocuvar/l1004">4. 10.</a> i <a href="/vodocuvar/l1005">5. 10.</a> Otvorite`},
		{[]int{1, 2, 3, 4, 5, 6}, `<a href="/vodocuvar/l1004">4. 10.</a>, <a href="/vodocuvar/l1005">5. 10.</a> i još <a href="/vodocuvar">1 dan</a>. Otvorite`},
		{[]int{1, 2, 3, 4, 5, 6, 7, 8}, `<a href="/vodocuvar/l1005">5. 10.</a> i još <a href="/vodocuvar">3 dana</a>. Otvorite`},
	} {
		s := tekst(upozorenjeNepredanih(listoviNaDane(slucaj.dani...)))
		if !strings.Contains(s, slucaj.want) || !strings.Contains(s, "alert-warning") {
			t.Errorf("%v: %s\nočekivano %s", slucaj.dani, s, slucaj.want)
		}
	}
	if u := upozorenjeNepredanih(listoviNaDane(1, 2, 3, 4, 5, 6, 7)); len(u.Dani) != 5 || u.Jos != 2 || u.Ukupno != 7 {
		t.Errorf("sedam nepredanih: %+v", u)
	}
}

func TestUpozorenjeNepredanihKrozRute(t *testing.T) {
	o := novaOkolinaDnevnika(t)
	listopad := func(dan int) time.Time { return time.Date(2026, time.October, dan, 0, 0, 0, 0, models.Zagreb) }
	o.vod.SetSat(func() time.Time { return listopad(11).Add(10 * time.Hour) })
	list := func(dan, nastao time.Time, predan, prenesen bool) *models.VodocuvarskiList {
		t.Helper()
		l := &models.VodocuvarskiList{ID: uuid.Must(uuid.NewV7()).String(), UserID: o.vodocuvar.ID.String(), Ime: "Pero Perić", Sektor: "P", AreaID: 1,
			Datum: dan, Od: "07:00", Do: "15:00", Opis: "obilazak nasipa", CreatedAt: nastao, Rekonstrukcija: prenesen}
		if predan {
			l.Broj, l.PredanoAt = 1, &nastao
		}
		if err := o.repo.Save(context.Background(), l); err != nil {
			t.Fatal(err)
		}
		return l
	}
	list(listopad(6), listopad(6), false, true)
	list(listopad(7), listopad(7), true, false)
	osmi := list(listopad(8), listopad(8).Add(8*time.Hour), false, false)
	unaprijed := list(listopad(9), listopad(1).Add(8*time.Hour), false, false)
	list(listopad(11), listopad(11).Add(8*time.Hour), false, false)
	list(listopad(12), listopad(1).Add(8*time.Hour), false, false)
	upozorenje := `Niste potpisali i predali dnevne listove za <a href="/vodocuvar/` + osmi.ID + `">8. 10.</a> i <a href="/vodocuvar/` + unaprijed.ID + `">9. 10.</a> Otvorite list, provjerite ga i predajte.`

	dashH := NewDashboardHandler(o.users, o.tmpl("dashboard.html"), o.tmpl("registri.html"))
	dashH.SetVodocuvar(func() *service.VodocuvarService { return o.vod })
	o.mux.HandleFunc("GET /{$}", dashH.ShowDashboard)
	for _, putanja := range []string{"/", "/vodocuvar"} {
		if s := o.zovi(t, o.vodocuvar, http.MethodGet, putanja, nil).Body.String(); !strings.Contains(s, upozorenje) {
			t.Errorf("%s: vodočuvar bez upozorenja %q", putanja, upozorenje)
		}
		// rukovoditeljica čita njegove listove, ali upozorenje je samo njegovo
		if s := o.zovi(t, o.rukovodi, http.MethodGet, putanja, nil).Body.String(); strings.Contains(s, "Niste potpisali") {
			t.Errorf("%s: upozorenje rukovoditeljici", putanja)
		}
	}

	// naslovna bez vodočuvarskog dnevnika, ili dok dnevnik nije spreman,
	// prikazuje se bez upozorenja
	cijeli, err := o.users.GetUserByID(o.vodocuvar.ID)
	if err != nil || cijeli == nil {
		t.Fatalf("vodočuvar: %v", err)
	}
	bez := NewDashboardHandler(o.users, o.tmpl("dashboard.html"), o.tmpl("registri.html"))
	nespreman := NewDashboardHandler(o.users, o.tmpl("dashboard.html"), o.tmpl("registri.html"))
	nespreman.SetVodocuvar(func() *service.VodocuvarService { return nil })
	for sto, h := range map[string]*DashboardHandler{"bez dnevnika": bez, "dnevnik nije spreman": nespreman} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		c := context.WithValue(context.WithValue(r.Context(), contextKeyUser, cijeli), contextKeyPerms, models.NewUserPermissions(*cijeli))
		w := httptest.NewRecorder()
		h.ShowDashboard(w, r.WithContext(c))
		if s := w.Body.String(); w.Code != http.StatusOK || !strings.Contains(s, "Centar obrane od poplava") || strings.Contains(s, "Niste potpisali") {
			t.Errorf("%s: %d %s", sto, w.Code, s)
		}
	}
}
