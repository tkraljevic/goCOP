package web

import (
	"context"
	"path/filepath"
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

// Dok se arhiva još računa, krajnost iz evidencije ne smije reći da letva
// nema niz u arhivi; kad je arhiva pročitana i prazna, kaže od kada ide
// evidencija. Na pločici je uvijek dan očitanja.
func TestUzEvidencijuSamoBezNizaKazeOdKada(t *testing.T) {
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

	mn, _ := uzEvidenciju(context.Background(), rs, st, nil, nil, false)
	if mn == nil || mn.Label() != "-211 cm · 21. 10. 2026." || mn.Od != "" {
		t.Fatalf("dok se arhiva računa: %+v", mn)
	}
	mn, _ = uzEvidenciju(context.Background(), rs, st, nil, nil, true)
	if mn == nil || mn.Od != "29. 11. 2022." || mn.Label() != "-211 cm · 21. 10. 2026." {
		t.Fatalf("letva bez niza u arhivi: %+v", mn)
	}
}
