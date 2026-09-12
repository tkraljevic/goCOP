package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/obracun"
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
		if strings.HasPrefix(code, j.CentarSektor+".") {
			return true
		}
	}
	return false
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
		cur, err := s.repo.GetDezurstvo(ctx, d.ID)
		if err != nil {
			return err
		}
		if cur == nil || cur.JournalID != j.ID {
			return errors.New("dežurstvo nije pronađeno u ovom dnevniku")
		}
		if !uprava && cur.UserID != u.ID.String() {
			return errors.New("tuđe dežurstvo mijenja voditelj ili zamjenik centra")
		}
		d.CreatedBy, d.CreatedAt = cur.CreatedBy, cur.CreatedAt
	} else {
		d.CreatedBy = u.ID.String()
	}
	d.Potvrdio, d.PotvrdenoAt = "", nil
	if uprava {
		now := time.Now().In(models.Zagreb)
		d.Potvrdio, d.PotvrdenoAt = u.FullName, &now
	}
	return s.repo.SaveDezurstvo(ctx, d)
}

// PotvrdiDezurstvo: uprava centra provjerila je upis i potvrđuje ga
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
	if d.Potvrdeno() {
		return nil
	}
	now := time.Now().In(models.Zagreb)
	d.Potvrdio, d.PotvrdenoAt = u.FullName, &now
	return s.repo.SaveDezurstvo(ctx, d)
}

// MakniDezurstvo miče dežurstvo iz plana; u knjizi verzija ostaje trag.
// Svoje nepotvrđeno miče svatko, ostalo uprava centra.
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
	if !s.UpravaCentra(perms, j) && (d.UserID != u.ID.String() || d.Potvrdeno()) {
		return errors.New("potvrđeno ili tuđe dežurstvo miče voditelj ili zamjenik centra")
	}
	return s.repo.ArhivirajDezurstvo(ctx, d)
}

// Obracun je ono što voditelj gleda: po "za koga" (branjeno područje, pa
// cijeli sektor), redak po osobi i mjestu rada, uz svaki redak samo razredi
// u kojima ima sati. Zbrojevi po grupi i za cijeli sektor su rekapitulacija.
type Obracun struct {
	Grupe       []ObracunGrupa
	Stvarni     time.Duration
	Obracunski  float64
	CekaPotvrdu time.Duration // sati u razdoblju koji još nisu potvrđeni; izvan zbroja
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
// daju ime područja po broju; prazan broj je cijeli sektor.
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
		a, b := d.Od, d.Do
		if a.Before(od) {
			a = od
		}
		if b.After(do) {
			b = do
		}
		if !b.After(a) {
			continue
		}
		if !d.Potvrdeno() {
			out.CekaPotvrdu += b.Sub(a)
			continue
		}
		kl := kljuc{d.PodrucjeID(), d.UserID, d.Mjesto}
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
		mjesto := obracun.Mjesto(kl.mjesto)
		if mjesto != obracun.Teren {
			mjesto = obracun.Ured
		}
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

// IORS je izvješće o radnim satima jedne osobe, kako ga obrazac traži: redak
// po danu i razmaku (dan, od–do, opis, mjesto, sati po razredu), pa obračun
// po razredu za ured i teren. Nepotvrđeni razmaci se vide, ali nisu u zbroju.
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
}

// IORSRedak je jedan dan jednog razmaka; razmak preko ponoći daje dva retka,
// jer svaki dan nosi svoju vrstu
type IORSRedak struct {
	Dan       time.Time
	Od, Do    time.Time
	Opis      string
	Mjesto    string
	Za        string
	Sati      obracun.Sati
	Ukupno    time.Duration
	Potvrdeno bool
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
		a, b := d.Od, d.Do
		if a.Before(od) {
			a = od
		}
		if b.After(do) {
			b = do
		}
		if !b.After(a) {
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
			r := IORSRedak{Dan: time.Date(pocetak.Year(), pocetak.Month(), pocetak.Day(), 0, 0, 0, 0, models.Zagreb),
				Od: pocetak, Do: kraj, Opis: d.Opis, Mjesto: d.Mjesto, Za: za, Sati: sati, Ukupno: sati.Ukupno(), Potvrdeno: d.Potvrdeno()}
			out.Redovi = append(out.Redovi, r)
			if d.Potvrdeno() {
				if d.Mjesto == models.MjestoTeren {
					out.Teren.Dodaj(sati)
				} else {
					out.Ured.Dodaj(sati)
				}
			} else {
				out.CekaPotvrdu += r.Ukupno
			}
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
// odmah samo kad ga uprava sama preda (svoje ili tuđe).
func (s *JournalService) zakljuciDezurstvo(ctx context.Context, u *models.User, j *models.Journal, kad time.Time, napomena string, potvrdi bool) error {
	od := j.DezurniOd.In(models.Zagreb)
	d := &models.Dezurstvo{JournalID: j.ID, UserID: j.DezurniID, UserName: j.DezurniIme, Od: od, Do: kad,
		Opis: models.OpisiRada[0].Opis, Mjesto: models.MjestoZaOpis(models.OpisiRada[0].Opis), Napomena: strings.TrimSpace(napomena), CreatedBy: u.ID.String()}
	if !kad.After(od) {
		d.Do = od.Add(time.Minute)
	}
	if potvrdi {
		d.Potvrdio, d.PotvrdenoAt = u.FullName, &kad
	}
	if err := s.repo.SaveDezurstvo(ctx, d); err != nil {
		return err
	}
	tekst := "Dežurstvo predaje " + j.DezurniIme + " (od " + od.Format("2.1. 15:04") + ", " + satiTekst(kad.Sub(od)) + " h)" + napomena + "."
	if err := s.repo.SaveEntry(ctx, &models.JournalEntry{JournalID: j.ID, Date: pocetakDana(kad), Kind: models.EntryKindDuty, HappenedAt: &kad,
		Text: tekst, UserID: u.ID.String(), UserName: u.FullName}); err != nil {
		return err
	}
	j.DezurniID, j.DezurniIme, j.DezurniOd = "", "", nil
	return s.repo.SaveJournal(ctx, j)
}

func pocetakDana(t time.Time) time.Time {
	t = t.In(models.Zagreb)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, models.Zagreb)
}

func satiTekst(d time.Duration) string {
	return fmt.Sprintf("%d:%02d", int(d.Hours()), int(d.Minutes())%60)
}
