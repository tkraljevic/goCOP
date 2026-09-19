package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/weather"
)

// Vodočuvarski dnevnik: dnevni list po vodočuvaru, kao papirna knjiga.
// Vodočuvar ga piše i potpisuje (preda), rukovoditelj branjenog područja
// ili nadređeni na sektoru ga ovjerava, a ostali rukovoditelji vezani uz
// područje mogu ga parafirati.

type VodocuvarService struct {
	repo    *repository.VodocuvarRepository
	users   *UserService
	org     *repository.OrgRepository
	weather *weather.Client
	cvor    string
}

func NewVodocuvarService(repo *repository.VodocuvarRepository, users *UserService, cvor string) *VodocuvarService {
	return &VodocuvarService{repo: repo, users: users, weather: &weather.Client{}, cvor: cvor}
}

// SetWeather daje servisu klijent za vremenske prilike (za testove drugi)
func (s *VodocuvarService) SetWeather(c *weather.Client) { s.weather = c }

// ErrNijeVodocuvar: osoba nema terensko zaduženje pa nema ni dnevnik
var ErrNijeVodocuvar = errors.New("dnevnik vodi tko ima terensko zaduženje (vodočuvar, strojar, rukovatelj, posada); nemate ga u zaduženjima")

// terenskaDuznost je zaduženje po kojem osoba vodi dnevnik: sektor i područje
func terenskaDuznost(u *models.User) *models.Duty {
	var prva *models.Duty
	for i := range u.Duties {
		d := &u.Duties[i]
		if !d.IsActive || !d.Role.IsField() {
			continue
		}
		if d.IsPrimary {
			return d
		}
		if prva == nil {
			prva = d
		}
	}
	return prva
}

// VodiDnevnik javlja vodi li osoba vodočuvarski dnevnik
func VodiDnevnik(u *models.User) bool { return u != nil && terenskaDuznost(u) != nil }

// SmijeOvjeriti javlja smije li osoba ovjeriti list: rukovoditelj branjenog
// područja ili zamjenik, te svi iznad njega na sektoru (uprava sektora,
// centar, uprava organizacije)
func (s *VodocuvarService) SmijeOvjeriti(perms *models.UserPermissions, l *models.VodocuvarskiList) bool {
	if perms == nil || l == nil {
		return false
	}
	if perms.IsGlobalAdmin {
		return true
	}
	for _, d := range perms.User.Duties {
		if !d.IsActive {
			continue
		}
		switch d.Role {
		case models.RoleNationalLeader, models.RoleNationalDeputy, models.RoleMainCenterLeader, models.RoleMainCenterDeputy:
			return true
		case models.RoleSectorLeader, models.RoleSectorDeputy, models.RoleSectorMainDeputy, models.RoleCopLeader, models.RoleCopDeputy:
			if d.SectorID != nil && *d.SectorID == l.Sektor {
				return true
			}
		case models.RoleSectorAreaDeputy, models.RoleAreaLeader, models.RoleAreaDeputy:
			if d.AreaID != nil && *d.AreaID == l.AreaID {
				return true
			}
		}
	}
	return false
}

// SmijeParafirati javlja smije li osoba parafirati list: svaki rukovoditelj
// ili ovlaštenik čije je zaduženje vezano uz to područje ili sektor, osim
// samog vodočuvara i terenskih uloga
func (s *VodocuvarService) SmijeParafirati(perms *models.UserPermissions, l *models.VodocuvarskiList) bool {
	if perms == nil || l == nil || perms.User.ID.String() == l.UserID {
		return false
	}
	if s.SmijeOvjeriti(perms, l) {
		return true
	}
	for _, d := range perms.User.Duties {
		if !d.IsActive || d.Role.IsField() {
			continue
		}
		if d.AreaID != nil && *d.AreaID == l.AreaID {
			return true
		}
		if d.AreaID == nil && d.SectorID != nil && *d.SectorID == l.Sektor && d.ScopeType == models.ScopeSector {
			return true
		}
	}
	return false
}

// SmijeVidjeti javlja smije li osoba čitati list
func (s *VodocuvarService) SmijeVidjeti(perms *models.UserPermissions, l *models.VodocuvarskiList) bool {
	if perms == nil || l == nil {
		return false
	}
	return perms.User.ID.String() == l.UserID || s.SmijeParafirati(perms, l) || perms.HasWriteAccess(l.Sektor, l.AreaID, "") || perms.CanAdminister(l.Sektor, l.AreaID)
}

// Pripremi vraća list vodočuvara za dan: postojeći, ili novi popunjen onim
// što program zna (radno vrijeme kao jučer, vremenske prilike, očitanja)
func (s *VodocuvarService) Pripremi(ctx context.Context, u *models.User, dan time.Time) (*models.VodocuvarskiList, error) {
	if u == nil {
		return nil, ErrUnauthorized
	}
	d := terenskaDuznost(u)
	if d == nil {
		return nil, ErrNijeVodocuvar
	}
	if l, err := s.repo.ZaDan(ctx, u.ID.String(), dan); err != nil || l != nil {
		return l, err
	}
	l := &models.VodocuvarskiList{UserID: u.ID.String(), Ime: u.FullName, Datum: dan, Od: "08:00", Do: "16:00", Cvor: s.cvor}
	if d.SectorID != nil {
		l.Sektor = *d.SectorID
	}
	if d.AreaID != nil {
		l.AreaID = *d.AreaID
	}
	if zadnji, err := s.repo.List(ctx, repository.FiltarListova{UserID: u.ID.String(), Limit: 1}); err == nil && len(zadnji) == 1 {
		l.Od, l.Do = zadnji[0].Od, zadnji[0].Do
	}
	l.Prilike = s.prilike(ctx, l)
	l.Ocitanja = s.ocitanja(ctx, u.ID.String(), dan)
	l.Zadaci = s.zadaciNaListu(ctx, u.ID.String(), dan, nil)
	return l, nil
}

// zadaciNaListu slaže otvorene zadatke do tog dana kako stoje na listu,
// zadržavajući stanje koje je vodočuvar već označio na tom listu
func (s *VodocuvarService) zadaciNaListu(ctx context.Context, userID string, dan time.Time, postojeci []models.ZadatakNaListu) []models.ZadatakNaListu {
	otvoreni, err := s.repo.OtvoreniZadaci(ctx, userID, dan)
	if err != nil {
		return postojeci
	}
	stanje := map[string]models.ZadatakNaListu{}
	for _, z := range postojeci {
		stanje[z.ID] = z
	}
	var out []models.ZadatakNaListu
	vidjen := map[string]bool{}
	for _, z := range otvoreni {
		vidjen[z.ID] = true
		if st, ok := stanje[z.ID]; ok {
			out = append(out, st)
			continue
		}
		out = append(out, models.ZadatakNaListu{ID: z.ID, Tekst: z.Tekst, Zadao: z.Zadao, ZadanoAt: z.ZadanoAt, Status: models.ZadatakOtvoren})
	}
	// zadaci zaključeni na ovom listu ostaju na njemu iako više nisu otvoreni
	for _, z := range postojeci {
		if z.Status != models.ZadatakOtvoren && !vidjen[z.ID] {
			out = append(out, z)
		}
	}
	return out
}

// prilike dohvaća vremenske prilike za područje s Open-Meteo; bez interneta
// ili koordinata ostaje prazno
func (s *VodocuvarService) prilike(ctx context.Context, l *models.VodocuvarskiList) string {
	if s.weather == nil || s.org == nil || l.AreaID == 0 {
		return ""
	}
	area, err := s.org.GetArea(ctx, l.AreaID)
	if err != nil || area == nil || !area.ImaKoordinate() {
		return ""
	}
	wctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	d, err := s.weather.Fetch(wctx, area.Latitude, area.Longitude, l.Datum, 12)
	if err != nil || d == nil {
		return ""
	}
	t := d.Description
	if d.Temperature != 0 {
		t += fmt.Sprintf(", %.0f °C", d.Temperature)
	}
	if d.Precipitation > 0 {
		t += fmt.Sprintf(", oborina %.0f mm", d.Precipitation)
	}
	return strings.TrimPrefix(t, ", ")
}

// SetOrg daje servisu registar organizacije, za koordinate područja
func (s *VodocuvarService) SetOrg(o *repository.OrgRepository) { s.org = o }

// ocitanja slaže tekst vodostaja koje je vodočuvar očitao tog dana
func (s *VodocuvarService) ocitanja(ctx context.Context, userID string, dan time.Time) string {
	o, err := s.repo.OcitanjaDana(ctx, userID, dan)
	if err != nil || len(o) == 0 {
		return ""
	}
	var b strings.Builder
	for _, x := range o {
		if x.Vodostaj == nil {
			continue
		}
		fmt.Fprintf(&b, "%s: %+d cm (%s)\n", x.Postaja, *x.Vodostaj, x.Kad.In(models.Zagreb).Format("15:04"))
	}
	return strings.TrimSpace(b.String())
}

// UnosLista je ono što vodočuvar upiše
type UnosLista struct {
	Od, Do, Prilike, Naredbe, Opis, Zapazanja string
	Zadaci                                    map[string]UnosZadatka // po ID-u zadatka
}

// UnosZadatka je kako je vodočuvar označio zadatak na listu
type UnosZadatka struct {
	Status    string // OTVOREN (prenosi se), OBAVLJEN, ODBACEN
	Obavljeno string
}

// Spremi upisuje list vodočuvara; predan list više se ne mijenja
func (s *VodocuvarService) Spremi(ctx context.Context, u *models.User, dan time.Time, unos UnosLista, predaj bool) (*models.VodocuvarskiList, error) {
	l, err := s.Pripremi(ctx, u, dan)
	if err != nil {
		return nil, err
	}
	if l.Predan() {
		return nil, fmt.Errorf("list od %s je predan i više se ne mijenja", l.Datum.In(models.Zagreb).Format("02.01.2006."))
	}
	for _, v := range []string{unos.Od, unos.Do} {
		if _, err := time.Parse("15:04", strings.TrimSpace(v)); err != nil {
			return nil, fmt.Errorf("radno vrijeme upišite kao sate i minute, npr. 08:00")
		}
	}
	l.Od, l.Do = strings.TrimSpace(unos.Od), strings.TrimSpace(unos.Do)
	l.Prilike, l.Naredbe, l.Opis, l.Zapazanja = strings.TrimSpace(unos.Prilike), strings.TrimSpace(unos.Naredbe), strings.TrimSpace(unos.Opis), strings.TrimSpace(unos.Zapazanja)
	// zadaci: stanje s obrasca
	l.Zadaci = s.zadaciNaListu(ctx, u.ID.String(), dan, l.Zadaci)
	for i := range l.Zadaci {
		z := &l.Zadaci[i]
		un, ok := unos.Zadaci[z.ID]
		if !ok {
			continue
		}
		switch un.Status {
		case models.ZadatakObavljen, models.ZadatakOdbacen:
			if strings.TrimSpace(un.Obavljeno) == "" {
				return nil, fmt.Errorf("uz zadatak „%s” upišite što je napravljeno ili zašto nije", z.Tekst)
			}
			z.Status, z.Obavljeno = un.Status, strings.TrimSpace(un.Obavljeno)
		default:
			z.Status, z.Obavljeno = models.ZadatakOtvoren, strings.TrimSpace(un.Obavljeno)
		}
	}
	// svaki list nosi redni broj od prvog spremanja, kao stranica u knjizi
	if l.Broj == 0 {
		l.Broj, err = s.repo.SljedeciBroj(ctx, u.ID.String(), dan.In(models.Zagreb).Year())
		if err != nil {
			return nil, err
		}
	}
	if predaj {
		if l.Opis == "" {
			return nil, fmt.Errorf("prije predaje upišite opis radnih aktivnosti")
		}
		// neobavljeni zadatak ostaje upisan na listu, ali s obrazloženjem
		for _, z := range l.Zadaci {
			if z.Status == models.ZadatakOtvoren && strings.TrimSpace(z.Obavljeno) == "" {
				return nil, fmt.Errorf("zadatak „%s” nije obavljen: prije predaje obrazložite zašto (prenosi se na sljedeći list)", z.Tekst)
			}
		}
		l.Ocitanja = s.ocitanja(ctx, u.ID.String(), dan)
		kad := time.Now()
		l.PredanoAt = &kad
	}
	if err := s.repo.Save(ctx, l); err != nil {
		return nil, err
	}
	// zaključeni zadaci zaključuju se i u evidenciji zadataka, tek pri predaji;
	// otvoreni ostaju otvoreni i sami se prenose na sljedeći list
	if predaj {
		for _, z := range l.Zadaci {
			if z.Status == models.ZadatakOtvoren {
				continue
			}
			zad, err := s.repo.GetZadatak(ctx, z.ID)
			if err != nil || zad == nil || !zad.Otvoren() {
				continue
			}
			kad := time.Now()
			zad.Status, zad.Obavljeno, zad.ObavljenoAt, zad.ListID = z.Status, z.Obavljeno, &kad, l.ID
			if err := s.repo.SaveZadatak(ctx, zad); err != nil {
				return nil, err
			}
		}
	}
	return l, nil
}

// zaVodocuvara slaže probni list po terenskom zaduženju osobe, za provjeru prava
func zaVodocuvara(v *models.User) *models.VodocuvarskiList {
	d := terenskaDuznost(v)
	if d == nil {
		return nil
	}
	l := &models.VodocuvarskiList{UserID: v.ID.String()}
	if d.SectorID != nil {
		l.Sektor = *d.SectorID
	}
	if d.AreaID != nil {
		l.AreaID = *d.AreaID
	}
	return l
}

// ZadajZadatak zadaje zadatak vodočuvaru; smije tko smije parafirati ili
// ovjeriti njegov list (rukovoditelji vezani uz područje i sektor)
func (s *VodocuvarService) ZadajZadatak(ctx context.Context, perms *models.UserPermissions, u *models.User, vodocuvarID, tekst string) (*models.Zadatak, error) {
	tekst = strings.TrimSpace(tekst)
	if tekst == "" {
		return nil, fmt.Errorf("upišite zadatak")
	}
	vid, err := uuid.Parse(vodocuvarID)
	if err != nil {
		return nil, fmt.Errorf("nepoznat vodočuvar")
	}
	v, err := s.users.GetUserByID(vid)
	if err != nil || v == nil {
		return nil, fmt.Errorf("nepoznat vodočuvar")
	}
	probni := zaVodocuvara(v)
	if probni == nil {
		return nil, fmt.Errorf("%s nema terensko zaduženje pa nema ni dnevnik", v.FullName)
	}
	if !s.SmijeParafirati(perms, probni) {
		return nil, ErrUnauthorized
	}
	z := &models.Zadatak{UserID: v.ID.String(), Sektor: probni.Sektor, AreaID: probni.AreaID, Tekst: tekst, ZadaoID: u.ID.String(), Zadao: u.FullName, ZadanoAt: time.Now(), Status: models.ZadatakOtvoren, Cvor: s.cvor}
	return z, s.repo.SaveZadatak(ctx, z)
}

// Zadaci vraća zadatke vodočuvara, najnoviji prvo
func (s *VodocuvarService) Zadaci(ctx context.Context, vodocuvarID string) []models.Zadatak {
	z, _ := s.repo.ZadaciVodocuvara(ctx, vodocuvarID, 100)
	return z
}

// Vodocuvari vraća osobe s terenskim zaduženjem kojima osoba smije zadavati zadatke
func (s *VodocuvarService) Vodocuvari(ctx context.Context, perms *models.UserPermissions) []models.User {
	svi, err := s.users.ListUsers("", 0, "", "", "active")
	if err != nil {
		return nil
	}
	var out []models.User
	for i := range svi {
		if probni := zaVodocuvara(&svi[i]); probni != nil && s.SmijeParafirati(perms, probni) {
			out = append(out, svi[i])
		}
	}
	return out
}

// Ovjeri potvrđuje list: rukovoditelj branjenog područja ili nadređeni
func (s *VodocuvarService) Ovjeri(ctx context.Context, perms *models.UserPermissions, u *models.User, id string) (*models.VodocuvarskiList, error) {
	l, err := s.repo.Get(ctx, id)
	if err != nil || l == nil {
		return nil, fmt.Errorf("list ne postoji")
	}
	if !l.Predan() {
		return nil, fmt.Errorf("vodočuvar list još nije predao")
	}
	if l.Potvrden() {
		return l, nil
	}
	if !s.SmijeOvjeriti(perms, l) {
		return nil, ErrUnauthorized
	}
	kad := time.Now()
	l.PotvrdioID, l.Potvrdio, l.PotvrdenoAt = u.ID.String(), u.FullName, &kad
	return l, s.repo.Save(ctx, l)
}

// Parafiraj dodaje parafu rukovoditelja vezanog uz područje
func (s *VodocuvarService) Parafiraj(ctx context.Context, perms *models.UserPermissions, u *models.User, id string) (*models.VodocuvarskiList, error) {
	l, err := s.repo.Get(ctx, id)
	if err != nil || l == nil {
		return nil, fmt.Errorf("list ne postoji")
	}
	if !l.Predan() {
		return nil, fmt.Errorf("vodočuvar list još nije predao")
	}
	if !s.SmijeParafirati(perms, l) {
		return nil, ErrUnauthorized
	}
	if l.Parafirao(u.ID.String()) {
		return l, nil
	}
	funkcija := ""
	if d := najvisaDuznost(u); d != nil {
		funkcija = d.Title
	}
	l.Parafe = append(l.Parafe, models.Parafa{UserID: u.ID.String(), Ime: u.FullName, Funkcija: funkcija, Kad: time.Now()})
	return l, s.repo.Save(ctx, l)
}

// Obrisi briše nepredan list vodočuvara
func (s *VodocuvarService) Obrisi(ctx context.Context, u *models.User, id string) error {
	l, err := s.repo.Get(ctx, id)
	if err != nil || l == nil {
		return err
	}
	if l.UserID != u.ID.String() || l.Predan() {
		return ErrUnauthorized
	}
	return s.repo.Delete(ctx, id)
}

// Get čita list uz provjeru prava
func (s *VodocuvarService) Get(ctx context.Context, perms *models.UserPermissions, id string) (*models.VodocuvarskiList, error) {
	l, err := s.repo.Get(ctx, id)
	if err != nil || l == nil {
		return nil, err
	}
	if !s.SmijeVidjeti(perms, l) {
		return nil, ErrUnauthorized
	}
	return l, nil
}

// Moji vraća listove osobe u godini
func (s *VodocuvarService) Moji(ctx context.Context, u *models.User, godina int) ([]models.VodocuvarskiList, error) {
	return s.repo.List(ctx, repository.FiltarListova{UserID: u.ID.String(), Godina: godina})
}

// Tudji vraća listove vodočuvara koje osoba smije vidjeti, po filtru
func (s *VodocuvarService) Tudji(ctx context.Context, perms *models.UserPermissions, f repository.FiltarListova) ([]models.VodocuvarskiList, error) {
	svi, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, err
	}
	out := svi[:0]
	for _, l := range svi {
		if l.UserID != perms.User.ID.String() && s.SmijeVidjeti(perms, &l) {
			out = append(out, l)
		}
	}
	return out, nil
}
