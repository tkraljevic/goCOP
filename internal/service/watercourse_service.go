package service

import (
	"context"
	"fmt"
	"strings"

	"gocop/internal/geometrija"
	"gocop/internal/hydro"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// WatercourseService daje pristup registru vodnih tijela.
//
// Registar je preslika službenog izvora (Odluka o popisu voda I. reda), pa se
// ovdje samo čita — mijenja se osvježavanjem izvora, ne kroz sučelje.
type WatercourseService struct {
	repo          *repository.WatercourseRepository
	geometrijaDir string
	postaje       provjeraPostaje
}

// provjeraPostaje kaže smije li korisnik mijenjati postaju. Vodotok postaje
// dio je postaje, pa za njega vrijedi isto pravo kao za svaku njezinu izmjenu.
type provjeraPostaje interface {
	PostajaZaIzmjenu(ctx context.Context, perms *models.UserPermissions, stationID string) (string, error)
}

// SetPostaje daje servisu provjeru prava nad postajama. Bez nje vodotok
// postaje mijenja samo globalni administrator.
func (s *WatercourseService) SetPostaje(p provjeraPostaje) {
	s.postaje = p
}

func NewWatercourseService(repo *repository.WatercourseRepository) *WatercourseService {
	return &WatercourseService{repo: repo}
}

// SetGeometrijaDir postavlja stazu do mape s GeoJSON geometrijama vodotoka na disku.
func (s *WatercourseService) SetGeometrijaDir(dir string) {
	s.geometrijaDir = dir
}

// GetWatercourseGeometry vraća GeoJSON geometriju (tok rijeke i rkm točke) za vodno tijelo.
// Primarno čita iz baze podataka (kako bi vrijedila sinkronizacija među čvorovima),
// a ako u bazi nije upisano, poseže za datotekom na disku ili ugrađenim paketom geometrija.
func (s *WatercourseService) GetWatercourseGeometry(ctx context.Context, code string) ([]byte, error) {
	if w, err := s.GetWatercourse(ctx, code); err == nil && w != nil && strings.TrimSpace(w.Geometry) != "" {
		return geometrija.PrimijeniStacionazu(code, []byte(w.Geometry))
	}
	data, err := geometrija.Ucitaj(s.geometrijaDir, code)
	if err != nil {
		return nil, err
	}
	return geometrija.PrimijeniStacionazu(code, data)
}

// HasGeometry provjerava postoji li dostupna geometrija za vodno tijelo.
func (s *WatercourseService) HasGeometry(ctx context.Context, code string) bool {
	if w, err := s.GetWatercourse(ctx, code); err == nil && w != nil && w.HasGeometry() {
		return true
	}
	return geometrija.Ima(s.geometrijaDir, code)
}

// SetWatercourseGeometry postavlja ili mijenja GeoJSON geometriju vodnog tijela u bazi.
// Zapis se bilježi u knjizi verzija i sinkronizira na druge čvorove.
func (s *WatercourseService) SetWatercourseGeometry(ctx context.Context, perms *models.UserPermissions, code, geojson string) error {
	if err := requireGlobalAdmin(perms, "izmjenu geometrije vodnog tijela"); err != nil {
		return err
	}
	w, err := s.repo.GetWatercourse(ctx, code)
	if err != nil {
		return err
	}
	if w == nil {
		return fmt.Errorf("vodno tijelo %q ne postoji", code)
	}
	geojson, err = PripremiGeometrijuVode(geojson)
	if err != nil {
		return err
	}
	w.Geometry = geojson
	return s.repo.UpdateWatercourse(ctx, w)
}

// SetWatercourseLines sprema tok nacrtan na karti. S karte uzima samo crte,
// a točke uz tok ostaju iz spremljenog zapisa (vidi SpojiCrteToka).
func (s *WatercourseService) SetWatercourseLines(ctx context.Context, perms *models.UserPermissions, code, crte string) error {
	if err := requireGlobalAdmin(perms, "izmjenu geometrije vodnog tijela"); err != nil {
		return err
	}
	w, err := s.repo.GetWatercourse(ctx, code)
	if err != nil {
		return err
	}
	if w == nil {
		return fmt.Errorf("vodno tijelo %q ne postoji", code)
	}
	spojeno, err := SpojiCrteToka(s.zapisToka(w), crte, geometrija.RacunaStacionazu(code))
	if err != nil {
		return err
	}
	return s.SetWatercourseGeometry(ctx, perms, code, spojeno)
}

// zapisToka je tok kakav je spremljen, prije izračuna stacionaže: iz baze, a
// dok ga u bazi nema, s diska ili iz ugrađenog paketa
func (s *WatercourseService) zapisToka(w *models.Watercourse) string {
	if strings.TrimSpace(w.Geometry) != "" {
		return w.Geometry
	}
	data, err := geometrija.Ucitaj(s.geometrijaDir, w.Code)
	if err != nil {
		return "" // za ovu vodu nema zapisa toka, pa ni točaka uz njega
	}
	return string(data)
}

func (s *WatercourseService) ListWatercourses(ctx context.Context, search, category string, onlyUsed bool) ([]models.Watercourse, error) {
	return s.repo.ListWatercourses(ctx, search, category, onlyUsed)
}

func (s *WatercourseService) GetWatercourse(ctx context.Context, code string) (*models.Watercourse, error) {
	return s.repo.GetWatercourse(ctx, code)
}

func (s *WatercourseService) ListCategories(ctx context.Context) ([]string, error) {
	return s.repo.ListCategories(ctx)
}

func (s *WatercourseService) Counts(ctx context.Context) (total, used, unlinkedSections, unlinkedStations int, err error) {
	return s.repo.CountWatercourses(ctx)
}

// Registar vodnih tijela preslika je službenog izvora, ali se smije dopunjavati
// i ispravljati — Odluka o popisu voda I. reda ne navodi male potoke i kanale
// koji se ipak štite, a poklapanja po nazivu ponegdje treba potvrditi ručno.

// CreateWatercourse upisuje novo vodno tijelo.
// Registar je zajednički svim dionicama, pa ga mijenja globalni administrator.
func (s *WatercourseService) CreateWatercourse(ctx context.Context, perms *models.UserPermissions, w *models.Watercourse) error {
	if err := requireGlobalAdmin(perms, "unos vodnog tijela"); err != nil {
		return err
	}
	if err := validateWatercourse(w); err != nil {
		return err
	}

	if strings.TrimSpace(w.Code) == "" {
		w.Code = hydro.WatercourseCode(w.OfficialName)
	}
	if w.Origin == "" {
		w.Origin = models.WatercourseOriginManual
	}

	existing, err := s.repo.GetWatercourse(ctx, w.Code)
	if err != nil {
		return err
	}
	if existing != nil {
		return fmt.Errorf("vodno tijelo sa šifrom %q već postoji (%s)", w.Code, existing.OfficialName)
	}

	return s.repo.CreateWatercourse(ctx, w)
}

// UpdateWatercourse mijenja podatke vodnog tijela
func (s *WatercourseService) UpdateWatercourse(ctx context.Context, perms *models.UserPermissions, w *models.Watercourse) error {
	if err := requireGlobalAdmin(perms, "izmjenu vodnog tijela"); err != nil {
		return err
	}
	if err := validateWatercourse(w); err != nil {
		return err
	}
	return s.repo.UpdateWatercourse(ctx, w)
}

// DeleteWatercourse briše vodno tijelo, ali ne dok je u upotrebi —
// brisanje bi dionice i postaje ostavilo da pokazuju u prazno
func (s *WatercourseService) DeleteWatercourse(ctx context.Context, perms *models.UserPermissions, code string) error {
	if err := requireGlobalAdmin(perms, "brisanje vodnog tijela"); err != nil {
		return err
	}

	sections, stations, err := s.repo.CountUsage(ctx, code)
	if err != nil {
		return err
	}
	if sections+stations > 0 {
		return fmt.Errorf("vodno tijelo se ne može obrisati jer je pridruženo: %d %s, %d %s — prvo ih prevežite na drugu vodu",
			sections, models.UzBrojHR(sections, "dionica", "dionice", "dionica"), stations, models.UzBrojHR(stations, "postaja", "postaje", "postaja"))
	}

	return s.repo.DeleteWatercourse(ctx, code)
}

// SetStationWatercourse pridružuje vodno tijelo postaji.
//
// Ručno pridruživanje nadjačava strojno utvrđivanje i tako se i bilježi —
// operater zna ono što dokumentacija ne kaže.
func (s *WatercourseService) SetStationWatercourse(ctx context.Context, perms *models.UserPermissions, stationID, watercourseCode string) error {
	stationID, err := s.postajaZaIzmjenu(ctx, perms, stationID)
	if err != nil {
		return err
	}

	name, source := "", ""
	if watercourseCode != "" {
		water, err := s.repo.GetWatercourse(ctx, watercourseCode)
		if err != nil {
			return err
		}
		if water == nil {
			return fmt.Errorf("vodno tijelo %q ne postoji u registru", watercourseCode)
		}
		name, source = water.Name, models.WatercourseFromOperator
	}

	return s.repo.SetStationWatercourse(ctx, stationID, watercourseCode, name, source)
}

// postajaZaIzmjenu traži isto pravo kao izmjena postaje: globalnog
// administratora ili pravo na barem jednu dionicu na kojoj postaja stoji.
// Vraća identifikator postaje u zapisu baze.
func (s *WatercourseService) postajaZaIzmjenu(ctx context.Context, perms *models.UserPermissions, stationID string) (string, error) {
	if perms == nil {
		return "", greskaOvlasti("nemate ovlasti za izmjenu vodotoka postaje")
	}
	if s.postaje == nil {
		return stationID, requireGlobalAdmin(perms, "mijenjanje vodotoka postaje")
	}
	return s.postaje.PostajaZaIzmjenu(ctx, perms, stationID)
}

func (s *WatercourseService) SectionsForWatercourse(ctx context.Context, code string) ([]string, error) {
	return s.repo.ListSectionsForWatercourse(ctx, code)
}

func requireGlobalAdmin(perms *models.UserPermissions, action string) error {
	if perms == nil || !perms.IsGlobalAdmin {
		return greskaOvlasti(fmt.Sprintf("%s dopušteno je samo globalnom administratoru", action))
	}
	return nil
}

func validateWatercourse(w *models.Watercourse) error {
	if w == nil {
		return fmt.Errorf("podaci o vodnom tijelu nedostaju")
	}
	if strings.TrimSpace(w.OfficialName) == "" {
		return fmt.Errorf("naziv vodnog tijela je obavezan")
	}
	if strings.TrimSpace(w.Name) == "" {
		w.Name = w.OfficialName
	}
	for _, m := range []struct {
		label string
		value *float64
	}{
		{"duljina", w.LengthKm},
		{"površina porječja", w.BasinKm2},
		{"prosječni protok", w.AvgFlowM3S},
	} {
		if m.value != nil && *m.value < 0 {
			return fmt.Errorf("%s ne može biti negativna", m.label)
		}
	}
	return nil
}
