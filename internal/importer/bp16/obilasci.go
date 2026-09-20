package bp16

// Uvoz obilazaka terena iz zbirke evidencije_obilaska u Directusu VGI
// Baranja u zadatke vodočuvara i njihove dnevne listove.
//
// Obilazak je zadatak: netko ga je zadao, vodočuvar ga je dobio za određeni
// dan, i ponegdje je upisao što je zatekao. Dvije trećine nemaju taj upis, i
// to nije propust vodočuvara: aplikacija u kojoj su zadatke dobivali nije
// bila službena, pa upise nisu radili. Zato takav zadatak ne stoji kao
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
const ObjasnjenjeBezOdgovora = "Odgovor ne postoji: zadatak je zadan u ranijoj aplikaciji koja nije bila službena, pa se upisi o izvršenju nisu vodili."

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
	DryRun         bool
	Log            func(string, ...any)
}

// ObilasciReport je izvješće uvoza
type ObilasciReport struct {
	Ukupno, Upisano, Postoje, Preskoceno, BezKorisnika  int
	Listova, SOdgovorom, BezOdgovora, Slika, SObuhvatom int
	Nepoznati                                           map[string]int
	DryRun                                              bool
}

// Summary sažima izvješće u jedan redak
func (r ObilasciReport) Summary() string {
	return fmt.Sprintf("%d obilazaka: %d upisano, %d već postoji, %d preskočeno, %d bez poznatog vodočuvara; %d listova, %d s odgovorom, %d bez odgovora, %d fotografija, %d s ucrtanim obuhvatom",
		r.Ukupno, r.Upisano, r.Postoje, r.Preskoceno, r.BezKorisnika, r.Listova, r.SOdgovorom, r.BezOdgovora, r.Slika, r.SObuhvatom)
}

// RunObilasci prenosi obilaske u zadatke i rekonstruirane dnevne listove.
// Ponovni uvoz preskače ono što je već preneseno, pa se smije ponavljati dok
// se stara evidencija ne ugasi.
func RunObilasci(ctx context.Context, src Source, deps ObilasciDeps) (ObilasciReport, error) {
	rep := ObilasciReport{Nepoznati: map[string]int{}, DryRun: deps.DryRun}
	logf := deps.Log
	if logf == nil {
		logf = func(string, ...any) {}
	}
	imena := map[string]string{}
	if us, ok := src.(interface {
		Users(ctx context.Context) ([]json.RawMessage, error)
	}); ok {
		raw, err := us.Users(ctx)
		if err != nil {
			return rep, fmt.Errorf("korisnici: %w", err)
		}
		for _, r := range raw {
			var u struct {
				ID        string `json:"id"`
				FirstName string `json:"first_name"`
				LastName  string `json:"last_name"`
			}
			if json.Unmarshal(r, &u) == nil {
				imena[u.ID] = strings.TrimSpace(u.FirstName + " " + u.LastName)
			}
		}
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
		if z, _ := deps.Vodocuvar.ZadatakPoIzvoru(ctx, izvor); z != nil {
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
		var prilozi []models.PrilogLista
		var bajtovi [][]byte
		for _, fid := range []string{o.Foto1, o.Foto2, o.Foto3} {
			if fid == "" || deps.Datoteka == nil {
				continue
			}
			b, err := deps.Datoteka(ctx, fid, "")
			if err != nil {
				logf("  obilazak %d: fotografija %s: %v", o.ID, fid, err)
				continue
			}
			jpg, w, h, err := slike.Smanji(b)
			if err != nil {
				logf("  obilazak %d: fotografija %s: %v", o.ID, fid, err)
				continue
			}
			prilozi = append(prilozi, models.PrilogLista{
				ID:    uuid.NewSHA1(uuid.NameSpaceURL, []byte("gocop/"+izvor+"/"+fid)).String(),
				Naziv: fmt.Sprintf("fotografija %d", len(prilozi)+1),
				Vrsta: "image/jpeg", Bajtova: len(jpg), Sirina: w, Visina: h,
				Sadrzaj: sadrzaj.Otisak(jpg), ZadatakID: z.ID,
			})
			bajtovi = append(bajtovi, jpg)
			rep.Slika++
		}

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
			rep.Listova++
		}
		z.ListID = l.ID
		l.Zadaci = append(l.Zadaci, models.ZadatakNaListu{
			ID: z.ID, Tekst: z.Tekst, Zadao: z.Zadao, ZadanoAt: z.ZadanoAt, Status: z.Status,
			Obavljeno: z.Obavljeno, Od: z.Od, Do: z.Do, Udaljenost: z.Udaljenost, ImaObuhvat: z.Obuhvat != "",
		})
		l.Prilozi = append(l.Prilozi, prilozi...)
		if err := deps.Vodocuvar.Save(ctx, l); err != nil {
			return rep, fmt.Errorf("obilazak %d: list: %w", o.ID, err)
		}
		z.ListID = l.ID
		if err := deps.Vodocuvar.SaveZadatak(ctx, z); err != nil {
			return rep, fmt.Errorf("obilazak %d: zadatak: %w", o.ID, err)
		}
		for i, pr := range prilozi {
			if err := deps.Vodocuvar.SavePrilog(ctx, l.ID, pr.ID, bajtovi[i]); err != nil {
				return rep, fmt.Errorf("obilazak %d: prilog: %w", o.ID, err)
			}
		}
		rep.Upisano++
	}
	return rep, nil
}

// sredi miče višak razmaka i tabulatora iz teksta stare evidencije
func sredi(s string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " \n ")), " "))
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
