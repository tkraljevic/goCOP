package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// osobe i list za provjeru stranice prenesenog lista; podaci su izmišljeni
type preneseniList struct {
	k                                      *knjigaRute
	pero, rukovoditelj, operater, dionicar *models.User
	dan                                    time.Time
	id                                     string
}

// noviPreneseniList sprema list prenesen iz ranije evidencije: jedan zadatak bez
// odgovora, jedan obavljen, vrijeme iz arhive i ništa drugo
func noviPreneseniList(t *testing.T) *preneseniList {
	t.Helper()
	k := novaKnjigaRute(t)
	bp := 1
	p := &preneseniList{k: k}
	p.pero = k.osoba(t, "pperic", "Pero Perić", models.Duty{Title: "Vodočuvar P.1.1", Role: models.RoleWaterGuard, ScopeType: models.ScopeSection, AreaID: &bp, SectionCodes: "P.1.1"})
	p.rukovoditelj = k.osoba(t, "iivic", "Ivo Ivić", models.Duty{Title: "Rukovoditelj BP 1", Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &bp})
	p.operater = k.osoba(t, "aanic", "Ana Anić", models.Duty{Title: "Dežurni operater", Role: models.RoleOperator, ScopeType: models.ScopeSector})
	n := time.Now().In(models.Zagreb)
	danas := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, models.Zagreb)
	// isti dan u godini: u knjigu prošle godine više se ne upisuje
	if p.dan = danas.AddDate(0, 0, -3); p.dan.Year() != danas.Year() {
		p.dan = danas
	}
	zadano := p.dan.AddDate(0, 0, -1)
	l := &models.VodocuvarskiList{UserID: p.pero.ID.String(), Ime: p.pero.FullName, Sektor: "P", AreaID: bp, Datum: p.dan,
		Rekonstrukcija: true, Izvor: "probni-zapis-1", Prilike: "27°C, vjetar 1–3 m/s · Open-Meteo, naknadno",
		Zadaci: []models.ZadatakNaListu{
			{ID: uuid.NewString(), Tekst: "Obiđi kanal Travnik", Zadao: "Marko Marić", ZadanoAt: zadano, Status: models.ZadatakBezOdgovora, Obavljeno: models.ObjasnjenjeBezOdgovora},
			{ID: uuid.NewString(), Tekst: "Obiđi nasip uz potok", Zadao: "Marko Marić", ZadanoAt: zadano, Status: models.ZadatakBezOdgovora, Obavljeno: models.ObjasnjenjeBezOdgovora},
			{ID: uuid.NewString(), Tekst: "Pregledaj ustavu", Zadao: "Marko Marić", ZadanoAt: zadano, Status: models.ZadatakObavljen, Obavljeno: "Ustava pregledana, sve radi."},
		}}
	if err := k.vodRepo.Save(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	p.id = l.ID
	for _, z := range l.Zadaci {
		p.spremiZadatak(t, z, l.ID)
	}
	return p
}

// spremiZadatak upisuje zadatak s lista i među zadatke osobe, kao uvoz
func (p *preneseniList) spremiZadatak(t *testing.T, z models.ZadatakNaListu, listID string) {
	t.Helper()
	if err := p.k.vodRepo.SaveZadatak(context.Background(), &models.Zadatak{ID: z.ID, UserID: p.pero.ID.String(), Sektor: "P", AreaID: 1, Tekst: z.Tekst, Zadao: z.Zadao,
		ZadanoAt: z.ZadanoAt, Za: p.dan, Status: z.Status, Obavljeno: z.Obavljeno, ListID: listID}); err != nil {
		t.Fatal(err)
	}
}

// stranica vraća stranicu lista s jednim razmakom među riječima
func (p *preneseniList) stranica(t *testing.T, u *models.User) string {
	t.Helper()
	w := p.k.zovi(t, u, http.MethodGet, "/vodocuvar/"+p.id, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("%s: status %d", u.FullName, w.Code)
	}
	return strings.Join(strings.Fields(w.Body.String()), " ")
}

// Preneseni list nema radnog vremena, potpisa ni ovjere, pa ih stranica ne
// pokazuje, a obrasca upisa nema (Odluke (7. 11. 2026.)). Zadatak bez odgovora ima
// sivu oznaku, a rečenica zašto odgovora nema stoji jednom, na vrhu lista.
func TestPreneseniListStranica(t *testing.T) {
	p := noviPreneseniList(t)
	for _, x := range []struct {
		ime     string
		u       *models.User
		povrat  string
		sadrzi  []string
		nesadrz []string
	}{
		{"rukovoditelj", p.rukovoditelj, "← Vodočuvarski dnevnik", []string{"· Pero Perić · BP 1<"}, []string{"← Moja knjiga"}},
		{"vodočuvar", p.pero, "← Moja knjiga", nil, []string{"← Vodočuvarski dnevnik", "· Pero Perić · BP 1<", `action="/vodocuvar/spremi"`, "Današnji"}},
	} {
		s := p.stranica(t, x.u)
		for _, da := range append(x.sadrzi, x.povrat,
			`<span class="oznaka-stanja pozor">ovjereno prijenosom</span>`,
			"Ovjereno prijenosom, bez potpisa vodočuvara. List je prenesen iz ranije evidencije.",
			`<span class="zadatak-oznaka BEZ_ODGOVORA">bez upisanog odgovora</span>`,
			`<span class="zadatak-oznaka OBAVLJEN">obavljeno</span>; Ustava pregledana, sve radi.`,
			"27°C, vjetar 1–3 m/s", "naknadno, iz arhive vremena",
			"<dt>Izvornik</dt><dd>bez elektroničkog potpisa</dd>", `href="/vodocuvar/`+p.id+`/list.pdf"`,
		) {
			if !strings.Contains(s, da) {
				t.Errorf("%s: na stranici nema %q", x.ime, da)
			}
		}
		for _, ne := range append(x.nesadrz,
			"Radno vrijeme", "ukupno", "Potpis vodočuvara", "Ovjera rukovoditelja", "čeka ovjeru",
			"Opis radnih aktivnosti", "Posebna zapažanja",
			"Upis u dnevnik", `action="/vodocuvar/upis"`, `value="ovjeri"`, `value="parafiraj"`,
			">nije obavljen<", "ne mijenja", "Open-Meteo", "Odgovor nije upisan",
		) {
			if strings.Contains(s, ne) {
				t.Errorf("%s: na stranici stoji %q", x.ime, ne)
			}
		}
		if n := strings.Count(s, models.RazlogBezOdgovora); n != 1 {
			t.Errorf("%s: rečenica o izostanku upisa stoji %d puta, a ne jednom", x.ime, n)
		}
		vrh := strings.Index(s, models.RazlogBezOdgovora)
		if vrh < 0 || vrh > strings.Index(s, `<dl class="vodocuvar-dl">`) {
			t.Errorf("%s: rečenica o izostanku upisa nije na vrhu lista", x.ime)
		}
	}
	stil := procitajStil(t)
	if i := strings.Index(stil, ".zadatak-oznaka.BEZ_ODGOVORA"); i < 0 || !strings.Contains(stil[i:i+150], "--md-sys-color-surface-container-high") {
		t.Error("oznaka bez upisanog odgovora nije siva")
	}
}

// Bilješka preko obrasca uz knjigu i dalje ide na preneseni list (Odluke (8. 10.
// 2026.), pitanje 6), stranica je pokazuje i svi je vide, poruka ne obećava broj
// lista, a obrasca na stranici i dalje nema. Rukovoditeljev upis stoji pod
// naredbama, a operaterova bilješka pod zapažanjima, kojih na listu prije nije bilo.
func TestBiljeskaNaPreneseniListOstajeVidljiva(t *testing.T) {
	p := noviPreneseniList(t)
	knjiga := p.k.zovi(t, p.rukovoditelj, http.MethodGet, "/vodocuvar?sektor=P&godina="+p.dan.Format("2006"), nil).Body.String()
	if !strings.Contains(knjiga, `action="/vodocuvar/upis"`) {
		t.Error("uz knjigu nema obrasca upisa")
	}
	for _, x := range []struct {
		u     *models.User
		tekst string
	}{{p.rukovoditelj, "Obilazak potvrđen telefonom"}, {p.operater, "Javljeno iz COP-a da vodostaj raste"}} {
		upis := url.Values{"vodocuvar": {p.pero.ID.String()}, "datum": {p.dan.Format("2006-01-02")}, "tekst": {x.tekst}}
		kamo := mustUnescape(p.k.zovi(t, x.u, http.MethodPost, "/vodocuvar/upis", upis).Header().Get("Location"))
		if !strings.HasPrefix(kamo, "/vodocuvar/"+p.id) || !strings.Contains(kamo, "na prenesenom listu od "+p.dan.Format("02.01.2006.")) || strings.Contains(kamo, "broj dobiva") {
			t.Fatalf("upis %s: %s", x.u.FullName, kamo)
		}
	}
	l, err := p.k.vodRepo.ZaDan(context.Background(), p.pero.ID.String(), p.dan)
	if err != nil || l == nil || l.ID != p.id || !l.Rekonstrukcija || l.Broj != 0 || len(l.Upisi) != 2 {
		t.Fatalf("list nakon upisa: %v %+v", err, l)
	}
	for _, u := range []*models.User{p.rukovoditelj, p.pero} {
		s := p.stranica(t, u)
		for _, da := range []string{"Obilazak potvrđen telefonom", "Bilješka: Javljeno iz COP-a da vodostaj raste", "<dt>Posebna zapažanja</dt>"} {
			if !strings.Contains(s, da) {
				t.Errorf("%s: na stranici nema %q", u.FullName, da)
			}
		}
		if strings.Contains(s, "Upis u dnevnik") || strings.Contains(s, `action="/vodocuvar/upis"`) || strings.Contains(s, "Radno vrijeme") {
			t.Errorf("%s: stranica nakon upisa ima obrazac ili prazan redak", u.FullName)
		}
	}
}

// Na običnom listu iste osobe redci ostaju, a zadaci iz ranije evidencije nisu
// „nije obavljen”: rečenica stoji jednom, iznad popisa zadataka, jer se ne tiče
// baš tog lista.
func TestZadatakBezOdgovoraNaObicnomListu(t *testing.T) {
	p := noviPreneseniList(t)
	sad := time.Now()
	obicni := &models.VodocuvarskiList{UserID: p.pero.ID.String(), Ime: p.pero.FullName, Sektor: "P", AreaID: 1, Datum: p.dan.AddDate(0, 0, 1), Broj: 1, PredanoAt: &sad, Opis: "Obilazak nasipa"}
	if err := p.k.vodRepo.Save(context.Background(), obicni); err != nil {
		t.Fatal(err)
	}
	p.id = obicni.ID
	s := p.stranica(t, p.rukovoditelj)
	for _, da := range []string{"Radno vrijeme", "Potpis vodočuvara", "Ovjera rukovoditelja", "Upis u dnevnik", `<span class="zadatak-oznaka BEZ_ODGOVORA">bez upisanog odgovora</span>`, `<span class="zadatak-oznaka OBAVLJEN">obavljen</span>: Ustava pregledana`} {
		if !strings.Contains(s, da) {
			t.Errorf("na stranici nema %q", da)
		}
	}
	if strings.Contains(s, ">nije obavljen<") || strings.Contains(s, "Odgovor nije upisan") {
		t.Error("zadatak bez odgovora stoji kao neobavljen ili s objašnjenjem uz sebe")
	}
	if n := strings.Count(s, models.RazlogBezOdgovora); n != 1 {
		t.Errorf("rečenica o izostanku upisa stoji %d puta", n)
	}
	if strings.Index(s, models.RazlogBezOdgovora) < strings.Index(s, `<dl class="vodocuvar-dl">`) {
		t.Error("rečenica stoji na vrhu lista koji takve zadatke nema")
	}
}

// premjestiPeru mijenja Perino zaduženje jučer u 18 h; do tada je od prije
// mjesec dana čuvao P.1.1, pa i na dan prenesenog lista
func (p *preneseniList) premjestiPeru(t *testing.T, podrucje int, dionica string) {
	t.Helper()
	n := time.Now().In(models.Zagreb)
	danas := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, models.Zagreb)
	for _, q := range []string{
		`UPDATE record_versions SET created_at = ? WHERE entity = 'duties' AND entity_id IN (SELECT id FROM duties WHERE user_id = ?)`,
		`UPDATE duties SET created_at = ? WHERE user_id = ?`,
	} {
		if _, err := p.k.baza.Exec(q, danas.AddDate(0, 0, -30).UTC(), p.pero.ID.String()); err != nil {
			t.Fatal(err)
		}
	}
	zaduzenja, err := p.k.userRepo.GetDutiesForUser(p.pero.ID)
	if err != nil || len(zaduzenja) != 1 {
		t.Fatalf("Perina zaduženja: %v %+v", err, zaduzenja)
	}
	zaduzenja[0].AreaID, zaduzenja[0].SectionCodes = &podrucje, dionica
	if err := p.k.userRepo.UpdateDuty(&zaduzenja[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := p.k.baza.Exec(`UPDATE record_versions SET created_at = ? WHERE entity = 'duties' AND entity_id = ? AND created_at > ?`,
		danas.Add(-6*time.Hour).UTC(), zaduzenja[0].ID.String(), danas.AddDate(0, 0, -1).UTC()); err != nil {
		t.Fatal(err)
	}
}

// Obrazac uz knjigu ide po današnjem pravu, a pravo na preneseni list po listu.
// Tko smije na list, ali vodočuvaru danas više ne smije upisivati, nema nijedan
// obrazac i pomoć to kaže. Servis takav upis i dalje prima. Podaci su izmišljeni.
func TestPreneseniListBezObrascaPoDanasnjemPravu(t *testing.T) {
	for _, x := range []struct {
		ime      string
		pripremi func(*testing.T, *preneseniList)
		bez      func(*preneseniList) []*models.User
		sa       func(*preneseniList) []*models.User
	}{
		{"premješten na drugu dionicu",
			func(t *testing.T, p *preneseniList) { p.premjestiPeru(t, 1, "P.1.2") },
			func(p *preneseniList) []*models.User { return []*models.User{p.dionicar} },
			func(p *preneseniList) []*models.User { return []*models.User{p.rukovoditelj} }},
		{"premješten na drugo područje",
			func(t *testing.T, p *preneseniList) { p.premjestiPeru(t, 2, "P.2.1") },
			func(p *preneseniList) []*models.User { return []*models.User{p.dionicar, p.rukovoditelj} },
			func(p *preneseniList) []*models.User { return []*models.User{p.operater} }},
		{"isključen račun",
			func(t *testing.T, p *preneseniList) {
				if _, err := p.k.baza.Exec(`UPDATE users SET is_active = 0 WHERE id = ?`, p.pero.ID.String()); err != nil {
					t.Fatal(err)
				}
			},
			func(p *preneseniList) []*models.User { return []*models.User{p.rukovoditelj, p.operater} },
			func(*preneseniList) []*models.User { return nil }},
	} {
		t.Run(x.ime, func(t *testing.T) {
			p := noviPreneseniList(t)
			if time.Now().In(models.Zagreb).Format("2006-01-02") == p.dan.Format("2006-01-02") {
				t.Skip("dan prije tri dana pada u prošlu godinu")
			}
			if _, err := p.k.baza.Exec(`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (2, 'P', 'Drugo', 'VGI Drugo', 'Drugo')`); err != nil {
				t.Fatal(err)
			}
			bp := 1
			p.dionicar = p.k.osoba(t, "llukic", "Luka Lukić", models.Duty{Title: "Rukovoditelj dionice P.1.1", Role: models.RoleSectionLeader, ScopeType: models.ScopeSection, AreaID: &bp, SectionCodes: "P.1.1"})
			x.pripremi(t, p)
			knjiga := "/vodocuvar?sektor=P&godina=" + p.dan.Format("2006")
			for _, u := range x.sa(p) {
				if s := p.k.zovi(t, u, http.MethodGet, knjiga, nil).Body.String(); !strings.Contains(s, `action="/vodocuvar/upis"`) {
					t.Errorf("%s: uz knjigu nema obrasca, a danas smije upisivati", u.FullName)
				}
			}
			l, err := p.k.vodRepo.Get(context.Background(), p.id)
			if err != nil {
				t.Fatal(err)
			}
			for i, u := range x.bez(p) {
				cijeli, err := p.k.users.GetUserByID(u.ID)
				if err != nil || !p.k.vod.SmijeUpisati(models.NewUserPermissions(*cijeli), l) {
					t.Errorf("%s: ne smije upisati na preneseni list (%v)", u.FullName, err)
				}
				if s := p.k.zovi(t, u, http.MethodGet, knjiga, nil).Body.String(); !strings.Contains(s, "Pero Perić") || strings.Contains(s, `action="/vodocuvar/upis"`) {
					t.Errorf("%s: knjigu treba vidjeti, a obrazac uz nju ne", u.FullName)
				}
				if strings.Contains(p.stranica(t, u), `action="/vodocuvar/upis"`) {
					t.Errorf("%s: na stranici prenesenog lista ima obrazac", u.FullName)
				}
				upis := url.Values{"vodocuvar": {p.pero.ID.String()}, "datum": {p.dan.Format("2006-01-02")}, "tekst": {"Javljeno naknadno"}}
				kamo := mustUnescape(p.k.zovi(t, u, http.MethodPost, "/vodocuvar/upis", upis).Header().Get("Location"))
				if !strings.Contains(kamo, "na prenesenom listu od ") {
					t.Errorf("%s: upis %s", u.FullName, kamo)
				}
				if l, err = p.k.vodRepo.Get(context.Background(), p.id); err != nil || len(l.Upisi) != i+1 {
					t.Errorf("%s: upisa na listu nakon upisa: %v %+v", u.FullName, err, l)
				}
			}
		})
	}
}
