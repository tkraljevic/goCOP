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
	"gocop/internal/slike"
)

// Uvoz obavijesti s terena: zbirka obavijesti_sa_terena u Directusu VGI
// Baranja (izvješća, prijave, obavijesti i zahtjevi vodočuvara, sa slikama i
// skenom potpisanog ispisa) prenosi se u prijave s terena goCOP-a kao
// rekonstrukcija: bez novog potpisa, sken je izvornik gdje ga ima, a slike se
// smanjuju kao i u programu. Ponovni uvoz preskače što već ima, po izvoru.

// KorisnikUvoza je vodočuvar u goCOP-u kojem se prijave pripisuju
type KorisnikUvoza struct {
	ID     string
	Ime    string
	Sektor string
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

// RunPrijave uvozi obavijesti s terena
func RunPrijave(ctx context.Context, src Source, deps PrijaveDeps) (PrijaveReport, error) {
	rep := PrijaveReport{DryRun: deps.DryRun, PoKorisniku: map[string]int{}, PoPodrucju: map[string]int{}, Nepoznati: map[string]int{}}
	logf := deps.Log
	if logf == nil {
		logf = func(string, ...any) {}
	}
	// korisnici Directusa po oznaci, vode po oznaci
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
	vode := map[int]string{}
	if raw, err := src.Items(ctx, "vode"); err == nil {
		for _, r := range raw {
			var v struct {
				ID   int    `json:"id"`
				Voda string `json:"voda"`
			}
			if json.Unmarshal(r, &v) == nil {
				vode[v.ID] = v.Voda
			}
		}
	}
	raw, err := src.Items(ctx, "obavijesti_sa_terena")
	if err != nil {
		return rep, err
	}
	var redovi []obavijestRow
	for _, r := range raw {
		var o obavijestRow
		if err := json.Unmarshal(r, &o); err != nil {
			return rep, fmt.Errorf("obavijest: %w", err)
		}
		redovi = append(redovi, o)
	}
	sort.SliceStable(redovi, func(i, j int) bool { return redovi[i].DateCreated < redovi[j].DateCreated })
	rep.Ukupno = len(redovi)

	brojevi := map[int]int{} // sljedeći broj po godini
	for _, o := range redovi {
		izvor := fmt.Sprintf("bp16:obavijesti_sa_terena:%d", o.ID)
		if p, _ := deps.Prijave.PoIzvoru(ctx, izvor); p != nil {
			rep.Postoje++
			continue
		}
		ime := imena[o.UserCreated]
		k, ok := deps.Korisnici[ime]
		if !ok {
			rep.BezKorisnika++
			rep.Nepoznati[ime]++
			continue
		}
		area, ok := deps.Podrucja[strings.ToUpper(strings.TrimSpace(o.Podrucje))]
		if !ok {
			rep.BezPodrucja++
			rep.Nepoznati["područje "+o.Podrucje]++
			continue
		}
		kad, err := time.Parse(time.RFC3339, o.DateCreated)
		if err != nil {
			kad = time.Now()
		}
		kad = kad.In(models.Zagreb)
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
		if len(o.NazivVodotoka) > 0 {
			var id int
			if json.Unmarshal(o.NazivVodotoka, &id) == nil {
				p.Vodotok = vode[id]
			} else {
				var v struct {
					Voda string `json:"voda"`
				}
				if json.Unmarshal(o.NazivVodotoka, &v) == nil {
					p.Vodotok = v.Voda
				}
			}
		}
		if o.Lokacija != nil {
			if lon, lat, ok := prvaTocka(o.Lokacija.Coordinates); ok && lat > 40 && lat < 50 && lon > 13 && lon < 20 {
				p.Latitude, p.Longitude = &lat, &lon
			}
		}
		switch strings.ToLower(o.Status) {
		case "objavljeno":
			p.Status = models.PrijavaObjavljena
		case "arhivirano":
			p.Status = models.PrijavaArhivirana
		default:
			p.Status = models.PrijavaNacrt
			rep.Nacrta++
		}
		if p.Status != models.PrijavaNacrt {
			t := kad
			p.ObjavljenoAt = &t
			p.Godina = kad.Year()
			if brojevi[p.Godina] == 0 {
				n, err := deps.Prijave.SljedeciBroj(ctx, deps.Sektor, p.Godina)
				if err != nil {
					return rep, err
				}
				brojevi[p.Godina] = n
			}
			p.Broj = brojevi[p.Godina]
			brojevi[p.Godina]++
		}
		rep.PoKorisniku[k.Ime]++
		rep.PoPodrucju[fmt.Sprintf("BP %d (%s)", area, o.Podrucje)]++

		if deps.DryRun {
			// bez preuzimanja: samo prebroji što bi se prenijelo
			for _, sid := range []string{o.Slika1, o.Slika2, o.Slika3} {
				if sid != "" {
					rep.Slika++
				}
			}
			if o.Datoteka != "" {
				rep.Skenova++
			}
			rep.Upisano++
			continue
		}
		// slike: smanjene JPEG kako ih Directus daje na traženu mjeru
		var slikeBajtovi [][]byte
		for _, sid := range []string{o.Slika1, o.Slika2, o.Slika3} {
			if sid == "" || deps.Datoteka == nil {
				continue
			}
			// Uzima se izvorna datoteka, jer samo ona nosi zapis fotoaparata
			// (kad je snimljeno, čime); Directusova smanjena ga briše. Tek kad
			// izvorne nema, uzme se smanjena.
			b, err := deps.Datoteka(ctx, sid, "")
			if err != nil {
				b, err = deps.Datoteka(ctx, sid, "width=1600&height=1600&fit=inside&quality=82&format=jpg")
			}
			if err != nil {
				logf("  obavijest %d: slika %s: %v", o.ID, sid, err)
				continue
			}
			jpg, w, h, err := slike.Smanji(b)
			if err != nil {
				logf("  obavijest %d: slika %s: %v", o.ID, sid, err)
				continue
			}
			sl := models.SlikaPrijave{ID: uuid.NewSHA1(uuid.NameSpaceURL, []byte("gocop/"+izvor+"/"+sid)).String(), Naziv: "fotografija " + fmt.Sprint(len(p.Slike)+1), Sirina: w, Visina: h, Bajtova: len(jpg)}
			if e := slike.Procitaj(b); true {
				if !e.Snimljeno.IsZero() {
					k := e.Snimljeno
					sl.Snimljeno = &k
				}
				sl.Lat, sl.Lon, sl.Uredjaj = e.Lat, e.Lon, e.Uredjaj
				otisak := sha256.Sum256(b)
				sl.IzvornoBajtova, sl.Otisak = len(b), hex.EncodeToString(otisak[:])
				if !p.ImaKoordinate() && sl.ImaPolozaj() {
					lat, lon := e.Lat, e.Lon
					p.Latitude, p.Longitude = &lat, &lon
				}
			}
			p.Slike = append(p.Slike, sl)
			slikeBajtovi = append(slikeBajtovi, jpg)
			rep.Slika++
		}
		// sken potpisanog ispisa preuzima se samo kad ga ima kamo spremiti ili
		// kad PDF iz podataka nije moguć; inače ostaje u staroj evidenciji
		var sken []byte
		if o.Datoteka != "" && deps.Datoteka != nil && (deps.SpremiSken != nil || deps.IzradiPDF == nil) {
			b, err := deps.Datoteka(ctx, o.Datoteka, "")
			if err != nil {
				logf("  obavijest %d: datoteka %s: %v", o.ID, o.Datoteka, err)
			} else if len(b) > 8 && string(b[:4]) == "%PDF" {
				sken = b
				rep.Skenova++
			} else {
				logf("  obavijest %d: datoteka %s nije PDF, preskačem", o.ID, o.Datoteka)
			}
		}
		if err := deps.Prijave.Save(ctx, p); err != nil {
			return rep, fmt.Errorf("obavijest %d: %w", o.ID, err)
		}
		slikeMapa := map[string][]byte{}
		for i, sl := range p.Slike {
			slikeMapa[sl.ID] = slikeBajtovi[i]
			if deps.SlikeOdmah && p.Status != models.PrijavaNacrt {
				continue
			}
			if err := deps.Prijave.SaveSlika(ctx, sl.ID, p.ID, slikeBajtovi[i]); err != nil {
				return rep, fmt.Errorf("obavijest %d, slika: %w", o.ID, err)
			}
		}
		if len(sken) > 0 && deps.SpremiSken != nil {
			naziv, err := deps.SpremiSken(p, sken)
			if err != nil {
				return rep, fmt.Errorf("obavijest %d, sken: %w", o.ID, err)
			}
			p.Sken = naziv
			if err := deps.Prijave.Save(ctx, p); err != nil {
				return rep, fmt.Errorf("obavijest %d: %w", o.ID, err)
			}
		}
		// izvornik u knjizi: sken kad nema kamo drugamo, inače PDF iz podataka
		var izvornik []byte
		if len(sken) > 0 && deps.SpremiSken == nil {
			izvornik = sken
		} else if deps.IzradiPDF != nil && p.Status != models.PrijavaNacrt {
			izvornik = deps.IzradiPDF(p, slikeMapa)
		}
		if len(izvornik) > 0 {
			h := sha256.Sum256(izvornik)
			if err := deps.Prijave.SpremiIzvornik(ctx, p.ID, izvornik, hex.EncodeToString(h[:])); err != nil {
				return rep, fmt.Errorf("obavijest %d, izvornik: %w", o.ID, err)
			}
		}
		rep.Upisano++
	}
	return rep, nil
}
