package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Stranica povezanosti pokazuje svaku ustavu i crpnu stanicu s vodotokom i
// letvom obje strane, ističe što nedostaje i nudi „Uredi” onome tko smije
func TestStranicaPovezanostiObjekata(t *testing.T) {
	cs := models.Structure{Name: "CS Proba", Kind: models.StructureKindPumpingStation, AreaID: 16, Reverzibilna: true, ImaSifon: true}
	ustava := models.Structure{Name: "Ustava Proba", Kind: models.StructureKindSluice, AreaID: 16}
	potpun := RedakPovezanosti{Objekt: cs, Vodotok: "Kanal Proba", VodotokDruge: "Dunavac Proba", Letva: "Proba kanal", LetvaDruge: "Proba dunavac", MozeUredjivati: true}
	nepotpun := RedakPovezanosti{Objekt: ustava, Vodotok: "Kanal Proba", Letva: "Proba kanal", USklopu: "CS Proba"}
	if potpun.Nepotpun() || !nepotpun.Nepotpun() {
		t.Fatal("ocjena potpunosti")
	}
	html := iscrtaj(t, "povezanost_objekata.html", PovezanostPageData{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Redovi: []RedakPovezanosti{potpun, nepotpun}, Nepotpunih: 1})
	for _, zeli := range []string{"CS Proba", "crpna stanica, odvodnja i navodnjavanje", "sa sifonom", "Dunavac Proba", "Ustava Proba", "ustava u sklopu: CS Proba", "nema vodotoka", "nema letve", "nepotpunih: 1", "/edit"} {
		if !strings.Contains(html, zeli) {
			t.Errorf("stranica povezanosti nema %q", zeli)
		}
	}
	if strings.Count(html, "/edit") != 1 {
		t.Error("„Uredi” se nudi i bez prava uređivanja")
	}
}

// Obrazac objekta nudi vodotoke iz registra za obje strane, a šifra koje
// nema u registru ostaje izabrana uz napomenu
func TestObrazacObjektaNudiVodotokeObjeStrane(t *testing.T) {
	st := &models.Structure{Name: "CS Proba", Kind: models.StructureKindPumpingStation, WatercourseCode: "stari-kod", VodotokDrugeStrane: "dunavac"}
	html := iscrtaj(t, "structure_form.html", StructurePageData{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Structure: st, IsEdit: true, Vodotoci: []models.Watercourse{{Code: "dunavac", Name: "Dunavac Proba", Kind: "dunavac"}, {Code: "kanal", Name: "Kanal Proba", Kind: "kanal"}}})
	for _, zeli := range []string{`name="vodotok_druge_strane"`, `value="dunavac" selected`, "stari-kod (nije u registru)", `name="smjer_rada"`, `name="ima_sifon"`, `name="sifon_min_cm"`} {
		if !strings.Contains(html, zeli) {
			t.Errorf("obrazac objekta nema %q", zeli)
		}
	}
}

// Stranica povezanosti kroz rukovatelja: crpne stanice i ustave područja s
// nazivima letava umjesto šifri; područje se bira u adresi. Vodotoci se
// čitaju iz registra, a bez registra ponuda je prazna.
func TestPovezanostKrozRukovatelja(t *testing.T) {
	o := novaOkolinaSastavljaca(t)
	ctx := context.Background()
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	cs := &models.Structure{Name: "CS Proba", Kind: models.StructureKindPumpingStation, AreaID: 1, StationID: o.letva.ID.String()}
	ustava := &models.Structure{Name: "Ustava Proba", Kind: models.StructureKindSluice, AreaID: 1}
	for _, s := range []*models.Structure{cs, ustava} {
		if err := o.structures.Create(ctx, admin, s); err != nil {
			t.Fatal(err)
		}
	}
	h := NewStructuresHandler(o.structures, o.stations, nil, o.users, nil, nil, nil)
	if h.vodotoci(ctx) != nil {
		t.Error("bez registra vodotoka ponuda nije prazna")
	}
	h.SetPovezanost(nil, o.tmpl("povezanost_objekata.html"))
	for _, podrucje := range []string{"1", "nije-broj"} {
		r := httptest.NewRequest(http.MethodGet, "/registri/povezanost-objekata?area="+url.QueryEscape(podrucje), nil)
		r = r.WithContext(context.WithValue(ctx, contextKeyPerms, admin))
		w := httptest.NewRecorder()
		h.ShowPovezanost(w, r)
		s := w.Body.String()
		if w.Code != http.StatusOK || !strings.Contains(s, "CS Proba") || !strings.Contains(s, "Ustava Proba") || !strings.Contains(s, o.letva.Name) {
			t.Errorf("područje %q: %d\n%s", podrucje, w.Code, s)
		}
	}
}

// Najniži vodostaj za sifon čita se samo uz kvačicu „Ima sifon”
func TestNajniziZaSifon(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/structures", strings.NewReader(url.Values{"sifon_min_cm": {"250"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if v := najniziZaSifon(r, true); v == nil || *v != 250 {
		t.Errorf("sa sifonom: %v", v)
	}
	if v := najniziZaSifon(r, false); v != nil {
		t.Errorf("bez sifona: %v", *v)
	}
}

// Ponuda vodotoka na obrascu objekta dolazi iz registra; kad registar ne
// odgovara, ponuda je prazna, a obrazac i dalje radi
func TestVodotociZaObrazacObjekta(t *testing.T) {
	o := novaOkolinaSastavljaca(t)
	ctx := context.Background()
	vodotoci := service.NewWatercourseService(repository.NewWatercourseRepository(o.baza, ledger.New(o.baza, "test")))
	if err := vodotoci.CreateWatercourse(ctx, &models.UserPermissions{IsGlobalAdmin: true}, &models.Watercourse{Code: "kanal-proba", Name: "Kanal Proba", OfficialName: "kanal Proba", Kind: "kanal"}); err != nil {
		t.Fatal(err)
	}
	h := NewStructuresHandler(o.structures, o.stations, nil, o.users, nil, nil, nil)
	h.SetPovezanost(vodotoci, nil)
	v := h.vodotoci(ctx)
	if !imaVodotok(v, "kanal-proba") || imaVodotok(v, "nema-ga") {
		t.Errorf("ponuda vodotoka: %+v", v)
	}
	if err := o.baza.Close(); err != nil {
		t.Fatal(err)
	}
	if v := h.vodotoci(ctx); v != nil {
		t.Errorf("nedostupan registar: %+v", v)
	}
}

// Obrazac crpne stanice nudi broj agregata, a prazno polje znači da se
// broj čita iz kapaciteta; zapis koji nije broj provjera odbije
func TestObrazacObjektaBrojAgregata(t *testing.T) {
	st := &models.Structure{Name: "CS Proba", Kind: models.StructureKindPumpingStation, BrojAgregata: 4}
	html := iscrtaj(t, "structure_form.html", StructurePageData{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Structure: st, IsEdit: true})
	if !strings.Contains(html, `name="broj_agregata"`) || !strings.Contains(html, `value="4"`) {
		t.Error("obrazac crpne stanice nema upisani broj agregata")
	}
	for zapis, zeli := range map[string]int{"": 0, " 3 ": 3, "tri": -1} {
		if got := brojAgregataIzObrasca(zapis); got != zeli {
			t.Errorf("%q: %d, želi %d", zapis, got, zeli)
		}
	}
}

// Obrazac crpne stanice nudi tri smjera rada s odabranim smjerom; stara
// reverzibilna stanica je „odvodnja i navodnjavanje”, a obrazac upisuje
// smjer i oznaku za starije izdanje
func TestObrazacObjektaSmjerRada(t *testing.T) {
	st := &models.Structure{Name: "CS Proba", Kind: models.StructureKindPumpingStation, Reverzibilna: true}
	html := iscrtaj(t, "structure_form.html", StructurePageData{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Structure: st, IsEdit: true})
	for _, zeli := range []string{`name="smjer_rada"`, `value="NAVODNJAVANJE">navodnjavanje`, `value="ODVODNJA_I_NAVODNJAVANJE" selected>odvodnja i navodnjavanje`} {
		if !strings.Contains(html, zeli) {
			t.Errorf("obrazac nema %q", zeli)
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/structures", strings.NewReader("name=CS+Topolje&kind=CRPNA_STANICA&smjer_rada=NAVODNJAVANJE"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if got := structureFromForm(r); got.Smjer() != models.SmjerNavodnjavanje || got.Reverzibilna {
		t.Errorf("iz obrasca: %+v", got)
	}
}

// Kota „0” letve uz objekt piše se s tri decimale, kao na obrascu letve, pa
// spremanje bez izmjene ne zaokruži 82,064 na 82,06
func TestObrazacObjektaKotaNuleTriDecimale(t *testing.T) {
	kota := 82.064
	st := &models.Structure{Name: "CS Proba", Kind: models.StructureKindPumpingStation, ZeroDatum: &kota}
	html := iscrtaj(t, "structure_form.html", StructurePageData{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Structure: st, IsEdit: true})
	if !strings.Contains(html, `name="zero_datum" class="form-control" value="82,064"`) {
		t.Error("kota nule objekta nije upisana s tri decimale")
	}
}

// Povezanost po crtežu 5f: čipovi „Sva” i samo područja koja imaju ustavu ili
// crpnu stanicu, samostalna ustava tako i piše, a tablica nije u table-responsive.
func TestPovezanostPoCrtezu(t *testing.T) {
	cipovi := cipoviPodrucjaPovezanosti(map[int]bool{16: true, 34: true},
		[]models.Area{{ID: 15}, {ID: 16}, {ID: 34}}, 16)
	if len(cipovi) != 3 || cipovi[0].URL != "/registri/povezanost-objekata" || cipovi[0].Odabran ||
		!cipovi[1].Odabran || cipovi[1].URL != "/registri/povezanost-objekata?area=16" || cipovi[2].Naziv != models.Terms().AreaShort+" 34" {
		t.Errorf("čipovi područja: %+v", cipovi)
	}
	samostalna := RedakPovezanosti{Objekt: models.Structure{Name: "Ustava Samostalna", Kind: models.StructureKindSluice, AreaID: 16}}
	html := iscrtaj(t, "povezanost_objekata.html", PovezanostPageData{CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{}, Redovi: []RedakPovezanosti{samostalna}, Podrucje: 16, CipoviPodrucja: cipovi})
	for _, zeli := range []string{"samostalna", `class="povratna-poveznica">← Crpne stanice`, `aria-pressed="true">`, "1 objekt u"} {
		if !strings.Contains(html, zeli) {
			t.Errorf("povezanost nema %q", zeli)
		}
	}
	if strings.Contains(html, "table-responsive") || strings.Contains(html, "/edit") {
		t.Error("tablica je u table-responsive ili se Uredi nudi bez prava")
	}
}
