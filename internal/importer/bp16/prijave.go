package bp16

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/sadrzaj"
	"gocop/internal/slike"
)

// Uvoz obavijesti s terena: zbirka obavijesti_sa_terena u Directusu VGI
// Baranja (izvješća, prijave, obavijesti i zahtjevi vodočuvara, sa slikama i
// skenom potpisanog ispisa) prenosi se u prijave s terena goCOP-a kao
// rekonstrukcija: bez novog potpisa, sken je izvornik gdje ga ima, a slike se
// smanjuju kao i u programu. Ponovni uvoz preskače što već ima, po izvoru,
// osim fotografija koje ranije nisu stigle.

// KorisnikUvoza je vodočuvar u goCOP-u kojem se prijave pripisuju
type KorisnikUvoza struct {
	ID     string
	Ime    string
	Sektor string
	AreaID int // branjeno područje iz zaduženja; 0 kad ga nema
}

// PrijaveDeps je što uvoz treba od programa
type PrijaveDeps struct {
	Prijave   *repository.PrijavaRepository
	Korisnici map[string]KorisnikUvoza // po imenu i prezimenu kako stoji u Directusu
	Podrucja  map[string]int           // vodočuvarsko područje iz Directusa → branjeno područje
	Sektor    string
	Cvor      string
	Datoteka  func(ctx context.Context, id, upit string) ([]byte, error) // Directus /assets
	// IzradiPDF crta izvornik iz podataka kad skena nema; SlikeOdmah briše
	// izvorne slike čim je izvornik spremljen
	IzradiPDF func(p *models.PrijavaSTerena, slike map[string][]byte) []byte
	// SpremiSken sprema sken potpisanog ispisa uz bazu i vraća naziv datoteke;
	// sken ostaje lokalni prilog, izvornik u knjizi je PDF iz podataka
	SpremiSken func(p *models.PrijavaSTerena, pdf []byte) (string, error)
	SlikeOdmah bool
	DryRun     bool
	Log        func(string, ...any)
}

// PrijaveReport je izvješće uvoza
type PrijaveReport struct {
	Ukupno, Upisano, Postoje, BezKorisnika, BezPodrucja int
	Slika, Skenova, Nacrta                              int
	Dopunjeno                                           int // prenesene prijave kojima su stigle fotografije, broje se i pod Postoje
	PoKorisniku, PoPodrucju, Nepoznati                  map[string]int
	DryRun                                              bool
}

// Summary sažima izvješće u jedan redak
func (r PrijaveReport) Summary() string {
	return fmt.Sprintf("%d obavijesti: %d upisano, %d već postoji, %d bez poznatog vodočuvara, %d bez područja; %d slika, %d skenova, %d nacrta",
		r.Ukupno, r.Upisano, r.Postoje, r.BezKorisnika, r.BezPodrucja, r.Slika, r.Skenova, r.Nacrta)
}

// obavijestRow je zapis zbirke obavijesti_sa_terena
type obavijestRow struct {
	ID            int             `json:"id"`
	Status        string          `json:"status"`
	DateCreated   string          `json:"date_created"`
	DateUpdated   string          `json:"date_updated"`
	UserCreated   string          `json:"user_created"`
	Podrucje      string          `json:"vodocuvarsko_podrucje"`
	Cuvarnica     string          `json:"cuvarnica"`
	Vrsta         string          `json:"vrsta_dokumenta"`
	Naslov        string          `json:"naslov"`
	Element       string          `json:"Konstrukcijski_element"`
	Opis          string          `json:"opis"`
	Vaznost       string          `json:"vaznost_objekta"`
	NazivVodotoka json.RawMessage `json:"naziv_vodotoka"`
	Slika1        string          `json:"slika_1"`
	Slika2        string          `json:"slika_2"`
	Slika3        string          `json:"slika_3"`
	Datoteka      string          `json:"datoteka"`
	Lokacija      *struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
	} `json:"lokacija"`

	kad    time.Time // vrijemeUpisa, po zagrebačkom vremenu
	imaKad bool
}

// vrijemeUpisa je kad je obavijest upisana u staroj evidenciji: date_created,
// a kad njega nema ili se ne da pročitati, date_updated. Bez obaju se ne zna
// ni dan ni godina prijave, pa se obavijest ne uvozi (Odluke (13. 10. 2026.)).
func vrijemeUpisa(o obavijestRow) (time.Time, bool) {
	for _, s := range []string{o.DateCreated, o.DateUpdated} {
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(s)); err == nil {
			return t.In(models.Zagreb), true
		}
	}
	return time.Time{}, false
}

// prvaTocka vadi prvi par koordinata iz GeoJSON geometrije: točku kako jest,
// a iz linije ili poligona prvu točku
func prvaTocka(raw json.RawMessage) (lon, lat float64, ok bool) {
	var par []float64
	if json.Unmarshal(raw, &par) == nil {
		if len(par) >= 2 {
			return par[0], par[1], true
		}
		return 0, 0, false
	}
	var niz []json.RawMessage
	if json.Unmarshal(raw, &niz) == nil && len(niz) > 0 {
		return prvaTocka(niz[0])
	}
	return 0, 0, false
}

// vrstaPrijave prevodi vrstu dokumenta iz Directusa
func vrstaPrijave(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "IZVJEŠĆE", "IZVJESCE":
		return models.PrijavaIzvjesce
	case "PRIJAVA":
		return models.PrijavaPrijava
	case "ZAHTJEV":
		return models.PrijavaZahtjev
	}
	return models.PrijavaObavijest
}

// vodePoOznaci čita zbirku vode: naziv vode po oznaci kojom ga obavijest
// navodi. Greška čitanja prekida uvoz, jer bi obavijest ostala bez vode, a
// ponovni uvoz je preskače. Ime zbirke u grešku stavlja izvor, kao i za
// ostale zbirke.
func vodePoOznaci(ctx context.Context, src Source) (map[int]string, error) {
	raw, err := src.Items(ctx, "vode")
	if err != nil {
		return nil, err
	}
	vode := map[int]string{}
	for _, r := range raw {
		var v struct {
			ID   int    `json:"id"`
			Voda string `json:"voda"`
		}
		if json.Unmarshal(r, &v) == nil {
			vode[v.ID] = v.Voda
		}
	}
	return vode, nil
}

// RunPrijave uvozi obavijesti s terena
func RunPrijave(ctx context.Context, src Source, deps PrijaveDeps) (PrijaveReport, error) {
	rep := PrijaveReport{DryRun: deps.DryRun, PoKorisniku: map[string]int{}, PoPodrucju: map[string]int{}, Nepoznati: map[string]int{}}
	u := &uvozPrijava{deps: deps, rep: &rep, logf: zapisnik(deps.Log), brojevi: map[int]int{}}
	var err error
	if u.imena, err = imenaKorisnika(ctx, src); err != nil {
		return rep, err
	}
	if u.vode, err = vodePoOznaci(ctx, src); err != nil {
		return rep, err
	}
	redovi, err := obavijestiPoVremenu(ctx, src)
	if err != nil {
		return rep, err
	}
	rep.Ukupno = len(redovi)
	for _, o := range redovi {
		if err := u.uvezi(ctx, o); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

// uvozPrijava je stanje jednog uvoza obavijesti: što uvoz treba od
// programa, izvješće i ono što je pročitao prije prve obavijesti
type uvozPrijava struct {
	deps    PrijaveDeps
	rep     *PrijaveReport
	logf    func(string, ...any)
	imena   map[string]string // korisnici Directusa po oznaci
	vode    map[int]string    // vode po oznaci
	brojevi map[int]int       // sljedeći broj prijave po godini
}

// zapisnik je zadani zapisnik uvoza, a bez njega zapisnik koji ništa ne piše
func zapisnik(logf func(string, ...any)) func(string, ...any) {
	if logf == nil {
		return func(string, ...any) {}
	}
	return logf
}

// imenaKorisnika su imena i prezimena korisnika Directusa po oznaci; izvor
// bez korisnika, npr. mapa bez directus_users, daje prazan popis
func imenaKorisnika(ctx context.Context, src Source) (map[string]string, error) {
	imena := map[string]string{}
	us, ok := src.(UserSource)
	if !ok {
		return imena, nil
	}
	raw, err := us.Users(ctx)
	if err != nil {
		return nil, fmt.Errorf("korisnici: %w", err)
	}
	for _, r := range raw {
		var k struct {
			ID        string `json:"id"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
		}
		if json.Unmarshal(r, &k) == nil {
			imena[k.ID] = strings.TrimSpace(k.FirstName + " " + k.LastName)
		}
	}
	return imena, nil
}

// obavijestiPoVremenu čita zbirku obavijesti poredanu po vremenu upisa, jer
// brojevi prijava u godini idu redom kojim su obavijesti nastale
func obavijestiPoVremenu(ctx context.Context, src Source) ([]obavijestRow, error) {
	raw, err := src.Items(ctx, "obavijesti_sa_terena")
	if err != nil {
		return nil, err
	}
	redovi := make([]obavijestRow, 0, len(raw))
	for _, r := range raw {
		var o obavijestRow
		if err := json.Unmarshal(r, &o); err != nil {
			return nil, fmt.Errorf("obavijest: %w", err)
		}
		o.kad, o.imaKad = vrijemeUpisa(o)
		redovi = append(redovi, o)
	}
	sort.SliceStable(redovi, func(i, j int) bool { return redovi[i].kad.Before(redovi[j].kad) })
	return redovi, nil
}

// uvezi prenosi jednu obavijest. Prenesenoj dopuni fotografije koje joj
// nedostaju, a preskače onu kojoj se ne zna vodočuvar, područje ili vrijeme
// upisa.
func (u *uvozPrijava) uvezi(ctx context.Context, o obavijestRow) error {
	izvor := fmt.Sprintf("bp16:obavijesti_sa_terena:%d", o.ID)
	prenesena, err := u.deps.Prijave.PoIzvoru(ctx, izvor)
	if err != nil {
		return fmt.Errorf("obavijest %d: čitanje prenesene prijave: %w", o.ID, err)
	}
	if prenesena != nil {
		return u.dopuni(ctx, o, izvor, prenesena)
	}
	p := u.prijava(o, izvor)
	if p == nil {
		return nil
	}
	if err := u.numeriraj(ctx, p); err != nil {
		return err
	}
	u.rep.PoKorisniku[p.Ime]++
	u.rep.PoPodrucju[fmt.Sprintf("BP %d (%s)", p.AreaID, o.Podrucje)]++
	if u.deps.DryRun {
		u.prebroji(o)
		return nil
	}
	return u.prenesi(ctx, o, izvor, p)
}

// prijava slaže prijavu iz obavijesti; nil kad se ne zna vodočuvar,
// područje ili vrijeme upisa, što izvješće broji
func (u *uvozPrijava) prijava(o obavijestRow, izvor string) *models.PrijavaSTerena {
	k, area, ok := u.osobaIPodrucje(o)
	if !ok {
		return nil
	}
	if !o.imaKad {
		u.rep.Nepoznati["vrijeme upisa"]++
		u.logf("  obavijest %d nije uvezena: u evidenciji nema čitljivog vremena upisa ni izmjene", o.ID)
		return nil
	}
	p := novaPrijava(o, izvor, k, area, o.kad, u.deps)
	p.Vodotok = vodotokObavijesti(o.NazivVodotoka, u.vode)
	p.Latitude, p.Longitude = tockaObavijesti(o)
	p.Status = statusObavijesti(o.Status)
	if p.Status == models.PrijavaNacrt {
		u.rep.Nacrta++
	}
	return p
}

// osobaIPodrucje nalazi vodočuvara i branjeno područje obavijesti; kad
// jednog od njih nema, izvješće ga broji pod nepoznato
func (u *uvozPrijava) osobaIPodrucje(o obavijestRow) (KorisnikUvoza, int, bool) {
	ime := u.imena[o.UserCreated]
	k, ok := u.deps.Korisnici[ime]
	if !ok {
		u.rep.BezKorisnika++
		u.rep.Nepoznati[ime]++
		return k, 0, false
	}
	area, ok := u.deps.Podrucja[strings.ToUpper(strings.TrimSpace(o.Podrucje))]
	if !ok {
		u.rep.BezPodrucja++
		u.rep.Nepoznati["područje "+o.Podrucje]++
		return k, 0, false
	}
	return k, area, true
}

// novaPrijava je rekonstruirana prijava s podacima obavijesti; oznaka je
// izvedena iz izvora, pa je ista pri svakom uvozu
func novaPrijava(o obavijestRow, izvor string, k KorisnikUvoza, area int, kad time.Time, deps PrijaveDeps) *models.PrijavaSTerena {
	p := &models.PrijavaSTerena{
		ID: uuid.NewSHA1(uuid.NameSpaceURL, []byte("gocop/"+izvor)).String(), UserID: k.ID, Ime: k.Ime, Sektor: deps.Sektor, AreaID: area,
		Vrsta: vrstaPrijave(o.Vrsta), Naslov: strings.TrimSpace(o.Naslov), Opis: strings.TrimSpace(o.Opis), Datum: kad,
		Element: strings.TrimSpace(o.Element), Vaznost: strings.TrimSpace(o.Vaznost), Cvor: deps.Cvor,
		Izvor: izvor, Rekonstrukcija: true, CreatedAt: kad, UpdatedAt: kad,
	}
	if p.Naslov == "" {
		p.Naslov = "(bez naslova)"
	}
	if o.Cuvarnica != "" && o.Cuvarnica != "-" {
		p.Objekt = "čuvarnica " + strings.Title(strings.ToLower(o.Cuvarnica))
	}
	return p
}

// vodotokObavijesti je naziv vode: obavijest je navodi oznakom iz zbirke
// vode ili kao zapis s nazivom
func vodotokObavijesti(raw json.RawMessage, vode map[int]string) string {
	if len(raw) == 0 {
		return ""
	}
	var id int
	if json.Unmarshal(raw, &id) == nil {
		return vode[id]
	}
	var v struct {
		Voda string `json:"voda"`
	}
	_ = json.Unmarshal(raw, &v) // nečitljiv zapis ostavlja prijavu bez vode
	return v.Voda
}

// tockaObavijesti je mjesto obavijesti kad je ucrtano u Hrvatskoj; točka
// izvan nje je omaška pri ucrtavanju i ne uzima se
func tockaObavijesti(o obavijestRow) (lat, lon *float64) {
	if o.Lokacija == nil {
		return nil, nil
	}
	if x, y, ok := prvaTocka(o.Lokacija.Coordinates); ok && uHrvatskoj(y, x) {
		return &y, &x
	}
	return nil, nil
}

// uHrvatskoj je grub okvir oko Hrvatske, dovoljan da odbaci zamijenjene ili
// izmišljene koordinate
func uHrvatskoj(lat, lon float64) bool {
	return lat > 40 && lat < 50 && lon > 13 && lon < 20
}

// statusObavijesti prevodi stanje zapisa u Directusu
func statusObavijesti(s string) string {
	switch strings.ToLower(s) {
	case "objavljeno":
		return models.PrijavaObjavljena
	case "arhivirano":
		return models.PrijavaRijesena
	}
	return models.PrijavaNacrt
}

// numeriraj daje objavljenoj prijavi broj u godini, nastavljajući na brojeve
// koje sektor već ima; nacrt ostaje bez broja
func (u *uvozPrijava) numeriraj(ctx context.Context, p *models.PrijavaSTerena) error {
	if p.Status == models.PrijavaNacrt {
		return nil
	}
	t := p.Datum
	p.ObjavljenoAt = &t
	p.Godina = p.Datum.Year()
	if u.brojevi[p.Godina] == 0 {
		n, err := u.deps.Prijave.SljedeciBroj(ctx, u.deps.Sektor, p.Godina)
		if err != nil {
			return err
		}
		u.brojevi[p.Godina] = n
	}
	p.Broj = u.brojevi[p.Godina]
	u.brojevi[p.Godina]++
	return nil
}

// prebroji broji što bi suhi prolaz prenio, bez preuzimanja
func (u *uvozPrijava) prebroji(o obavijestRow) {
	u.rep.Slika += len(slikeObavijesti(o))
	if o.Datoteka != "" {
		u.rep.Skenova++
	}
	u.rep.Upisano++
}

// slikeObavijesti su oznake fotografija obavijesti u Directusu, redom
func slikeObavijesti(o obavijestRow) []string {
	var out []string
	for _, sid := range []string{o.Slika1, o.Slika2, o.Slika3} {
		if sid != "" {
			out = append(out, sid)
		}
	}
	return out
}

// prenesi preuzima fotografije i sken obavijesti i sprema prijavu
func (u *uvozPrijava) prenesi(ctx context.Context, o obavijestRow, izvor string, p *models.PrijavaSTerena) error {
	slike := u.fotografije(ctx, o, izvor)
	staviSlike(p, slike)
	u.rep.Slika += len(slike)
	sken := u.sken(ctx, o)
	if err := u.spremi(ctx, o.ID, p, slike, sken); err != nil {
		return err
	}
	u.rep.Upisano++
	return nil
}

// slikaUvoza je fotografija obavijesti, opisana za prijavu, sa smanjenim
// bajtovima
type slikaUvoza struct {
	models.SlikaPrijave
	jpg []byte
}

// fotografije preuzima fotografije obavijesti redom kojim ih evidencija
// navodi; fotografija koju Directus ne da ili koja nije slika samo se
// zapiše
func (u *uvozPrijava) fotografije(ctx context.Context, o obavijestRow, izvor string) []slikaUvoza {
	if u.deps.Datoteka == nil {
		return nil
	}
	var out []slikaUvoza
	for _, sid := range slikeObavijesti(o) {
		if s, ok := u.fotografija(ctx, o.ID, izvor, sid); ok {
			s.Naziv = fmt.Sprintf("fotografija %d", len(out)+1)
			out = append(out, s)
		}
	}
	return out
}

// fotografija preuzima i smanjuje jednu fotografiju. Uzima se izvorna
// datoteka, jer samo ona nosi zapis fotoaparata (kad je snimljeno, čime);
// Directusova smanjena ga briše. Tek kad izvorne nema, uzme se smanjena.
func (u *uvozPrijava) fotografija(ctx context.Context, oid int, izvor, sid string) (slikaUvoza, bool) {
	b, err := u.deps.Datoteka(ctx, sid, "")
	if err != nil {
		b, err = u.deps.Datoteka(ctx, sid, "width=1600&height=1600&fit=inside&quality=82&format=jpg")
	}
	if err != nil {
		u.logf("  obavijest %d: slika %s: %v", oid, sid, err)
		return slikaUvoza{}, false
	}
	jpg, w, h, err := slike.Smanji(b)
	if err != nil {
		u.logf("  obavijest %d: slika %s: %v", oid, sid, err)
		return slikaUvoza{}, false
	}
	sl := models.SlikaPrijave{ID: oznakaSlike(izvor, sid), Sirina: w, Visina: h, Bajtova: len(jpg), Sadrzaj: sadrzaj.Otisak(jpg)}
	e := slike.Procitaj(b)
	if !e.Snimljeno.IsZero() {
		k := e.Snimljeno
		sl.Snimljeno = &k
	}
	sl.Lat, sl.Lon, sl.Uredjaj = e.Lat, e.Lon, e.Uredjaj
	otisak := sha256.Sum256(b)
	sl.IzvornoBajtova, sl.Otisak = len(b), hex.EncodeToString(otisak[:])
	return slikaUvoza{SlikaPrijave: sl, jpg: jpg}, true
}

// oznakaSlike je oznaka fotografije obavijesti, izvedena iz izvora, pa je
// ista pri svakom uvozu
func oznakaSlike(izvor, sid string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("gocop/"+izvor+"/"+sid)).String()
}

// staviSlike stavlja fotografije na prijavu; prijava bez ucrtanog mjesta
// uzme mjesto prve fotografije koja ga zna
func staviSlike(p *models.PrijavaSTerena, slike []slikaUvoza) {
	for _, s := range slike {
		p.Slike = append(p.Slike, s.SlikaPrijave)
		if !p.ImaKoordinate() && s.ImaPolozaj() {
			lat, lon := s.Lat, s.Lon
			p.Latitude, p.Longitude = &lat, &lon
		}
	}
}

// sken preuzima sken potpisanog ispisa kad ga ima kamo spremiti ili kad
// PDF iz podataka nije moguć; inače ostaje u staroj evidenciji. Sken koji
// se ne da preuzeti ili nije PDF samo se zapiše.
func (u *uvozPrijava) sken(ctx context.Context, o obavijestRow) []byte {
	if !u.trebaSken(o) {
		return nil
	}
	b, err := u.deps.Datoteka(ctx, o.Datoteka, "")
	if err != nil {
		u.logf("  obavijest %d: datoteka %s: %v", o.ID, o.Datoteka, err)
		return nil
	}
	if !jePDF(b) {
		u.logf("  obavijest %d: datoteka %s nije PDF, preskačem", o.ID, o.Datoteka)
		return nil
	}
	u.rep.Skenova++
	return b
}

// trebaSken javlja treba li obavijesti preuzeti sken
func (u *uvozPrijava) trebaSken(o obavijestRow) bool {
	return o.Datoteka != "" && u.deps.Datoteka != nil && (u.deps.SpremiSken != nil || u.deps.IzradiPDF == nil)
}

// jePDF javlja počinju li bajtovi kao PDF
func jePDF(b []byte) bool {
	return len(b) > 8 && string(b[:4]) == "%PDF"
}

// spremi sprema sken, pa fotografije i izvornik s prijavom. Prijava ide
// zadnja, jer po njoj ponovni uvoz zna da je obavijest prenesena: uvoz koji
// stane prije nje ponovni uvoz dovrši (Odluke (13. 10. 2026.)).
func (u *uvozPrijava) spremi(ctx context.Context, oid int, p *models.PrijavaSTerena, slike []slikaUvoza, sken []byte) error {
	if err := u.spremiSken(p, sken); err != nil {
		return fmt.Errorf("obavijest %d: sken: %w", oid, err)
	}
	izvornik := u.izvornik(p, slike, sken)
	h := sha256.Sum256(izvornik)
	if err := u.deps.Prijave.SpremiPrenesenu(ctx, p, u.zaSpremiste(p, slike), izvornik, hex.EncodeToString(h[:])); err != nil {
		return fmt.Errorf("obavijest %d: %w", oid, err)
	}
	return nil
}

// zaSpremiste su fotografije koje se čuvaju uz prijavu, po oznaci; uz
// SlikeOdmah objavljena prijava ih ne čuva, jer ih nosi izvornik
func (u *uvozPrijava) zaSpremiste(p *models.PrijavaSTerena, slike []slikaUvoza) map[string][]byte {
	if u.deps.SlikeOdmah && p.Status != models.PrijavaNacrt {
		return nil
	}
	return bajtoviSlika(slike)
}

// bajtoviSlika su smanjene fotografije po oznaci
func bajtoviSlika(slike []slikaUvoza) map[string][]byte {
	m := map[string][]byte{}
	for _, s := range slike {
		m[s.ID] = s.jpg
	}
	return m
}

// spremiSken sprema sken uz bazu kad ga ima kamo spremiti; sken ostaje
// lokalni prilog, izvornik u knjizi je PDF iz podataka
func (u *uvozPrijava) spremiSken(p *models.PrijavaSTerena, sken []byte) error {
	if len(sken) == 0 || u.deps.SpremiSken == nil {
		return nil
	}
	naziv, err := u.deps.SpremiSken(p, sken)
	if err != nil {
		return err
	}
	p.Sken = naziv
	return nil
}

// izvornik je PDF koji ide u knjigu: sken kad ga nema kamo drugamo, inače
// PDF iz podataka za objavljenu prijavu; nacrt izvornika nema
func (u *uvozPrijava) izvornik(p *models.PrijavaSTerena, slike []slikaUvoza, sken []byte) []byte {
	if len(sken) > 0 && u.deps.SpremiSken == nil {
		return sken
	}
	if u.deps.IzradiPDF == nil || p.Status == models.PrijavaNacrt {
		return nil
	}
	return u.deps.IzradiPDF(p, bajtoviSlika(slike))
}

// dopuni dodaje prenesenoj prijavi fotografije koje Directus ranije nije dao
// i iznova izradi izvornik iz podataka, jer izvornik nosi fotografije.
// Zapis u zapisniku nije dovoljan, jer bi ponovni uvoz prenesenu prijavu
// inače preskočio i fotografija ne bi stigla nikad (Odluke (13. 10. 2026.)).
// Preuzimaju se sve fotografije: uz SlikeOdmah prijava svoje ne čuva, a novi
// izvornik mora nositi i njih.
func (u *uvozPrijava) dopuni(ctx context.Context, o obavijestRow, izvor string, p *models.PrijavaSTerena) error {
	u.rep.Postoje++
	if !u.trebaDopunu(o, izvor, p) {
		return nil
	}
	if razlog := razlogBezDopune(p); razlog != "" {
		u.logf(razlog, o.ID)
		return nil
	}
	slike := u.fotografije(ctx, o, izvor)
	if !dopunjuje(p, slike) {
		return nil
	}
	dopunjena := *p
	dopunjena.Slike = nil
	staviSlike(&dopunjena, slike)
	if err := u.spremi(ctx, o.ID, &dopunjena, slike, nil); err != nil {
		return err
	}
	n := len(slike) - len(p.Slike)
	u.rep.Slika += n
	u.rep.Dopunjeno++
	u.logf(models.UzBrojHR(n,
		"  obavijest %d: dodana je %d fotografija koja ranije nije stigla",
		"  obavijest %d: dodane su %d fotografije koje ranije nisu stigle",
		"  obavijest %d: dodano je %d fotografija koje ranije nisu stigle"), o.ID, n)
	return nil
}

// trebaDopunu javlja nedostaje li prenesenoj prijavi fotografija koju bi
// uvoz mogao preuzeti; suhi prolaz ništa ne preuzima
func (u *uvozPrijava) trebaDopunu(o obavijestRow, izvor string, p *models.PrijavaSTerena) bool {
	return !u.deps.DryRun && u.deps.Datoteka != nil && nedostajeSlika(o, izvor, p)
}

// nedostajeSlika javlja navodi li obavijest fotografiju koje na prijavi nema
func nedostajeSlika(o obavijestRow, izvor string, p *models.PrijavaSTerena) bool {
	ima := map[string]bool{}
	for _, sl := range p.Slike {
		ima[sl.ID] = true
	}
	for _, sid := range slikeObavijesti(o) {
		if !ima[oznakaSlike(izvor, sid)] {
			return true
		}
	}
	return false
}

// razlogBezDopune je poruka zapisnika kad uvoz prijavi ne smije dodati
// fotografije, a prazno kad smije. Nacrtu je vodočuvar fotografiju možda sam
// maknuo, a urudžbiranoj prijavi novi izvornik izgubio bi bilješku urudžbe.
func razlogBezDopune(p *models.PrijavaSTerena) string {
	switch {
	case p.Status == models.PrijavaNacrt:
		return "  obavijest %d: prijava je nacrt, pa joj uvoz ne dodaje fotografije koje ranije nisu stigle. Vodočuvar ih može dodati sam."
	case p.Klasa != "" || p.Urbroj != "":
		return "  obavijest %d: prijava je urudžbirana, pa joj uvoz ne dodaje fotografije koje ranije nisu stigle. Novi izvornik izgubio bi bilješku urudžbe."
	}
	return ""
}

// dopunjuje javlja donose li preuzete fotografije nešto novo, a da prijavi
// ništa ne oduzmu: svaka fotografija koju prijava već ima mora stići i sad,
// inače bi novi izvornik bio bez nje
func dopunjuje(p *models.PrijavaSTerena, slike []slikaUvoza) bool {
	stigle := map[string]bool{}
	for _, s := range slike {
		stigle[s.ID] = true
	}
	for _, sl := range p.Slike {
		if !stigle[sl.ID] {
			return false
		}
	}
	return len(slike) > len(p.Slike)
}
