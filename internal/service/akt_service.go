package service

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gocop/internal/razmjena"

	"gocop/internal/models"
	"gocop/internal/pdfpotpis"
	"gocop/internal/repository"
)

// AktService sastavlja, čuva i ovjerava akte o stupnju obrane. Akt se
// proglašava po vodomjeru i branjenom području, a dionice slijede iz toga:
// one za koje je vodomjer mjerodavan. Primatelji dolaze iz registra
// primatelja, iz ugroženih područja dionica (županije i općine, od
// izvanrednog stanja) i iz zaduženja na dionicama (rukovoditelji).
type AktService struct {
	repo        *repository.AktiRepository
	stations    *repository.StationRepository
	sections    *repository.SectionRepository
	territories *repository.TerritoryRepository
	readings    *repository.ReadingRepository
	users       *UserService
	episodes    *EpisodeService
	cvor        string
	kljuc       ed25519.PrivateKey // ključ čvora kojim se ovjera potpisuje
}

// SetKljuc daje servisu ključ čvora; bez njega se ovjera ne potpisuje
func (s *AktService) SetKljuc(k ed25519.PrivateKey) { s.kljuc = k }

// Stanja elektroničkog potpisa akta
const (
	PotpisVrijedi   = "VRIJEDI"
	PotpisNeVrijedi = "NE_VRIJEDI"
	PotpisNema      = "NEMA"
)

// ProvjeriPotpis provjerava potpis akta javnim ključem čvora koji ga je
// ovjerio: vrijedi ako sadržaj od ovjere nije mijenjan
func ProvjeriPotpis(a *models.Akt) string {
	if a == nil || a.Potpis == "" || a.KljucCvora == "" {
		return PotpisNema
	}
	pub, err := razmjena.ParsePublicKey(a.KljucCvora)
	if err != nil {
		return PotpisNeVrijedi
	}
	sig, err := base64.StdEncoding.DecodeString(a.Potpis)
	if err != nil {
		return PotpisNeVrijedi
	}
	if ed25519.Verify(pub, a.PorukaPotpisa(), sig) {
		return PotpisVrijedi
	}
	return PotpisNeVrijedi
}

func NewAktService(repo *repository.AktiRepository, stations *repository.StationRepository, sections *repository.SectionRepository,
	territories *repository.TerritoryRepository, readings *repository.ReadingRepository, users *UserService, episodes *EpisodeService, cvor string) *AktService {
	return &AktService{repo: repo, stations: stations, sections: sections, territories: territories, readings: readings, users: users, episodes: episodes, cvor: cvor}
}

// ZahtjevAkta je što čovjek zada; ostalo se izvede
type ZahtjevAkta struct {
	StationID string
	Radnja    string
	Stupanj   models.DefensePhase
	Vrijedi   time.Time
	Prognoza  string // prazno = po izmjerenom vodostaju
	Napomena  string
	Dionice   []string // prazno = sve za koje je vodomjer mjerodavan
	// OcitanjeID je očitanje na koje se akt poziva; prazno = zadnje.
	// Tendencija prazno = izračunata prema očitanju prije odabranoga.
	OcitanjeID string
	Tendencija string
	// PrekidaAktID je akt o uspostavi koji prekid stavlja izvan snage;
	// prazno = zadnji ovjereni akt o uspostavi istog stupnja po vodomjeru
	PrekidaAktID string
}

// Pripremi sastavlja nacrt akta iz vodomjera: dionice, zadnji vodostaj s
// tendencijom, potpisnik po stupnju i primatelji. Ništa ne sprema.
func (s *AktService) Pripremi(ctx context.Context, perms *models.UserPermissions, u *models.User, z ZahtjevAkta) (*models.Akt, error) {
	if perms == nil || u == nil {
		return nil, ErrUnauthorized
	}
	if z.Radnja != models.AktUspostava && z.Radnja != models.AktPrekid {
		return nil, fmt.Errorf("odaberi uspostavu ili prekid")
	}
	if !z.Stupanj.InForce() {
		return nil, fmt.Errorf("odaberi stupanj obrane")
	}
	id, err := uuid.Parse(z.StationID)
	if err != nil {
		return nil, fmt.Errorf("neispravan vodomjer")
	}
	st, err := s.stations.GetStationByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, fmt.Errorf("vodomjer ne postoji")
	}
	a := &models.Akt{
		Radnja: z.Radnja, Stupanj: z.Stupanj, StationID: st.ID.String(), StationName: st.Name,
		Watercourse: nazivVode(st.Watercourse), Prognoza: strings.TrimSpace(z.Prognoza), Napomena: strings.TrimSpace(z.Napomena),
		Vrijedi: z.Vrijedi, Status: models.AktNacrt,
		IzradioID: u.ID.String(), Izradio: u.FullName, IzradenoAt: time.Now(),
	}
	if a.Vrijedi.IsZero() {
		a.Vrijedi = time.Now().Truncate(time.Minute)
	}

	// dionice za koje je vodomjer mjerodavan, ili uži izbor među njima
	sifre := st.SectionCodes
	if len(sifre) == 0 {
		sifre, _ = s.stations.GetSectionCodesForStation(ctx, st.ID)
	}
	if len(z.Dionice) > 0 {
		dopustene := map[string]bool{}
		for _, c := range sifre {
			dopustene[c] = true
		}
		var izbor []string
		for _, c := range z.Dionice {
			if dopustene[c] {
				izbor = append(izbor, c)
			}
		}
		sifre = izbor
	}
	if len(sifre) == 0 {
		return nil, fmt.Errorf("vodomjer %s nije mjerodavan ni za jednu dionicu; poveži ga s dionicama u registru", st.Name)
	}
	sort.Strings(sifre)
	podrucja := map[int]int{}
	for _, c := range sifre {
		sec, err := s.sections.GetSectionByCode(c)
		if err != nil || sec == nil {
			continue
		}
		a.Dionice = append(a.Dionice, models.AktDionica{Code: sec.Code, Opis: strings.TrimSpace(sec.Description)})
		podrucja[sec.AreaID]++
		if a.Sektor == "" {
			a.Sektor = sec.SectorID
		}
	}
	for areaID, n := range podrucja {
		if n > podrucja[a.AreaID] || a.AreaID == 0 {
			a.AreaID = areaID
		}
	}
	if !perms.HasWriteAccess(a.Sektor, a.AreaID, "") && !s.SmijeOvjeriti(perms, a) {
		return nil, fmt.Errorf("%w: akt za branjeno područje %d sastavlja tko ondje vodi obranu", ErrUnauthorized, a.AreaID)
	}

	// odabrano očitanje (ili zadnje) i tendencija prema očitanju prije njega
	if a.Prognoza == "" {
		izbor, err := s.OcitanjaZaAkt(ctx, st.ID.String(), 0)
		if err != nil {
			return nil, err
		}
		i := -1
		for k, o := range izbor {
			if z.OcitanjeID == "" || o.ID.String() == z.OcitanjeID {
				i = k
				break
			}
		}
		if z.OcitanjeID != "" && i < 0 {
			return nil, fmt.Errorf("odabrano očitanje nije među očitanjima vodomjera %s", st.Name)
		}
		if i >= 0 {
			o := izbor[i]
			v := *o.LevelCm
			a.VodostajCm, a.VodostajKad = &v, o.MeasuredAt
			a.Tendencija = models.TendencijaStagnacija
			if i+1 < len(izbor) {
				switch d := v - *izbor[i+1].LevelCm; {
				case d > 0:
					a.Tendencija = models.TendencijaPorast
				case d < 0:
					a.Tendencija = models.TendencijaOpadanje
				}
			}
		}
	}
	if z.Tendencija != "" && models.TendencijaNaziv(z.Tendencija) != "" {
		a.Tendencija = z.Tendencija
	}
	if a.Radnja == models.AktPrekid {
		if u, err := s.aktKojiSePrekida(ctx, a, z.PrekidaAktID); err != nil {
			return nil, err
		} else if u != nil {
			a.PrekidaAktID, a.IzvanSnage = u.ID, models.RecenicaIzvanSnage(*u)
		}
	}
	a.Potpisnik = s.potpisnik(a)
	a.Primatelji = s.primatelji(ctx, a)
	sp, _ := s.repo.GetSpranca(ctx, a.Sektor)
	a.Uvod, a.Zavrsno, a.Poveznice = sp.Uvod(*a), sp.Zavrsno, strings.TrimSpace(sp.Poveznice)
	return a, nil
}

// Spranca vraća šprancu sektora
func (s *AktService) Spranca(ctx context.Context, sektor string) (models.Spranca, error) {
	return s.repo.GetSpranca(ctx, sektor)
}

// SpremiSprancu upisuje šprancu; smije uprava sektora. Vrijedi za nove
// nacrte; već sastavljeni i ovjereni akti zadržavaju svoj tekst.
func (s *AktService) SpremiSprancu(ctx context.Context, perms *models.UserPermissions, u *models.User, sp *models.Spranca) error {
	if perms == nil || !perms.CanAdminister(sp.Sektor, 0) {
		return fmt.Errorf("%w: šprancu uređuje uprava sektora", ErrUnauthorized)
	}
	sp.Osnova = strings.TrimSpace(sp.Osnova)
	sp.Zavrsno = strings.TrimSpace(sp.Zavrsno)
	if sp.Osnova == "" || sp.Zavrsno == "" {
		return fmt.Errorf("pravna osnova i završna rečenica ne smiju biti prazne")
	}
	if u != nil {
		sp.Uredio = u.FullName
	}
	return s.repo.SaveSpranca(ctx, sp)
}

// UrediTekst mijenja tekst nacrta: uvod, završnu rečenicu i napomenu. Smije
// tko je nacrt sastavio ili tko ga smije ovjeriti; ovjeren akt se ne mijenja.
func (s *AktService) UrediTekst(ctx context.Context, perms *models.UserPermissions, u *models.User, id, uvod, izvanSnage, zavrsno, napomena string) (*models.Akt, error) {
	a, err := s.repo.GetAkt(ctx, id)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, fmt.Errorf("akt ne postoji")
	}
	if a.Ovjeren() {
		return nil, fmt.Errorf("akt %s je ovjeren i ne mijenja se; ispravak je novi akt", a.Oznaka())
	}
	if u == nil || (a.IzradioID != u.ID.String() && !s.SmijeOvjeriti(perms, a)) {
		return nil, ErrUnauthorized
	}
	uvod, zavrsno = strings.TrimSpace(uvod), strings.TrimSpace(zavrsno)
	if uvod == "" || zavrsno == "" {
		return nil, fmt.Errorf("uvod i završna rečenica ne smiju biti prazni")
	}
	a.Uvod, a.Zavrsno, a.Napomena = uvod, zavrsno, strings.TrimSpace(napomena)
	// tekst se promijenio: PDF-ovi preuzeti za potpis više ne vrijede
	a.ZaPotpis = nil
	if a.Radnja == models.AktPrekid {
		a.IzvanSnage = strings.TrimSpace(izvanSnage)
	}
	if err := s.repo.SaveAkt(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

// nazivVode je voda kako stoji u rečenici akta: "r. Dunav"
func nazivVode(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	l := strings.ToLower(v)
	if strings.HasPrefix(l, "r. ") || strings.HasPrefix(l, "p. ") || strings.HasPrefix(l, "k. ") || strings.HasPrefix(l, "rijeka ") || strings.HasPrefix(l, "potok ") || strings.HasPrefix(l, "kanal ") {
		return v
	}
	return "r. " + v
}

// potpisnik je funkcija koja akt donosi: pripremno i redovnu rukovoditelj
// branjenog područja, izvanrednu i izvanredno stanje rukovoditelj sektora
func (s *AktService) potpisnik(a *models.Akt) string {
	if a.Stupanj == models.PhaseEmergency || a.Stupanj == models.PhaseState {
		return "Rukovoditelj obrane od poplava Sektora " + a.Sektor
	}
	return fmt.Sprintf("Rukovoditelj obrane od poplava za branjeno područje %d", a.AreaID)
}

// SmijeOvjeriti javlja smije li osoba ovjeriti akt. Ovjeravaju samo
// rukovoditelji obrane i njihovi zamjenici, po Državnom planu:
//   - pripremno stanje i redovitu obranu rukovoditelj branjenog područja
//     (XXII, XXIII), ili razina sektora;
//   - izvanrednu obranu rukovoditelj sektora (XXIV);
//   - izvanredno stanje rukovoditelj sektora, a u hitnim slučajevima i
//     rukovoditelj branjenog područja (XXV).
//
// Uprava organizacije (glavni rukovoditelj, Glavni centar) smije uvijek.
// Upravljanje područjem samo po sebi ne daje ovjeru: voditelj usluga
// izvođača upravlja svojim područjem, a nije rukovoditelj obrane.
func (s *AktService) SmijeOvjeriti(perms *models.UserPermissions, a *models.Akt) bool {
	if perms == nil || a == nil {
		return false
	}
	if perms.IsGlobalAdmin {
		return true
	}
	if razinaSektora(perms, a) {
		return true
	}
	if a.Stupanj == models.PhaseEmergency {
		return false
	}
	return razinaPodrucja(perms, a)
}

// aktivnaZaduzenja su zaduženja osobe koja vrijede sada
func aktivnaZaduzenja(perms *models.UserPermissions) []models.Duty {
	var out []models.Duty
	for _, d := range perms.User.Duties {
		if d.IsActive && (d.ExpiresAt == nil || d.ExpiresAt.After(time.Now())) {
			out = append(out, d)
		}
	}
	return out
}

// razinaSektora javlja je li osoba rukovoditelj obrane sektora akta ili
// njegov zamjenik; zamjenik rukovoditelja sektora za branjeno područje to
// je samo za svoje područje
func razinaSektora(perms *models.UserPermissions, a *models.Akt) bool {
	for _, d := range aktivnaZaduzenja(perms) {
		sektor := d.SectorID != nil && *d.SectorID == a.Sektor
		switch d.Role {
		case models.RoleSectorLeader, models.RoleSectorDeputy, models.RoleSectorMainDeputy:
			if sektor {
				return true
			}
		case models.RoleSectorAreaDeputy:
			if d.AreaID != nil && *d.AreaID == a.AreaID {
				return true
			}
		}
	}
	return false
}

// razinaPodrucja javlja je li osoba rukovoditelj obrane branjenog područja
// akta ili njegov zamjenik
func razinaPodrucja(perms *models.UserPermissions, a *models.Akt) bool {
	for _, d := range aktivnaZaduzenja(perms) {
		if (d.Role == models.RoleAreaLeader || d.Role == models.RoleAreaDeputy) && d.AreaID != nil && *d.AreaID == a.AreaID {
			return true
		}
	}
	return false
}

// primatelji slaže popis primatelja akta, po skupinama i bez ponavljanja
func (s *AktService) primatelji(ctx context.Context, a *models.Akt) []models.AktPrimatelj {
	var out []models.AktPrimatelj
	vidjeno := map[string]bool{}
	dodaj := func(skupina, naziv, email string) {
		naziv = strings.TrimSpace(naziv)
		if naziv == "" {
			return
		}
		kljuc := strings.ToLower(naziv + "|" + email)
		if vidjeno[kljuc] {
			return
		}
		vidjeno[kljuc] = true
		out = append(out, models.AktPrimatelj{Naziv: naziv, Email: strings.TrimSpace(email), Skupina: skupina})
	}

	// ugroženo područje dionica: županije, gradovi i općine
	zupanije := map[int]string{}
	var zupRed []int
	opcineUgrozene := map[int]bool{}
	var opcine []models.SectionTerritory
	if s.territories != nil {
		for _, d := range a.Dionice {
			ter, err := s.territories.GetSectionTerritories(ctx, d.Code)
			if err != nil {
				continue
			}
			for _, t := range ter {
				if _, ok := zupanije[t.CountyID]; !ok {
					zupRed = append(zupRed, t.CountyID)
				}
				zupanije[t.CountyID] = t.CountyName
				if !opcineUgrozene[t.MunicipalityID] {
					opcine = append(opcine, t)
				}
				opcineUgrozene[t.MunicipalityID] = true
			}
		}
	}

	// službe županija ugroženog područja, za svaki stupanj: civilna zaštita
	// s prevencijom i 112 kao podstavkama, policija, lučke kapetanije
	for _, id := range zupRed {
		sluzbe, err := s.territories.ListSluzbe(ctx, id)
		if err != nil {
			continue
		}
		sort.SliceStable(sluzbe, func(i, j int) bool {
			if models.RedVrste(sluzbe[i].Vrsta) != models.RedVrste(sluzbe[j].Vrsta) {
				return models.RedVrste(sluzbe[i].Vrsta) < models.RedVrste(sluzbe[j].Vrsta)
			}
			return sluzbe[i].Redoslijed < sluzbe[j].Redoslijed
		})
		imaCZ := false
		for _, x := range sluzbe {
			if x.MunicipalityID != 0 && !opcineUgrozene[x.MunicipalityID] {
				continue
			}
			if !models.NaAktu(x.Vrsta, a.Stupanj) {
				continue
			}
			naziv := x.Naziv
			if x.Vrsta == models.SluzbaCivilnaZastita {
				imaCZ = true
			}
			if imaCZ && models.PodCivilnomZastitom(x.Vrsta) {
				naziv = "– " + naziv
			}
			dodaj(models.SkupinaSluzbe, naziv, x.Email)
		}
	}

	// registar: sektor i područje, od stupnja
	if reg, err := s.repo.ListPrimatelji(ctx, a.Sektor); err == nil {
		sort.SliceStable(reg, func(i, j int) bool {
			if reg[i].Skupina != reg[j].Skupina {
				return redSkupine(reg[i].Skupina) < redSkupine(reg[j].Skupina)
			}
			return reg[i].Redoslijed < reg[j].Redoslijed
		})
		for _, p := range reg {
			if p.Vrijedi(a.AreaID, a.Stupanj) {
				dodaj(p.Skupina, p.Naziv, p.Email)
			}
		}
	}
	// licencirane pravne osobe za obranu na području, iz registra firmi;
	// kad ih registar nema, ugovorna osoba upisana uz područje
	if firme, err := s.repo.UgovorneFirme(ctx, a.AreaID); err == nil && len(firme) > 0 {
		for _, f := range firme {
			dodaj(models.SkupinaIspostava, f.Naziv, f.Email)
		}
	} else if areas, err := s.users.ListAreas(a.Sektor); err == nil {
		for _, ar := range areas {
			if ar.ID == a.AreaID && ar.ContractorName != "" {
				dodaj(models.SkupinaIspostava, ar.ContractorName, "")
			}
		}
	}
	// župan, gradovi i općine ugroženih područja, od izvanrednog stanja
	if a.Stupanj == models.PhaseState && s.territories != nil {
		for _, id := range zupRed {
			naziv := zupanije[id]
			email := ""
			if c, err := s.territories.GetCountyByID(ctx, id); err == nil && c != nil {
				email = c.Email
				naziv = c.Name
			}
			dodaj(models.SkupinaSamouprava, "Župan, "+naziv, email)
		}
		// e-pošta općina iz registra, jednim upitom po županiji
		emailOpcine := map[int]string{}
		for id := range zupanije {
			if ms, err := s.territories.ListMunicipalities(ctx, id, "", ""); err == nil {
				for _, m := range ms {
					emailOpcine[m.ID] = m.Email
				}
			}
		}
		sort.Slice(opcine, func(i, j int) bool { return opcine[i].MunicipalityName < opcine[j].MunicipalityName })
		for _, t := range opcine {
			vrsta := "Općina"
			if strings.EqualFold(t.MunicipalityType, "GRAD") {
				vrsta = "Grad"
			}
			dodaj(models.SkupinaSamouprava, vrsta+" "+t.MunicipalityName, emailOpcine[t.MunicipalityID])
		}
	}
	// rukovoditelji i zamjenici branjenog područja (razina 3) i dionica
	// (razina 4), svaka osoba jednom sa svim svojim funkcijama. Sektor i
	// Glavni centar primaju akt kao ustanove, pa njihovi ljudi ne idu
	// poimence, kao ni teren.
	type osoba struct {
		naziv, email string
		funkcije     []string
	}
	var redoslijed []string
	osobe := map[string]*osoba{}
	for _, d := range a.Dionice {
		lista, err := s.sections.GetSectionPersonnel(d.Code, a.AreaID, a.Sektor)
		if err != nil {
			continue
		}
		for _, o := range lista {
			if o.Rank != 3 && o.Rank != 4 {
				continue
			}
			x := osobe[o.UserID]
			if x == nil {
				naziv := o.FullName
				if o.Title != "" {
					naziv += ", " + o.Title
				}
				x = &osoba{naziv: naziv, email: o.Email}
				osobe[o.UserID] = x
				redoslijed = append(redoslijed, o.UserID)
			}
			f := o.DutyTitle
			if f == "" {
				f = o.RoleLabel
			}
			if f != "" && !sadrzi(x.funkcije, f) {
				x.funkcije = append(x.funkcije, f)
			}
		}
	}
	for _, id := range redoslijed {
		x := osobe[id]
		naziv := x.naziv
		if len(x.funkcije) > 0 {
			naziv += ", " + strings.Join(x.funkcije, "; ")
		}
		dodaj(models.SkupinaOsobe, naziv, x.email)
	}
	dodaj(models.SkupinaPismohrana, "Pismohrana", "")
	sort.SliceStable(out, func(i, j int) bool { return redSkupine(out[i].Skupina) < redSkupine(out[j].Skupina) })
	return out
}

// osnovaEpizode je po čemu je obrana proglašena, za epizodu na dionici
func osnovaEpizode(a *models.Akt) string {
	if a.Prognoza != "" {
		return models.BasisForecast
	}
	return models.BasisThreshold
}

func sadrzi(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func redSkupine(s string) int {
	for i, x := range models.SkupinePrimatelja {
		if x == s {
			return i
		}
	}
	return len(models.SkupinePrimatelja)
}

// Spremi sprema nacrt; ovjeren akt se ne mijenja
func (s *AktService) Spremi(ctx context.Context, perms *models.UserPermissions, a *models.Akt) error {
	if perms == nil {
		return ErrUnauthorized
	}
	if a.ID != "" {
		postojeci, err := s.repo.GetAkt(ctx, a.ID)
		if err != nil {
			return err
		}
		if postojeci != nil && postojeci.Ovjeren() {
			return fmt.Errorf("akt %s je ovjeren i ne mijenja se; ispravak je novi akt", postojeci.Oznaka())
		}
	}
	if !perms.HasWriteAccess(a.Sektor, a.AreaID, "") && !s.SmijeOvjeriti(perms, a) {
		return ErrUnauthorized
	}
	if len(a.Dionice) == 0 {
		return fmt.Errorf("akt bez dionica")
	}
	if a.Vrijedi.IsZero() {
		return fmt.Errorf("upiši dan i sat od kojeg vrijedi")
	}
	a.Status = models.AktNacrt
	a.Godina = a.Vrijedi.In(models.Zagreb).Year()
	return s.repo.SaveAkt(ctx, a)
}

// Ovjeri ovjerava nacrt: dodijeli broj, upiše tko i kad, izračuna kod i
// proglasi ili prekine obranu na dionicama. Vraća upozorenja s dionica na
// kojima se stanje obrane nije dalo uskladiti; akt je svejedno ovjeren, jer
// je odluka donesena, a stanje se može ispraviti na dionici.
func (s *AktService) Ovjeri(ctx context.Context, perms *models.UserPermissions, u *models.User, id string) (*models.Akt, []string, error) {
	if perms == nil || u == nil {
		return nil, nil, ErrUnauthorized
	}
	a, err := s.repo.GetAkt(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if a == nil {
		return nil, nil, fmt.Errorf("akt ne postoji")
	}
	if a.Ovjeren() {
		return a, nil, fmt.Errorf("akt %s je već ovjeren", a.Oznaka())
	}
	if !s.SmijeOvjeriti(perms, a) {
		return nil, nil, fmt.Errorf("%w: %s ovjerava %s", ErrUnauthorized, a.Naslov(), strings.ToLower(a.Potpisnik))
	}
	return s.zakljuciOvjeru(ctx, perms, perms, u, a, time.Now())
}

// zakljuciOvjeru dovršava ovjeru: broj, tko i kad, kod, potpis ključem
// čvora, spremanje i usklađivanje obrane na dionicama. potpisnikPerms su
// ovlasti onoga tko akt ovjerava (za "u.z." i potpisnika), a perms onoga tko
// radnju izvodi u programu (za epizode obrane).
func (s *AktService) zakljuciOvjeru(ctx context.Context, perms, potpisnikPerms *models.UserPermissions, u *models.User, a *models.Akt, sad time.Time) (*models.Akt, []string, error) {
	var err error
	a.Godina = a.Vrijedi.In(models.Zagreb).Year()
	if a.Broj, err = s.repo.SljedeciBroj(ctx, a.Sektor, a.Godina); err != nil {
		return nil, nil, err
	}
	a.Status = models.AktOvjeren
	a.OvjerioID, a.Ovjerio, a.OvjerenoAt, a.Cvor = u.ID.String(), u.FullName, &sad, s.cvor
	// Izvanredno stanje u hitnom slučaju proglašava rukovoditelj branjenog
	// područja: tada je potpisnik on, a ne sektor
	if a.Stupanj == models.PhaseState && !potpisnikPerms.IsGlobalAdmin && !razinaSektora(potpisnikPerms, a) {
		a.Potpisnik = fmt.Sprintf("Rukovoditelj obrane od poplava za branjeno područje %d", a.AreaID)
		a.UZamjeni = !models.Akt{Stupanj: models.PhaseRegular, AreaID: a.AreaID, Sektor: a.Sektor}.NositeljFunkcije(u.Duties)
	} else {
		a.UZamjeni = !a.NositeljFunkcije(u.Duties)
	}
	a.OvjeraKod = a.KodOvjere(a.OvjerioID, sad)
	if len(s.kljuc) == ed25519.PrivateKeySize {
		a.KljucCvora = razmjena.PublicKeyString(s.kljuc.Public().(ed25519.PublicKey))
		a.Potpis = base64.StdEncoding.EncodeToString(ed25519.Sign(s.kljuc, a.PorukaPotpisa()))
	}
	if err := s.repo.SaveAkt(ctx, a); err != nil {
		return nil, nil, err
	}

	var upozorenja []string
	if s.episodes != nil {
		// Ovjeren akt ovlašćuje promjenu stanja obrane na svojim dionicama,
		// bez obzira na to piše li onaj tko ga vraća u program baš na njima
		// (voditelj COP-a i rukovoditelj područja pišu na razini sektora i
		// područja, a epizoda se vodi po dionici).
		poAktu := &models.UserPermissions{User: *u, AllowedSections: map[string]bool{}}
		for _, d := range a.Dionice {
			poAktu.AllowedSections[d.Code] = true
		}
		perms = poAktu
		st, _ := s.stations.GetStationByID(ctx, uuid.MustParse(a.StationID))
		if st == nil {
			st = &models.Station{Name: a.StationName}
		}
		biljeska := a.Naslov() + " " + a.Oznaka()
		for _, d := range a.Dionice {
			var err error
			if a.Radnja == models.AktUspostava {
				otvorena, _ := s.episodes.Open(ctx, d.Code)
				if otvorena == nil {
					_, err = s.episodes.Declare(ctx, perms, u.ID.String(), d.Code, *st, a.Vrijedi, a.Stupanj, osnovaEpizode(a), biljeska)
				} else if a.Stupanj.Severity() > otvorena.Phase.Severity() {
					err = s.episodes.Raise(ctx, perms, d.Code, a.Stupanj, biljeska)
				}
			} else if a.Stupanj == models.PhasePrep {
				err = s.episodes.End(ctx, perms, u.ID.String(), d.Code, a.Vrijedi, biljeska)
			} else if otvorena, _ := s.episodes.Open(ctx, d.Code); otvorena != nil {
				err = s.episodes.Raise(ctx, perms, d.Code, otvorena.Phase, biljeska)
			}
			if err != nil {
				upozorenja = append(upozorenja, d.Code+": "+err.Error())
			}
		}
	}
	return a, upozorenja, nil
}

// smijePripremiti javlja smije li osoba raditi s nacrtom: tko ga je
// sastavio, tko piše na tom području ili sektoru, ili tko ga smije ovjeriti
func (s *AktService) smijePripremiti(perms *models.UserPermissions, u *models.User, a *models.Akt) bool {
	if perms == nil || u == nil {
		return false
	}
	return a.IzradioID == u.ID.String() || perms.HasWriteAccess(a.Sektor, a.AreaID, "") || s.SmijeOvjeriti(perms, a)
}

// ZabiljeziZaPotpis bilježi PDF nacrta preuzet za potpis u SIGNATOR-u
func (s *AktService) ZabiljeziZaPotpis(ctx context.Context, perms *models.UserPermissions, u *models.User, id string, pdf []byte) error {
	a, err := s.repo.GetAkt(ctx, id)
	if err != nil {
		return err
	}
	if a == nil {
		return fmt.Errorf("akt ne postoji")
	}
	if a.Ovjeren() {
		return fmt.Errorf("akt %s je već ovjeren", a.Oznaka())
	}
	if !s.smijePripremiti(perms, u, a) {
		return ErrUnauthorized
	}
	h := sha256.Sum256(pdf)
	z := models.ZapisZaPotpis{Duljina: len(pdf), Sazetak: hex.EncodeToString(h[:]), Kad: time.Now(), Tko: u.FullName}
	for _, x := range a.ZaPotpis {
		if x.Sazetak == z.Sazetak {
			return nil // isti PDF već je zabilježen
		}
	}
	a.ZaPotpis = append(a.ZaPotpis, z)
	if n := len(a.ZaPotpis); n > 20 {
		a.ZaPotpis = a.ZaPotpis[n-20:]
	}
	return s.repo.SaveAkt(ctx, a)
}

// UcitajPotpisani prima PDF potpisan u SIGNATOR-u i njime ovjerava akt:
// potpis mora biti ispravan, kvalificiran i pokrivati cijeli dokument;
// dokument mora biti PDF za potpis baš ovog nacrta; potpisnik mora biti
// rukovoditelj koji akt smije ovjeriti. Potpisani PDF postaje izvornik.
func (s *AktService) UcitajPotpisani(ctx context.Context, perms *models.UserPermissions, u *models.User, id string, pdf []byte) (*models.Akt, []string, error) {
	a, err := s.repo.GetAkt(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if a == nil {
		return nil, nil, fmt.Errorf("akt ne postoji")
	}
	if a.Ovjeren() {
		return nil, nil, fmt.Errorf("akt %s je već ovjeren", a.Oznaka())
	}
	if !s.smijePripremiti(perms, u, a) {
		return nil, nil, ErrUnauthorized
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF")) {
		return nil, nil, fmt.Errorf("datoteka nije PDF")
	}
	p := pdfpotpis.Zadnji(pdfpotpis.Pronadji(pdf))
	switch {
	case p == nil:
		return nil, nil, fmt.Errorf("u PDF-u nema potpisa koji pokriva cijeli dokument; je li potpisan u SIGNATOR-u?")
	case !p.Ispravan:
		return nil, nil, fmt.Errorf("potpis nije ispravan: %s", p.Greska)
	case !p.Kvalificiran:
		return nil, nil, fmt.Errorf("potpis %s nije kvalificiran (certifikat: %s); akt se potpisuje kvalificiranim potpisom", p.Ime, p.Izdavatelj)
	}
	// potpisan je baš PDF za potpis ovog nacrta
	nasao := false
	for _, z := range a.ZaPotpis {
		if len(pdf) >= z.Duljina {
			h := sha256.Sum256(pdf[:z.Duljina])
			if hex.EncodeToString(h[:]) == z.Sazetak {
				nasao = true
				break
			}
		}
	}
	if !nasao {
		return nil, nil, fmt.Errorf("potpisani PDF nije PDF za potpis ovog nacrta: ili je nacrt mijenjan nakon preuzimanja, ili je potpisan drugi dokument. Preuzmite PDF za potpis ponovno i potpišite ga")
	}
	// potpisnik je rukovoditelj s pravom ovjere
	potpisnik, err := s.potpisnikIzCertifikata(a, p)
	if err != nil {
		return nil, nil, err
	}
	potpisnikPerms := models.NewUserPermissions(*potpisnik)
	kad := time.Now()
	if !p.Vrijeme.IsZero() {
		kad = p.Vrijeme
	}
	h := sha256.Sum256(pdf)
	a.Kvalificirani = &models.KvalificiraniPotpis{Ime: p.Ime, OIB: p.OIB, Izdavatelj: p.Izdavatelj, Serijski: p.Serijski, VrijediDo: p.VrijediDo,
		Vrijeme: kad, Sazetak: hex.EncodeToString(h[:]), Ucitao: u.FullName, UcitanoAt: time.Now()}
	if err := s.repo.SaveIzvornik(ctx, &repository.Izvornik{AktID: a.ID, PDF: pdf, Sazetak: a.Kvalificirani.Sazetak}); err != nil {
		return nil, nil, err
	}
	return s.zakljuciOvjeru(ctx, perms, potpisnikPerms, potpisnik, a, kad)
}

// potpisnikIzCertifikata pronalazi korisnika čije ime stoji u certifikatu
// i koji akt smije ovjeriti
func (s *AktService) potpisnikIzCertifikata(a *models.Akt, p *pdfpotpis.Potpis) (*models.User, error) {
	svi, err := s.users.ListUsers("", 0, "", "", "")
	if err != nil {
		return nil, err
	}
	kljuc := kljucImena(p.Ime)
	var istoIme []string
	for i := range svi {
		if kljucImena(svi[i].FullName) != kljuc {
			continue
		}
		istoIme = append(istoIme, svi[i].FullName)
		if s.SmijeOvjeriti(models.NewUserPermissions(svi[i]), a) {
			return &svi[i], nil
		}
	}
	if len(istoIme) > 0 {
		return nil, fmt.Errorf("%s je potpisao akt, ali po zaduženjima ne smije ovjeriti %s; ovjerava %s", p.Ime, strings.ToLower(a.Naslov()), strings.ToLower(a.Potpisnik))
	}
	return nil, fmt.Errorf("potpisnik %s nije pronađen među korisnicima goCOP-a; ime u certifikatu mora odgovarati imenu na računu", p.Ime)
}

// kljucImena svodi ime na usporedivi oblik: mala slova, bez dijakritike,
// dijelovi imena poredani ("KUNAC MILE" i "Mile Kunac" su isto)
func kljucImena(s string) string {
	s = strings.ToLower(strings.NewReplacer("č", "c", "ć", "c", "đ", "d", "š", "s", "ž", "z", "Č", "c", "Ć", "c", "Đ", "d", "Š", "s", "Ž", "z").Replace(s))
	dijelovi := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' || r == '-' || r == '.' })
	sort.Strings(dijelovi)
	return strings.Join(dijelovi, " ")
}

// Izvornik vraća potpisani PDF akta; nil kad akt nije potpisan u SIGNATOR-u
func (s *AktService) Izvornik(ctx context.Context, id string) ([]byte, error) {
	iz, err := s.repo.GetIzvornik(ctx, id)
	if err != nil || iz == nil {
		return nil, err
	}
	return iz.PDF, nil
}

// ProvjeriIzvornik ponovno provjerava potpis na spremljenom izvorniku
func (s *AktService) ProvjeriIzvornik(ctx context.Context, a *models.Akt) *pdfpotpis.Potpis {
	pdf, err := s.Izvornik(ctx, a.ID)
	if err != nil || pdf == nil {
		return nil
	}
	return pdfpotpis.Zadnji(pdfpotpis.Pronadji(pdf))
}

// OcitanjaZaAkt su očitanja letve s vodostajem, najnovije prvo, za izbor
// u obrascu akta; limit 0 znači zadanih 200
func (s *AktService) OcitanjaZaAkt(ctx context.Context, stationID string, limit int) ([]models.Reading, error) {
	if limit <= 0 {
		limit = 200
	}
	sva, err := s.readings.List(ctx, repository.ReadingFilter{StationID: stationID, Limit: limit})
	if err != nil {
		return nil, err
	}
	out := sva[:0]
	for _, o := range sva {
		if o.LevelCm != nil {
			out = append(out, o)
		}
	}
	return out, nil
}

// aktKojiSePrekida je ovjereni akt o uspostavi koji prekid stavlja izvan
// snage: zadani, ili zadnji istog stupnja po istom vodomjeru
func (s *AktService) aktKojiSePrekida(ctx context.Context, a *models.Akt, id string) (*models.Akt, error) {
	if id != "" {
		u, err := s.repo.GetAkt(ctx, id)
		if err != nil {
			return nil, err
		}
		if u == nil || !u.Ovjeren() || u.Radnja != models.AktUspostava {
			return nil, fmt.Errorf("odabrani akt nije ovjereni akt o uspostavi")
		}
		return u, nil
	}
	kandidati, err := s.AktiZaPrekid(ctx, a.StationID, a.Stupanj)
	if err != nil || len(kandidati) == 0 {
		return nil, err
	}
	return &kandidati[0], nil
}

// AktiZaPrekid su ovjereni akti o uspostavi po vodomjeru, najnoviji prvo;
// stupanj prazno = svi stupnjevi
func (s *AktService) AktiZaPrekid(ctx context.Context, stationID string, stupanj models.DefensePhase) ([]models.Akt, error) {
	return s.repo.ListAkti(ctx, repository.FiltarAkata{StationID: stationID, Stupanj: stupanj, Radnja: models.AktUspostava, Status: models.AktOvjeren, Limit: 20})
}

// ZadnjeOcitanje je zadnje očitanje letve, za obrazac akta
func (s *AktService) ZadnjeOcitanje(ctx context.Context, stationID string) (*models.Reading, error) {
	zadnja, err := s.readings.List(ctx, repository.ReadingFilter{StationID: stationID, Limit: 1})
	if err != nil || len(zadnja) == 0 {
		return nil, err
	}
	return &zadnja[0], nil
}

// DioniceLetve su dionice za koje je vodomjer mjerodavan, s opisom
func (s *AktService) DioniceLetve(ctx context.Context, st *models.Station) []models.Section {
	sifre := st.SectionCodes
	if len(sifre) == 0 {
		sifre, _ = s.stations.GetSectionCodesForStation(ctx, st.ID)
	}
	var out []models.Section
	for _, c := range sifre {
		if sec, err := s.sections.GetSectionByCode(c); err == nil && sec != nil {
			out = append(out, *sec)
		}
	}
	return out
}

// Get čita akt
func (s *AktService) Get(ctx context.Context, id string) (*models.Akt, error) {
	return s.repo.GetAkt(ctx, id)
}

// List vraća akte po filtru, samo iz sektora koje osoba vidi
func (s *AktService) List(ctx context.Context, perms *models.UserPermissions, f repository.FiltarAkata) ([]models.Akt, error) {
	akti, err := s.repo.ListAkti(ctx, f)
	if err != nil || perms == nil || perms.IsGlobalAdmin {
		return akti, err
	}
	var out []models.Akt
	for _, a := range akti {
		if perms.AllowedSectors[a.Sektor] || perms.AllowedAreas[a.AreaID] || perms.AdminSectors[a.Sektor] || perms.AdminAreas[a.AreaID] || vidiDionicu(perms, a) {
			out = append(out, a)
		}
	}
	return out, nil
}

func vidiDionicu(perms *models.UserPermissions, a models.Akt) bool {
	for _, d := range a.Dionice {
		if perms.AllowedSections[d.Code] {
			return true
		}
	}
	return false
}

// Obrisi briše nacrt; smije tko ga je sastavio ili tko bi ga smio ovjeriti
func (s *AktService) Obrisi(ctx context.Context, perms *models.UserPermissions, u *models.User, id string) error {
	a, err := s.repo.GetAkt(ctx, id)
	if err != nil || a == nil {
		return err
	}
	if a.Ovjeren() {
		return fmt.Errorf("ovjeren akt se ne briše")
	}
	if u == nil || (a.IzradioID != u.ID.String() && !s.SmijeOvjeriti(perms, a)) {
		return ErrUnauthorized
	}
	return s.repo.DeleteAkt(ctx, id)
}

// ---- registar primatelja ----

// Primatelji vraća registar primatelja sektora
func (s *AktService) Primatelji(ctx context.Context, sektor string) ([]models.Primatelj, error) {
	return s.repo.ListPrimatelji(ctx, sektor)
}

// SpremiPrimatelja upisuje primatelja; smije uprava sektora
func (s *AktService) SpremiPrimatelja(ctx context.Context, perms *models.UserPermissions, p *models.Primatelj) error {
	if perms == nil || !perms.CanAdminister(p.Sektor, p.AreaID) {
		return ErrUnauthorized
	}
	p.Naziv = strings.TrimSpace(p.Naziv)
	if p.Naziv == "" {
		return fmt.Errorf("upiši naziv primatelja")
	}
	if p.Skupina == "" {
		p.Skupina = models.SkupinaSluzbe
	}
	return s.repo.SavePrimatelj(ctx, p)
}

// ObrisiPrimatelja briše primatelja iz registra
func (s *AktService) ObrisiPrimatelja(ctx context.Context, perms *models.UserPermissions, id string) error {
	p, err := s.repo.GetPrimatelj(ctx, id)
	if err != nil || p == nil {
		return err
	}
	if perms == nil || !perms.CanAdminister(p.Sektor, p.AreaID) {
		return ErrUnauthorized
	}
	return s.repo.DeletePrimatelj(ctx, id)
}
