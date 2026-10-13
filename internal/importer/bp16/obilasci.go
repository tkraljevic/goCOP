package bp16

// Uvoz obilazaka terena iz zbirke evidencije_obilaska u Directusu VGI
// Baranja u zadatke vodočuvara i njihove dnevne listove.
//
// Obilazak je zadatak: netko ga je zadao, vodočuvar ga je dobio za određeni
// dan, i ponegdje je upisao što je zatekao. Dvije trećine nemaju taj upis, i
// to nije propust vodočuvara: aplikacija u kojoj su zadatke dobivali nije
// upisa nema. Upisi o izvršenju vodili su se, ali aplikacija nije bila
// službena, pa ih dio vodočuvara nije radio. Zato takav zadatak ne stoji kao
// neobavljen nego kao zadatak bez upisanog odgovora, s objašnjenjem zašto.
//
// Listovi koji iz ovoga nastanu su rekonstrukcija: nisu vođeni u goCOP-u,
// nemaju potpis ni ovjeru i ne uzimaju brojeve iz vodočuvareve knjige.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/sadrzaj"
	"gocop/internal/slike"
)

// ObjasnjenjeBezOdgovora stoji uz zadatak kojem u ranijoj evidenciji nema
// odgovora; bez njega bi prazan zadatak izgledao kao neobavljen posao
const ObjasnjenjeBezOdgovora = "Odgovor nije upisan. Upisi o izvršenju vodili su se u ranijoj evidenciji, ali ta aplikacija nije bila službena, pa ih dio vodočuvara nije upisivao. Iz izostanka upisa ne slijedi da posao nije obavljen."

// obilazakRow je zapis zbirke evidencije_obilaska
type obilazakRow struct {
	ID          int             `json:"id_obilasci"`
	Status      string          `json:"status"`
	DateCreated string          `json:"date_created"`
	UserCreated string          `json:"user_created"`
	Dodijeljeno string          `json:"dodijeljeno"`
	Datum       string          `json:"datum"`
	VrijemeOd   string          `json:"vrijeme_od"`
	VrijemeDo   string          `json:"vrijeme_do"`
	Opis        string          `json:"opis_aktivnosti"`
	Napomena    string          `json:"napomena"`
	Udaljenost  json.RawMessage `json:"udaljenost"`
	Lokacija    json.RawMessage `json:"lokacija"`
	Foto1       string          `json:"foto_1"`
	Foto2       string          `json:"foto_2"`
	Foto3       string          `json:"foto_3"`
}

// ObilasciRepo je što uvoz treba od dnevnika vodočuvara
type ObilasciRepo interface {
	ZadatakPoIzvoru(ctx context.Context, izvor string) (*models.Zadatak, error)
	SaveZadatak(ctx context.Context, z *models.Zadatak) error
	ZaDan(ctx context.Context, userID string, dan time.Time) (*models.VodocuvarskiList, error)
	Save(ctx context.Context, l *models.VodocuvarskiList) error
	SavePrilog(ctx context.Context, listID, prilogID string, b []byte) error
}

// ObilasciDeps je što uvoz treba od programa
type ObilasciDeps struct {
	Vodocuvar ObilasciRepo
	Korisnici map[string]KorisnikUvoza // po imenu i prezimenu kako stoji u Directusu
	Podrucja  map[string]int           // područje po vodočuvaru (ime), 0 = iz sektora
	Sektor    string
	Cvor      string
	Datoteka  func(ctx context.Context, id, upit string) ([]byte, error) // Directus /assets
	// SamoArhivirane preskače obilaske koji su još u planu: oni su živi
	// zadaci u staroj aplikaciji, a ne povijest
	SamoArhivirane bool
	// Meteo daje vremenske prilike za dan iz arhive (Open-Meteo) kad ih
	// stara evidencija za taj dan nema; prazno kad se ne može dohvatiti
	Meteo  func(ctx context.Context, dan time.Time) string
	DryRun bool
	Log    func(string, ...any)
}

// ObilasciReport je izvješće uvoza
type ObilasciReport struct {
	Ukupno, Upisano, Postoje, Preskoceno, BezKorisnika  int
	Listova, SOdgovorom, BezOdgovora, Slika, SObuhvatom int
	VrijemeIzEvidencije, VrijemeIzArhive                int
	Dopunjeno                                           int // preneseni obilasci kojima su stigle fotografije, broje se i pod Postoje
	Nepoznati                                           map[string]int
	DryRun                                              bool
}

// Summary sažima izvješće u jedan redak
func (r ObilasciReport) Summary() string {
	return fmt.Sprintf("%d obilazaka: %d upisano, %d već postoji, %d preskočeno, %d bez poznatog vodočuvara; %d listova, %d s odgovorom, %d bez odgovora, %d fotografija, %d s ucrtanim obuhvatom",
		r.Ukupno, r.Upisano, r.Postoje, r.Preskoceno, r.BezKorisnika, r.Listova, r.SOdgovorom, r.BezOdgovora, r.Slika, r.SObuhvatom) +
		fmt.Sprintf("; vrijeme: %d iz evidencije, %d iz arhive", r.VrijemeIzEvidencije, r.VrijemeIzArhive)
}

// RunObilasci prenosi obilaske u zadatke i rekonstruirane dnevne listove.
// Ponovni uvoz preskače ono što je već preneseno, osim fotografija koje
// ranije nisu stigle, pa se smije ponavljati dok se stara evidencija ne
// ugasi.
func RunObilasci(ctx context.Context, src Source, deps ObilasciDeps) (ObilasciReport, error) {
	rep := ObilasciReport{Nepoznati: map[string]int{}, DryRun: deps.DryRun}
	u := &uvozObilazaka{deps: deps, rep: &rep, logf: zapisnik(deps.Log)}
	var err error
	if u.imena, err = imenaKorisnika(ctx, src); err != nil {
		return rep, err
	}
	u.vrijeme = vrijemeZaList(ctx, src, u.logf)
	redovi, err := obilasciPoDanu(ctx, src)
	if err != nil {
		return rep, err
	}
	rep.Ukupno = len(redovi)
	for _, o := range redovi {
		if err := u.uvezi(ctx, o); err != nil {
			return rep, fmt.Errorf("obilazak %d: %w", o.ID, err)
		}
	}
	return rep, nil
}

// uvozObilazaka je stanje jednog uvoza obilazaka: što uvoz treba od
// programa, izvješće i ono što je pročitao prije prvog obilaska
type uvozObilazaka struct {
	deps    ObilasciDeps
	rep     *ObilasciReport
	logf    func(string, ...any)
	imena   map[string]string      // korisnici Directusa po oznaci
	vrijeme map[string]vrijemeDana // vrijeme i vodostaji iz stare evidencije, po danu
}

// vrijemeZaList čita vrijeme i vodostaje kakve je vodočuvar tog dana imao
// pred sobom, po danu; od dva zapisa istog dana vrijedi prvi. Kad se zbirka
// ne da pročitati, to se zapiše, a nov list vrijeme uzima iz arhive.
func vrijemeZaList(ctx context.Context, src Source, logf func(string, ...any)) map[string]vrijemeDana {
	vrijeme := map[string]vrijemeDana{}
	raw, err := src.Items(ctx, "vrijeme_za_list")
	if err != nil {
		logf("vrijeme za list: %v", err)
		return vrijeme
	}
	for _, r := range raw {
		var v struct {
			Datum     string `json:"datum"`
			Opisno    string `json:"vrijeme_opisno"`
			Vodostaji string `json:"vodostaji_opisno"`
		}
		if json.Unmarshal(r, &v) != nil || v.Datum == "" {
			continue
		}
		if _, ima := vrijeme[v.Datum]; !ima {
			vrijeme[v.Datum] = vrijemeDana{Prilike: sredi(v.Opisno), Vodostaji: sredi(v.Vodostaji)}
		}
	}
	return vrijeme
}

// obilasciPoDanu čita zbirku obilazaka poredanu po danu, a unutar dana po
// broju obilaska, jer tim redom zadaci stoje na listu dana
func obilasciPoDanu(ctx context.Context, src Source) ([]obilazakRow, error) {
	raw, err := src.Items(ctx, "evidencije_obilaska")
	if err != nil {
		return nil, err
	}
	redovi := make([]obilazakRow, 0, len(raw))
	for _, r := range raw {
		var o obilazakRow
		if err := json.Unmarshal(r, &o); err != nil {
			return nil, fmt.Errorf("obilazak: %w", err)
		}
		redovi = append(redovi, o)
	}
	sort.SliceStable(redovi, func(i, j int) bool {
		if redovi[i].Datum != redovi[j].Datum {
			return redovi[i].Datum < redovi[j].Datum
		}
		return redovi[i].ID < redovi[j].ID
	})
	return redovi, nil
}

// uvezi prenosi jedan obilazak. Prenesenom dopuni fotografije koje mu
// nedostaju, a preskače obilazak koji je još u planu i onaj kojem se ne zna
// vodočuvar ili dan.
func (u *uvozObilazaka) uvezi(ctx context.Context, o obilazakRow) error {
	izvor := fmt.Sprintf("bp16:evidencije_obilaska:%d", o.ID)
	prenesen, err := u.deps.Vodocuvar.ZadatakPoIzvoru(ctx, izvor)
	if err != nil {
		return fmt.Errorf("čitanje prenesenog zadatka: %w", err)
	}
	if prenesen != nil {
		return u.dopuni(ctx, o, izvor, prenesen)
	}
	k, dan, ok := u.osobaIDan(o)
	if !ok {
		return nil
	}
	return u.prenesi(ctx, o, k, u.zadatak(o, izvor, k, dan))
}

// osobaIDan nalazi vodočuvara i dan obilaska. Obilazak koji je još u planu
// (kad se uvoze samo arhivirani), obilazak nepoznatog vodočuvara i obilazak
// nečitljivog dana izvješće broji, a uvoz ih preskače.
func (u *uvozObilazaka) osobaIDan(o obilazakRow) (KorisnikUvoza, time.Time, bool) {
	if u.deps.SamoArhivirane && strings.ToLower(strings.TrimSpace(o.Status)) != "arhivirano" {
		u.rep.Preskoceno++
		return KorisnikUvoza{}, time.Time{}, false
	}
	ime := u.imena[o.Dodijeljeno]
	k, ok := u.deps.Korisnici[ime]
	if !ok {
		u.rep.BezKorisnika++
		u.rep.Nepoznati[ime]++
		return k, time.Time{}, false
	}
	dan, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(o.Datum), models.Zagreb)
	if err != nil {
		u.rep.Nepoznati["datum "+o.Datum]++
		return k, time.Time{}, false
	}
	return k, dan, true
}

// zadatak slaže zadatak iz obilaska; oznaka je izvedena iz izvora, pa je
// ista pri svakom uvozu
func (u *uvozObilazaka) zadatak(o obilazakRow, izvor string, k KorisnikUvoza, dan time.Time) *models.Zadatak {
	area, sektor := u.podrucjeISektor(o, k)
	z := &models.Zadatak{
		ID:     uuid.NewSHA1(uuid.NameSpaceURL, []byte("gocop/"+izvor)).String(),
		UserID: k.ID, Sektor: sektor, AreaID: area,
		Tekst: sredi(o.Opis), ZadaoID: "", Zadao: u.imena[o.UserCreated], ZadanoAt: zadanoAt(o, dan), Za: dan,
		Izvor: izvor, Cvor: u.deps.Cvor,
		Od: sat(o.VrijemeOd), Do: sat(o.VrijemeDo), Udaljenost: broj(o.Udaljenost),
	}
	u.odgovor(z, o, dan)
	if g := geometrija(o.Lokacija); g != "" {
		z.Obuhvat = g
		u.rep.SObuhvatom++
	}
	return z
}

// podrucjeISektor su branjeno područje i sektor zadatka. Područje je iz
// popisa područja po vodočuvaru, a kad ga ondje nema ili je nula, iz
// zaduženja. Sektor je iz zaduženja, a bez njega sektor uvoza.
func (u *uvozObilazaka) podrucjeISektor(o obilazakRow, k KorisnikUvoza) (int, string) {
	area := k.AreaID
	if a, ok := u.deps.Podrucja[u.imena[o.Dodijeljeno]]; ok && a > 0 {
		area = a
	}
	sektor := k.Sektor
	if sektor == "" {
		sektor = u.deps.Sektor
	}
	return area, sektor
}

// zadanoAt je kad je obilazak zadan; kad se vrijeme upisa ne da pročitati,
// zadan je na dan obilaska
func zadanoAt(o obilazakRow, dan time.Time) time.Time {
	if t, err := time.Parse(time.RFC3339, o.DateCreated); err == nil {
		return t.In(models.Zagreb)
	}
	return dan
}

// odgovor upisuje u zadatak ono što je vodočuvar zatekao. Bez upisa zadatak
// ne stoji kao neobavljen, nego kao zadatak bez odgovora, s objašnjenjem
// zašto ga nema.
func (u *uvozObilazaka) odgovor(z *models.Zadatak, o obilazakRow, dan time.Time) {
	odgovor := sredi(o.Napomena)
	if odgovor == "" {
		z.Status, z.Obavljeno = models.ZadatakBezOdgovora, ObjasnjenjeBezOdgovora
		u.rep.BezOdgovora++
		return
	}
	z.Status, z.Obavljeno = models.ZadatakObavljen, odgovor
	kad := dan
	z.ObavljenoAt = &kad
	u.rep.SOdgovorom++
}

// prenesi upisuje nov obilazak: fotografije, list dana i zadatak. Suhi
// prolaz samo broji.
func (u *uvozObilazaka) prenesi(ctx context.Context, o obilazakRow, k KorisnikUvoza, z *models.Zadatak) error {
	// fotografije obilaska: smanjene, u spremište sadržaja, kao prilozi lista
	prilozi, bajtovi := fotografijeObilaska(ctx, u.deps, o, z.Izvor, z.ID, nil, u.logf)
	u.rep.Slika += len(prilozi)
	if u.deps.DryRun {
		u.rep.Upisano++
		return nil
	}
	l, err := u.listDana(ctx, o, k, z)
	if err != nil {
		return err
	}
	if err := upisiObilazak(ctx, u.deps.Vodocuvar, l, z, prilozi, bajtovi); err != nil {
		return err
	}
	u.rep.Upisano++
	return nil
}

// listDana je list vodočuvara za dan obilaska: postojeći, a bez njega nov,
// označen kao rekonstrukcija
func (u *uvozObilazaka) listDana(ctx context.Context, o obilazakRow, k KorisnikUvoza, z *models.Zadatak) (*models.VodocuvarskiList, error) {
	l, err := u.deps.Vodocuvar.ZaDan(ctx, k.ID, z.Za)
	if err != nil {
		return nil, fmt.Errorf("list dana: %w", err)
	}
	if l != nil {
		return l, nil
	}
	l = &models.VodocuvarskiList{
		UserID: k.ID, Ime: k.Ime, Sektor: z.Sektor, AreaID: z.AreaID, Datum: z.Za,
		Rekonstrukcija: true, Izvor: "bp16:evidencije_obilaska",
		Cvor: u.deps.Cvor,
	}
	u.vrijemeNaList(ctx, l, o.Datum)
	u.rep.Listova++
	return l, nil
}

// vrijemeNaList upisuje na nov list vrijeme i vodostaje dana iz stare
// evidencije, a kad vremena ondje nema, vrijeme iz arhive
func (u *uvozObilazaka) vrijemeNaList(ctx context.Context, l *models.VodocuvarskiList, datum string) {
	if v, ima := u.vrijeme[datum]; ima && v.Prilike != "" {
		l.Prilike, l.Ocitanja = v.Prilike, v.Vodostaji
		u.rep.VrijemeIzEvidencije++
		return
	}
	if u.deps.Meteo == nil {
		return
	}
	if p := u.deps.Meteo(ctx, l.Datum); p != "" {
		l.Prilike = p
		u.rep.VrijemeIzArhive++
	}
}

// upisiObilazak stavlja zadatak i fotografije obilaska na list dana i
// sprema fotografije, list i zadatak, tim redom. Fotografije idu prve, pa
// list ne nosi opis fotografije koje nema: opis jednom upisan na prenesen
// list ostaje na njemu i na drugim čvorovima. Zadatak ide zadnji jer po
// njemu ponovni uvoz zna da je obilazak prenesen: kad spremanje stane
// prije njega, ponovni uvoz obilazak dovrši. Zadatak i fotografije koje je
// takav prekinut uvoz već stavio na list zamijene se, pa ih na listu ne bude
// dvaput.
func upisiObilazak(ctx context.Context, repo ObilasciRepo, l *models.VodocuvarskiList, z *models.Zadatak, prilozi []models.PrilogLista, bajtovi [][]byte) error {
	if err := spremiFotografije(ctx, repo, oznakaLista(l), prilozi, bajtovi); err != nil {
		return err
	}
	l.Zadaci = zamijeniIliDodaj(l.Zadaci, models.ZadatakNaListu{
		ID: z.ID, Tekst: z.Tekst, Zadao: z.Zadao, ZadanoAt: z.ZadanoAt, Status: z.Status,
		Obavljeno: z.Obavljeno, Od: z.Od, Do: z.Do, Udaljenost: z.Udaljenost, ImaObuhvat: z.Obuhvat != "",
	}, func(s models.ZadatakNaListu) string { return s.ID })
	staviFotografije(l, prilozi)
	if err := repo.Save(ctx, l); err != nil {
		return fmt.Errorf("list: %w", err)
	}
	z.ListID = l.ID
	if err := repo.SaveZadatak(ctx, z); err != nil {
		return fmt.Errorf("zadatak: %w", err)
	}
	return nil
}

// staviFotografije stavlja fotografije obilaska na list; fotografiju koju
// list već ima zamijeni, pa je na listu ne bude dvaput
func staviFotografije(l *models.VodocuvarskiList, prilozi []models.PrilogLista) {
	for _, pr := range prilozi {
		l.Prilozi = zamijeniIliDodaj(l.Prilozi, pr, func(p models.PrilogLista) string { return p.ID })
	}
}

// dopuni dodaje na list prenesenog obilaska fotografije koje
// Directus ranije nije dao. Zapis u zapisniku nije dovoljan, jer bi ponovni
// uvoz preneseni obilazak inače preskočio i fotografija ne bi stigla nikad
// (Odluke (13. 10. 2026.)). Zadatak ostaje kakav jest.
func (u *uvozObilazaka) dopuni(ctx context.Context, o obilazakRow, izvor string, z *models.Zadatak) error {
	u.rep.Postoje++
	l, err := u.listObilaska(ctx, o, z)
	if err != nil || l == nil {
		return err
	}
	prilozi, bajtovi := fotografijeObilaska(ctx, u.deps, o, izvor, z.ID, priloziZadatka(l, z.ID), u.logf)
	if len(prilozi) == 0 {
		return nil
	}
	if err := dodajFotografije(ctx, u.deps.Vodocuvar, l, prilozi, bajtovi); err != nil {
		return err
	}
	u.rep.Slika += len(prilozi)
	u.rep.Dopunjeno++
	u.logf(models.UzBrojHR(len(prilozi),
		"  obilazak %d: dodana je %d fotografija koja ranije nije stigla",
		"  obilazak %d: dodane su %d fotografije koje ranije nisu stigle",
		"  obilazak %d: dodano je %d fotografija koje ranije nisu stigle"), o.ID, len(prilozi))
	return nil
}

// listObilaska je list dana na koji je obilazak prenesen. Čita se samo kad
// obilazak ima fotografija i kad ih ima odakle preuzeti; suhi prolaz ga ne
// čita, jer ništa ne dopunjuje.
func (u *uvozObilazaka) listObilaska(ctx context.Context, o obilazakRow, z *models.Zadatak) (*models.VodocuvarskiList, error) {
	if u.deps.DryRun || u.deps.Datoteka == nil || o.Foto1+o.Foto2+o.Foto3 == "" {
		return nil, nil
	}
	l, err := u.deps.Vodocuvar.ZaDan(ctx, z.UserID, z.Za)
	if err != nil {
		return nil, fmt.Errorf("list dana: %w", err)
	}
	return l, nil
}

// priloziZadatka su oznake fotografija koje zadatak već ima na listu
func priloziZadatka(l *models.VodocuvarskiList, zadatakID string) map[string]bool {
	ima := map[string]bool{}
	for _, pr := range l.Prilozi {
		if pr.ZadatakID == zadatakID {
			ima[pr.ID] = true
		}
	}
	return ima
}

// dodajFotografije sprema fotografije, a tek onda ih stavlja na list i
// sprema list, kao i pri prvom upisu obilaska: list ne nosi opis fotografije
// koje nema
func dodajFotografije(ctx context.Context, repo ObilasciRepo, l *models.VodocuvarskiList, prilozi []models.PrilogLista, bajtovi [][]byte) error {
	if err := spremiFotografije(ctx, repo, oznakaLista(l), prilozi, bajtovi); err != nil {
		return err
	}
	staviFotografije(l, prilozi)
	if err := repo.Save(ctx, l); err != nil {
		return fmt.Errorf("list: %w", err)
	}
	return nil
}

// fotografijeObilaska preuzima i smanjuje fotografije obilaska koje
// zadatak na listu još nema (ima, po oznaci priloga); broj u nazivu
// nastavlja se na fotografije koje već ima. Fotografija koju Directus ne da
// ili koja nije slika samo se zapiše.
func fotografijeObilaska(ctx context.Context, deps ObilasciDeps, o obilazakRow, izvor, zadatakID string, ima map[string]bool, logf func(string, ...any)) ([]models.PrilogLista, [][]byte) {
	if deps.Datoteka == nil {
		return nil, nil
	}
	var prilozi []models.PrilogLista
	var bajtovi [][]byte
	for _, fid := range []string{o.Foto1, o.Foto2, o.Foto3} {
		id := uuid.NewSHA1(uuid.NameSpaceURL, []byte("gocop/"+izvor+"/"+fid)).String()
		if fid == "" || ima[id] {
			continue
		}
		jpg, w, h, ok := smanjenaFotografija(ctx, deps.Datoteka, o.ID, fid, logf)
		if !ok {
			continue
		}
		prilozi = append(prilozi, models.PrilogLista{
			ID:    id,
			Naziv: fmt.Sprintf("fotografija %d", len(ima)+len(prilozi)+1),
			Vrsta: "image/jpeg", Bajtova: len(jpg), Sirina: w, Visina: h,
			Sadrzaj: sadrzaj.Otisak(jpg), ZadatakID: zadatakID,
		})
		bajtovi = append(bajtovi, jpg)
	}
	return prilozi, bajtovi
}

// smanjenaFotografija preuzima izvornu fotografiju obilaska i smanjuje je;
// greška se zapiše, a fotografija preskoči
func smanjenaFotografija(ctx context.Context, datoteka func(ctx context.Context, id, upit string) ([]byte, error), oid int, fid string, logf func(string, ...any)) ([]byte, int, int, bool) {
	b, err := datoteka(ctx, fid, "")
	if err != nil {
		logf("  obilazak %d: fotografija %s: %v", oid, fid, err)
		return nil, 0, 0, false
	}
	jpg, w, h, err := slike.Smanji(b)
	if err != nil {
		logf("  obilazak %d: fotografija %s: %v", oid, fid, err)
		return nil, 0, 0, false
	}
	return jpg, w, h, true
}

// spremiFotografije sprema fotografije obilaska u spremište sadržaja, uz list
func spremiFotografije(ctx context.Context, repo ObilasciRepo, listID string, prilozi []models.PrilogLista, bajtovi [][]byte) error {
	for i, pr := range prilozi {
		if err := repo.SavePrilog(ctx, listID, pr.ID, bajtovi[i]); err != nil {
			return fmt.Errorf("prilog: %w", err)
		}
	}
	return nil
}

// oznakaLista je oznaka lista u dnevniku; nov list je dobiva pri prvom
// spremanju, iz osobe i dana
func oznakaLista(l *models.VodocuvarskiList) string {
	if l.ID != "" {
		return l.ID
	}
	return models.IDListaVodocuvara(l.UserID, l.Datum)
}

// zamijeniIliDodaj dodaje stavku na kraj popisa, a stavku iste oznake
// zamijeni na njezinu mjestu
func zamijeniIliDodaj[T any](popis []T, nova T, oznaka func(T) string) []T {
	for i := range popis {
		if oznaka(popis[i]) == oznaka(nova) {
			popis[i] = nova
			return popis
		}
	}
	return append(popis, nova)
}

// vrijemeDana je ono što je stara evidencija imala o danu
type vrijemeDana struct{ Prilike, Vodostaji string }

// sredi miče višak razmaka i tabulatora iz teksta stare evidencije; razmaci
// se sažimlju unutar retka, a retci ostaju
func sredi(s string) string {
	retci := strings.Split(s, "\n")
	for i, r := range retci {
		retci[i] = strings.Join(strings.Fields(r), " ")
	}
	return strings.TrimSpace(strings.Join(retci, "\n"))
}

// sat skraćuje "07:00:00" na "07:00"
func sat(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 5 {
		return s[:5]
	}
	return s
}

// broj čita udaljenost, koja u staroj evidenciji dolazi i kao broj i kao tekst
func broj(raw json.RawMessage) float64 {
	if len(raw) == 0 {
		return 0
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return f
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		var v float64
		if _, err := fmt.Sscanf(strings.ReplaceAll(strings.TrimSpace(s), ",", "."), "%g", &v); err == nil {
			return v
		}
	}
	return 0
}

// geometrija vraća ono što je u staroj evidenciji rukom ucrtano na karti,
// kao GeoJSON tekst; prazno kad ništa nije ucrtano
func geometrija(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var g struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &g) != nil || g.Type == "" {
		return ""
	}
	return string(raw)
}
