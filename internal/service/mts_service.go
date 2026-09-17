package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// MtsService vodi materijalno-tehnička sredstva: katalog, skladišta, promet
// i godišnji popis.
//
// Stanje se nigdje ne upisuje kao broj — zbraja se iz prometa. Zato svaki
// zahvat ovdje piše retke, a ne mijenja količinu: tko je što uzeo i po
// čijem nalogu ostaje zapisano, a zbroj se ne može razići s poviješću.
type MtsService struct {
	repo     *repository.MtsRepository
	sections *repository.SectionRepository
	users    *repository.UserRepository
}

func NewMtsService(repo *repository.MtsRepository, sections *repository.SectionRepository, users *repository.UserRepository) *MtsService {
	return &MtsService{repo: repo, sections: sections, users: users}
}

// SmijePisati: promet i popis upisuje skladištar tog područja i uprava
// područja i sektora. Vodočuvar ili strojar s dosegom na području ne —
// sredstva vodi tko za njih odgovara.
func (s *MtsService) SmijePisati(perms *models.UserPermissions, sk *models.Skladiste) bool {
	if perms == nil || sk == nil {
		return false
	}
	return perms.CanAdminister(sk.Sektor, sk.AreaID) || perms.VodiSkladista(sk.Sektor, sk.AreaID)
}

// SmijeVidjeti: stanje sredstava vidi cijela obrana. Kad negdje ponestane,
// mora se vidjeti gdje ima viška — i u drugom sektoru.
func (s *MtsService) SmijeVidjeti(perms *models.UserPermissions) bool {
	return perms != nil
}

// SmijeUrediti javlja smije li osoba mijenjati registar skladišta i katalog
func (s *MtsService) SmijeUrediti(perms *models.UserPermissions, sektor string, areaID int) bool {
	return perms != nil && perms.CanAdminister(sektor, areaID)
}

// ---- katalog i skladišta

func (s *MtsService) Vrste(ctx context.Context) ([]models.VrstaSredstva, error) {
	return s.repo.ListVrste(ctx, false)
}

func (s *MtsService) SveVrste(ctx context.Context) ([]models.VrstaSredstva, error) {
	return s.repo.ListVrste(ctx, true)
}

func (s *MtsService) Vrsta(ctx context.Context, id string) (*models.VrstaSredstva, error) {
	return s.repo.GetVrsta(ctx, id)
}

// SpremiVrstu upisuje vrstu sredstva; naziv i jedinica su obvezni
func (s *MtsService) SpremiVrstu(ctx context.Context, v *models.VrstaSredstva) error {
	v.Naziv = strings.TrimSpace(v.Naziv)
	v.Jedinica = strings.TrimSpace(v.Jedinica)
	if v.Naziv == "" {
		return errors.New("vrsta sredstva mora imati naziv")
	}
	if v.Jedinica == "" {
		return errors.New("vrsta sredstva mora imati jedinicu mjere")
	}
	if !models.PostojiGrupa(v.Grupa) {
		return errors.New("nepoznata skupina sredstava")
	}
	if v.ID == "" {
		v.ID = models.OznakaSredstva(v.Naziv)
		if v.ID == "" {
			return errors.New("iz naziva se ne može složiti oznaka; upišite je ručno")
		}
		if postoji, err := s.repo.GetVrsta(ctx, v.ID); err != nil {
			return err
		} else if postoji != nil {
			return fmt.Errorf("vrsta s oznakom %s već postoji", v.ID)
		}
	}
	return s.repo.SaveVrsta(ctx, v)
}

func (s *MtsService) Skladista(ctx context.Context, sektor string, areaID int, iUgasena bool) ([]models.Skladiste, error) {
	return s.repo.ListSkladista(ctx, sektor, areaID, iUgasena)
}

func (s *MtsService) Skladiste(ctx context.Context, id string) (*models.Skladiste, error) {
	return s.repo.GetSkladiste(ctx, id)
}

// SpremiSkladiste upisuje skladište; naziv, sektor i područje su obvezni
func (s *MtsService) SpremiSkladiste(ctx context.Context, perms *models.UserPermissions, sk *models.Skladiste) error {
	sk.Naziv = strings.TrimSpace(sk.Naziv)
	if sk.Naziv == "" {
		return errors.New("skladište mora imati naziv")
	}
	if sk.Sektor == "" {
		return errors.New("skladište mora pripadati sektoru")
	}
	if !s.SmijeUrediti(perms, sk.Sektor, sk.AreaID) {
		return errors.New("skladišta uređuje uprava branjenog područja ili sektora")
	}
	return s.repo.SaveSkladiste(ctx, sk)
}

func (s *MtsService) BrojSkladista(ctx context.Context) (int, error) {
	return s.repo.BrojSkladista(ctx)
}

// ---- stanje

// StanjeSkladista slaže stanje jednog skladišta po vrstama, redom popisa.
// Vrste kojih nema ostaju u popisu s nulom: popis koji ide Glavnom centru
// mora imati sve retke, i kad su prazni.
func (s *MtsService) StanjeSkladista(ctx context.Context, skladisteID string, naDan *time.Time) ([]models.StanjeVrste, error) {
	vrste, err := s.repo.ListVrste(ctx, false)
	if err != nil {
		return nil, err
	}
	stanja, err := s.repo.Stanje(ctx, skladisteID, "", naDan)
	if err != nil {
		return nil, err
	}
	return slozi(vrste, stanja), nil
}

// StanjeSektora zbraja sva skladišta sektora; prazan sektor znači sve
func (s *MtsService) StanjeSektora(ctx context.Context, sektor string, naDan *time.Time) ([]models.StanjeVrste, error) {
	vrste, err := s.repo.ListVrste(ctx, false)
	if err != nil {
		return nil, err
	}
	stanja, err := s.repo.Stanje(ctx, "", sektor, naDan)
	if err != nil {
		return nil, err
	}
	return slozi(vrste, stanja), nil
}

// slozi spaja katalog i zbrojeve u redak po vrsti
func slozi(vrste []models.VrstaSredstva, stanja []models.Stanje) []models.StanjeVrste {
	po := map[string]map[string]float64{}
	for _, st := range stanja {
		if po[st.VrstaID] == nil {
			po[st.VrstaID] = map[string]float64{}
		}
		po[st.VrstaID][st.Oblik] += st.Kolicina
	}
	out := make([]models.StanjeVrste, 0, len(vrste))
	for _, v := range vrste {
		red := models.StanjeVrste{Vrsta: v, PoOblicima: map[string]float64{}}
		for _, o := range v.SviOblici() {
			red.PoOblicima[o] = po[v.ID][o]
			red.Ukupno += po[v.ID][o]
		}
		// oblik koji katalog više ne nudi, a promet ga ima, ne smije nestati
		for o, k := range po[v.ID] {
			if _, ok := red.PoOblicima[o]; !ok {
				red.PoOblicima[o] = k
				red.Ukupno += k
			}
		}
		out = append(out, red)
	}
	return out
}

// MjestoZalihe je jedno skladište s količinom jedne vrste — za pitanje
// „gdje ima viška“ kad u jednom sektoru ponestane
type MjestoZalihe struct {
	Skladiste  models.Skladiste
	Kolicina   float64
	PoOblicima map[string]float64
}

// GdjeIma vraća skladišta u kojima te vrste ima, najviše prvo, kroz sve
// sektore. Vreće koje u Osijeku nedostaju možda čekaju u Metkoviću, pa se u
// obrani ne pita telefonom nego se pogleda.
func (s *MtsService) GdjeIma(ctx context.Context, vrstaID string) ([]MjestoZalihe, error) {
	stanja, err := s.repo.Stanje(ctx, "", "", nil)
	if err != nil {
		return nil, err
	}
	skladista, err := s.repo.ListSkladista(ctx, "", 0, true)
	if err != nil {
		return nil, err
	}
	po := map[string]*MjestoZalihe{}
	for i := range skladista {
		po[skladista[i].ID] = &MjestoZalihe{Skladiste: skladista[i], PoOblicima: map[string]float64{}}
	}
	for _, st := range stanja {
		if st.VrstaID != vrstaID || st.Kolicina == 0 {
			continue
		}
		m, ok := po[st.SkladisteID]
		if !ok {
			continue
		}
		m.Kolicina += st.Kolicina
		m.PoOblicima[st.Oblik] += st.Kolicina
	}
	var out []MjestoZalihe
	for _, m := range po {
		if m.Kolicina != 0 {
			out = append(out, *m)
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Kolicina != out[b].Kolicina {
			return out[a].Kolicina > out[b].Kolicina
		}
		return out[a].Skladiste.Naziv < out[b].Skladiste.Naziv
	})
	return out, nil
}

// NaTerenu vraća što je izdano a nije vraćeno ni ugrađeno, po dionicama
// jedne obrane
func (s *MtsService) NaTerenu(ctx context.Context, journalID, sektor string) ([]models.Stanje, error) {
	stanja, err := s.repo.StanjeNaTerenu(ctx, journalID, sektor)
	if err != nil {
		return nil, err
	}
	var out []models.Stanje
	for _, st := range stanja {
		if st.Kolicina != 0 {
			out = append(out, st)
		}
	}
	return out, nil
}

// kolikoIma javlja koliko jedne vrste u jednom obliku stoji na mjestu
func (s *MtsService) kolikoIma(ctx context.Context, skladisteID, sectionCode, journalID, vrstaID, oblik string) (float64, error) {
	var stanja []models.Stanje
	var err error
	if skladisteID != "" {
		stanja, err = s.repo.Stanje(ctx, skladisteID, "", nil)
	} else {
		stanja, err = s.repo.StanjeNaTerenu(ctx, journalID, "")
	}
	if err != nil {
		return 0, err
	}
	for _, st := range stanja {
		if st.VrstaID != vrstaID || st.Oblik != oblik {
			continue
		}
		if skladisteID != "" && st.SkladisteID == skladisteID {
			return st.Kolicina, nil
		}
		if skladisteID == "" && st.SectionCode == sectionCode {
			return st.Kolicina, nil
		}
	}
	return 0, nil
}

// ---- promet

// Zahvat je jedan potez u skladištu, kako ga obrazac nosi. Iz njega se
// izvode retci prometa — jedan ili dva, ovisno o tome seli li se sredstvo.
type Zahvat struct {
	Vrsta       string // Promet*
	Datum       time.Time
	SkladisteID string // skladište na kojem se radi
	VrstaID     string
	Oblik       string
	Kolicina    float64 // uvijek pozitivno; smjer određuje vrsta zahvata

	// odredište, ovisno o vrsti
	NaSkladisteID string // prijenos u drugo skladište
	SectionCode   string // dionica na koju se izdaje ili s koje se vraća
	JournalID     string // obrana uz koju izdavanje stoji
	UOblik        string // punjenje: oblik u koji prelazi

	Nalozio  string
	Preuzeo  string
	Dokument string
	Napomena string
}

// Provedi upisuje zahvat kao promet. Vraća retke koji su nastali.
func (s *MtsService) Provedi(ctx context.Context, u *models.User, perms *models.UserPermissions, z Zahvat) ([]models.Promet, error) {
	if u == nil {
		return nil, errors.New("upis zahtijeva prijavu")
	}
	if z.Kolicina <= 0 {
		return nil, errors.New("količina mora biti veća od nule")
	}
	vrsta, err := s.repo.GetVrsta(ctx, z.VrstaID)
	if err != nil {
		return nil, err
	}
	if vrsta == nil {
		return nil, errors.New("nepoznata vrsta sredstva")
	}
	sk, err := s.repo.GetSkladiste(ctx, z.SkladisteID)
	if err != nil {
		return nil, err
	}
	if sk == nil {
		return nil, errors.New("nepoznato skladište")
	}
	if !s.SmijePisati(perms, sk) {
		return nil, errors.New("promet sredstava upisuje uprava branjenog područja ili sektora")
	}
	if z.Datum.IsZero() {
		z.Datum = time.Now().In(models.Zagreb)
	}
	if pocetakDana(z.Datum).After(pocetakDana(time.Now().In(models.Zagreb))) {
		return nil, errors.New("promet se upisuje za danas ili unatrag, ne unaprijed")
	}
	if !vrsta.ImaOblike() {
		z.Oblik = models.OblikOsnovni
	}

	osnova := models.Promet{Datum: z.Datum, VrstaID: z.VrstaID, Oblik: z.Oblik, Vrsta: z.Vrsta, Sektor: sk.Sektor, JournalID: z.JournalID,
		Nalozio: strings.TrimSpace(z.Nalozio), Preuzeo: strings.TrimSpace(z.Preuzeo), Dokument: strings.TrimSpace(z.Dokument),
		Napomena: strings.TrimSpace(z.Napomena), UserID: u.ID.String(), UserName: u.FullName}
	veza := uuid.New().String()

	// koliko se smije skinuti s mjesta
	provjeriZalihu := func(skladisteID, sectionCode, oblik string, treba float64) error {
		ima, err := s.kolikoIma(ctx, skladisteID, sectionCode, z.JournalID, z.VrstaID, oblik)
		if err != nil {
			return err
		}
		if ima+1e-9 < treba {
			gdje := "u skladištu"
			if skladisteID == "" {
				gdje = "na terenu"
			}
			return fmt.Errorf("%s stoji %s %s, a skida se %s — upišite prvo primku ili ispravite količinu",
				gdje, kolicinaTekst(ima), vrsta.Jedinica, kolicinaTekst(treba))
		}
		return nil
	}

	var redci []models.Promet
	switch z.Vrsta {
	case models.PrometPrimka, models.PrometPocetno:
		r := osnova
		r.SkladisteID = sk.ID
		r.Kolicina = z.Kolicina
		redci = append(redci, r)

	case models.PrometOtpis:
		if err := provjeriZalihu(sk.ID, "", z.Oblik, z.Kolicina); err != nil {
			return nil, err
		}
		r := osnova
		r.SkladisteID = sk.ID
		r.Kolicina = -z.Kolicina
		redci = append(redci, r)

	case models.PrometPunjenje:
		// prazne vreće postaju napunjene: isti komad, drugi oblik
		if !vrsta.ImaOblike() {
			return nil, errors.New("punjenje ima smisla samo za sredstvo koje se vodi u više oblika")
		}
		iz, u := z.Oblik, z.UOblik
		if iz == "" {
			iz = models.OblikPrazno
		}
		if u == "" {
			u = models.OblikPunjeno
		}
		if iz == u {
			return nil, errors.New("punjenje mora mijenjati oblik")
		}
		if err := provjeriZalihu(sk.ID, "", iz, z.Kolicina); err != nil {
			return nil, err
		}
		a, b := osnova, osnova
		a.SkladisteID, a.Oblik, a.Kolicina, a.VezaID = sk.ID, iz, -z.Kolicina, veza
		b.SkladisteID, b.Oblik, b.Kolicina, b.VezaID = sk.ID, u, z.Kolicina, veza
		redci = append(redci, a, b)

	case models.PrometPrijenos:
		cilj, err := s.repo.GetSkladiste(ctx, z.NaSkladisteID)
		if err != nil {
			return nil, err
		}
		if cilj == nil || cilj.ID == sk.ID {
			return nil, errors.New("prijenos traži drugo skladište")
		}
		if err := provjeriZalihu(sk.ID, "", z.Oblik, z.Kolicina); err != nil {
			return nil, err
		}
		a, b := osnova, osnova
		a.SkladisteID, a.Kolicina, a.VezaID = sk.ID, -z.Kolicina, veza
		b.SkladisteID, b.Kolicina, b.VezaID, b.Sektor = cilj.ID, z.Kolicina, veza, cilj.Sektor
		if a.Napomena == "" {
			a.Napomena = "u " + cilj.Naziv
		}
		if b.Napomena == "" {
			b.Napomena = "iz " + sk.Naziv
		}
		redci = append(redci, a, b)

	case models.PrometIzdano:
		if err := provjeriZalihu(sk.ID, "", z.Oblik, z.Kolicina); err != nil {
			return nil, err
		}
		a, b := osnova, osnova
		a.SkladisteID, a.Kolicina, a.VezaID = sk.ID, -z.Kolicina, veza
		b.SectionCode, b.Kolicina, b.VezaID = z.SectionCode, z.Kolicina, veza
		redci = append(redci, a, b)

	case models.PrometPovrat:
		if err := provjeriZalihu("", z.SectionCode, z.Oblik, z.Kolicina); err != nil {
			return nil, err
		}
		a, b := osnova, osnova
		a.SectionCode, a.Kolicina, a.VezaID = z.SectionCode, -z.Kolicina, veza
		b.SkladisteID, b.Kolicina, b.VezaID = sk.ID, z.Kolicina, veza
		redci = append(redci, a, b)

	case models.PrometUtrosak:
		// ugrađeno na terenu: odlazi s terena i ne vraća se
		if err := provjeriZalihu("", z.SectionCode, z.Oblik, z.Kolicina); err != nil {
			return nil, err
		}
		r := osnova
		r.SectionCode, r.Kolicina = z.SectionCode, -z.Kolicina
		redci = append(redci, r)

	default:
		return nil, fmt.Errorf("nepoznata vrsta prometa %q", z.Vrsta)
	}

	if err := s.repo.SavePromet(ctx, redci); err != nil {
		return nil, err
	}
	return redci, nil
}

// kolicinaTekst piše količinu bez suvišnih nula, sa zarezom
func kolicinaTekst(v float64) string {
	s := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", v), "0"), ".")
	if s == "" || s == "-" {
		s = "0"
	}
	return strings.Replace(s, ".", ",", 1)
}

// Promet vraća knjigu prometa po filtru
func (s *MtsService) Promet(ctx context.Context, f repository.FiltarPrometa) ([]models.Promet, error) {
	return s.repo.ListPromet(ctx, f)
}

// ---- godišnji popis

// PredlozakPopisa vraća popis skladišta na dan; kad ga još nema, slaže novi
// s knjižnim stanjem toga dana i prošlogodišnjim potrebama za nabavom
func (s *MtsService) PredlozakPopisa(ctx context.Context, skladisteID string, dan time.Time) (*models.Popis, error) {
	sk, err := s.repo.GetSkladiste(ctx, skladisteID)
	if err != nil {
		return nil, err
	}
	if sk == nil {
		return nil, errors.New("nepoznato skladište")
	}
	dan = pocetakDana(dan)
	if postojeci, err := s.repo.PopisZaDan(ctx, skladisteID, dan); err != nil {
		return nil, err
	} else if postojeci != nil {
		return s.dopuniPopis(ctx, postojeci)
	}
	p := &models.Popis{SkladisteID: sk.ID, Sektor: sk.Sektor, Dan: dan, Godina: dan.Year(),
		SkladisteNaziv: sk.Naziv, AreaID: sk.AreaID}
	stanje, err := s.StanjeSkladista(ctx, skladisteID, &dan)
	if err != nil {
		return nil, err
	}
	for _, sv := range stanje {
		for _, o := range sv.Vrsta.SviOblici() {
			k := sv.PoOblicima[o]
			p.Stavke = append(p.Stavke, models.PopisnaStavka{VrstaID: sv.Vrsta.ID, Oblik: o, Utvrdjeno: k, Knjizno: k,
				VrstaNaziv: sv.Vrsta.Naziv, Jedinica: sv.Vrsta.Jedinica, Grupa: sv.Vrsta.Grupa})
		}
	}
	return p, nil
}

// dopuniPopis puni nazive vrsta na spremljenom popisu
func (s *MtsService) dopuniPopis(ctx context.Context, p *models.Popis) (*models.Popis, error) {
	vrste, err := s.repo.ListVrste(ctx, true)
	if err != nil {
		return nil, err
	}
	po := map[string]models.VrstaSredstva{}
	for _, v := range vrste {
		po[v.ID] = v
	}
	for i := range p.Stavke {
		if v, ok := po[p.Stavke[i].VrstaID]; ok {
			p.Stavke[i].VrstaNaziv, p.Stavke[i].Jedinica, p.Stavke[i].Grupa = v.Naziv, v.Jedinica, v.Grupa
		}
	}
	return p, nil
}

// SpremiPopis upisuje popis kao nacrt; zaključeni se ne mijenja
func (s *MtsService) SpremiPopis(ctx context.Context, u *models.User, perms *models.UserPermissions, p *models.Popis) error {
	if u == nil {
		return errors.New("upis zahtijeva prijavu")
	}
	sk, err := s.repo.GetSkladiste(ctx, p.SkladisteID)
	if err != nil {
		return err
	}
	if sk == nil {
		return errors.New("nepoznato skladište")
	}
	if !s.SmijePisati(perms, sk) {
		return errors.New("popis vodi uprava branjenog područja ili sektora")
	}
	if p.Dan.IsZero() {
		return errors.New("popis mora imati dan")
	}
	p.Dan = pocetakDana(p.Dan)
	if p.Dan.After(pocetakDana(time.Now().In(models.Zagreb))) {
		return errors.New("popis se radi za dan koji je prošao, ne unaprijed")
	}
	p.Sektor, p.Godina = sk.Sektor, p.Dan.Year()
	postojeci, err := s.repo.PopisZaDan(ctx, p.SkladisteID, p.Dan)
	if err != nil {
		return err
	}
	if postojeci != nil && postojeci.ID != p.ID {
		return fmt.Errorf("popis skladišta %s na dan %s već postoji", sk.Naziv, p.Dan.Format("2.1.2006."))
	}
	if p.ID != "" {
		cur, err := s.repo.GetPopis(ctx, p.ID)
		if err != nil {
			return err
		}
		if cur == nil {
			return errors.New("popis nije pronađen")
		}
		if cur.Zakljucen() {
			return errors.New("zaključeni popis se ne mijenja; razlika je proknjižena")
		}
		p.CreatedAt, p.IzradioID, p.Izradio, p.ZakljucenoAt = cur.CreatedAt, cur.IzradioID, cur.Izradio, cur.ZakljucenoAt
	}
	if p.IzradioID == "" {
		p.IzradioID, p.Izradio = u.ID.String(), u.FullName
	}
	p.IzradenoAt = time.Now().In(models.Zagreb)
	return s.repo.SavePopis(ctx, p)
}

// ZakljuciPopis proknjižava razliku između police i knjige. Za svaku stavku
// koja odstupa piše se usklađenje, pa stanje poslije popisa pokazuje ono
// što je prebrojano — a povijest ostaje čitljiva: vidi se koliko je i kada
// nedostajalo.
func (s *MtsService) ZakljuciPopis(ctx context.Context, u *models.User, perms *models.UserPermissions, id string) error {
	p, err := s.repo.GetPopis(ctx, id)
	if err != nil {
		return err
	}
	if p == nil {
		return errors.New("popis nije pronađen")
	}
	sk, err := s.repo.GetSkladiste(ctx, p.SkladisteID)
	if err != nil {
		return err
	}
	if u == nil || sk == nil || !s.SmijePisati(perms, sk) {
		return errors.New("popis zaključuje uprava branjenog područja ili sektora")
	}
	if p.Zakljucen() {
		return nil
	}
	if p.Prazan() {
		return errors.New("prazan popis se ne zaključuje: upišite prebrojano stanje")
	}
	// knjižno stanje se čita iznova u trenutku zaključenja: između sastavljanja
	// i zaključenja mogao je netko upisati promet
	stanje, err := s.StanjeSkladista(ctx, p.SkladisteID, &p.Dan)
	if err != nil {
		return err
	}
	knjizno := map[string]float64{}
	for _, sv := range stanje {
		for o, k := range sv.PoOblicima {
			knjizno[sv.Vrsta.ID+"|"+o] = k
		}
	}
	var redci []models.Promet
	sad := time.Now().In(models.Zagreb)
	for i := range p.Stavke {
		st := &p.Stavke[i]
		st.Knjizno = knjizno[st.VrstaID+"|"+st.Oblik]
		razlika := st.Utvrdjeno - st.Knjizno
		if razlika == 0 {
			continue
		}
		redci = append(redci, models.Promet{Datum: p.Dan, VrstaID: st.VrstaID, Oblik: st.Oblik, Kolicina: razlika,
			Vrsta: models.PrometPopis, Sektor: sk.Sektor, SkladisteID: p.SkladisteID, PopisID: p.ID, UserID: u.ID.String(), UserName: u.FullName,
			Napomena: "usklađenje po popisu na dan " + p.Dan.Format("2.1.2006.")})
	}
	if err := s.repo.SavePromet(ctx, redci); err != nil {
		return err
	}
	p.ZakljucenoAt = &sad
	return s.repo.SavePopis(ctx, p)
}

func (s *MtsService) Popis(ctx context.Context, id string) (*models.Popis, error) {
	p, err := s.repo.GetPopis(ctx, id)
	if err != nil || p == nil {
		return p, err
	}
	return s.dopuniPopis(ctx, p)
}

func (s *MtsService) Popisi(ctx context.Context, sektor, skladisteID string, godina int) ([]models.Popis, error) {
	return s.repo.ListPopisi(ctx, sektor, skladisteID, godina)
}

// ---- tablica za Glavni centar

// TablicaSredstava je popis sredstava po skladištima sektora na dan, kako
// ga sektor jednom godišnje šalje Glavnom centru: redak po vrsti, dva
// stupca po skladištu (stanje na dan, dodatne potrebe za nabavom) i zbroj
// sektora. Skladište s popisom na taj dan daje prebrojano i potrebe;
// skladište bez popisa daje knjižno stanje i nula potreba.
type TablicaSredstava struct {
	Sektor    string
	Dan       time.Time
	Skladista []models.Skladiste
	Vrste     []models.VrstaSredstva
	stanje    map[string]map[string]float64 // skladište → vrsta → stanje
	potrebe   map[string]map[string]float64
	Popisi    map[string]*models.Popis // skladište → popis na dan, kad postoji
}

// StanjeU vraća stanje vrste u skladištu, preko svih oblika
func (t TablicaSredstava) StanjeU(skladisteID, vrstaID string) float64 {
	return t.stanje[skladisteID][vrstaID]
}

// PotrebeU vraća dodatne potrebe vrste u skladištu
func (t TablicaSredstava) PotrebeU(skladisteID, vrstaID string) float64 {
	return t.potrebe[skladisteID][vrstaID]
}

// UkupnoStanje zbraja stanje vrste kroz sva skladišta
func (t TablicaSredstava) UkupnoStanje(vrstaID string) float64 {
	var s float64
	for _, sk := range t.Skladista {
		s += t.stanje[sk.ID][vrstaID]
	}
	return s
}

// UkupnoPotrebe zbraja potrebe vrste kroz sva skladišta
func (t TablicaSredstava) UkupnoPotrebe(vrstaID string) float64 {
	var s float64
	for _, sk := range t.Skladista {
		s += t.potrebe[sk.ID][vrstaID]
	}
	return s
}

// Izvor kaže odakle je stupac skladišta: zaključen popis, nacrt popisa ili knjiga
func (t TablicaSredstava) Izvor(skladisteID string) string {
	p, ok := t.Popisi[skladisteID]
	switch {
	case !ok:
		return "knjižno stanje"
	case p.Zakljucen():
		return "popis zaključen"
	}
	return "popis u nacrtu"
}

// Tablica slaže popis sredstava sektora na dan
func (s *MtsService) Tablica(ctx context.Context, sektor string, dan time.Time) (*TablicaSredstava, error) {
	dan = pocetakDana(dan)
	skladista, err := s.repo.ListSkladista(ctx, sektor, 0, false)
	if err != nil {
		return nil, err
	}
	vrste, err := s.repo.ListVrste(ctx, false)
	if err != nil {
		return nil, err
	}
	t := &TablicaSredstava{Sektor: sektor, Dan: dan, Skladista: skladista, Vrste: vrste,
		stanje: map[string]map[string]float64{}, potrebe: map[string]map[string]float64{}, Popisi: map[string]*models.Popis{}}
	for _, sk := range skladista {
		t.stanje[sk.ID], t.potrebe[sk.ID] = map[string]float64{}, map[string]float64{}
		p, err := s.repo.PopisZaDan(ctx, sk.ID, dan)
		if err != nil {
			return nil, err
		}
		if p != nil {
			t.Popisi[sk.ID] = p
			for _, st := range p.Stavke {
				t.stanje[sk.ID][st.VrstaID] += st.Utvrdjeno
				t.potrebe[sk.ID][st.VrstaID] += st.Potrebno
			}
			continue
		}
		stanja, err := s.repo.Stanje(ctx, sk.ID, "", &dan)
		if err != nil {
			return nil, err
		}
		for _, st := range stanja {
			t.stanje[sk.ID][st.VrstaID] += st.Kolicina
		}
	}
	return t, nil
}

// ObrisiVrstu miče vrstu iz kataloga, ali samo nekorištenu: na koju ne
// pokazuje nijedan redak prometa ni popis s količinom. Korištena se gasi.
func (s *MtsService) ObrisiVrstu(ctx context.Context, id string) error {
	v, err := s.repo.GetVrsta(ctx, id)
	if err != nil {
		return err
	}
	if v == nil {
		return errors.New("vrsta nije pronađena")
	}
	prometa, popisa, err := s.repo.UpotrebaVrste(ctx, id)
	if err != nil {
		return err
	}
	if prometa > 0 || popisa > 0 {
		return fmt.Errorf("vrsta „%s“ se ne može ukloniti: na nju pokazuje %d redaka prometa i %d popisa — ugasite je umjesto toga", v.Naziv, prometa, popisa)
	}
	return s.repo.ArhivirajVrstu(ctx, v)
}
