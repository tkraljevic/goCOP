package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/prognoza"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Izvor ranijeg izdanja: izdavač, tuđe prognoze u rukama i jutro na profilu
// čitaju se uz izdanje, a ne uz zadnje izdanje ni prema današnjem satu.

// podaciRanijeg slaže podatke izvoza za izdanje zadanog sata; 0 je zadnje.
func podaciRanijeg(h *PrognozeHandler, sat int64) PrognozePageData {
	r := httptest.NewRequest(http.MethodGet, "/prognoze.xlsx", nil)
	if sat != 0 {
		izdano := satOdEpohe(sat)
		r = r.WithContext(UzRanijeIzdanje(r.Context(), RanijeIzdanje{Trazeno: izdano, Izdano: izdano}))
	}
	return h.podaci(r, "", "")
}

// Čvor koji je prije izdavao sam, a sada prima razmjenom, svoje staro
// izdanje ne pripisuje čvoru od kojeg prima, a primljeno staro izdanje nosi
// čvor koji ga je izdao.
func TestIzdavacRanijegIzdanja(t *testing.T) {
	put := bazaDvaIzdanja(t) // 5. i 9. 10. izdao je ovaj čvor
	pb, err := prognoza.Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	zadnji := satNovog + 1
	for _, p := range []struct {
		sat    int64
		knjiga string
		cvor   string
	}{{zadnji, "v7", "cvor-drugi"}, {satPrastarog, "v3", "cvor-treci"}} {
		if _, err := prognoza.PrimiIzdanje(pb, p.knjiga, prognoza.PaketIzdanja{Zapis: prognoza.ZapisIzdanja{Izdano: p.sat,
			Verzija: 1, Nastalo: satOdEpohe(p.sat).Add(10 * time.Minute), Cvor: p.cvor}}); err != nil {
			t.Fatal(err)
		}
		if err := prognoza.SpremiIzdane(pb, []prognoza.Izdana{
			{Letva: "belisce", Velicina: "vodostaj", Izdano: p.sat, Ciljni: p.sat + 1, Vrijednost: 40, Dolje: 38, Gore: 42, Model: "proba"},
			{Letva: "belisce", Velicina: "vodostaj", Izdano: p.sat, Ciljni: p.sat + 2, Vrijednost: 41, Dolje: 39, Gore: 43, Model: "proba"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	pb.Close()
	h := rukovateljBaze(t, put)
	for sat, want := range map[int64]string{0: "cvor-drugi", satStarog: "", satPrastarog: "cvor-treci"} {
		if got := podaciRanijeg(h, sat).IzdaoCvor; got != want {
			t.Errorf("izdanje %v: izdavač %q, želim %q", satOdEpohe(sat), got, want)
		}
	}
}

// Uz ranije izdanje tuđa prognoza je ona koju je izdavač tada imao u
// rukama, a ne svaka izdana do sata izdanja: mađarska prognoza starijeg sata
// koja je stigla tek poslije izdanja u izvozu toga izdanja ne stoji.
func TestRanijeIzdanjeTudePrognozeIzZapisa(t *testing.T) {
	put := bazaDvaIzdanja(t) // uz 5. 10. stoji mađarska prognoza izdana 2 h ranije (222)
	pb, err := prognoza.Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	_, ciljevi := daniPregleda(satOdEpohe(satStarog))
	if _, err := pb.Exec(`INSERT INTO tude (izvor, letva, izdano, ciljni, vrijednost, raspon) VALUES (?, 'belisce', ?, ?, 211, 0)`,
		prognoza.Podrijetlo, satStarog-5, ciljevi[0]); err != nil {
		t.Fatal(err)
	}
	// Izdavač je u rukama imao onu od 5 h ranije; ona od 2 h ranije stigla je poslije.
	if _, err := pb.Exec(`UPDATE izdanja SET tude = ? WHERE izdano = ?`,
		`{"`+prognoza.Podrijetlo+`":`+strconv.FormatInt(satStarog-5, 10)+`}`, satStarog); err != nil {
		t.Fatal(err)
	}
	pb.Close()
	h := rukovateljBaze(t, put)
	w := izvezi(h, "?dan=2026-10-05&sat=10")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	txt := tekstZipa(t, w.Body.Bytes())
	if !strings.Contains(txt, ">211<") || strings.Contains(txt, ">222<") {
		t.Error("izvoz ranijeg izdanja ne uzima tuđu prognozu koju je izdavač tada imao u rukama")
	}
}

// Jutro na profilu ranijeg izdanja je jutro dana izdanja, ne današnje:
// očitanja se čitaju do sata izdanja.
func TestJutroRanijegIzdanja(t *testing.T) {
	h := rukovateljRanijih(t)
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "op.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	stanice := repository.NewStationRepository(baza, rec)
	ocitanja := repository.NewReadingRepository(baza, rec)
	h.stations = service.NewStationService(stanice, nil, nil)
	h.SetReadings(service.NewReadingService(ocitanja, stanice, nil, nil, nil))
	ctx := context.Background()
	kota := 80.0
	jucer := time.Now().In(models.Zagreb).AddDate(0, 0, -1)
	sedamU := func(t time.Time) time.Time {
		return time.Date(t.Year(), t.Month(), t.Day(), JutroSat, 0, 0, 0, models.Zagreb)
	}
	type mjerenje struct {
		kad time.Time
		cm  int
	}
	for i, l := range []struct{ kod, ime, rkm string }{
		{"belisce", "Gornji Brod", "rkm 50+000"}, {"donji-miholjac", "Donji Brod", "rkm 40+000"}} {
		st := &models.Station{ID: uuid.New(), Code: l.kod, Name: l.ime, Watercourse: "Brodska rijeka", ZeroDatum: &kota, Stationing: l.rkm}
		if err := stanice.CreateStation(ctx, st); err != nil {
			t.Fatal(err)
		}
		ocitano := []mjerenje{
			{sedamU(satOdEpohe(satStarog).In(models.Zagreb)), 120 + i}, // jutro dana izdanja
			{sedamU(jucer.AddDate(0, 0, -1)), 300 + i},
			{sedamU(jucer), 310 + i},
		}
		// Uobičajena voda (medijan mjesec dana prije jutra) treba barem tjedan dana mjerenja.
		for d := 1; d <= MjesecNajmanjeDana+1; d++ {
			ocitano = append(ocitano, mjerenje{time.Date(2026, 10, 5-d, 12, 0, 0, 0, models.Zagreb), 100 + i})
		}
		for _, o := range ocitano {
			if err := ocitanja.Create(ctx, &models.Reading{StationID: st.ID.String(), MeasuredAt: o.kad.UTC(),
				LevelCm: &o.cm, Source: models.ReadingSourceManual}); err != nil {
				t.Fatal(err)
			}
		}
	}
	data := podaciRanijeg(h, satStarog)
	if len(data.Profili) == 0 {
		t.Fatalf("uz ranije izdanje nema profila: %s", data.BezProfila)
	}
	if got := data.Profili[0].JutroOpis; got != "jutro 5. 10. u 7 h" {
		t.Errorf("jutro na profilu ranijeg izdanja: %q, želim jutro dana izdanja", got)
	}
}
