package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"gocop/internal/models"
)

// Popis primatelja akta, „O tome obavijest” (Odluke (8. 10. 2026.), pitanje 19).
// Slaže se iz više registara. Izvor koji se ne da pročitati i dionica bez
// upisanog teritorija ne preskaču se šutke: nacrt se sastavi, ali s
// upozorenjem, a ovjera dok upozorenje stoji traži izričitu potvrdu. Pri
// ovjeri se popis slaže ponovno i razlika prema nacrtu pokaže se prije
// potvrde. Firme i rukovoditelji idu za sva branjena područja čije su
// dionice u aktu, a ne samo za područje akta.

// slaganjePrimatelja skuplja primatelje po skupinama, bez ponavljanja, i
// upozorenja o izvorima
type slaganjePrimatelja struct {
	popis      []models.AktPrimatelj
	vidjeno    map[string]bool
	upozorenja []string
}

func (p *slaganjePrimatelja) dodaj(skupina, naziv, email string) {
	naziv = strings.TrimSpace(naziv)
	if naziv == "" {
		return
	}
	kljuc := strings.ToLower(naziv + "|" + email)
	if p.vidjeno[kljuc] {
		return
	}
	p.vidjeno[kljuc] = true
	p.popis = append(p.popis, models.AktPrimatelj{Naziv: naziv, Email: strings.TrimSpace(email), Skupina: skupina})
}

func (p *slaganjePrimatelja) upozori(format string, args ...any) {
	p.upozorenja = append(p.upozorenja, fmt.Sprintf(format, args...))
}

// dionicaPrimatelja je dionica akta s područjem i sektorom iz registra
type dionicaPrimatelja struct {
	code, sektor string
	podrucje     int
}

// teritorijPrimatelja je ugroženo područje dionica akta: županije redom
// kojim se javljaju i općine
type teritorijPrimatelja struct {
	zupanije map[int]string
	redom    []int
	ugrozene map[int]bool
	opcine   []models.SectionTerritory
}

func (t *teritorijPrimatelja) dodaj(ter []models.SectionTerritory) {
	for _, x := range ter {
		if _, ok := t.zupanije[x.CountyID]; !ok {
			t.redom = append(t.redom, x.CountyID)
		}
		t.zupanije[x.CountyID] = x.CountyName
		if !t.ugrozene[x.MunicipalityID] {
			t.opcine = append(t.opcine, x)
		}
		t.ugrozene[x.MunicipalityID] = true
	}
}

// sloziPrimatelje slaže popis primatelja akta i upozorenja o onome što
// nije pročitano
func (s *AktService) sloziPrimatelje(ctx context.Context, a *models.Akt) ([]models.AktPrimatelj, []string) {
	p := &slaganjePrimatelja{vidjeno: map[string]bool{}}
	dionice := s.dioniceSPodrucjem(a, p)
	ter := s.teritorijDionica(ctx, a, p)
	s.dodajSluzbe(ctx, a.Stupanj, ter, p)
	s.dodajRegistar(ctx, a, p)
	for _, podrucje := range podrucjaPrimatelja(a.AreaID, dionice) {
		s.dodajFirme(ctx, podrucje, p)
	}
	if a.Stupanj == models.PhaseState {
		s.dodajSamoupravu(ctx, ter, p)
	}
	s.dodajOsobe(dionice, p)
	p.dodaj(models.SkupinaPismohrana, "Pismohrana", "")
	sort.SliceStable(p.popis, func(i, j int) bool { return redSkupine(p.popis[i].Skupina) < redSkupine(p.popis[j].Skupina) })
	return p.popis, p.upozorenja
}

// dioniceSPodrucjem su dionice akta s područjem i sektorom iz registra.
// Dionica koja se ne da pročitati ili je nema u registru ostaje uz područje
// akta, s upozorenjem.
func (s *AktService) dioniceSPodrucjem(a *models.Akt, p *slaganjePrimatelja) []dionicaPrimatelja {
	out := make([]dionicaPrimatelja, 0, len(a.Dionice))
	for _, d := range a.Dionice {
		x := dionicaPrimatelja{code: d.Code, sektor: a.Sektor, podrucje: a.AreaID}
		sec, err := s.sections.GetSectionByCode(d.Code)
		switch {
		case err != nil:
			p.upozori("dionica %s nije pročitana iz registra dionica: firme i rukovoditelji uzeti su za branjeno područje akta", d.Code)
		case sec == nil:
			p.upozori("dionice %s nema u registru dionica: firme i rukovoditelji uzeti su za branjeno područje akta", d.Code)
		default:
			x.sektor, x.podrucje = sec.SectorID, sec.AreaID
		}
		out = append(out, x)
	}
	return out
}

// podrucjaPrimatelja su branjena područja dionica akta: područje akta
// prvo, ostala redom kojim se javljaju, svako jednom
func podrucjaPrimatelja(podrucjeAkta int, dionice []dionicaPrimatelja) []int {
	out := []int{podrucjeAkta}
	for _, d := range dionice {
		if !sadrziBroj(out, d.podrucje) {
			out = append(out, d.podrucje)
		}
	}
	return out
}

func sadrziBroj(xs []int, x int) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// teritorijDionica je ugroženo područje dionica akta; dionica čiji se
// teritorij ne da pročitati ili ga nema upisanog ostaje bez službi
// županije, s upozorenjem
func (s *AktService) teritorijDionica(ctx context.Context, a *models.Akt, p *slaganjePrimatelja) teritorijPrimatelja {
	t := teritorijPrimatelja{zupanije: map[int]string{}, ugrozene: map[int]bool{}}
	if s.territories == nil {
		return t
	}
	var bez []string
	for _, d := range a.Dionice {
		ter, err := s.territories.GetSectionTerritories(ctx, d.Code)
		switch {
		case err != nil:
			p.upozori("teritorij dionice %s nije pročitan: nema službi njezine županije", d.Code)
		case len(ter) == 0:
			bez = append(bez, d.Code)
		default:
			t.dodaj(ter)
		}
	}
	if len(bez) > 0 {
		p.upozori("dionice bez upisanog teritorija (naselja i općine): %s; nema službi njihovih županija", strings.Join(bez, ", "))
	}
	return t
}

// dodajSluzbe dodaje službe županija ugroženog područja, za svaki stupanj:
// civilnu zaštitu s prevencijom i 112 kao podstavkama, policiju, lučke
// kapetanije
func (s *AktService) dodajSluzbe(ctx context.Context, stupanj models.DefensePhase, t teritorijPrimatelja, p *slaganjePrimatelja) {
	for _, id := range t.redom {
		sluzbe, err := s.territories.ListSluzbe(ctx, id)
		if err != nil {
			p.upozori("službe županije %s nisu pročitane", t.zupanije[id])
			continue
		}
		dodajSluzbeZupanije(sluzbe, stupanj, t.ugrozene, p)
	}
}

// dodajSluzbeZupanije dodaje službe jedne županije koje idu na akt tog
// stupnja; služba uz općinu ide samo kad je općina ugrožena
func dodajSluzbeZupanije(sluzbe []models.Sluzba, stupanj models.DefensePhase, ugrozene map[int]bool, p *slaganjePrimatelja) {
	sort.SliceStable(sluzbe, func(i, j int) bool {
		if models.RedVrste(sluzbe[i].Vrsta) != models.RedVrste(sluzbe[j].Vrsta) {
			return models.RedVrste(sluzbe[i].Vrsta) < models.RedVrste(sluzbe[j].Vrsta)
		}
		return sluzbe[i].Redoslijed < sluzbe[j].Redoslijed
	})
	imaCZ := false
	for _, x := range sluzbe {
		if (x.MunicipalityID != 0 && !ugrozene[x.MunicipalityID]) || !models.NaAktu(x.Vrsta, stupanj) {
			continue
		}
		naziv := x.Naziv
		imaCZ = imaCZ || x.Vrsta == models.SluzbaCivilnaZastita
		if imaCZ && models.PodCivilnomZastitom(x.Vrsta) {
			naziv = "– " + naziv
		}
		p.dodaj(models.SkupinaSluzbe, naziv, x.Email)
	}
}

// dodajRegistar dodaje primatelje iz registra sektora: za cijeli sektor ili
// za branjeno područje akta, od zadanog stupnja
func (s *AktService) dodajRegistar(ctx context.Context, a *models.Akt, p *slaganjePrimatelja) {
	reg, err := s.repo.ListPrimatelji(ctx, a.Sektor)
	if err != nil {
		p.upozori("registar primatelja sektora %s nije pročitan", a.Sektor)
		return
	}
	sort.SliceStable(reg, func(i, j int) bool {
		if reg[i].Skupina != reg[j].Skupina {
			return redSkupine(reg[i].Skupina) < redSkupine(reg[j].Skupina)
		}
		return reg[i].Redoslijed < reg[j].Redoslijed
	})
	for _, x := range reg {
		if x.Vrijedi(a.AreaID, a.Stupanj) {
			p.dodaj(x.Skupina, x.Naziv, x.Email)
		}
	}
}

// dodajFirme dodaje licencirane pravne osobe za obranu na branjenom
// području, iz registra firmi; kad ih registar nema ili se ne da pročitati,
// ugovornu osobu upisanu uz područje
func (s *AktService) dodajFirme(ctx context.Context, podrucje int, p *slaganjePrimatelja) {
	firme, err := s.repo.UgovorneFirme(ctx, podrucje)
	if err != nil {
		p.upozori("registar licenciranih firmi za branjeno područje %d nije pročitan", podrucje)
	}
	for _, f := range firme {
		p.dodaj(models.SkupinaIspostava, f.Naziv, f.Email)
	}
	if len(firme) == 0 {
		s.dodajUgovornuOsobu(podrucje, p)
	}
}

// dodajUgovornuOsobu dodaje ugovornu osobu upisanu uz branjeno područje
func (s *AktService) dodajUgovornuOsobu(podrucje int, p *slaganjePrimatelja) {
	podrucja, err := s.users.ListAreas("")
	if err != nil {
		p.upozori("branjena područja nisu pročitana: nema ugovorne osobe područja %d", podrucje)
		return
	}
	for _, ar := range podrucja {
		if ar.ID == podrucje {
			p.dodaj(models.SkupinaIspostava, ar.ContractorName, "")
		}
	}
}

// dodajSamoupravu dodaje, od izvanrednog stanja, župane, gradove i općine
// ugroženog područja, s e-poštom iz registra
func (s *AktService) dodajSamoupravu(ctx context.Context, t teritorijPrimatelja, p *slaganjePrimatelja) {
	if s.territories == nil {
		return
	}
	emailOpcine := map[int]string{}
	for _, id := range t.redom {
		naziv, email := t.zupanije[id], ""
		if c, err := s.territories.GetCountyByID(ctx, id); err != nil {
			p.upozori("županija %s nije pročitana: župan je na popisu bez e-pošte", naziv)
		} else if c != nil {
			naziv, email = c.Name, c.Email
		}
		p.dodaj(models.SkupinaSamouprava, "Župan, "+naziv, email)
		s.emailOpcina(ctx, id, t.zupanije[id], emailOpcine, p)
	}
	opcine := append([]models.SectionTerritory(nil), t.opcine...)
	sort.SliceStable(opcine, func(i, j int) bool { return opcine[i].MunicipalityName < opcine[j].MunicipalityName })
	for _, o := range opcine {
		vrsta := "Općina"
		if strings.EqualFold(o.MunicipalityType, "GRAD") {
			vrsta = "Grad"
		}
		p.dodaj(models.SkupinaSamouprava, vrsta+" "+o.MunicipalityName, emailOpcine[o.MunicipalityID])
	}
}

// emailOpcina upisuje e-poštu općina županije, jednim upitom
func (s *AktService) emailOpcina(ctx context.Context, zupanija int, naziv string, email map[int]string, p *slaganjePrimatelja) {
	ms, err := s.territories.ListMunicipalities(ctx, zupanija, "", "")
	if err != nil {
		p.upozori("općine županije %s nisu pročitane: gradovi i općine su na popisu bez e-pošte", naziv)
		return
	}
	for _, m := range ms {
		email[m.ID] = m.Email
	}
}

// osobaPrimatelj je osoba na popisu sa svim svojim funkcijama
type osobaPrimatelj struct {
	naziv, email string
	funkcije     []string
}

// dodajOsobe dodaje rukovoditelje i zamjenike branjenog područja (razina 3)
// i dionica (razina 4) svake dionice akta, za područje te dionice; svaka
// osoba jednom, sa svim svojim funkcijama. Sektor i Glavni centar primaju
// akt kao ustanove, pa njihovi ljudi ne idu poimence, kao ni teren.
func (s *AktService) dodajOsobe(dionice []dionicaPrimatelja, p *slaganjePrimatelja) {
	var redom []string
	osobe := map[string]*osobaPrimatelj{}
	for _, d := range dionice {
		lista, err := s.sections.GetSectionPersonnel(d.code, d.podrucje, d.sektor)
		if err != nil {
			p.upozori("rukovoditelji dionice %s nisu pročitani", d.code)
			continue
		}
		for _, o := range lista {
			if o.Rank != 3 && o.Rank != 4 {
				continue
			}
			if osobe[o.UserID] == nil {
				osobe[o.UserID] = noviPrimateljOsoba(o)
				redom = append(redom, o.UserID)
			}
			osobe[o.UserID].dodajFunkciju(o)
		}
	}
	for _, id := range redom {
		p.dodaj(models.SkupinaOsobe, osobe[id].naslov(), osobe[id].email)
	}
}

func noviPrimateljOsoba(o models.SectionOfficer) *osobaPrimatelj {
	naziv := o.FullName
	if o.Title != "" {
		naziv += ", " + o.Title
	}
	return &osobaPrimatelj{naziv: naziv, email: o.Email}
}

func (x *osobaPrimatelj) dodajFunkciju(o models.SectionOfficer) {
	f := o.DutyTitle
	if f == "" {
		f = o.RoleLabel
	}
	if f != "" && !sadrzi(x.funkcije, f) {
		x.funkcije = append(x.funkcije, f)
	}
}

func (x *osobaPrimatelj) naslov() string {
	if len(x.funkcije) == 0 {
		return x.naziv
	}
	return x.naziv + ", " + strings.Join(x.funkcije, "; ")
}

// PregledPrimatelja je popis primatelja složen iznova, kakav bi akt dobio
// ovjerom, s razlikom prema popisu u aktu i upozorenjima
type PregledPrimatelja struct {
	Primatelji []models.AktPrimatelj
	Upozorenja []string
	Dodani     []models.AktPrimatelj // ima ih novi popis, a akt nema
	Uklonjeni  []models.AktPrimatelj // ima ih akt, a novi popis nema
	// Otisak je otisak novog popisa i upozorenja: ovjera ga uspoređuje s
	// onim što je osoba vidjela
	Otisak string
}

// Razlikuje javlja razlikuje li se novi popis od popisa u aktu
func (p PregledPrimatelja) Razlikuje() bool { return len(p.Dodani)+len(p.Uklonjeni) > 0 }

// OpisRazlike je razlika u jednoj rečenici, za upozorenje i potvrdu
func (p PregledPrimatelja) OpisRazlike() string {
	return fmt.Sprintf("dodano %d, uklonjeno %d", len(p.Dodani), len(p.Uklonjeni))
}

// PregledPrimatelja slaže popis primatelja akta iznova i uspoređuje ga s
// popisom u aktu
func (s *AktService) PregledPrimatelja(ctx context.Context, a *models.Akt) PregledPrimatelja {
	novi, upozorenja := s.sloziPrimatelje(ctx, a)
	return PregledPrimatelja{Primatelji: novi, Upozorenja: upozorenja,
		Dodani: bezPrimatelja(novi, a.Primatelji), Uklonjeni: bezPrimatelja(a.Primatelji, novi), Otisak: otisakPrimatelja(novi, upozorenja)}
}

// bezPrimatelja su primatelji iz prvog popisa kojih u drugom nema
func bezPrimatelja(prvi, drugi []models.AktPrimatelj) []models.AktPrimatelj {
	ima := map[string]bool{}
	for _, x := range drugi {
		ima[x.Naziv+"<"+x.Email+">"] = true
	}
	var out []models.AktPrimatelj
	for _, x := range prvi {
		if !ima[x.Naziv+"<"+x.Email+">"] {
			out = append(out, x)
		}
	}
	return out
}

// otisakPrimatelja je otisak popisa primatelja s upozorenjima, redom
func otisakPrimatelja(popis []models.AktPrimatelj, upozorenja []string) string {
	h := sha256.New()
	for _, x := range popis {
		_, _ = fmt.Fprintf(h, "%s<%s>%s\n", x.Naziv, x.Email, x.Skupina)
	}
	for _, u := range upozorenja {
		_, _ = fmt.Fprintf(h, "! %s\n", u)
	}
	return hex.EncodeToString(h.Sum(nil)[:12])
}

// PonovnoSloziPrimatelje slaže popis primatelja nacrta iznova, s
// upozorenjima; smije tko smije raditi s nacrtom
func (s *AktService) PonovnoSloziPrimatelje(ctx context.Context, perms *models.UserPermissions, u *models.User, id string) (*models.Akt, error) {
	a, err := s.repo.GetAkt(ctx, id)
	switch {
	case err != nil:
		return nil, err
	case a == nil:
		return nil, fmt.Errorf("akt ne postoji")
	case a.Ovjeren():
		return nil, fmt.Errorf("akt %s je ovjeren: popis primatelja ostaje onaj s kojim je ovjeren", a.Oznaka())
	case !s.smijePripremiti(perms, u, a):
		return nil, ErrUnauthorized
	}
	a.Primatelji, a.UpozorenjaPrimatelja = s.sloziPrimatelje(ctx, a)
	// slaganje traje; nacrt ovjeren u međuvremenu ostaje ovjeren (Odluke
	// (8. 10. 2026.), pitanje 4)
	return a, s.repo.SaveNacrt(ctx, a)
}

// PotvrdaOvjere je ono što je osoba izričito potvrdila pri ovjeri
type PotvrdaOvjere struct {
	// Otisak je otisak popisa primatelja koji je osoba vidjela prije ovjere
	// (PregledPrimatelja); kad se popis u međuvremenu promijenio, ovjera se
	// ne izvodi. Prazno: pozivatelj popis nije prikazao, pa se ne uspoređuje.
	Otisak string
	// Primatelji: osoba potvrđuje ovjeru dok stoji upozorenje o popisu
	// primatelja
	Primatelji bool
	// Vrijedi: osoba potvrđuje „Vrijedi od” više od 24 sata unatrag ili više
	// od 3 dana unaprijed kad nacrt potvrdu još ne nosi
	// (PitanjeVrijediZaOvjeru; Odluke (8. 10. 2026.), pitanje 17)
	Vrijedi bool
}

// primateljiZaOvjeru slaže popis primatelja iznova za ovjeru u programu: akt
// se ovjerava s novim popisom, a dok stoji upozorenje, samo uz potvrdu
func primateljiZaOvjeru(u *models.User, a *models.Akt, p PotvrdaOvjere, pr PregledPrimatelja) error {
	if p.Otisak != "" && p.Otisak != pr.Otisak {
		return fmt.Errorf("popis primatelja ili upozorenje uz njega promijenili su se otkad je stranica otvorena; pregledajte ih pod Primatelji pri ovjeri i ovjerite ponovno")
	}
	if err := potvrdiPrimatelje(a, u, pr.Upozorenja, p.Primatelji, "Popis primatelja nije potpun: ", "Ovjereno uz upozorenje o popisu primatelja: "); err != nil {
		return err
	}
	a.Primatelji, a.UpozorenjaPrimatelja = pr.Primatelji, pr.Upozorenja
	return nil
}

// primateljiSkena: sken nosi popis primatelja s ispisa nacrta, pa se popis
// ne mijenja; potvrda treba dok stoji upozorenje nacrta ili kad bi popis
// složen sada bio drukčiji
func primateljiSkena(u *models.User, a *models.Akt, p PotvrdaOvjere, pr PregledPrimatelja) error {
	razlozi := append([]string(nil), a.UpozorenjaPrimatelja...)
	pitanje := "Popis primatelja s ispisa nije potpun: "
	if len(razlozi) == 0 {
		pitanje = "Sken nosi popis primatelja s ispisa: "
	}
	if pr.Razlikuje() {
		razlozi = append(razlozi, "popis složen sada bio bi drukčiji od onoga na ispisu ("+pr.OpisRazlike()+")")
	}
	return potvrdiPrimatelje(a, u, razlozi, p.Primatelji, pitanje, "Sken ovjeren s popisom primatelja s ispisa, uz upozorenje: ")
}

// potvrdiPrimatelje: dok stoje upozorenja, ovjera prolazi samo uz izričitu
// potvrdu, koja se bilježi uz akt. pitanje uvodi upozorenja u pitanju, a
// uvod u zabilježenoj potvrdi.
func potvrdiPrimatelje(a *models.Akt, u *models.User, upozorenja []string, potvrdeno bool, pitanje, uvod string) error {
	if len(upozorenja) == 0 {
		return nil
	}
	tekst := strings.Join(upozorenja, "; ")
	if !potvrdeno {
		return &TrebaPotvrduError{Pitanje: pitanje + tekst + ". Ovjera traži izričitu potvrdu."}
	}
	a.Potvrde = append(a.Potvrde, models.PotvrdaAkta{Vrsta: models.PotvrdaPrimatelji, Tekst: uvod + tekst,
		TkoID: u.ID.String(), Tko: u.FullName, Kad: time.Now().UTC().Truncate(time.Second)})
	return nil
}
