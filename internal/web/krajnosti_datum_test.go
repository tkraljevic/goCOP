package web

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"

	"github.com/google/uuid"
)

func TestDanOcitanja(t *testing.T) {
	if got := danOcitanja("2026-10-21"); got != "21. 10. 2026." {
		t.Errorf("dan očitanja: %q", got)
	}
	if got := danOcitanja("nije-dan"); got != "" {
		t.Errorf("nepoznat zapis: %q", got)
	}
	if got := (Krajnost{Cm: -211}).Label(); got != "-211 cm" {
		t.Errorf("bez vremena: %q", got)
	}
}

// Iza ponoći kartica prognoze pokazuje jučerašnje krajnosti iz arhive dok se
// današnje računaju, a ne samo evidenciju očitanja (Osijek, 4. 11. 2026.).
func TestKrajnostiJucerDokSeRacunaju(t *testing.T) {
	godine := []repository.GodinaVodostaja{{Godina: 2003, Min: -160, Max: 512}}
	h := &PrognozeHandler{arhiva: func() *repository.ArhivaRepository { return &repository.ArhivaRepository{} }}
	h.godisnji.dan, h.godisnji.po = "2000-01-01", map[string][]repository.GodinaVodostaja{"osijek": godine}
	h.godisnji.puni = true // punjenje u pozadini već traje, test ga ne pokreće

	out, cekaju := h.krajnosti([]string{"osijek", "nova-letva"})
	if k := out["osijek"]; k[0] == nil || k[0].Cm != -160 || k[0].Godina != 2003 || cekaju["osijek"] {
		t.Fatalf("jučerašnje krajnosti: %+v, čekaju %v", k, cekaju)
	}
	if !cekaju["nova-letva"] {
		t.Error("letva bez ikakvog izračuna treba čekati arhivu")
	}
	if _, ima := h.godisnji.izPredmemorije("osijek"); ima {
		t.Error("jučerašnji izračun ne smije vrijediti kao današnji")
	}
	h.godisnji.zapamti("osijek", godine)
	if _, ima, svjeze := h.godisnji.zadnje("osijek"); !ima || !svjeze {
		t.Error("današnji izračun treba biti svjež")
	}
	// računalo bez arhive ne čeka: evidencija je sve što ima
	if _, cekaju := (&PrognozeHandler{}).krajnosti([]string{"nova-letva"}); cekaju["nova-letva"] {
		t.Error("bez arhive letva ne smije čekati arhivu")
	}
}

// Dok se arhiva još računa, kartica prognoze ne pokazuje evidenciju kao MIN i
// MAX letve: evidencija je samo dio povijesti. Kad je arhiva pročitana i prazna,
// krajnost iz evidencije kaže od kada evidencija ide, a na pločici je dan očitanja.
func TestKarticaBezEvidencijeDokArhivaCeka(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "krajnosti.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	rec := ledger.New(baza, "test")
	stationRepo := repository.NewStationRepository(baza, rec)
	st := models.Station{ID: uuid.New(), Code: "proba", Name: "Proba"}
	if err := stationRepo.CreateStation(context.Background(), &st); err != nil {
		t.Fatal(err)
	}
	for i, o := range []struct {
		kad time.Time
		cm  int
	}{{time.Date(2022, 11, 29, 6, 0, 0, 0, time.UTC), -115}, {time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC), -211}} {
		if _, err := baza.Exec(`INSERT INTO readings (id, station_id, measured_at, level_cm, source, user_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, 'UVOZ', '', ?, ?)`, "r"+string(rune('0'+i)), st.ID.String(), o.kad, o.cm, o.kad, o.kad); err != nil {
			t.Fatal(err)
		}
	}
	rs := service.NewReadingService(repository.NewReadingRepository(baza, rec), stationRepo, nil, nil, nil)

	h := NewPrognozeHandler(nil, nil, nil)
	h.readings = rs
	h.arhiva = func() *repository.ArhivaRepository { return &repository.ArhivaRepository{} }
	h.godisnji.puni = true // punjenje u pozadini već traje, test ga ne pokreće
	popis := map[string]models.Station{st.Code: st}
	pregled := []PregledLetve{{Letva: st.Code}}

	if got := h.opisiLetve(popis, pregled); len(got) != 1 || len(got[0].Pragovi) != 0 {
		t.Fatalf("dok arhiva čeka: %+v", got)
	}

	h.godisnji.zapamti(st.Code, nil) // arhiva pročitana, letva u njoj nema niz
	got := h.opisiLetve(popis, pregled)
	if len(got) != 1 || len(got[0].Pragovi) != 2 {
		t.Fatalf("nakon arhive: %+v", got)
	}
	if p := got[0].Pragovi[1]; p.Oznaka != "MIN" || p.Vrijednost != "-211 cm · 21. 10. 2026." || !strings.Contains(p.Naslov, "29. 11. 2022.") {
		t.Errorf("krajnost iz evidencije: %+v", p)
	}
}

// Letva s izvorom krajnosti dok još čeka svoju arhivu ne dobiva krajnosti
// izvora: možda ima vlastiti niz, a pločica bi tvrdila da ga nema.
func TestKarticaBezPrenesenihDokArhivaCeka(t *testing.T) {
	h := NewPrognozeHandler(nil, nil, nil)
	h.arhiva = func() *repository.ArhivaRepository { return &repository.ArhivaRepository{} }
	h.godisnji.puni = true // punjenje u pozadini već traje, test ga ne pokreće
	h.godisnji.zapamti("ilok", []repository.GodinaVodostaja{{Godina: 1965, Min: -150, Max: 812}})
	popis := map[string]models.Station{
		"ilok":          {Name: "Ilok", ZeroDatum: kota(73.968)},
		"backa-palanka": {Name: "Bačka Palanka (Srbija)", ZeroDatum: kota(73.97)},
	}
	pregled := []PregledLetve{{Letva: "backa-palanka"}}

	if got := h.opisiLetve(popis, pregled); len(got) != 1 || len(got[0].Pragovi) != 0 {
		t.Fatalf("dok arhiva čeka: %+v", got)
	}
	h.godisnji.zapamti("backa-palanka", nil) // pročitano, vlastitog niza nema
	if got := h.opisiLetve(popis, pregled); len(got) != 1 || len(got[0].Pragovi) != 2 || !strings.Contains(got[0].Pragovi[0].Vrijednost, "s letve Ilok") {
		t.Errorf("nakon arhive: %+v", got)
	}
}

// Pročitano se pamti po veličini, a letva čije čitanje padne ne pamti se, pa je
// idući pregled čita ponovno.
func TestGodisnjeVelicinePamtiSamoProcitano(t *testing.T) {
	arh := arhivaBrojki(t, nil)
	h := &PrognozeHandler{arhiva: func() *repository.ArhivaRepository { return arh }}
	ctx := context.Background()

	if got := h.godisnjeVelicine(ctx, "protok", []string{"osijek"}); len(got) != 1 {
		t.Fatalf("pročitano: %+v", got)
	}
	if _, ima := h.godisnji.izPredmemorije("protok:osijek"); !ima {
		t.Error("protok se pamti pod protok:letva")
	}
	arh.Close()
	if got := h.godisnjeVelicine(ctx, "protok", []string{"osijek", "botovo"}); len(got) != 1 {
		t.Errorf("zapamćena letva ostaje, a nepročitana ne: %+v", got)
	}
	if _, ima := h.godisnji.izPredmemorije("botovo"); ima {
		t.Error("nepročitana letva ne smije se zapamtiti")
	}
	if got := (&PrognozeHandler{}).godisnjeVelicine(ctx, "vodostaj", []string{"osijek"}); got != nil {
		t.Errorf("bez arhive: %+v", got)
	}
	h.arhiva = func() *repository.ArhivaRepository { return nil }
	if got := h.godisnjeVelicine(ctx, "vodostaj", []string{"osijek"}); got != nil {
		t.Errorf("arhiva nije otvorena: %+v", got)
	}
}
