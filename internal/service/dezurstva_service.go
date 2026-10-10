package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/obracun"

	"github.com/google/uuid"
)

// Plan dežurstava i obračun sati su jedna evidencija: razmak od–do jedne
// osobe s opisom rada. Prije obrane je plan, poslije nje ulaz za obrazac.
//
// Svatko upisuje sebe, od vodočuvara do glavnog rukovoditelja; uprava centra
// upisuje bilo koga i poslije provjeri i potvrdi tuđe upise. U obračun ulazi
// samo potvrđeno.

// UpravaCentra: voditelj ili zamjenik centra — uprava sektora, isto pravo
// koje dnevnik COP-a i otvara. Ona slaže plan za druge i potvrđuje upise.
func (s *JournalService) UpravaCentra(perms *models.UserPermissions, j *models.Journal) bool {
	return perms != nil && j != nil && j.CentarSektor != "" && perms.CanAdminister(j.CentarSektor, 0)
}

// MozeSebeUPlan: smije li osoba upisati vlastito dežurstvo. Dovoljno je da
// piše igdje u sektoru centra — po sektoru, po području u njemu, ili po
// dionici u njemu; vodočuvar ima samo dionice, i to mu je dosta.
func (s *JournalService) MozeSebeUPlan(perms *models.UserPermissions, o models.Opseg, j *models.Journal) bool {
	if perms == nil || j == nil || j.CentarSektor == "" {
		return false
	}
	if s.CanWrite(perms, o) {
		return true
	}
	for code := range perms.AllowedSections {
		if dionicaUSektoru(perms, code, j.CentarSektor) {
			return true
		}
	}
	return false
}

var reOstatakSifreDionice = regexp.MustCompile(`^(\d{1,2})\.\d{1,3}$`)

// dionicaUSektoru: šifra je dionica sektora kad ima puni oblik
// SEKTOR.PODRUČJE.BROJ sa sektorom centra, a osoba po registru ima dužnost na
// dionicama tog područja (dionice dužnosti provjerava registar, i područje
// dužnosti iz njega slijedi). Sam prefiks „P.” nije dionica.
func dionicaUSektoru(perms *models.UserPermissions, sifra, sektor string) bool {
	ostatak, ok := strings.CutPrefix(sifra, sektor+".")
	m := reOstatakSifreDionice.FindStringSubmatch(ostatak)
	if !ok || m == nil {
		return false
	}
	podrucje, _ := strconv.Atoi(m[1])
	return perms.RadiNaDionicamaU(podrucje)
}

// BrojDezurstava broji sva dežurstva
func (s *JournalService) BrojDezurstava(ctx context.Context) (int, error) {
	return s.repo.BrojDezurstava(ctx)
}

// Dezurstva vraća dežurstva dnevnika redom početka
func (s *JournalService) Dezurstva(ctx context.Context, journalID string) ([]models.Dezurstvo, error) {
	return s.repo.ListDezurstva(ctx, journalID)
}

// SpremiDezurstvo upisuje ili mijenja dežurstvo. Razmak mora biti unutar
// trajanja dnevnika: dežurstvo prije početka obrane ili poslije zaključenja
// nije dežurstvo u ovoj obrani. Što uprava upiše potvrđeno je odmah; što
// osoba upiše za sebe čeka potvrdu, a izmjena potvrđenog vraća ga na čekanje.
func (s *JournalService) SpremiDezurstvo(ctx context.Context, u *models.User, perms *models.UserPermissions, o models.Opseg, j *models.Journal, d *models.Dezurstvo) error {
	if u == nil {
		return errors.New("upis zahtijeva prijavu")
	}
	if j == nil || j.CentarSektor == "" {
		return errors.New("plan dežurstava vodi se uz dnevnik COP-a")
	}
	if d.UserID == "" || strings.TrimSpace(d.UserName) == "" {
		return errors.New("odaberite osobu")
	}
	uprava := s.UpravaCentra(perms, j)
	sebe := d.UserID == u.ID.String() && s.MozeSebeUPlan(perms, o, j)
	if !uprava && !sebe {
		return errors.New("tuđe dežurstvo upisuje voditelj ili zamjenik centra; svoje upisuje svatko tko radi u sektoru")
	}
	if !d.Do.After(d.Od) {
		return errors.New("kraj dežurstva mora biti poslije početka")
	}
	if d.Do.Sub(d.Od) > 36*time.Hour {
		return errors.New("dežurstvo dulje od 36 sati upišite kao više razmaka")
	}
	if j.StartedAt != nil && d.Od.Before(*j.StartedAt) {
		return fmt.Errorf("dnevnik počinje %s: dežurstvo prije toga u njega ne ide", j.StartedAt.In(models.Zagreb).Format("2.1.2006."))
	}
	if j.EndedAt != nil && d.Do.After(j.EndedAt.AddDate(0, 0, 1)) {
		return fmt.Errorf("dnevnik je zaključen %s: dežurstvo poslije toga ide u novi dnevnik", j.EndedAt.In(models.Zagreb).Format("2.1.2006."))
	}
	d.Mjesto = models.MjestoZaOpis(d.Opis)
	if d.Mjesto == "" {
		return errors.New("odaberite opis rada s popisa")
	}
	// Za koga: područje mora biti u sektoru centra; prazno je cijeli sektor.
	if d.ZaPodrucje() {
		uSektoru := false
		for _, id := range o.Podrucja {
			if id == *d.Podrucje {
				uSektoru = true
			}
		}
		if !uSektoru {
			return errors.New("branjeno područje nije u sektoru ovog centra")
		}
	} else {
		d.Podrucje = nil
	}
	d.JournalID = j.ID
	d.UserName, d.Napomena = strings.TrimSpace(d.UserName), strings.TrimSpace(d.Napomena)
	if d.ID != "" {
		if err := s.izmjenaDezurstva(ctx, u, uprava, j, d); err != nil {
			return err
		}
	} else {
		d.CreatedBy = u.ID.String()
	}
	d.Potvrdio, d.PotvrdenoAt = "", nil
	if potvrdiOdmah(uprava, d) {
		now := time.Now().In(models.Zagreb)
		d.Potvrdio, d.PotvrdenoAt = u.FullName, &now
	}
	return s.repo.SaveDezurstvo(ctx, d)
}

// izmjenaDezurstva provjerava izmjenu upisanog dežurstva i preuzima ono što
// izmjena ne mijenja: tko je i kada upisao, i trag ispravka opisa u
// napomeni. Tuđe dežurstvo mijenja uprava centra. Opis rada dežurstva
// kojemu se ne zna mjesto, ili kojemu je opis već ispravila, bira uprava
// centra (Odluke (8. 10. 2026.), pitanje 10); kad ga uprava promijeni
// običnim spremanjem, ostaje isti trag kao kod ispravka opisa, pa napomena
// kaže tko je izabrao sadašnji opis.
func (s *JournalService) izmjenaDezurstva(ctx context.Context, u *models.User, uprava bool, j *models.Journal, d *models.Dezurstvo) error {
	cur, err := s.repo.GetDezurstvo(ctx, d.ID)
	if err != nil {
		return err
	}
	if cur == nil || cur.JournalID != j.ID {
		return errors.New("dežurstvo nije pronađeno u ovom dnevniku")
	}
	if !uprava {
		if err := izmjenaBezUprave(u, cur, d); err != nil {
			return err
		}
	}
	d.CreatedBy, d.CreatedAt = cur.CreatedBy, cur.CreatedAt
	d.Napomena = zadrziTragIspravka(cur.Napomena, d.Napomena)
	if cur.OpisBiraUprava() && d.Opis != cur.Opis {
		d.Napomena = strings.TrimSpace(d.Napomena + " " + tragIspravkaOpisa(cur.Opis, u.FullName, time.Now().In(models.Zagreb)))
	}
	return nil
}

// izmjenaBezUprave: bez uprave centra osoba mijenja samo svoje dežurstvo, i
// u njemu ne opis rada koji bira uprava centra — ni kad se ne zna mjesto
// rada, ni kad je opis uprava već ispravila
func izmjenaBezUprave(u *models.User, cur, d *models.Dezurstvo) error {
	switch {
	case cur.UserID != u.ID.String():
		return errors.New("tuđe dežurstvo mijenja voditelj ili zamjenik centra")
	case cur.ZaProvjeru():
		return errors.New("mjesto rada nije poznato: opis rada bira voditelj ili zamjenik centra")
	case cur.OpisBiraUprava() && d.Opis != cur.Opis:
		return errors.New("opis rada izabrala je uprava centra: mijenja ga voditelj ili zamjenik centra")
	}
	return nil
}

// zadrziTragIspravka vraća novu napomenu uz tragove ispravka opisa iz stare
// koje nova nema: izmjena napomene ne briše tko je i kada ispravio opis
func zadrziTragIspravka(staro, novo string) string {
	for _, trag := range models.TragoviIspravkaOpisa(staro) {
		if !strings.Contains(novo, trag) {
			novo = strings.TrimSpace(novo + " " + trag)
		}
	}
	return novo
}

// PotvrdiDezurstvo: uprava centra provjerila je upis i potvrđuje ga.
// Dežurstvo kojemu se ne zna mjesto rada ne potvrđuje se dok uprava ne
// ispravi opis: potvrdom bi ušlo u obračun kao ured ili teren nasumce.
// Mjesto izvedeno iz opisa potvrdom se zapisuje, pa potvrđeni sati ne ovise
// o popisu opisa ni o tome kako ga čita čvor sa starijim programom.
func (s *JournalService) PotvrdiDezurstvo(ctx context.Context, u *models.User, perms *models.UserPermissions, j *models.Journal, id string) error {
	if u == nil || !s.UpravaCentra(perms, j) {
		return errors.New("dežurstvo potvrđuje voditelj ili zamjenik centra")
	}
	d, err := s.repo.GetDezurstvo(ctx, id)
	if err != nil {
		return err
	}
	if d == nil || d.JournalID != j.ID {
		return errors.New("dežurstvo nije pronađeno u ovom dnevniku")
	}
	if d.ZaProvjeru() {
		return errors.New("mjesto rada nije poznato: najprije ispravite opis rada izborom s popisa, pa potvrdite")
	}
	if d.Potvrdeno() {
		return nil
	}
	now := time.Now().In(models.Zagreb)
	d.Potvrdio, d.PotvrdenoAt, d.Mjesto = u.FullName, &now, d.MjestoRada()
	return s.repo.SaveDezurstvo(ctx, d)
}

// IspraviOpisDezurstva: uprava centra mijenja opis rada postojećeg
// dežurstva, bez micanja i ponovnog upisa. Opis mora biti s popisa, a
// mjesto rada izvodi se iz njega. Prijašnji opis, tko je ispravio i kada
// ostaju u napomeni, a cijela prijašnja inačica u knjizi verzija. Potvrda se
// povlači: ispravljeno dežurstvo uprava potvrđuje kao i svako drugo.
// Promijenjeno je false kad dežurstvo već ima taj opis i mjesto: tada se
// ništa ne sprema, a potvrda ostaje.
func (s *JournalService) IspraviOpisDezurstva(ctx context.Context, u *models.User, perms *models.UserPermissions, j *models.Journal, id, opis string) (promijenjeno bool, err error) {
	if u == nil || !s.UpravaCentra(perms, j) {
		return false, errors.New("opis rada ispravlja voditelj ili zamjenik centra")
	}
	mjesto := models.MjestoZaOpis(opis)
	if mjesto == "" {
		return false, errors.New("odaberite opis rada s popisa")
	}
	d, err := s.repo.GetDezurstvo(ctx, id)
	if err != nil {
		return false, err
	}
	if d == nil || d.JournalID != j.ID {
		return false, errors.New("dežurstvo nije pronađeno u ovom dnevniku")
	}
	if d.Opis == opis && d.Mjesto == mjesto {
		return false, nil
	}
	d.Napomena = strings.TrimSpace(d.Napomena + " " + tragIspravkaOpisa(d.Opis, u.FullName, time.Now().In(models.Zagreb)))
	d.Opis, d.Mjesto = opis, mjesto
	d.Potvrdio, d.PotvrdenoAt = "", nil
	return true, s.repo.SaveDezurstvo(ctx, d)
}

// tragIspravkaOpisa je bilješka o ispravku opisa rada: tko, kada i što je
// prije pisalo
func tragIspravkaOpisa(prije, tko string, kad time.Time) string {
	if strings.TrimSpace(prije) == "" {
		prije = "—"
	}
	return "[opis ispravio " + tko + " " + kad.Format("2.1.2006. 15:04") + "; prije: „" + prije + "”]"
}

// MakniDezurstvo miče dežurstvo iz plana; u knjizi verzija ostaje trag.
// Svoje nepotvrđeno miče svatko, osim kad mu opis rada bira uprava centra;
// ostalo uprava centra.
func (s *JournalService) MakniDezurstvo(ctx context.Context, u *models.User, perms *models.UserPermissions, j *models.Journal, id string) error {
	if u == nil || j == nil {
		return errors.New("upis zahtijeva prijavu")
	}
	d, err := s.repo.GetDezurstvo(ctx, id)
	if err != nil {
		return err
	}
	if d == nil || d.JournalID != j.ID {
		return errors.New("dežurstvo nije pronađeno u ovom dnevniku")
	}
	if !s.UpravaCentra(perms, j) {
		if err := micanjeVlastitog(u, d); err != nil {
			return err
		}
	}
	return s.repo.ArhivirajDezurstvo(ctx, d)
}

// micanjeVlastitog: bez uprave centra osoba miče samo svoje nepotvrđeno
// dežurstvo, i to ne ono kojemu opis bira uprava (mjesto rada se ne zna, ili
// je opis uprava već ispravila) — takvo bi osoba maknula i upisala ponovno s
// opisom po svom izboru
func micanjeVlastitog(u *models.User, d *models.Dezurstvo) error {
	switch {
	case d.UserID != u.ID.String() || d.Potvrdeno():
		return errors.New("potvrđeno ili tuđe dežurstvo miče voditelj ili zamjenik centra")
	case d.ZaProvjeru():
		return errors.New("mjesto rada nije poznato: takvo dežurstvo miče ili mu opis bira voditelj ili zamjenik centra")
	case d.OpisBiraUprava():
		return errors.New("opis rada izabrala je uprava centra: takvo dežurstvo miče voditelj ili zamjenik centra")
	}
	return nil
}

// Obracun je ono što voditelj gleda: po "za koga" (branjeno područje, pa
// cijeli sektor), redak po osobi i mjestu rada, uz svaki redak samo razredi
// u kojima ima sati. Zbrojevi po grupi i za cijeli sektor su rekapitulacija.
type Obracun struct {
	Grupe       []ObracunGrupa
	Stvarni     time.Duration
	Obracunski  float64
	CekaPotvrdu time.Duration // sati u razdoblju koji još nisu potvrđeni; izvan zbroja
	ZaProvjeru  time.Duration // sati dežurstava kojima se ne zna mjesto rada; izvan zbroja
}

type ObracunGrupa struct {
	Za         string
	Podrucje   *int
	Redovi     []ObracunRedak
	Stvarni    time.Duration
	Obracunski float64
}

type ObracunRedak struct {
	UserID, UserName string
	Mjesto           string // MjestoUred ili MjestoTeren
	Stavke           []ObracunStavka
	Stvarni          time.Duration
	Obracunski       float64
}

// ObracunStavka je jedan razred s brojkom: stvarni sati i obračunski,
// zaokruženi na pola sata
type ObracunStavka struct {
	Razred     obracun.Razred
	Sati       time.Duration
	Obracunski float64
}

// Obracun zbraja POTVRĐENA dežurstva dnevnika u razdoblju [od, do). Razmak
// koji viri iz razdoblja uzima se samo onim dijelom koji je unutra, pa se
// obračun za mjesec ne mijenja time što smjena prelazi u sljedeći. Nazivi
// daju ime područja po broju; prazan broj je cijeli sektor. Dežurstvo
// kojemu se ne zna mjesto rada ne ulazi u zbroj ni kad je potvrđeno: broji
// se kao „za provjeru”, kao nepotvrđeno kao „čeka potvrdu”.
func (s *JournalService) Obracun(ctx context.Context, j *models.Journal, od, do time.Time, kal obracun.Kalendar, k obracun.Koeficijenti, nazivi map[int]string) (Obracun, error) {
	var out Obracun
	dez, err := s.repo.ListDezurstva(ctx, j.ID)
	if err != nil {
		return out, err
	}
	type kljuc struct {
		podrucje int
		user     string
		mjesto   string
	}
	sati := map[kljuc]obracun.Sati{}
	imena := map[string]string{}
	for _, d := range dez {
		a, b, ok := dioURazdoblju(d, od, do)
		if !ok || izvanZbroja(d, b.Sub(a), &out.ZaProvjeru, &out.CekaPotvrdu) {
			continue
		}
		kl := kljuc{d.PodrucjeID(), d.UserID, d.MjestoRada()}
		if sati[kl] == nil {
			sati[kl] = obracun.Sati{}
		}
		sati[kl].Dodaj(obracun.Razvrstaj(a, b, kal))
		imena[d.UserID] = d.UserName
	}
	grupe := map[int]*ObracunGrupa{}
	for kl, st := range sati {
		g := grupe[kl.podrucje]
		if g == nil {
			za := "cijeli " + models.Terms().Lower("sektor") + " " + j.CentarSektor
			var podrucje *int
			if kl.podrucje > 0 {
				n := kl.podrucje
				podrucje = &n
				if za = nazivi[n]; za == "" {
					za = fmt.Sprintf("%s %d", models.Terms().Lower("podrucje"), n)
				}
			}
			g = &ObracunGrupa{Za: za, Podrucje: podrucje}
			grupe[kl.podrucje] = g
		}
		// u zbroju su samo dežurstva s poznatim mjestom: ured ili teren
		mjesto := obracun.Mjesto(kl.mjesto)
		r := ObracunRedak{UserID: kl.user, UserName: imena[kl.user], Mjesto: string(mjesto), Stvarni: st.Ukupno()}
		obr := k.ObracunskiPoRazredu(st, mjesto)
		for _, razred := range obracun.Razredi {
			if st[razred] > 0 {
				r.Stavke = append(r.Stavke, ObracunStavka{Razred: razred, Sati: st[razred], Obracunski: obr[razred]})
				r.Obracunski += obr[razred]
			}
		}
		r.Obracunski = obracun.Zaokruzi(r.Obracunski, obracun.Korak)
		g.Redovi = append(g.Redovi, r)
	}
	for _, g := range grupe {
		sort.Slice(g.Redovi, func(i, l int) bool {
			if g.Redovi[i].UserName != g.Redovi[l].UserName {
				return g.Redovi[i].UserName < g.Redovi[l].UserName
			}
			return g.Redovi[i].Mjesto < g.Redovi[l].Mjesto
		})
		for _, r := range g.Redovi {
			g.Stvarni += r.Stvarni
			g.Obracunski += r.Obracunski
		}
		out.Stvarni += g.Stvarni
		out.Obracunski += g.Obracunski
		out.Grupe = append(out.Grupe, *g)
	}
	// Po području, cijeli sektor na kraju — kao list "B i ostali".
	sort.Slice(out.Grupe, func(i, l int) bool {
		a, b := out.Grupe[i], out.Grupe[l]
		if (a.Podrucje == nil) != (b.Podrucje == nil) {
			return a.Podrucje != nil
		}
		if a.Podrucje != nil {
			return *a.Podrucje < *b.Podrucje
		}
		return false
	})
	return out, nil
}

// dioURazdoblju je dio razmaka unutar razdoblja [od, do); ok je false kad
// unutra nema ničega
func dioURazdoblju(d models.Dezurstvo, od, do time.Time) (a, b time.Time, ok bool) {
	a, b = d.Od, d.Do
	if a.Before(od) {
		a = od
	}
	if b.After(do) {
		b = do
	}
	return a, b, b.After(a)
}

// izvanZbroja broji sate dežurstva koje ne ulazi u obračun: kad se ne zna
// mjesto rada, u „za provjeru”, a kad nije potvrđeno, u „čeka potvrdu”.
// Vraća true kad je dežurstvo izvan zbroja.
func izvanZbroja(d models.Dezurstvo, sati time.Duration, zaProvjeru, cekaPotvrdu *time.Duration) bool {
	switch {
	case d.ZaProvjeru():
		*zaProvjeru += sati
	case !d.Potvrdeno():
		*cekaPotvrdu += sati
	default:
		return false
	}
	return true
}

// IORS je izvješće o radnim satima jedne osobe, kako ga obrazac traži: redak
// po danu i razmaku (dan, od–do, opis, mjesto, sati po razredu), pa obračun
// po razredu za ured i teren. Nepotvrđeni razmaci i razmaci kojima se ne zna
// mjesto rada se vide, ali nisu u zbroju.
type IORS struct {
	UserID, UserName string
	Od, Do           time.Time
	Redovi           []IORSRedak
	Ured, Teren      obracun.Sati
	UredObr          map[obracun.Razred]float64
	TerenObr         map[obracun.Razred]float64
	UredObracunski   float64 // zbroj obračunskih u uredu
	TerenObracunski  float64
	Stvarni          time.Duration
	Obracunski       float64
	CekaPotvrdu      time.Duration
	ZaProvjeru       time.Duration // mjesto rada nepoznato; vidi se, nije u zbroju
}

// IORSRedak je jedan dan jednog razmaka; razmak preko ponoći daje dva retka,
// jer svaki dan nosi svoju vrstu
type IORSRedak struct {
	Dan        time.Time
	Od, Do     time.Time
	Opis       string
	Mjesto     string // ured ili teren; prazno kad se ne zna
	Za         string
	Sati       obracun.Sati
	Ukupno     time.Duration
	Potvrdeno  bool
	ZaProvjeru bool // mjesto rada nepoznato: izvan zbroja dok uprava centra ne ispravi opis
}

// UObracunu javlja ulazi li redak u zbroj: potvrđen i s poznatim mjestom
func (r IORSRedak) UObracunu() bool { return r.Potvrdeno && !r.ZaProvjeru }

// MjestoTekst je mjesto rada retka za ispis
func (r IORSRedak) MjestoTekst() string { return models.NazivMjesta(r.Mjesto) }

// dodaj upisuje redak i njegove sate: potvrđeni s poznatim mjestom idu u
// ured ili teren, ostali se broje posebno — vide se, a nisu u zbroju
func (o *IORS) dodaj(r IORSRedak) {
	o.Redovi = append(o.Redovi, r)
	switch {
	case r.ZaProvjeru:
		o.ZaProvjeru += r.Ukupno
	case !r.Potvrdeno:
		o.CekaPotvrdu += r.Ukupno
	case r.Mjesto == models.MjestoTeren:
		o.Teren.Dodaj(r.Sati)
	default:
		o.Ured.Dodaj(r.Sati)
	}
}

// Sat vraća sate razreda u retku, za tablicu
func (r IORSRedak) Sat(razred obracun.Razred) time.Duration { return r.Sati[razred] }

// Obr vraća obračunske sate razreda za mjesto
func (o IORS) Obr(mjesto obracun.Mjesto, r obracun.Razred) float64 {
	if mjesto == obracun.Teren {
		return o.TerenObr[r]
	}
	return o.UredObr[r]
}

// Sat vraća stvarne sate razreda za mjesto
func (o IORS) Sat(mjesto obracun.Mjesto, r obracun.Razred) time.Duration {
	if mjesto == obracun.Teren {
		return o.Teren[r]
	}
	return o.Ured[r]
}

// ObracunOsobe slaže IORS jedne osobe za razdoblje [od, do)
func (s *JournalService) ObracunOsobe(ctx context.Context, j *models.Journal, userID string, od, do time.Time, kal obracun.Kalendar, k obracun.Koeficijenti, nazivi map[int]string) (IORS, error) {
	out := IORS{UserID: userID, Od: od, Do: do, Ured: obracun.Sati{}, Teren: obracun.Sati{}}
	dez, err := s.repo.ListDezurstva(ctx, j.ID)
	if err != nil {
		return out, err
	}
	for _, d := range dez {
		if d.UserID != userID {
			continue
		}
		out.UserName = d.UserName
		a, b, ok := dioURazdoblju(d, od, do)
		if !ok {
			continue
		}
		za := "cijeli " + models.Terms().Lower("sektor") + " " + j.CentarSektor
		if d.ZaPodrucje() {
			if za = nazivi[d.PodrucjeID()]; za == "" {
				za = fmt.Sprintf("%s %d", models.Terms().Lower("podrucje"), d.PodrucjeID())
			}
		}
		// Po danima, kao u obrascu: redak ne prelazi ponoć.
		for pocetak := a.In(models.Zagreb); pocetak.Before(b); {
			ponoc := time.Date(pocetak.Year(), pocetak.Month(), pocetak.Day()+1, 0, 0, 0, 0, models.Zagreb)
			kraj := b.In(models.Zagreb)
			if kraj.After(ponoc) {
				kraj = ponoc
			}
			sati := obracun.Razvrstaj(pocetak, kraj, kal)
			out.dodaj(IORSRedak{Dan: time.Date(pocetak.Year(), pocetak.Month(), pocetak.Day(), 0, 0, 0, 0, models.Zagreb),
				Od: pocetak, Do: kraj, Opis: d.Opis, Mjesto: d.MjestoRada(), Za: za, Sati: sati, Ukupno: sati.Ukupno(), Potvrdeno: d.Potvrdeno(), ZaProvjeru: d.ZaProvjeru()})
			pocetak = ponoc
		}
	}
	sort.Slice(out.Redovi, func(i, l int) bool { return out.Redovi[i].Od.Before(out.Redovi[l].Od) })
	out.UredObr, out.TerenObr = k.ObracunskiPoRazredu(out.Ured, obracun.Ured), k.ObracunskiPoRazredu(out.Teren, obracun.Teren)
	out.Stvarni = out.Ured.Ukupno() + out.Teren.Ukupno()
	out.UredObracunski, out.TerenObracunski = k.Obracunski(out.Ured, obracun.Ured), k.Obracunski(out.Teren, obracun.Teren)
	out.Obracunski = out.UredObracunski + out.TerenObracunski
	return out, nil
}

// PlanoviOsobe vraća planove u kojima osoba ima dežurstva, za profil
func (s *JournalService) PlanoviOsobe(ctx context.Context, userID string) ([]models.PlanOsobe, error) {
	return s.repo.PlanoviOsobe(ctx, userID)
}

// IORSStavka je jedan razred s brojkom u retku po danu
type IORSStavka struct {
	Razred obracun.Razred
	Sati   time.Duration
}

// PoRazredima vraća razrede retka koji imaju sate, redom obrasca — za
// prikaz u kartici dana, gdje prazni razredi ne zauzimaju mjesto
func (r IORSRedak) PoRazredima() []IORSStavka {
	var out []IORSStavka
	for _, razred := range obracun.Razredi {
		if r.Sati[razred] > 0 {
			out = append(out, IORSStavka{Razred: razred, Sati: r.Sati[razred]})
		}
	}
	return out
}

// Dežurstvo u dnevniku COP-a: operater dolaskom preuzme, odlaskom preda.
// Oboje je zapis u dnevniku (vrsta DEZURSTVO), a odrađeni razmak ide u plan
// kao "Dežurstvo u COP-u" — pa u obračun, čim ga uprava potvrdi. Tko preuzme
// dok prethodnik nije predao, zaključi mu dežurstvo tim trenutkom: to se
// vidi u zapisu, i nitko ne ostaje "na dežurstvu" zauvijek.

// PreuzmiDezurstvo upisuje da osoba dežura od sada
func (s *JournalService) PreuzmiDezurstvo(ctx context.Context, u *models.User, perms *models.UserPermissions, o models.Opseg, j *models.Journal) error {
	if u == nil {
		return errors.New("upis zahtijeva prijavu")
	}
	if j == nil || j.CentarSektor == "" {
		return errors.New("dežurstvo se preuzima u dnevniku COP-a")
	}
	if j.EndedAt != nil {
		return errors.New("dnevnik je zaključen")
	}
	if !s.MozeSebeUPlan(perms, o, j) {
		return errors.New("dežurstvo preuzima tko radi u sektoru centra")
	}
	if j.DezurniID == u.ID.String() {
		return errors.New("već dežurate")
	}
	sad := time.Now().In(models.Zagreb)
	if j.NetkoDezura() {
		// Ne potvrđuje se ni kad preuzima uprava: nitko nije vidio kad je
		// prethodnik stvarno otišao, pa razmak čeka provjeru.
		if err := s.zakljuciDezurstvo(ctx, u, j, sad, " (nije predano; zaključeno preuzimanjem)", false); err != nil {
			return err
		}
	}
	j.DezurniID, j.DezurniIme, j.DezurniOd = u.ID.String(), u.FullName, &sad
	if err := s.repo.SaveJournal(ctx, j); err != nil {
		return err
	}
	return s.repo.SaveEntry(ctx, &models.JournalEntry{JournalID: j.ID, Date: pocetakDana(sad), Kind: models.EntryKindDuty, HappenedAt: &sad,
		Text: "Dežurstvo preuzima " + u.FullName + ".", UserID: u.ID.String(), UserName: u.FullName})
}

// PredajDezurstvo zaključuje dežurstvo osobe koja dežura; uprava centra
// može predati i tuđe
func (s *JournalService) PredajDezurstvo(ctx context.Context, u *models.User, perms *models.UserPermissions, j *models.Journal) error {
	if u == nil {
		return errors.New("upis zahtijeva prijavu")
	}
	if j == nil || !j.NetkoDezura() {
		return errors.New("nitko ne dežura")
	}
	if j.DezurniID != u.ID.String() && !s.UpravaCentra(perms, j) {
		return errors.New("dežurstvo predaje tko dežura, ili uprava centra")
	}
	return s.zakljuciDezurstvo(ctx, u, j, time.Now().In(models.Zagreb), "", s.UpravaCentra(perms, j))
}

// zakljuciDezurstvo upisuje predaju: zapis u dnevnik, razmak u plan, dnevnik
// bez dežurnog. Razmak potvrđuje uprava kao i svaki drugi; potvrđen je
// odmah samo kad ga uprava sama preda (svoje ili tuđe). Razmak ima stalan
// identitet (dnevnik, osoba, početak), pa ponovljena predaja nakon greške
// osvježi isti razmak, a ne upiše još jedan.
func (s *JournalService) zakljuciDezurstvo(ctx context.Context, u *models.User, j *models.Journal, kad time.Time, napomena string, potvrdi bool) error {
	od := j.DezurniOd.In(models.Zagreb)
	// U plan ide razmak u punim minutama; kraći od minute (predaja odmah
	// nakon preuzimanja) nije dežurstvo i ostaje samo zapis u dnevniku.
	if kad.Sub(od) >= time.Minute {
		d := &models.Dezurstvo{ID: idPredaje(j.ID, j.DezurniID, od), JournalID: j.ID, UserID: j.DezurniID, UserName: j.DezurniIme,
			Od: od.Truncate(time.Minute), Do: kad.Truncate(time.Minute),
			Opis: models.OpisiRada[0].Opis, Mjesto: models.MjestoZaOpis(models.OpisiRada[0].Opis), Napomena: strings.TrimSpace(napomena), CreatedBy: u.ID.String()}
		if potvrdiOdmah(potvrdi, d) {
			d.Potvrdio, d.PotvrdenoAt = u.FullName, &kad
		}
		if err := s.repo.SaveDezurstvo(ctx, d); err != nil {
			return err
		}
	}
	tekst := "Dežurstvo predaje " + j.DezurniIme + " (od " + od.Format("2.1. 15:04") + ", " + satiTekst(kad.Sub(od)) + " h)" + napomena + "."
	if err := s.repo.SaveEntry(ctx, &models.JournalEntry{JournalID: j.ID, Date: pocetakDana(kad), Kind: models.EntryKindDuty, HappenedAt: &kad,
		Text: tekst, UserID: u.ID.String(), UserName: u.FullName}); err != nil {
		return err
	}
	j.DezurniID, j.DezurniIme, j.DezurniOd = "", "", nil
	return s.repo.SaveJournal(ctx, j)
}

// potvrdiOdmah: što upiše ili preda uprava potvrđeno je odmah, osim
// dežurstva duljeg od 24 sata bez stanke — ono dobiva napomenu i čeka da ga
// uprava pogleda i potvrdi
func potvrdiOdmah(uprava bool, d *models.Dezurstvo) bool {
	if !d.BezStanke() {
		return uprava
	}
	if !strings.Contains(d.Napomena, napomenaBezStanke) {
		d.Napomena = strings.TrimSpace(d.Napomena + " " + napomenaBezStanke)
	}
	return false
}

const napomenaBezStanke = "[dulje od 24 h bez stanke — provjeriti prije potvrde]"

// idPredaje je stalan identitet razmaka koji nastaje predajom dežurstva
func idPredaje(dnevnik, osoba string, od time.Time) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("gocop:dezurstvo:"+dnevnik+"|"+osoba+"|"+od.UTC().Format(time.RFC3339Nano))).String()
}

func pocetakDana(t time.Time) time.Time {
	t = t.In(models.Zagreb)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, models.Zagreb)
}

func satiTekst(d time.Duration) string {
	return fmt.Sprintf("%d:%02d", int(d.Hours()), int(d.Minutes())%60)
}
