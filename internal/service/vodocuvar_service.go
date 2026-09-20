package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	// radnoVrijeme daje redovno radno vrijeme organizacije (postavka
	// obračuna, zadano 07:30–15:30) za novi list
	radnoVrijeme func(ctx context.Context) (od, do string)
}

// SetRadnoVrijeme daje servisu izvor redovnog radnog vremena
func (s *VodocuvarService) SetRadnoVrijeme(f func(ctx context.Context) (od, do string)) {
	s.radnoVrijeme = f
}

func NewVodocuvarService(repo *repository.VodocuvarRepository, users *UserService, cvor string) *VodocuvarService {
	return &VodocuvarService{repo: repo, users: users, weather: &weather.Client{}, cvor: cvor}
}

// SetWeather daje servisu klijent za vremenske prilike (za testove drugi)
func (s *VodocuvarService) SetWeather(c *weather.Client) { s.weather = c }

// ErrNijeVodocuvar: osoba nema zaduženje vodočuvara pa nema ni dnevnik
var ErrNijeVodocuvar = errors.New("vodočuvarski dnevnik vodi tko ima zaduženje vodočuvara; strojari i rukovatelji imaju svoje dnevnike")

// terenskaDuznost je zaduženje vodočuvara po kojem osoba vodi dnevnik:
// sektor i područje. Strojari i rukovatelji imaju svoje dnevnike.
func terenskaDuznost(u *models.User) *models.Duty {
	var prva *models.Duty
	for i := range u.Duties {
		d := &u.Duties[i]
		if !d.IsActive || d.Role != models.RoleWaterGuard {
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
	if perms == nil || l == nil || !perms.User.VidiVodocuvarskiDnevnik() {
		return false
	}
	return perms.User.ID.String() == l.UserID || s.SmijeParafirati(perms, l) || perms.HasWriteAccess(l.Sektor, l.AreaID, "") || perms.CanAdminister(l.Sektor, l.AreaID)
}

// Pripremi vraća list vodočuvara za dan: postojeći, ili novi popunjen onim
// što program zna (radno vrijeme, vremenske prilike, očitanja, zadaci)
func (s *VodocuvarService) Pripremi(ctx context.Context, u *models.User, dan time.Time) (*models.VodocuvarskiList, error) {
	return s.pripremiZa(ctx, u, dan)
}

func (s *VodocuvarService) pripremiZa(ctx context.Context, u *models.User, dan time.Time) (*models.VodocuvarskiList, error) {
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
	l := &models.VodocuvarskiList{UserID: u.ID.String(), Ime: u.FullName, Datum: dan, Od: "07:30", Do: "15:30", Cvor: s.cvor}
	if s.radnoVrijeme != nil {
		if od, do := s.radnoVrijeme(ctx); od != "" && do != "" {
			l.Od, l.Do = od, do
		}
	}
	if d.SectorID != nil {
		l.Sektor = *d.SectorID
	}
	if d.AreaID != nil {
		l.AreaID = *d.AreaID
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
	if dan.In(models.Zagreb).Year() < time.Now().In(models.Zagreb).Year() {
		return nil, fmt.Errorf("knjiga za %d. je arhivirana istekom godine i u nju se više ne upisuje", dan.In(models.Zagreb).Year())
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
// za je dan za koji se zadatak planira: danas ili do 30 dana unaprijed; nula = od danas
func (s *VodocuvarService) ZadajZadatak(ctx context.Context, perms *models.UserPermissions, u *models.User, vodocuvarID, tekst string, za time.Time) (*models.Zadatak, error) {
	tekst = strings.TrimSpace(tekst)
	if tekst == "" {
		return nil, fmt.Errorf("upišite zadatak")
	}
	if !za.IsZero() {
		n := time.Now().In(models.Zagreb)
		danas := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, models.Zagreb)
		za = time.Date(za.Year(), za.Month(), za.Day(), 0, 0, 0, 0, models.Zagreb)
		if za.Before(danas) {
			return nil, fmt.Errorf("zadatak se ne zadaje za prošli dan")
		}
		if za.After(danas.AddDate(0, 0, models.NajdaljePlaniranje)) {
			return nil, fmt.Errorf("zadaci se planiraju najviše %d dana unaprijed", models.NajdaljePlaniranje)
		}
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
	z := &models.Zadatak{UserID: v.ID.String(), Sektor: probni.Sektor, AreaID: probni.AreaID, Tekst: tekst, ZadaoID: u.ID.String(), Zadao: u.FullName, ZadanoAt: time.Now(), Za: za, Status: models.ZadatakOtvoren, Cvor: s.cvor}
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
	_ = s.repo.DeleteIzvornik(ctx, id)
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

// NajstarijaGodina je godina najstarijeg lista na čvoru; 0 kad listova nema.
// Popis godina se po njoj ravna, da prenesene starije knjige budu dohvatljive.
func (s *VodocuvarService) NajstarijaGodina(ctx context.Context) int {
	return s.repo.NajstarijaGodina(ctx)
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

// Knjiga vraća listove jednog vodočuvara u godini, ako ih osoba smije vidjeti
func (s *VodocuvarService) Knjiga(ctx context.Context, perms *models.UserPermissions, vodocuvarID string, godina int) ([]models.VodocuvarskiList, error) {
	listovi, err := s.repo.List(ctx, repository.FiltarListova{UserID: vodocuvarID, Godina: godina})
	if err != nil {
		return nil, err
	}
	if len(listovi) > 0 && !s.SmijeVidjeti(perms, &listovi[0]) {
		return nil, ErrUnauthorized
	}
	return listovi, nil
}

// Arhivirana javlja je li knjiga te godine zaključena
func Arhivirana(godina int) bool { return godina < time.Now().In(models.Zagreb).Year() }

// Kalendar vraća zadatke vodočuvara po danima u mjesecu, za kalendarski pregled
func (s *VodocuvarService) Kalendar(ctx context.Context, perms *models.UserPermissions, vodocuvarID string, mjesec time.Time) (map[string][]models.Zadatak, error) {
	vid, err := uuid.Parse(vodocuvarID)
	if err != nil {
		return nil, fmt.Errorf("nepoznat vodočuvar")
	}
	v, err := s.users.GetUserByID(vid)
	if err != nil || v == nil {
		return nil, fmt.Errorf("nepoznat vodočuvar")
	}
	if perms.User.ID != v.ID {
		probni := zaVodocuvara(v)
		if probni == nil || !s.SmijeVidjeti(perms, probni) {
			return nil, ErrUnauthorized
		}
	}
	od := time.Date(mjesec.Year(), mjesec.Month(), 1, 0, 0, 0, 0, models.Zagreb)
	do := od.AddDate(0, 1, 0)
	zadaci, err := s.repo.ZadaciURazdoblju(ctx, vodocuvarID, od, do)
	if err != nil {
		return nil, err
	}
	out := map[string][]models.Zadatak{}
	for _, z := range zadaci {
		k := z.Dan().Format("2006-01-02")
		out[k] = append(out[k], z)
	}
	return out, nil
}

// Upisi upisuje bilješku rukovoditelja ili ovlaštenika u dnevnik vodočuvara
// za zadani dan, neovisno o zadacima; list za taj dan nastaje ako ga nema.
// Upis nosi ime, funkciju i vrijeme, i ne mijenja se.
func (s *VodocuvarService) Upisi(ctx context.Context, perms *models.UserPermissions, u *models.User, vodocuvarID string, dan time.Time, tekst string) (*models.VodocuvarskiList, error) {
	tekst = strings.TrimSpace(tekst)
	if tekst == "" {
		return nil, fmt.Errorf("upišite bilješku")
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
		return nil, fmt.Errorf("%s nema zaduženje vodočuvara pa nema ni dnevnik", v.FullName)
	}
	if !s.SmijeParafirati(perms, probni) {
		return nil, ErrUnauthorized
	}
	if Arhivirana(dan.In(models.Zagreb).Year()) {
		return nil, fmt.Errorf("knjiga za %d. je arhivirana", dan.In(models.Zagreb).Year())
	}
	l, err := s.pripremiZa(ctx, v, dan)
	if err != nil {
		return nil, err
	}
	if l.Broj == 0 {
		if l.Broj, err = s.repo.SljedeciBroj(ctx, v.ID.String(), dan.In(models.Zagreb).Year()); err != nil {
			return nil, err
		}
	}
	funkcija := ""
	if d := najvisaDuznost(u); d != nil {
		funkcija = d.Title
	}
	l.Upisi = append(l.Upisi, models.UpisRukovoditelja{UserID: u.ID.String(), Ime: u.FullName, Funkcija: funkcija, Kad: time.Now(), Tekst: tekst})
	return l, s.repo.Save(ctx, l)
}

// ZadaciLista vraća zadatke zaključene na listu, s obuhvatom obilaska
func (s *VodocuvarService) ZadaciLista(ctx context.Context, listID string) []models.Zadatak {
	z, err := s.repo.ZadaciZaList(ctx, listID)
	if err != nil {
		return nil
	}
	return z
}

// Prilozi vraća bajtove priloga lista po oznaci priloga; što ovaj čvor nema,
// izostaje. Bajtovi žive u spremištu sadržaja, list nosi samo opis.
func (s *VodocuvarService) Prilozi(ctx context.Context, l *models.VodocuvarskiList) map[string][]byte {
	out := map[string][]byte{}
	if l == nil {
		return out
	}
	for _, p := range l.Prilozi {
		if b, err := s.repo.Prilog(ctx, p.ID); err == nil && len(b) > 0 {
			out[p.ID] = b
		}
	}
	return out
}

// Prilog vraća bajtove jednog priloga lista koji osoba smije vidjeti
func (s *VodocuvarService) Prilog(ctx context.Context, perms *models.UserPermissions, listID, prilogID string) ([]byte, string, error) {
	l, err := s.Get(ctx, perms, listID)
	if err != nil || l == nil {
		return nil, "", err
	}
	for _, p := range l.Prilozi {
		if p.ID == prilogID {
			b, err := s.repo.Prilog(ctx, prilogID)
			return b, p.Vrsta, err
		}
	}
	return nil, "", nil
}

// Izvornik vraća potpisani PDF lista, ako ga osoba smije vidjeti
func (s *VodocuvarService) Izvornik(ctx context.Context, perms *models.UserPermissions, id string) (*models.IzvornikLista, error) {
	l, err := s.Get(ctx, perms, id)
	if err != nil || l == nil {
		return nil, err
	}
	return s.repo.GetIzvornik(ctx, id)
}

// SpremiIzvornik sprema PDF lista kako je potpisan
func (s *VodocuvarService) SpremiIzvornik(ctx context.Context, listID string, pdf []byte) error {
	if len(pdf) == 0 {
		return nil
	}
	h := sha256.Sum256(pdf)
	return s.repo.SaveIzvornik(ctx, &models.IzvornikLista{ListID: listID, PDF: pdf, Sazetak: hex.EncodeToString(h[:])})
}
