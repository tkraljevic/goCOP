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
// Ponovni uvoz preskače ono što je već preneseno, pa se smije ponavljati dok
// se stara evidencija ne ugasi.
func RunObilasci(ctx context.Context, src Source, deps ObilasciDeps) (ObilasciReport, error) {
	rep := ObilasciReport{Nepoznati: map[string]int{}, DryRun: deps.DryRun}
	logf := zapisnik(deps.Log)
	imena, err := imenaKorisnika(ctx, src)
	if err != nil {
		return rep, err
	}
	// vrijeme i vodostaji kakve je vodočuvar tog dana imao pred sobom
	vrijeme := map[string]vrijemeDana{}
	if raw, err := src.Items(ctx, "vrijeme_za_list"); err == nil {
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
	} else {
		logf("vrijeme za list: %v", err)
	}

	raw, err := src.Items(ctx, "evidencije_obilaska")
	if err != nil {
		return rep, err
	}
	var redovi []obilazakRow
	for _, r := range raw {
		var o obilazakRow
		if err := json.Unmarshal(r, &o); err != nil {
			return rep, fmt.Errorf("obilazak: %w", err)
		}
		redovi = append(redovi, o)
	}
	sort.SliceStable(redovi, func(i, j int) bool {
		if redovi[i].Datum != redovi[j].Datum {
			return redovi[i].Datum < redovi[j].Datum
		}
		return redovi[i].ID < redovi[j].ID
	})
	rep.Ukupno = len(redovi)

	for _, o := range redovi {
		izvor := fmt.Sprintf("bp16:evidencije_obilaska:%d", o.ID)
		prenesen, err := deps.Vodocuvar.ZadatakPoIzvoru(ctx, izvor)
		if err != nil {
			return rep, fmt.Errorf("obilazak %d: čitanje prenesenog zadatka: %w", o.ID, err)
		}
		if prenesen != nil {
			rep.Postoje++
			continue
		}
		if deps.SamoArhivirane && strings.ToLower(strings.TrimSpace(o.Status)) != "arhivirano" {
			rep.Preskoceno++
			continue
		}
		ime := imena[o.Dodijeljeno]
		k, ok := deps.Korisnici[ime]
		if !ok {
			rep.BezKorisnika++
			rep.Nepoznati[ime]++
			continue
		}
		dan, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(o.Datum), models.Zagreb)
		if err != nil {
			rep.Nepoznati["datum "+o.Datum]++
			continue
		}
		zadao := imena[o.UserCreated]
		zadano := dan
		if t, err := time.Parse(time.RFC3339, o.DateCreated); err == nil {
			zadano = t.In(models.Zagreb)
		}
		area := k.AreaID
		if a, ok := deps.Podrucja[ime]; ok && a > 0 {
			area = a
		}
		sektor := k.Sektor
		if sektor == "" {
			sektor = deps.Sektor
		}

		z := &models.Zadatak{
			ID:     uuid.NewSHA1(uuid.NameSpaceURL, []byte("gocop/"+izvor)).String(),
			UserID: k.ID, Sektor: sektor, AreaID: area,
			Tekst: sredi(o.Opis), ZadaoID: "", Zadao: zadao, ZadanoAt: zadano, Za: dan,
			Izvor: izvor, Cvor: deps.Cvor,
			Od: sat(o.VrijemeOd), Do: sat(o.VrijemeDo), Udaljenost: broj(o.Udaljenost),
		}
		if odgovor := sredi(o.Napomena); odgovor != "" {
			z.Status, z.Obavljeno = models.ZadatakObavljen, odgovor
			kad := dan
			z.ObavljenoAt = &kad
			rep.SOdgovorom++
		} else {
			z.Status, z.Obavljeno = models.ZadatakBezOdgovora, ObjasnjenjeBezOdgovora
			rep.BezOdgovora++
		}
		if g := geometrija(o.Lokacija); g != "" {
			z.Obuhvat = g
			rep.SObuhvatom++
		}

		// fotografije obilaska: smanjene, u spremište sadržaja, kao prilozi lista
		prilozi, bajtovi := fotografijeObilaska(ctx, deps, o, izvor, z.ID, nil, logf)
		rep.Slika += len(prilozi)

		if deps.DryRun {
			rep.Upisano++
			continue
		}

		// list dana: postojeći ili nov, označen kao rekonstrukcija
		l, err := deps.Vodocuvar.ZaDan(ctx, k.ID, dan)
		if err != nil {
			return rep, fmt.Errorf("obilazak %d: list dana: %w", o.ID, err)
		}
		if l == nil {
			l = &models.VodocuvarskiList{
				UserID: k.ID, Ime: k.Ime, Sektor: sektor, AreaID: area, Datum: dan,
				Rekonstrukcija: true, Izvor: "bp16:evidencije_obilaska",
				Cvor: deps.Cvor,
			}
			if v, ima := vrijeme[o.Datum]; ima && v.Prilike != "" {
				l.Prilike, l.Ocitanja = v.Prilike, v.Vodostaji
				rep.VrijemeIzEvidencije++
			} else if deps.Meteo != nil {
				if p := deps.Meteo(ctx, dan); p != "" {
					l.Prilike = p
					rep.VrijemeIzArhive++
				}
			}
			rep.Listova++
		}
		if err := upisiObilazak(ctx, deps.Vodocuvar, l, z, prilozi, bajtovi); err != nil {
			return rep, fmt.Errorf("obilazak %d: %w", o.ID, err)
		}
		rep.Upisano++
	}
	return rep, nil
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
	for _, pr := range prilozi {
		l.Prilozi = zamijeniIliDodaj(l.Prilozi, pr, func(p models.PrilogLista) string { return p.ID })
	}
	if err := repo.Save(ctx, l); err != nil {
		return fmt.Errorf("list: %w", err)
	}
	z.ListID = l.ID
	if err := repo.SaveZadatak(ctx, z); err != nil {
		return fmt.Errorf("zadatak: %w", err)
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
