package service

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gocop/internal/razmjena"

	"gocop/internal/models"
	"gocop/internal/pdfw"
	"gocop/internal/posta"
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
	posta       posta.Postavke     // poslužitelj e-pošte za slanje akata
	// aktivna vraća otvoren dnevnik COP-a sektora: bez njega je obrana
	// preventivna i akti se ne sastavljaju ni ne ovjeravaju; objavi upisuje
	// ovjeren akt u dnevnike. Bez oboje (u testu) pravilo ne vrijedi.
	aktivna func(ctx context.Context, sektor string) *models.Journal
	objavi  func(ctx context.Context, u *models.User, a *models.Akt, j *models.Journal) []string
	// povijest drži po jednu bravu po sektoru (sektor → *sync.Mutex):
	// izvođenje povijesti obrane, od čitanja akata do upisa epizoda, jedan
	// je kritični odsječak, pa se ovjera, storno i krug čvora ne preklapaju
	povijest sync.Map
}

// bravaPovijesti je brava izvođenja povijesti obrane sektora
func (s *AktService) bravaPovijesti(sektor string) *sync.Mutex {
	b, _ := s.povijest.LoadOrStore(sektor, &sync.Mutex{})
	return b.(*sync.Mutex)
}

// SetObrana daje servisu pravilo aktivne obrane i objavu akata u dnevnike
func (s *AktService) SetObrana(aktivna func(ctx context.Context, sektor string) *models.Journal, objavi func(ctx context.Context, u *models.User, a *models.Akt, j *models.Journal) []string) {
	s.aktivna, s.objavi = aktivna, objavi
}

// AktivnaObrana vraća otvoren dnevnik COP-a sektora, ili nil kad je obrana
// preventivna; bez pravila (nil) vraća nil, a ErrPreventivna se ne diže
func (s *AktService) AktivnaObrana(ctx context.Context, sektor string) *models.Journal {
	if s == nil || s.aktivna == nil {
		return nil
	}
	return s.aktivna(ctx, sektor)
}

// trebaAktivnu provjerava da je obrana u sektoru akta aktivna
func (s *AktService) trebaAktivnu(ctx context.Context, a *models.Akt) (*models.Journal, error) {
	if s.aktivna == nil {
		return nil, nil
	}
	j := s.aktivna(ctx, a.Sektor)
	if j == nil {
		return nil, ErrPreventivnaObrana{Sektor: a.Sektor}
	}
	return j, nil
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

	if err := s.dosegAkta(ctx, a, st, z.Dionice); err != nil {
		return nil, err
	}
	if !s.smijeSastaviti(perms, a) {
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

// dosegAkta upisuje u akt njegove dionice (dioniceAkta), sektor i područje
func (s *AktService) dosegAkta(ctx context.Context, a *models.Akt, st *models.Station, zadane []string) error {
	dionice, err := s.dioniceAkta(ctx, st, zadane)
	if err != nil {
		return err
	}
	for _, sec := range dionice {
		a.Dionice = append(a.Dionice, models.AktDionica{Code: sec.Code, Opis: strings.TrimSpace(sec.Description)})
	}
	a.Sektor, a.AreaID = dionice[0].SectorID, podrucjeAkta(dionice)
	return nil
}

// smijeSastaviti javlja smije li osoba sastaviti akt: piše po njegovu
// dosegu ili ga smije ovjeriti
func (s *AktService) smijeSastaviti(perms *models.UserPermissions, a *models.Akt) bool {
	return s.pisePoAktu(perms, a) || s.SmijeOvjeriti(perms, a)
}

// SmijeAktZaLetvu javlja bi li Pripremi osobi prihvatio akt stupnja po
// letvi (bez užeg izbora dionica): dionice letve su u registru, a osoba
// piše po dosegu akta ili ga smije ovjeriti. Obrazac akta nudi samo takve
// letve.
func (s *AktService) SmijeAktZaLetvu(ctx context.Context, perms *models.UserPermissions, st *models.Station, stupanj models.DefensePhase) bool {
	if perms == nil || st == nil {
		return false
	}
	a := &models.Akt{Stupanj: stupanj}
	return s.dosegAkta(ctx, a, st, nil) == nil && s.smijeSastaviti(perms, a)
}

// dioniceAkta su dionice akta iz registra, po šifri: one za koje je
// vodomjer mjerodavan, ili zadani uži izbor među njima. Zadana dionica koja
// nije dionica letve ili je nema u registru greška je unosa; dionica letve
// koje nema u registru, a nije zadana, izostaje. Akt bez ijedne dionice iz
// registra ne priprema se.
func (s *AktService) dioniceAkta(ctx context.Context, st *models.Station, zadane []string) ([]models.Section, error) {
	sifre := st.SectionCodes
	if len(sifre) == 0 {
		sifre, _ = s.stations.GetSectionCodesForStation(ctx, st.ID)
	}
	if len(sifre) == 0 {
		return nil, fmt.Errorf("vodomjer %s nije mjerodavan ni za jednu dionicu; poveži ga s dionicama u registru", st.Name)
	}
	izbor, err := uziIzborDionica(sifre, zadane, st.Name)
	if err != nil {
		return nil, err
	}
	out, err := s.dioniceIzRegistra(izbor, len(zadane) > 0)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("nijedne dionice vodomjera %s nema u registru dionica; akt bez dionica se ne priprema", st.Name)
	}
	return out, nil
}

// uziIzborDionica su zadane dionice kad su sve među dionicama letve, a bez
// zadanih sve dionice letve
func uziIzborDionica(sifre, zadane []string, letva string) ([]string, error) {
	if len(zadane) == 0 {
		return sifre, nil
	}
	for _, c := range zadane {
		if !sadrzi(sifre, c) {
			return nil, fmt.Errorf("dionica %s nije među dionicama za koje je vodomjer %s mjerodavan", c, letva)
		}
	}
	return zadane, nil
}

// dioniceIzRegistra čita dionice iz registra, poredane po šifri. Dionica
// koje nema u registru izostaje, a kad je zadana, greška je unosa.
func (s *AktService) dioniceIzRegistra(sifre []string, zadane bool) ([]models.Section, error) {
	sifre = append([]string(nil), sifre...)
	sort.Strings(sifre)
	var out []models.Section
	for _, c := range sifre {
		sec, err := s.sections.GetSectionByCode(c)
		switch {
		case err != nil:
			return nil, err
		case sec != nil:
			out = append(out, *sec)
		case zadane:
			return nil, fmt.Errorf("dionice %s nema u registru dionica", c)
		}
	}
	return out, nil
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
	if !s.pisePoAktu(perms, a) && !s.SmijeOvjeriti(perms, a) {
		return ErrUnauthorized
	}
	if a.ID == "" {
		// novi nacrt nastaje samo u aktivnoj obrani; postojeći se smije doraditi
		if _, err := s.trebaAktivnu(ctx, a); err != nil {
			return err
		}
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
	if err := s.provjeriPrijeOvjere(ctx, a); err != nil {
		return nil, nil, err
	}
	return s.zakljuciOvjeru(ctx, perms, u, a, time.Now())
}

// letvaAkta vraća letvu akta iz registra. Akt bez letve, ili s letvom
// koja nije UUID (npr. pristigao razmjenom), dobiva letvu samo s nazivom iz
// akta: ovjera ne smije pasti, jer je akt već spremljen kao ovjeren.
func (s *AktService) letvaAkta(ctx context.Context, a *models.Akt) *models.Station {
	if id, err := uuid.Parse(a.StationID); err == nil && s.stations != nil {
		if st, _ := s.stations.GetStationByID(ctx, id); st != nil {
			return st
		}
	}
	return &models.Station{Name: a.StationName}
}

// StanjeObrane je stanje obrane dionice u trenutku t iz ovjerenih akata
// sektora (docs/NACRT-STADIJI-OBRANE.md) i ovjereni akti dionice koji tada još
// nisu stupili na snagu
func (s *AktService) StanjeObrane(ctx context.Context, sektor, dionica string, t time.Time) (models.StanjeObrane, []models.Akt, error) {
	akti, err := s.repo.ListAkti(ctx, repository.FiltarAkata{Sektor: sektor, Status: models.AktOvjeren})
	if err != nil {
		return models.StanjeObrane{}, nil, err
	}
	stanje, _ := models.StanjeDionice(akti, dionica, t)
	return stanje, models.NajavljeniAkti(akti, dionica, t), nil
}

// StanjaSektora su stanja obrane u trenutku t svih dionica sektora koje
// imaju ovjerenih akata
func (s *AktService) StanjaSektora(ctx context.Context, sektor string, t time.Time) (map[string]models.StanjeObrane, error) {
	akti, err := s.repo.ListAkti(ctx, repository.FiltarAkata{Sektor: sektor, Status: models.AktOvjeren})
	if err != nil {
		return nil, err
	}
	return models.StanjaDionica(akti, t), nil
}

// provjeriPrijeOvjere: isti preduvjeti na oba puta ovjere (izravno i skenom
// potpisanog akta): aktivna obrana u sektoru i slijed stadija na dionicama
// akta uz već ovjerene (docs/NACRT-STADIJI-OBRANE.md)
func (s *AktService) provjeriPrijeOvjere(ctx context.Context, a *models.Akt) error {
	if _, err := s.trebaAktivnu(ctx, a); err != nil {
		return err
	}
	ovjereni, err := s.repo.ListAkti(ctx, repository.FiltarAkata{Sektor: a.Sektor, Status: models.AktOvjeren})
	if err != nil {
		return err
	}
	if err := models.ProvjeriSlijed(ovjereni, *a); err != nil {
		return fmt.Errorf("akt se ne može ovjeriti — %w", err)
	}
	return nil
}

// spremanZaOvjeruSkenom: akt još nije ovjeren i prolazi iste preduvjete kao
// izravna ovjera; provjerava se prije nego što se sken spremi
func (s *AktService) spremanZaOvjeruSkenom(ctx context.Context, a *models.Akt) error {
	if a.Ovjeren() {
		return fmt.Errorf("akt %s je već ovjeren", a.Oznaka())
	}
	return s.provjeriPrijeOvjere(ctx, a)
}

// uskladiEpizode izvodi povijest obrane (epizode) dionica akta iz ovjerenih,
// neponištenih akata sektora: razdoblje po razdoblje, sa stalnim
// identitetom, pa svaki čvor iz istih akata dobije iste zapise, a poništen
// akt nestane i iz povijesti. Akt koji stupa na snagu kasnije ulazi u
// povijest kad stupi na snagu (UskladiStupileNaSnagu, u krugu čvora); stanje
// ga pokazuje u svoje vrijeme i bez toga. Uz povijest se preračunava i istek
// privremenih imenovanja. Sektor je za to vrijeme zaključan
// (bravaPovijesti): krug ne smije upisati epizode iz popisa akata
// pročitanog prije storna koji je u međuvremenu povijest već uskladio.
func (s *AktService) uskladiEpizode(ctx context.Context, a *models.Akt) []string {
	if s.episodes == nil {
		return nil
	}
	// kraj obrane mijenja i istek privremenih imenovanja
	return append(s.uskladiPovijest(ctx, a), s.uskladiPrivremene()...)
}

// uskladiPovijest je povijest obrane dionica akta (uskladiEpizode) bez
// privremenih imenovanja: upozorenja su samo ona vezana uz akt
func (s *AktService) uskladiPovijest(ctx context.Context, a *models.Akt) []string {
	b := s.bravaPovijesti(a.Sektor)
	b.Lock()
	defer b.Unlock()
	ovjereni, err := s.repo.ListAkti(ctx, repository.FiltarAkata{Sektor: a.Sektor, Status: models.AktOvjeren})
	if err != nil {
		return []string{"povijest obrane nije usklađena: " + err.Error()}
	}
	var upozorenja []string
	for _, d := range a.Dionice {
		if err := s.episodes.UskladiIzAkata(ctx, d.Code, epizodeIzAkata(ovjereni, d.Code, time.Now()), moguceEpizode(ovjereni, d.Code)); err != nil {
			upozorenja = append(upozorenja, d.Code+": "+err.Error())
		}
	}
	return upozorenja
}

// epizodeIzAkata su razdoblja obrane dionice do trenutka t kao epizode: tko
// je proglasio i prekinuo, po čemu, i akti razdoblja u bilješci
func epizodeIzAkata(akti []models.Akt, dionica string, t time.Time) []models.DefenseEpisode {
	var out []models.DefenseEpisode
	for _, r := range models.RazdobljaObrane(akti, dionica, t) {
		prvi := r.Akti[0]
		e := models.DefenseEpisode{ID: models.IDEpizodeIzAkta(dionica, prvi.ID), SectionCode: dionica, StationID: prvi.StationID,
			StartedAt: r.Od, EndedAt: r.Do, Phase: r.Najvisi, DeclaredBy: prvi.OvjerioID, Basis: osnovaEpizode(&prvi), Origin: models.EpisodeFromOperator}
		var biljeske []string
		for _, x := range r.Akti {
			biljeske = append(biljeske, x.Naslov()+" "+x.Oznaka())
		}
		e.Note = strings.Join(biljeske, "\n")
		if r.Do != nil {
			e.EndedBy = r.Akti[len(r.Akti)-1].OvjerioID
		}
		out = append(out, e)
	}
	return out
}

// moguceEpizode su identiteti epizoda koje su akti dionice mogli otvoriti:
// svaka ovjerena uspostava, i poništena
func moguceEpizode(akti []models.Akt, dionica string) []uuid.UUID {
	var out []uuid.UUID
	for _, a := range akti {
		if a.Radnja != models.AktUspostava {
			continue
		}
		for _, d := range a.Dionice {
			if d.Code == dionica {
				out = append(out, models.IDEpizodeIzAkta(dionica, a.ID))
			}
		}
	}
	return out
}

// SmijePonistiti: ovjeren, neponišten akt poništava onaj tko ga je pripremio
// (bez obzira na potpisnika) i svatko tko ga smije ovjeriti
func (s *AktService) SmijePonistiti(perms *models.UserPermissions, u *models.User, a *models.Akt) bool {
	if a == nil || u == nil || !a.Ovjeren() || a.Storniran() {
		return false
	}
	return a.IzradioID == u.ID.String() || s.SmijeOvjeriti(perms, a)
}

// Storniraj poništava ovjeren akt kad je pogreška to što je uopće izdan
// (ispravak ide novim aktom). Akt ostaje u popisu, označen; ne ulazi u stanje
// obrane, a povijest obrane se iznova izvodi. Poništava se najkasniji akt
// dionice; poništenje se potpisuje ključem čvora i razmjenjuje s aktom.
func (s *AktService) Storniraj(ctx context.Context, perms *models.UserPermissions, u *models.User, id, razlog string) (*models.Akt, []string, error) {
	a, err := s.repo.GetAkt(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if err := s.provjeriStorno(ctx, perms, u, a, razlog); err != nil {
		return nil, nil, err
	}
	a.Storno = &models.StornoAkta{PonistioID: u.ID.String(), Ponistio: u.FullName, PonistenoAt: time.Now().UTC().Truncate(time.Second),
		Razlog: strings.TrimSpace(razlog), Cvor: s.cvor}
	if len(s.kljuc) == ed25519.PrivateKeySize {
		a.Storno.KljucCvora = razmjena.PublicKeyString(s.kljuc.Public().(ed25519.PublicKey))
		a.Storno.Potpis = base64.StdEncoding.EncodeToString(ed25519.Sign(s.kljuc, a.PorukaStorna()))
	}
	if err := s.repo.SaveAkt(ctx, a); err != nil {
		return nil, nil, err
	}
	return a, s.uskladiEpizode(ctx, a), nil
}

// provjeriStorno: akt postoji, ovjeren je i nije poništen, smije ga
// poništiti, razlog je upisan i nijedan kasniji akt na njegovim dionicama ne
// stoji na njemu
func (s *AktService) provjeriStorno(ctx context.Context, perms *models.UserPermissions, u *models.User, a *models.Akt, razlog string) error {
	switch {
	case a == nil:
		return fmt.Errorf("akt ne postoji")
	case !a.Ovjeren():
		return fmt.Errorf("poništava se samo ovjeren akt; nacrt se briše")
	case a.Storniran():
		return fmt.Errorf("akt %s je već poništen", a.Oznaka())
	case !s.SmijePonistiti(perms, u, a):
		return fmt.Errorf("%w: akt poništava onaj tko ga je pripremio ili tko ga smije ovjeriti", ErrUnauthorized)
	case strings.TrimSpace(razlog) == "":
		return fmt.Errorf("upišite zašto se akt poništava")
	}
	ovjereni, err := s.repo.ListAkti(ctx, repository.FiltarAkata{Sektor: a.Sektor, Status: models.AktOvjeren})
	if err != nil {
		return err
	}
	if k, d, ima := models.KasnijiAkt(ovjereni, *a); ima {
		return fmt.Errorf("na dionici %s poslije njega vrijedi %s %s: najprije se poništava kasniji akt", d, strings.ToLower(k.Naslov()), k.Oznaka())
	}
	return nil
}

// zakljuciOvjeru dovršava ovjeru: broj, tko i kad, kod, potpis ključem
// čvora, spremanje i usklađivanje obrane na dionicama. potpisnikPerms su
// ovlasti onoga tko akt ovjerava (za "u.z." i potpisnika). Stanje obrane na
// dionicama mijenja se ovlašću samog akta, ne ovlastima onoga tko radnju
// izvodi u programu.
func (s *AktService) zakljuciOvjeru(ctx context.Context, potpisnikPerms *models.UserPermissions, u *models.User, a *models.Akt, sad time.Time) (*models.Akt, []string, error) {
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

	upozorenja := s.uskladiEpizode(ctx, a)
	// ovjeren akt ide u dnevnike: COP-a, vodočuvara i održavanja
	if s.objavi != nil {
		var j *models.Journal
		if s.aktivna != nil {
			j = s.aktivna(ctx, a.Sektor)
		}
		upozorenja = append(upozorenja, s.objavi(ctx, u, a, j)...)
	}
	return a, upozorenja, nil
}

// smijePripremiti javlja smije li osoba raditi s nacrtom: tko ga je
// sastavio, tko piše na tom području ili sektoru, ili tko ga smije ovjeriti
func (s *AktService) smijePripremiti(perms *models.UserPermissions, u *models.User, a *models.Akt) bool {
	if perms == nil || u == nil {
		return false
	}
	return a.IzradioID == u.ID.String() || s.pisePoAktu(perms, a) || s.SmijeOvjeriti(perms, a)
}

// SmijePripremiti javlja smije li osoba raditi s nacrtom akta (smijePripremiti)
func (s *AktService) SmijePripremiti(perms *models.UserPermissions, u *models.User, a *models.Akt) bool {
	return s.smijePripremiti(perms, u, a)
}

// pisePoAktu javlja piše li osoba po dosegu akta: u njegovu sektoru (dužnost
// sektora), području (dužnost područja) ili na bar jednoj njegovoj dionici,
// po dionici, njezinu području ili sektoru. Akt vodomjera obuhvaća i
// dionice drugog područja (Vukovar: B.15.x i B.34.5), a nosi područje s
// najviše dionica; uprava manjeg područja piše ga kao i njezin rukovoditelj
// dionice. Sektor upisan uz dužnost područja ili dionice ne daje akte
// cijelog sektora.
func (s *AktService) pisePoAktu(perms *models.UserPermissions, a *models.Akt) bool {
	if perms == nil || a == nil {
		return false
	}
	if perms.HasWriteAccess(a.Sektor, a.AreaID, "") {
		return true
	}
	for _, d := range a.Dionice {
		if perms.AllowedSections[d.Code] {
			return true
		}
		if s.sections == nil {
			continue
		}
		if sec, err := s.sections.GetSectionByCode(d.Code); err == nil && sec != nil && perms.HasWriteAccess(sec.SectorID, sec.AreaID, "") {
			return true
		}
	}
	return false
}

// podrucjeAkta je područje akta: ono s najviše dionica, a kad ih dva imaju
// jednako, ono čija dionica dolazi prva po redu (dionice su poredane po
// šifri). Ne smije ovisiti o redoslijedu obilaska mape, jer o području ovisi
// tko akt potpisuje.
func podrucjeAkta(dionice []models.Section) int {
	broj := map[int]int{}
	var redom []int
	for _, d := range dionice {
		if broj[d.AreaID] == 0 {
			redom = append(redom, d.AreaID)
		}
		broj[d.AreaID]++
	}
	podrucje := 0
	for _, id := range redom {
		if broj[id] > broj[podrucje] || podrucje == 0 {
			podrucje = id
		}
	}
	return podrucje
}

// MoguPotpisati su korisnici koji po zaduženjima smiju ovjeriti akt, za
// izbor potpisnika kad se učitava sken ručno potpisanog akta
func (s *AktService) MoguPotpisati(a *models.Akt) []models.User {
	svi, err := s.users.ListUsers(a.Sektor, 0, "", "", "")
	if err != nil {
		return nil
	}
	var out []models.User
	for _, x := range svi {
		p := models.NewUserPermissions(x)
		if p.IsGlobalAdmin {
			continue // uprava organizacije smije sve, ali akt područja ne potpisuje
		}
		if s.SmijeOvjeriti(p, a) {
			out = append(out, x)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FullName < out[j].FullName })
	return out
}

// UcitajSkenirani prima sken ispisa potpisanog vlastoručno i ovjerenog
// žigom (PDF, JPEG ili PNG) i njime ovjerava akt. Sken se ne može
// provjeriti strojno, pa onaj tko ga učitava navodi tko je potpisao; to
// mora biti osoba koja akt smije ovjeriti. Sken postaje izvornik.
func (s *AktService) UcitajSkenirani(ctx context.Context, perms *models.UserPermissions, u *models.User, id string, datoteka []byte, potpisnikID string) (*models.Akt, []string, error) {
	a, err := s.repo.GetAkt(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if a == nil {
		return nil, nil, fmt.Errorf("akt ne postoji")
	}
	if err := s.spremanZaOvjeruSkenom(ctx, a); err != nil {
		return nil, nil, err
	}
	if !s.smijePripremiti(perms, u, a) {
		return nil, nil, ErrUnauthorized
	}
	if len(datoteka) == 0 {
		return nil, nil, fmt.Errorf("odaberite sken potpisanog akta")
	}
	pdf := datoteka
	if !bytes.HasPrefix(datoteka, []byte("%PDF")) {
		if pdf, err = pdfw.PDFIzSlike(datoteka, a.Naslov()); err != nil {
			return nil, nil, fmt.Errorf("sken mora biti PDF")
		}
	}
	pid, err := uuid.Parse(potpisnikID)
	if err != nil {
		return nil, nil, fmt.Errorf("odaberite tko je akt potpisao")
	}
	potpisnik, err := s.users.GetUserByID(pid)
	if err != nil || potpisnik == nil {
		return nil, nil, fmt.Errorf("potpisnik ne postoji")
	}
	potpisnikPerms := models.NewUserPermissions(*potpisnik)
	if !s.SmijeOvjeriti(potpisnikPerms, a) {
		return nil, nil, fmt.Errorf("%s po zaduženjima ne smije ovjeriti %s", potpisnik.FullName, strings.ToLower(a.Naslov()))
	}
	h := sha256.Sum256(pdf)
	a.Rucno = &models.RucniPotpis{PotpisnikID: potpisnik.ID.String(), Potpisnik: potpisnik.FullName, Sazetak: hex.EncodeToString(h[:]), Ucitao: u.FullName, UcitanoAt: time.Now()}
	if err := s.repo.SaveIzvornik(ctx, &repository.Izvornik{AktID: a.ID, PDF: pdf, Sazetak: a.Rucno.Sazetak}); err != nil {
		return nil, nil, err
	}
	return s.zakljuciOvjeru(ctx, potpisnikPerms, potpisnik, a, time.Now())
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

// aktKojiSePrekida je ovjereni, neponišteni akt o uspostavi istog stupnja
// po istom vodomjeru koji još nije prekinut, a prekid ga stavlja izvan
// snage: zadani, ili zadnji. Zadani se prihvaća samo ako je među aktima koje
// nudi obrazac (AktiZaPrekid), pa ni ručno sastavljen ni zastario obrazac ne
// veže drugi prekid na već prekinutu uspostavu.
func (s *AktService) aktKojiSePrekida(ctx context.Context, a *models.Akt, id string) (*models.Akt, error) {
	kandidati, err := s.AktiZaPrekid(ctx, a.StationID, a.Stupanj)
	if err != nil {
		return nil, err
	}
	if id == "" {
		if len(kandidati) == 0 {
			return nil, nil
		}
		return &kandidati[0], nil
	}
	for i := range kandidati {
		if kandidati[i].ID == id {
			return &kandidati[i], nil
		}
	}
	return nil, fmt.Errorf("odabrani akt nije ovjereni, neponišteni, neprekinuti akt o uspostavi istog stupnja po vodomjeru %s", a.StationName)
}

// najviseZaPrekid: koliko se akata o uspostavi nudi za prekid
const najviseZaPrekid = 20

// AktiZaPrekid su ovjereni, neponišteni akti o uspostavi po vodomjeru koje
// još nijedan ovjereni, neponišteni prekid ne stavlja izvan snage, najnoviji
// prvo; stupanj prazno = svi stupnjevi
func (s *AktService) AktiZaPrekid(ctx context.Context, stationID string, stupanj models.DefensePhase) ([]models.Akt, error) {
	akti, err := s.repo.ListAkti(ctx, repository.FiltarAkata{StationID: stationID, Status: models.AktOvjeren})
	if err != nil {
		return nil, err
	}
	return neprekinuteUspostave(akti, stupanj, najviseZaPrekid), nil
}

// neprekinuteUspostave su neponišteni akti o uspostavi stupnja (prazno =
// svih) među aktima, njih najviše n redom kojim dolaze, koje nijedan
// neponišteni prekid među njima ne stavlja izvan snage
func neprekinuteUspostave(akti []models.Akt, stupanj models.DefensePhase, n int) []models.Akt {
	prekinut := map[string]bool{}
	for _, p := range akti {
		if p.Radnja == models.AktPrekid && !p.Storniran() {
			prekinut[p.PrekidaAktID] = true
		}
	}
	var out []models.Akt
	for _, u := range akti {
		if len(out) < n && neponistenaUspostava(u, stupanj) && !prekinut[u.ID] {
			out = append(out, u)
		}
	}
	return out
}

// neponistenaUspostava javlja je li akt neponišten akt o uspostavi stupnja
// (prazno = bilo kojeg)
func neponistenaUspostava(u models.Akt, stupanj models.DefensePhase) bool {
	return u.Radnja == models.AktUspostava && !u.Storniran() && (stupanj == "" || u.Stupanj == stupanj)
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
		if perms.RadiUSektoru(a.Sektor) || perms.RadiUPodrucju(a.AreaID) || perms.AdminSectors[a.Sektor] || perms.AdminAreas[a.AreaID] || vidiDionicu(perms, a) {
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

// ---- žig centra ----

// Zig vraća skenirani žig sektora; nil kad ga nema
func (s *AktService) Zig(ctx context.Context, sektor string) *models.Zig {
	z, err := s.repo.GetZig(ctx, sektor)
	if err != nil {
		return nil
	}
	return z
}

// SpremiZig sprema sken žiga sektora: PNG ili JPEG do 2 MB; smije uprava sektora
func (s *AktService) SpremiZig(ctx context.Context, perms *models.UserPermissions, u *models.User, sektor string, slika []byte) error {
	if perms == nil || !perms.CanAdminister(sektor, 0) {
		return ErrUnauthorized
	}
	if len(slika) == 0 {
		return fmt.Errorf("odaberite sliku žiga")
	}
	if len(slika) > models.ZigMaxBytes {
		return fmt.Errorf("slika žiga je prevelika (najviše 2 MB); smanjite razlučivost skena")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(slika))
	if err != nil || (format != "png" && format != "jpeg") {
		return fmt.Errorf("žig mora biti slika PNG ili JPEG")
	}
	if cfg.Width < 100 || cfg.Height < 100 {
		return fmt.Errorf("slika žiga je premala (%d×%d); skenirajte s najmanje 300 dpi", cfg.Width, cfg.Height)
	}
	uredio := ""
	if u != nil {
		uredio = u.FullName
	}
	return s.repo.SaveZig(ctx, &models.Zig{Sektor: sektor, Mime: "image/" + format, Slika: slika, Uredio: uredio})
}

// ObrisiZig briše žig sektora; smije uprava sektora
func (s *AktService) ObrisiZig(ctx context.Context, perms *models.UserPermissions, sektor string) error {
	if perms == nil || !perms.CanAdminister(sektor, 0) {
		return ErrUnauthorized
	}
	return s.repo.DeleteZig(ctx, sektor)
}

// ---- sken vlastoručnog potpisa ----

// kljucSlika je ključ kojim se sken potpisa šifrira na ovom čvoru
func (s *AktService) kljucSlika() []byte {
	if len(s.kljuc) == 0 {
		return nil
	}
	return posta.Kljuc(append([]byte("sken potpisa\x00"), s.kljuc.Seed()...))
}

// PotpisSlika vraća sken potpisa korisnika, otključan; nil kad ga na ovom
// čvoru nema ili je šifriran drugim ključem
func (s *AktService) PotpisSlika(ctx context.Context, userID string) *models.PotpisSlika {
	z, err := s.repo.GetPotpisSlika(ctx, userID)
	if err != nil || z == nil {
		return nil
	}
	slika, err := posta.Otkljucaj(s.kljucSlika(), z.Slika)
	if err != nil {
		return nil
	}
	z.Slika = []byte(slika)
	return z
}

// OtisciAkta skuplja slike za PDF ovjerenog akta: žig sektora i potpis ovjeritelja
func (s *AktService) OtisciAkta(ctx context.Context, a *models.Akt) models.OtisciAkta {
	o := models.OtisciAkta{Zig: s.Zig(ctx, a.Sektor)}
	if a.Ovjeren() && a.OvjerioID != "" {
		o.Potpis = s.PotpisSlika(ctx, a.OvjerioID)
	}
	return o
}

// SpremiPotpisSliku sprema sken vlastitog potpisa: PNG ili JPEG do 1 MB
func (s *AktService) SpremiPotpisSliku(ctx context.Context, u *models.User, slika []byte) error {
	if u == nil {
		return ErrUnauthorized
	}
	if len(slika) == 0 {
		return fmt.Errorf("odaberite sliku potpisa")
	}
	if len(slika) > 1<<20 {
		return fmt.Errorf("slika potpisa je prevelika (najviše 1 MB); izrežite je na sam potpis")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(slika))
	if err != nil || (format != "png" && format != "jpeg") {
		return fmt.Errorf("potpis mora biti slika PNG ili JPEG")
	}
	if cfg.Width < 120 || cfg.Height < 40 {
		return fmt.Errorf("slika potpisa je premala (%d×%d); skenirajte s najmanje 300 dpi", cfg.Width, cfg.Height)
	}
	k := s.kljucSlika()
	if k == nil {
		return fmt.Errorf("ključ čvora nije učitan; sken se ne može sigurno spremiti")
	}
	sifrirano, err := posta.Zakljucaj(k, string(slika))
	if err != nil {
		return err
	}
	return s.repo.SavePotpisSlika(ctx, &models.PotpisSlika{UserID: u.ID.String(), Mime: "image/" + format, Slika: sifrirano})
}

// SpremiIzvornikPDF sprema PDF ovjerenog akta, s otiskom žiga i potpisa,
// kao izvornik koji se dijeli među čvorovima; kad izvornik već postoji
// (sken), ne dira ga
func (s *AktService) SpremiIzvornikPDF(ctx context.Context, a *models.Akt, pdf []byte) error {
	if !a.Ovjeren() || len(pdf) == 0 {
		return nil
	}
	if iz, err := s.repo.GetIzvornik(ctx, a.ID); err != nil || iz != nil {
		return err
	}
	h := sha256.Sum256(pdf)
	return s.repo.SaveIzvornik(ctx, &repository.Izvornik{AktID: a.ID, PDF: pdf, Sazetak: hex.EncodeToString(h[:])})
}

// ObrisiPotpisSliku briše sken vlastitog potpisa
func (s *AktService) ObrisiPotpisSliku(ctx context.Context, u *models.User) error {
	if u == nil {
		return ErrUnauthorized
	}
	return s.repo.DeletePotpisSlika(ctx, u.ID.String())
}

// ---- opće opcije ----

// Opcije vraća opće prekidače programa
func (s *AktService) Opcije(ctx context.Context) models.Opcije {
	var o models.Opcije
	if v, err := s.repo.GetPostavka(ctx, repository.PostavkaOpcije); err == nil && v != "" {
		_ = json.Unmarshal([]byte(v), &o)
	}
	return o
}

// SpremiOpcije sprema prekidače; smije samo uprava organizacije
func (s *AktService) SpremiOpcije(ctx context.Context, perms *models.UserPermissions, o models.Opcije) error {
	if perms == nil || !perms.IsGlobalAdmin {
		return ErrUnauthorized
	}
	b, err := json.Marshal(o)
	if err != nil {
		return err
	}
	return s.repo.SavePostavka(ctx, repository.PostavkaOpcije, string(b))
}

// Tema vraća boje programa iz Administracije › Tema
func (s *AktService) Tema(ctx context.Context) models.Tema {
	v, _ := s.repo.GetPostavka(ctx, repository.PostavkaTema)
	return models.CitajTemu(v)
}

// SpremiTemu sprema boje programa; smije samo uprava organizacije. Boja s
// premalim kontrastom se ne sprema, jer se tekst tada ne bi dao pročitati.
// Tema odmah vrijedi na ovom čvoru, a razmjenom stiže i na ostale.
func (s *AktService) SpremiTemu(ctx context.Context, perms *models.UserPermissions, t models.Tema) error {
	if perms == nil || !perms.IsGlobalAdmin {
		return ErrUnauthorized
	}
	for _, p := range t.Provjere() {
		if p.Omjer < models.NajmanjiDopusteniKontrast {
			zarez := func(x float64) string { return strings.Replace(fmt.Sprintf("%.1f", x), ".", ",", 1) }
			return fmt.Errorf("%s tema: %s ima kontrast %s:1, a treba barem %s:1. Tema nije spremljena.",
				strings.ToUpper(p.Tema[:1])+p.Tema[1:], p.Opis, zarez(p.Omjer), zarez(models.NajmanjiDopusteniKontrast))
		}
	}
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	if err := s.repo.SavePostavka(ctx, repository.PostavkaTema, string(b)); err != nil {
		return err
	}
	models.SetTema(t)
	return nil
}

// SmijeObrisatiTrajno javlja smije li korisnik trajno obrisati ovjeren akt:
// samo kad je prekidač uključen, i samo uprava organizacije ili sektora
func (s *AktService) SmijeObrisatiTrajno(ctx context.Context, perms *models.UserPermissions, a *models.Akt) bool {
	if perms == nil || a == nil || !a.Ovjeren() {
		return false
	}
	if !s.Opcije(ctx).BrisanjeOvjerenihAkata {
		return false
	}
	return perms.IsGlobalAdmin || perms.CanAdminister(a.Sektor, 0)
}

// ObrisiAktTrajno briše ovjeren akt s izvornikom i dnevnikom slanja. Stanje
// obrane na dionicama koje je akt proglasio ne vraća se samo.
func (s *AktService) ObrisiAktTrajno(ctx context.Context, perms *models.UserPermissions, id string) error {
	a, err := s.repo.GetAkt(ctx, id)
	if err != nil || a == nil {
		return err
	}
	if !s.SmijeObrisatiTrajno(ctx, perms, a) {
		if !s.Opcije(ctx).BrisanjeOvjerenihAkata {
			return fmt.Errorf("ovjeren akt se ne briše; brisanje ovjerenih akata uključuje uprava u Administraciji › Opcije")
		}
		return ErrUnauthorized
	}
	return s.repo.DeleteAktTrajno(ctx, id)
}
