package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Sektorsko dnevno izvješće slaže voditelj COP-a: iz predanih izvješća
// dionica za taj dan program zbroji ljude, strojeve i stanje po branjenim
// područjima i predloži tekst; iz dnevnika COP-a preuzme zapise toga dana.
// Voditelj dopiše hidrometeorološke uvjete i ocjenu, odabere što ulazi, i
// preda. Tablice se snimaju u izvješće, pa dokument stoji i kad se izvješće
// dionice poslije popravi.

// SetSektorska daje servisu spremište sektorskih izvješća
func (s *IzvjescaService) SetSektorska(r *repository.SektorskaIzvjescaRepository) { s.sektorska = r }

// UpravaSektora: tko upravlja sektorom (voditelj COP-a, rukovoditelj sektora, admin)
func (s *IzvjescaService) UpravaSektora(perms *models.UserPermissions, sektor string) bool {
	return perms != nil && sektor != "" && perms.CanAdminister(sektor, 0)
}

// SmijeVidjetiSektor: uprava sektora i svi koji rade u sektoru
func (s *IzvjescaService) SmijeVidjetiSektor(perms *models.UserPermissions, sektor string) bool {
	if perms == nil || sektor == "" {
		return false
	}
	if s.UpravaSektora(perms, sektor) || perms.AllowedSectors[sektor] {
		return true
	}
	for code := range perms.AllowedSections {
		if strings.HasPrefix(code, sektor+".") {
			return true
		}
	}
	if s.sections != nil && (len(perms.AllowedAreas) > 0 || len(perms.AdminAreas) > 0) {
		if sve, err := s.sections.ListSections(sektor, 0, ""); err == nil {
			for _, sec := range sve {
				if perms.AllowedAreas[sec.AreaID] || perms.AdminAreas[sec.AreaID] {
					return true
				}
			}
		}
	}
	return false
}

// SektoriZaSastavljanje vraća sektore za koje osoba smije složiti izvješće,
// po redu oznaka — iz registra dionica, jer bez dionica nema ni izvješća
func (s *IzvjescaService) SektoriZaSastavljanje(perms *models.UserPermissions) []string {
	if s.sections == nil || perms == nil {
		return nil
	}
	sve, err := s.sections.ListSections("", 0, "")
	if err != nil {
		return nil
	}
	vidjeno := map[string]bool{}
	var out []string
	for _, sec := range sve {
		if !vidjeno[sec.SectorID] && s.UpravaSektora(perms, sec.SectorID) {
			vidjeno[sec.SectorID] = true
			out = append(out, sec.SectorID)
		}
	}
	sort.Strings(out)
	return out
}

// IzvjescaDana vraća sva izvješća dionica sektora za dan, nacrte uključivo
func (s *IzvjescaService) IzvjescaDana(ctx context.Context, sektor string, dan time.Time) ([]models.DnevnoIzvjesce, error) {
	dan = pocetakDana(dan)
	return s.repo.List(ctx, "", "", sektor, &dan)
}

// PregledSektora zbraja izabrana izvješća dionica po branjenim područjima i
// vodotocima. Ulaze samo izvješća čiji je ID u popisu (nil = sva predana).
func (s *IzvjescaService) PregledSektora(sva []models.DnevnoIzvjesce, ukljuci map[string]bool) models.SektorskiPregled {
	var p models.SektorskiPregled
	poPodrucju := map[int]*models.PodrucjeUPregledu{}
	poVodi := map[string]*models.VodotokUPregledu{}
	var redPodrucja []int
	var redVoda []string
	for _, iz := range sva {
		if ukljuci == nil && !iz.Predano() {
			p.Nacrta++
			continue
		}
		if ukljuci != nil && !ukljuci[iz.ID] {
			if !iz.Predano() {
				p.Nacrta++
			}
			continue
		}
		p.Izvjesca++
		p.Ukupno.Dodaj(iz.Sadrzaj)
		areaID := areaIzSifre(iz.SectionCode)
		pod, ok := poPodrucju[areaID]
		if !ok {
			pod = &models.PodrucjeUPregledu{AreaID: areaID, Naziv: iz.Podrucje}
			poPodrucju[areaID] = pod
			redPodrucja = append(redPodrucja, areaID)
		}
		if pod.Naziv == "" {
			pod.Naziv = iz.Podrucje
		}
		pod.Zbroj.Dodaj(iz.Sadrzaj)
		sad := iz.Sadrzaj
		pod.Dionice = append(pod.Dionice, models.DionicaUPregledu{IzvjesceID: iz.ID, Code: iz.SectionCode, Vodotok: sad.Vodotok, Stadij: iz.Stadij,
			Vodostaji: vodostajiTekst(sad.Vodostaji), Tendencija: sad.Tendencija, Vrece: sad.Vrece, Materijal: sad.Materijal, Nasipi: sad.Nasipi, Crpke: sad.Crpke, Izradio: iz.Izradio})
		for _, voda := range strings.Split(sad.Vodotok, ",") {
			voda = strings.TrimSpace(voda)
			if voda == "" {
				continue
			}
			v, ok := poVodi[voda]
			if !ok {
				v = &models.VodotokUPregledu{Vodotok: voda}
				poVodi[voda] = v
				redVoda = append(redVoda, voda)
			}
			v.Dionice = append(v.Dionice, iz.SectionCode)
			if iz.Stadij.Severity() > v.Stadij.Severity() || v.Tendencija == "" {
				v.Stadij, v.Tendencija = iz.Stadij, sad.Tendencija
			}
			if t := vodostajiTekst(sad.Vodostaji); t != "" && !strings.Contains(v.Vodostaji, t) {
				v.Vodostaji = spojiTekst(v.Vodostaji, t, "; ")
			}
		}
	}
	sort.Ints(redPodrucja)
	for _, id := range redPodrucja {
		pod := poPodrucju[id]
		sort.Slice(pod.Dionice, func(a, b int) bool { return pod.Dionice[a].Code < pod.Dionice[b].Code })
		p.Podrucja = append(p.Podrucja, *pod)
	}
	for _, voda := range redVoda {
		p.Vodotoci = append(p.Vodotoci, *poVodi[voda])
	}
	return p
}

// areaIzSifre čita broj branjenog područja iz šifre dionice "B.16.3" → 16
func areaIzSifre(code string) int {
	d := strings.Split(code, ".")
	if len(d) < 2 {
		return 0
	}
	n := 0
	for _, r := range d[1] {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func vodostajiTekst(v []models.VodostajUIzvjescu) string {
	var dijelovi []string
	for _, x := range v {
		if x.Vrijednost == "" {
			continue
		}
		dijelovi = append(dijelovi, strings.TrimSpace(x.Postaja+" "+x.Sat+" "+x.Vrijednost+" "+x.Jedinica))
	}
	return strings.Join(dijelovi, "; ")
}

func spojiTekst(a, b, sep string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + sep + b
}

// PrijedlogTekstova slaže zbirne tekstove iz izvješća dionica, po
// branjenim područjima: voditelj ih onda skrati ili dopuni
func (s *IzvjescaService) PrijedlogTekstova(sva []models.DnevnoIzvjesce, ukljuci map[string]bool, pregled models.SektorskiPregled) (ostecenja, mjere, objekti, poplavljeno, evakuacija string) {
	poID := map[string]models.DnevnoIzvjesce{}
	for _, iz := range sva {
		poID[iz.ID] = iz
	}
	var o, m, ob, pp, ev []string
	for _, pod := range pregled.Podrucja {
		naziv := pod.Naziv
		if naziv == "" {
			naziv = fmt.Sprintf("BP %d", pod.AreaID)
		}
		for _, d := range pod.Dionice {
			iz, ok := poID[d.IzvjesceID]
			if !ok {
				continue
			}
			sad := iz.Sadrzaj
			pref := naziv + " — " + d.Code
			if d.Vodotok != "" {
				pref += " (" + d.Vodotok + ")"
			}
			if t := strings.TrimSpace(sad.Pregled); t != "" {
				o = append(o, pref+": "+t)
			}
			if t := strings.TrimSpace(sad.Radnje); t != "" {
				var kol []string
				for _, k := range []struct{ n, v string }{{"vreće", sad.Vrece}, {"materijal", sad.Materijal}, {"nasipi", sad.Nasipi}, {"crpke", sad.Crpke}} {
					if k.v != "" {
						kol = append(kol, k.n+" "+k.v)
					}
				}
				if len(kol) > 0 {
					t += " [" + strings.Join(kol, ", ") + "]"
				}
				m = append(m, pref+": "+t)
			}
			if t := strings.TrimSpace(sad.Objekti); t != "" {
				ob = append(ob, pref+": "+t)
			}
			if p := sad.Poplavljeno; p != (models.Poplavljeno{}) {
				var dij []string
				if p.Naselja != "" {
					dij = append(dij, p.Naselja)
				}
				if p.Ljudi > 0 {
					dij = append(dij, fmt.Sprintf("%d ljudi", p.Ljudi))
				}
				if p.Stambeni > 0 {
					dij = append(dij, fmt.Sprintf("%d stambenih", p.Stambeni))
				}
				if p.Industrijski > 0 {
					dij = append(dij, fmt.Sprintf("%d industrijskih", p.Industrijski))
				}
				if p.Farme > 0 {
					dij = append(dij, fmt.Sprintf("%d farmi", p.Farme))
				}
				if p.Infrastruktura != "" {
					dij = append(dij, p.Infrastruktura)
				}
				if p.PoljoprivredneHa > 0 {
					dij = append(dij, fmt.Sprintf("%s ha poljoprivrednih", broj1(p.PoljoprivredneHa)))
				}
				if p.SumskeHa > 0 {
					dij = append(dij, fmt.Sprintf("%s ha šumskih", broj1(p.SumskeHa)))
				}
				if p.OstaleHa > 0 {
					dij = append(dij, fmt.Sprintf("%s ha ostalih", broj1(p.OstaleHa)))
				}
				pp = append(pp, pref+": "+strings.Join(dij, ", "))
			}
			if e := sad.Evakuacija; e != (models.Evakuacija{}) {
				var dij []string
				if e.Naselja != "" {
					dij = append(dij, e.Naselja)
				}
				if e.Ljudi > 0 {
					dij = append(dij, fmt.Sprintf("%d ljudi", e.Ljudi))
				}
				if e.Kucanstava > 0 {
					dij = append(dij, fmt.Sprintf("%d kućanstava", e.Kucanstava))
				}
				if e.Zivotinje != "" {
					dij = append(dij, e.Zivotinje)
				}
				ev = append(ev, pref+": "+strings.Join(dij, ", "))
			}
		}
	}
	return strings.Join(o, "\n"), strings.Join(m, "\n"), strings.Join(ob, "\n"), strings.Join(pp, "\n"), strings.Join(ev, "\n")
}

func broj1(v float64) string {
	return strings.Replace(strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%.1f", v), "0"), "."), ".", ",", 1)
}

// ZapisiDana vraća zapise dnevnika COP-a za dan koji mogu u izvješće:
// dojave, obavijesti i napomene, bez storniranih i bez dežurstava
func (s *IzvjescaService) ZapisiDana(ctx context.Context, journalID string, dan time.Time) ([]models.ZapisUIzvjescu, error) {
	if s.journals == nil || journalID == "" {
		return nil, nil
	}
	zapisi, err := s.journals.EntriesForJournal(ctx, journalID)
	if err != nil {
		return nil, err
	}
	nazivi := map[int]string{}
	if s.sections != nil {
		if sve, err := s.sections.ListSections("", 0, ""); err == nil {
			for _, sec := range sve {
				nazivi[sec.AreaID] = sec.AreaName
			}
		}
	}
	kljuc := pocetakDana(dan).Format("2006-01-02")
	var out []models.ZapisUIzvjescu
	for _, e := range zapisi {
		if e.Voided || e.Kind == models.EntryKindDuty || e.Date.In(models.Zagreb).Format("2006-01-02") != kljuc {
			continue
		}
		z := models.ZapisUIzvjescu{ID: e.ID, Vrsta: e.KindLabel(), Javio: e.ReportedBy, Tekst: e.Text}
		if e.HappenedAt != nil {
			z.Vrijeme = e.HappenedAt.In(models.Zagreb).Format("15:04")
		}
		if z.Javio == "" {
			z.Javio = e.UserName
		}
		if e.Podrucje != nil {
			z.Podrucje = nazivi[*e.Podrucje]
			if z.Podrucje == "" {
				z.Podrucje = fmt.Sprintf("BP %d", *e.Podrucje)
			}
		} else {
			z.Podrucje = "cijeli sektor"
		}
		if e.Place != "" {
			z.Tekst = e.Place + ": " + z.Tekst
		}
		out = append(out, z)
	}
	return out, nil
}

// otvoreniDnevnik vraća otvoren dnevnik COP-a sektora (ne prijepis), ili ""
func (s *IzvjescaService) otvoreniDnevnik(ctx context.Context, sektor string) string {
	if s.journals == nil {
		return ""
	}
	if dnevnici, err := s.journals.ListCOPJournals(ctx, sektor); err == nil {
		for _, j := range dnevnici {
			if j.EndedAt == nil && !j.Reconstruction {
				return j.ID
			}
		}
	}
	return ""
}

// PredlozakSektora vraća postojeće izvješće sektora za dan, ili novo s
// predloženim tekstom iz predanih izvješća dionica i svim zapisima dana
func (s *IzvjescaService) PredlozakSektora(ctx context.Context, sektor string, dan time.Time) (*models.SektorskoIzvjesce, error) {
	if s.sektorska == nil {
		return nil, errors.New("sektorska izvješća nisu uključena")
	}
	dan = pocetakDana(dan)
	if postojece, err := s.sektorska.ZaDan(ctx, sektor, dan); err != nil {
		return nil, err
	} else if postojece != nil {
		return postojece, nil
	}
	iz := &models.SektorskoIzvjesce{Sektor: sektor, Dan: dan, JournalID: s.otvoreniDnevnik(ctx, sektor)}
	sva, err := s.IzvjescaDana(ctx, sektor, dan)
	if err != nil {
		return nil, err
	}
	iz.Sadrzaj.Pregled = s.PregledSektora(sva, nil)
	iz.Sadrzaj.Ostecenja, iz.Sadrzaj.Mjere, iz.Sadrzaj.Objekti, iz.Sadrzaj.Poplavljeno, iz.Sadrzaj.Evakuacija = s.PrijedlogTekstova(sva, nil, iz.Sadrzaj.Pregled)
	iz.Sadrzaj.Zapisi, _ = s.ZapisiDana(ctx, iz.JournalID, dan)
	return iz, nil
}

// SpremiSektorsko upisuje izvješće sektora: ponovno zbroji izabrana
// izvješća dionica (po ID-u) i preuzme izabrane zapise, tekst ostaje
// voditeljev. Jedno po sektoru i danu, ne unaprijed.
func (s *IzvjescaService) SpremiSektorsko(ctx context.Context, u *models.User, perms *models.UserPermissions, iz *models.SektorskoIzvjesce, izvjesca, zapisi []string) error {
	if s.sektorska == nil {
		return errors.New("sektorska izvješća nisu uključena")
	}
	if u == nil {
		return errors.New("upis zahtijeva prijavu")
	}
	if iz.Sektor == "" {
		return errors.New("izvješće nema sektor")
	}
	if !s.UpravaSektora(perms, iz.Sektor) {
		return errors.New("izvješće sektora slaže voditelj centra ili rukovoditelj sektora")
	}
	if iz.Dan.IsZero() {
		return errors.New("izvješće mora imati dan")
	}
	iz.Dan = pocetakDana(iz.Dan)
	if iz.Dan.After(pocetakDana(time.Now().In(models.Zagreb))) {
		return errors.New("izvješće se piše za protekli dan ili za danas, ne unaprijed")
	}
	postojece, err := s.sektorska.ZaDan(ctx, iz.Sektor, iz.Dan)
	if err != nil {
		return err
	}
	if postojece != nil && postojece.ID != iz.ID {
		return fmt.Errorf("izvješće sektora %s za %s već postoji", iz.Sektor, iz.Dan.Format("2.1.2006."))
	}
	if iz.ID != "" {
		cur, err := s.sektorska.Get(ctx, iz.ID)
		if err != nil {
			return err
		}
		if cur == nil {
			return errors.New("izvješće nije pronađeno")
		}
		iz.CreatedAt, iz.IzradioID, iz.Izradio, iz.PredanoAt, iz.JournalID = cur.CreatedAt, cur.IzradioID, cur.Izradio, cur.PredanoAt, cur.JournalID
	} else {
		iz.IzradioID, iz.Izradio = u.ID.String(), u.FullName
		if iz.JournalID == "" {
			iz.JournalID = s.otvoreniDnevnik(ctx, iz.Sektor)
		}
	}
	// snimka iz dionica i dnevnika
	sva, err := s.IzvjescaDana(ctx, iz.Sektor, iz.Dan)
	if err != nil {
		return err
	}
	ukljuci := map[string]bool{}
	for _, id := range izvjesca {
		ukljuci[id] = true
	}
	iz.Sadrzaj.Pregled = s.PregledSektora(sva, ukljuci)
	iz.Sadrzaj.Zapisi = nil
	if len(zapisi) > 0 {
		zeli := map[string]bool{}
		for _, id := range zapisi {
			zeli[id] = true
		}
		dostupni, err := s.ZapisiDana(ctx, iz.JournalID, iz.Dan)
		if err != nil {
			return err
		}
		for _, z := range dostupni {
			if zeli[z.ID] {
				iz.Sadrzaj.Zapisi = append(iz.Sadrzaj.Zapisi, z)
			}
		}
	}
	iz.IzradenoAt = time.Now().In(models.Zagreb)
	return s.sektorska.Save(ctx, iz)
}

// PredajSektorsko označava izvješće predanim Glavnom centru
func (s *IzvjescaService) PredajSektorsko(ctx context.Context, u *models.User, perms *models.UserPermissions, id string) error {
	iz, err := s.GetSektorsko(ctx, id)
	if err != nil {
		return err
	}
	if iz == nil {
		return errors.New("izvješće nije pronađeno")
	}
	if u == nil || !s.UpravaSektora(perms, iz.Sektor) {
		return errors.New("izvješće sektora predaje voditelj centra ili rukovoditelj sektora")
	}
	if iz.Predano() {
		return nil
	}
	if iz.Sadrzaj.Prazno() {
		return errors.New("prazno izvješće se ne predaje: uključite bar jedno izvješće dionice ili opišite stanje")
	}
	sad := time.Now().In(models.Zagreb)
	iz.PredanoAt = &sad
	return s.sektorska.Save(ctx, iz)
}

// ObrisiSektorsko arhivira izvješće sektora
func (s *IzvjescaService) ObrisiSektorsko(ctx context.Context, u *models.User, perms *models.UserPermissions, id string) error {
	iz, err := s.GetSektorsko(ctx, id)
	if err != nil {
		return err
	}
	if iz == nil {
		return errors.New("izvješće nije pronađeno")
	}
	if u == nil || !s.UpravaSektora(perms, iz.Sektor) {
		return errors.New("izvješće sektora briše voditelj centra ili rukovoditelj sektora")
	}
	return s.sektorska.Arhiviraj(ctx, iz)
}

func (s *IzvjescaService) GetSektorsko(ctx context.Context, id string) (*models.SektorskoIzvjesce, error) {
	if s.sektorska == nil {
		return nil, nil
	}
	return s.sektorska.Get(ctx, id)
}

func (s *IzvjescaService) ListSektorska(ctx context.Context, sektor string) ([]models.SektorskoIzvjesce, error) {
	if s.sektorska == nil {
		return nil, nil
	}
	return s.sektorska.List(ctx, sektor)
}
