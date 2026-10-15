package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"gocop/internal/models"
)

// Veza lista i prijave s dionicom gleda zaduženja vodočuvara za dan lista
// ili prijave, a ne samo ono što vrijedi sada (Odluke (8. 10. 2026.),
// pitanje 1: rukovoditelj dionice „osobito u obrani”). Privremeni
// vodočuvar Pero Perić čuva P.1.1 dok traje obrana; kad zaduženje istekne,
// njegov jučerašnji list i prijava ostaju vezani uz P.1.1, a list za dan
// nakon isteka nije. Isto vrijedi kad zaduženje bude opozvano jer je
// vodočuvar prešao na drugu dionicu. Podaci su izmišljeni.
func TestVezaSDionicomPoZaduzenjuNaDanLista(t *testing.T) {
	k := novaKnjigaProba(t)
	ctx := context.Background()
	vs := k.o.vs
	prijave := &PrijavaService{vodocuvar: vs}
	sad := time.Now()
	danas := sad.In(models.Zagreb)
	jucer := time.Date(danas.Year(), danas.Month(), danas.Day()-1, 0, 0, 0, 0, models.Zagreb)
	sutra := time.Date(danas.Year(), danas.Month(), danas.Day()+1, 0, 0, 0, 0, models.Zagreb)
	// zaduženja Pere i Ane postoje od prije mjesec dana
	for _, u := range []*models.User{k.pero, k.ana} {
		k.zaduzenOd(t, u, sad.AddDate(0, 0, -30))
	}
	list := func(dan time.Time) *models.VodocuvarskiList {
		return &models.VodocuvarskiList{UserID: k.pero.ID.String(), Ime: k.pero.FullName, Sektor: "P", AreaID: 1, Datum: dan, PredanoAt: &sad}
	}
	prijava := func(dan time.Time) *models.PrijavaSTerena {
		return &models.PrijavaSTerena{UserID: k.pero.ID.String(), Sektor: "P", AreaID: 1, Datum: dan, Status: models.PrijavaObjavljena}
	}
	dionicar := k.dionicar(models.RoleSectionLeader, &k.p1, "P.1.1")
	rukovoditelj := k.osoba(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &k.p1})
	type ocekivano struct{ jucer, sutra bool }
	provjeri := func(stanje string, o ocekivano) {
		t.Helper()
		for _, x := range []struct {
			dan  time.Time
			veza bool
		}{{jucer, o.jucer}, {sutra, o.sutra}} {
			dan := x.dan.Format("2.1.")
			if !vs.SmijeVidjeti(dionicar, list(x.dan)) {
				t.Errorf("%s: rukovoditelj dionice ne čita list za %s", stanje, dan)
			}
			if got := vs.SmijeParafirati(dionicar, list(x.dan)); got != x.veza {
				t.Errorf("%s: parafa lista za %s = %v, očekivano %v", stanje, dan, got, x.veza)
			}
			if got := vs.SmijeUpisati(dionicar, list(x.dan)); got != x.veza {
				t.Errorf("%s: bilješka na list za %s = %v, očekivano %v", stanje, dan, got, x.veza)
			}
			if got := prijave.SmijeVidjeti(dionicar, prijava(x.dan)); got != x.veza {
				t.Errorf("%s: prijava bez dionice za %s = %v, očekivano %v", stanje, dan, got, x.veza)
			}
			if !vs.SmijeParafirati(rukovoditelj, list(x.dan)) || !prijave.SmijeVidjeti(rukovoditelj, prijava(x.dan)) {
				t.Errorf("%s: rukovoditelj područja za %s", stanje, dan)
			}
		}
	}
	provjeri("valjano zaduženje", ocekivano{true, true})

	// privremeno zaduženje isteklo je prije sat vremena (kraj obrane)
	if _, err := k.o.baza.Exec(`UPDATE duties SET is_temporary = 1, expires_at = ? WHERE user_id = ?`, sad.Add(-time.Hour).UTC(), k.pero.ID.String()); err != nil {
		t.Fatal(err)
	}
	provjeri("isteklo zaduženje", ocekivano{true, false})
	// zadatak gleda samo ono što vrijedi sada
	k.obrana(map[string]models.DefensePhase{"P.1.1": models.PhaseRegular})
	if vs.SmijeZadati(ctx, dionicar, list(jucer)) {
		t.Error("rukovoditelj dionice zadaje zadatak vodočuvaru kojemu je zaduženje isteklo")
	}

	// Ana Anić prelazi s cijelog područja na dionicu P.1.2: staro zaduženje
	// se opoziva. Njezin jučerašnji list veže se i uz P.1.1, sutrašnji samo
	// uz P.1.2; bilješka kroz servis gleda isti dan.
	stara, err := k.users.GetDutiesForUser(k.ana.ID)
	if err != nil || len(stara) != 1 {
		t.Fatalf("Anina zaduženja: %v %+v", err, stara)
	}
	if err := k.users.RevokeDuty(stara[0].ID); err != nil {
		t.Fatal(err)
	}
	nova := models.Duty{UserID: k.ana.ID, Title: "Vodočuvar P.1.2", Role: models.RoleWaterGuard, ScopeType: models.ScopeSection, SectorID: &k.sektor, AreaID: &k.p1, SectionCodes: "P.1.2", IsActive: true, IsPrimary: true}
	if err := k.users.AddDuty(&nova); err != nil {
		t.Fatal(err)
	}
	listAne := func(dan time.Time) *models.VodocuvarskiList {
		return &models.VodocuvarskiList{UserID: k.ana.ID.String(), Ime: k.ana.FullName, Sektor: "P", AreaID: 1, Datum: dan, PredanoAt: &sad}
	}
	if !vs.SmijeParafirati(dionicar, listAne(jucer)) || vs.SmijeParafirati(dionicar, listAne(sutra)) {
		t.Error("veza Anina lista s P.1.1 ne prati opoziv zaduženja")
	}
	drugi := k.dionicar(models.RoleSectionLeader, &k.p1, "P.1.2")
	if !vs.SmijeParafirati(drugi, listAne(sutra)) {
		t.Error("Anin list nakon prelaska nije vezan uz P.1.2")
	}
	// bilješka za prošli dan ide samo na list koji za taj dan postoji: list
	// za dan koji je prošao više se ne otvara (Odluke (8. 10. 2026.), pitanje 6)
	if err := k.o.repo.Save(ctx, &models.VodocuvarskiList{UserID: k.ana.ID.String(), Ime: k.ana.FullName, Sektor: "P", AreaID: 1, Datum: jucer, Od: "07:00", Do: "15:00"}); err != nil {
		t.Fatal(err)
	}
	if _, err := vs.Upisi(ctx, dionicar, &dionicar.User, k.ana.ID.String(), jucer, "Javljeno s dionice P.1.1"); err != nil {
		t.Errorf("bilješka za dan prije prelaska: %v", err)
	}
	if _, err := vs.Upisi(ctx, dionicar, &dionicar.User, k.ana.ID.String(), sutra, "Javljeno s dionice P.1.1"); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("bilješka za dan nakon prelaska: %v", err)
	}
}

// Povijest zaduženja vodočuvara čita se samo kad je dan zadan, i to
// jednom; bez vodočuvara nema zaduženja, a bez izvora povijesti dan nije
// vezan ni uz jedno zaduženje.
func TestZaduzenjaCuvara(t *testing.T) {
	k := novaKnjigaProba(t)
	vs := k.o.vs
	if got := vs.zaduzenjaCuvara(nil, time.Now()); got != nil {
		t.Errorf("bez čuvara: %+v", got)
	}
	if got := vs.zaduzenjaCuvara(&cuvar{}, time.Now()); got != nil {
		t.Errorf("bez vodočuvara: %+v", got)
	}
	pero, _ := vs.users.GetUserByID(k.pero.ID)
	c := &cuvar{v: pero}
	if got := vs.zaduzenjaCuvara(c, time.Time{}); len(got) != 1 || c.procitana {
		t.Errorf("bez dana: %d zaduženja, povijest pročitana %v", len(got), c.procitana)
	}
	if got := vs.zaduzenjaCuvara(c, time.Now()); len(got) != 1 || !c.procitana {
		t.Errorf("s danom: %d zaduženja, povijest pročitana %v", len(got), c.procitana)
	}
	c.povijest = nil
	if got := vs.zaduzenjaCuvara(c, time.Now()); len(got) != 0 {
		t.Errorf("povijest se čita ponovno: %d", len(got))
	}
	if got := (&VodocuvarService{}).zaduzenjaCuvara(&cuvar{v: pero}, time.Now()); got != nil {
		t.Errorf("bez izvora povijesti: %+v", got)
	}
}

// zaduzenOd pomiče nastanak svih zaduženja osobe, i njihovih verzija u
// knjizi, na trenutak kad: zaduženja iz probe tada već postoje
func (k *knjigaProba) zaduzenOd(t *testing.T, u *models.User, kad time.Time) {
	t.Helper()
	for _, q := range []string{
		`UPDATE record_versions SET created_at = ? WHERE entity = 'duties' AND entity_id IN (SELECT id FROM duties WHERE user_id = ?)`,
		`UPDATE duties SET created_at = ? WHERE user_id = ?`,
	} {
		if _, err := k.o.baza.Exec(q, kad.UTC(), u.ID.String()); err != nil {
			t.Fatal(err)
		}
	}
}

// Veza lista i prijave s dionicom gleda zaduženje koje je vrijedilo na dan
// lista, u obliku u kojem je tada vrijedilo (Odluke (8. 10. 2026.),
// pitanje 1: parafa, bilješka i prijava samo uz vezu s njegovom dionicom).
// Ivo Ivić čuva P.1.1 i danas prelazi na P.1.2 opozivom i novim
// zaduženjem; Pero Perić isto, izmjenom zaduženja. Jučerašnji list obojice
// vezan je uz P.1.1, a ne uz P.1.2; sutrašnji uz P.1.2, a ne uz P.1.1. List
// iz vremena prije ikojeg zaduženja u programu vezan je uz zaduženje s
// kojim je vodočuvar ušao u program, P.1.1, i nakon izmjene u mjestu
// (dosad nije bio vezan ni uz jednu; Odluke (8. 10. 2026.), pitanje 1,
// TestVezaSDionicomPrijeZaduzenjaUProgramu). Podaci su izmišljeni.
func TestVezaSDionicomPratiZaduzenjeKojeJeTadaVrijedilo(t *testing.T) {
	k := novaKnjigaProba(t)
	vs := k.o.vs
	prijave := &PrijavaService{vodocuvar: vs}
	sad := time.Now()
	danas := sad.In(models.Zagreb)
	dan := func(pomak int) time.Time {
		return time.Date(danas.Year(), danas.Month(), danas.Day()+pomak, 0, 0, 0, 0, models.Zagreb)
	}
	ivo := k.vodocuvar(t, "iivic", "Ivo Ivić", "P.1.1")
	for _, u := range []*models.User{k.pero, ivo} {
		k.zaduzenOd(t, u, dan(-60))
	}
	prvi := k.dionicar(models.RoleSectionLeader, &k.p1, "P.1.1")
	drugi := k.dionicar(models.RoleSectionLeader, &k.p1, "P.1.2")
	provjeri := func(stanje string, u *models.User, pomak int, uzPrvu, uzDrugu bool) {
		t.Helper()
		l := &models.VodocuvarskiList{UserID: u.ID.String(), Ime: u.FullName, Sektor: "P", AreaID: 1, Datum: dan(pomak), PredanoAt: &sad}
		p := &models.PrijavaSTerena{UserID: u.ID.String(), Sektor: "P", AreaID: 1, Datum: dan(pomak), Status: models.PrijavaObjavljena}
		for _, x := range []struct {
			dionica string
			perms   *models.UserPermissions
			veza    bool
		}{{"P.1.1", prvi, uzPrvu}, {"P.1.2", drugi, uzDrugu}} {
			if got := vs.SmijeParafirati(x.perms, l); got != x.veza {
				t.Errorf("%s: %s, list za dan %+d, parafa rukovoditelja %s = %v, očekivano %v", stanje, u.FullName, pomak, x.dionica, got, x.veza)
			}
			if got := vs.SmijeUpisati(x.perms, l); got != x.veza {
				t.Errorf("%s: %s, list za dan %+d, bilješka rukovoditelja %s = %v, očekivano %v", stanje, u.FullName, pomak, x.dionica, got, x.veza)
			}
			if got := prijave.SmijeVidjeti(x.perms, p); got != x.veza {
				t.Errorf("%s: %s, prijava bez dionice za dan %+d, rukovoditelj %s = %v, očekivano %v", stanje, u.FullName, pomak, x.dionica, got, x.veza)
			}
		}
	}
	provjeri("prije zaduženja", k.pero, -61, true, false)
	provjeri("prije prelaska", k.pero, -1, true, false)

	// Ivo prelazi opozivom i novim zaduženjem
	stara, err := k.users.GetDutiesForUser(ivo.ID)
	if err != nil || len(stara) != 1 {
		t.Fatalf("Ivina zaduženja: %v %+v", err, stara)
	}
	if err := k.users.RevokeDuty(stara[0].ID); err != nil {
		t.Fatal(err)
	}
	nova := models.Duty{UserID: ivo.ID, Title: "Vodočuvar P.1.2", Role: models.RoleWaterGuard, ScopeType: models.ScopeSection, SectorID: &k.sektor, AreaID: &k.p1, SectionCodes: "P.1.2", IsActive: true, IsPrimary: true}
	if err := k.users.AddDuty(&nova); err != nil {
		t.Fatal(err)
	}
	provjeri("opoziv i novo zaduženje", ivo, -1, true, false)
	provjeri("opoziv i novo zaduženje", ivo, -30, true, false)
	provjeri("opoziv i novo zaduženje", ivo, 1, false, true)

	// Pero prelazi izmjenom zaduženja u mjestu
	perina, err := k.users.GetDutiesForUser(k.pero.ID)
	if err != nil || len(perina) != 1 {
		t.Fatalf("Perina zaduženja: %v %+v", err, perina)
	}
	izmijenjena := perina[0]
	izmijenjena.Title, izmijenjena.SectionCodes = "Vodočuvar P.1.2", "P.1.2"
	if err := k.users.UpdateDuty(&izmijenjena); err != nil {
		t.Fatal(err)
	}
	provjeri("izmjena zaduženja", k.pero, -1, true, false)
	provjeri("izmjena zaduženja", k.pero, 1, false, true)
	provjeri("izmjena zaduženja", k.pero, -61, true, false)
}

// List i prijava s dana prije nego što je vodočuvar u programu dobio ikoje
// zaduženje (prijenos iz ranije evidencije, uvoz prijava iz BP16, koji ne
// upisuje dionicu) vezani su uz zaduženja s kojima je u program ušao
// (Odluke (8. 10. 2026.), pitanje 1: rukovoditelj dionice čita prijave
// vezane uz svoju dionicu). Pero Perić u program je ušao kao vodočuvar
// P.1.1, Ana Anić kao vodočuvar cijelog područja; njihova prijava i list
// od 1. 5. 2025. vezani su uz P.1.1, odnosno uz svaku dionicu područja.
// Ivo Ivić ušao je kao vodočuvar P.1.3 i poslije prešao na P.1.2: stari
// zapis veže se uz P.1.3. Podaci su izmišljeni.
func TestVezaSDionicomPrijeZaduzenjaUProgramu(t *testing.T) {
	k := novaKnjigaProba(t)
	vs := k.o.vs
	prijave := &PrijavaService{vodocuvar: vs}
	sad := time.Now()
	ivo := k.vodocuvar(t, "iivic", "Ivo Ivić", "P.1.3")
	for _, u := range []*models.User{k.pero, k.ana, ivo} {
		k.zaduzenOd(t, u, sad.AddDate(0, 0, -30))
	}
	stara, err := k.users.GetDutiesForUser(ivo.ID)
	if err != nil || len(stara) != 1 {
		t.Fatalf("Ivina zaduženja: %v %+v", err, stara)
	}
	if err := k.users.RevokeDuty(stara[0].ID); err != nil {
		t.Fatal(err)
	}
	nova := models.Duty{UserID: ivo.ID, Title: "Vodočuvar P.1.2", Role: models.RoleWaterGuard, ScopeType: models.ScopeSection, SectorID: &k.sektor, AreaID: &k.p1, SectionCodes: "P.1.2", IsActive: true, IsPrimary: true}
	if err := k.users.AddDuty(&nova); err != nil {
		t.Fatal(err)
	}
	dan := time.Date(2025, 5, 1, 9, 30, 0, 0, models.Zagreb)
	rukovoditelj := k.osoba(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &k.p1})
	dionicari := map[string]*models.UserPermissions{}
	for _, d := range []string{"P.1.1", "P.1.2", "P.1.3"} {
		dionicari[d] = k.dionicar(models.RoleSectionLeader, &k.p1, d)
	}
	for _, x := range []struct {
		u      *models.User
		vezane map[string]bool
	}{
		{k.pero, map[string]bool{"P.1.1": true}},
		{k.ana, map[string]bool{"P.1.1": true, "P.1.2": true, "P.1.3": true}},
		{ivo, map[string]bool{"P.1.3": true}},
	} {
		p := &models.PrijavaSTerena{UserID: x.u.ID.String(), Sektor: "P", AreaID: 1, Datum: dan, Status: models.PrijavaObjavljena, Rekonstrukcija: true, Izvor: "bp16:obavijesti_sa_terena:1"}
		l := &models.VodocuvarskiList{UserID: x.u.ID.String(), Ime: x.u.FullName, Sektor: "P", AreaID: 1, Datum: dan, Rekonstrukcija: true}
		if !prijave.SmijeVidjeti(rukovoditelj, p) || !vs.SmijeVidjeti(rukovoditelj, l) {
			t.Errorf("%s: rukovoditelj područja ne čita stari zapis", x.u.FullName)
		}
		for d, perms := range dionicari {
			if got := prijave.SmijeVidjeti(perms, p); got != x.vezane[d] {
				t.Errorf("%s: prijava od 1. 5. 2025., rukovoditelj %s = %v, očekivano %v", x.u.FullName, d, got, x.vezane[d])
			}
			if got := vs.SmijeUpisati(perms, l); got != x.vezane[d] {
				t.Errorf("%s: list od 1. 5. 2025., bilješka rukovoditelja %s = %v, očekivano %v", x.u.FullName, d, got, x.vezane[d])
			}
			if !vs.SmijeVidjeti(perms, l) {
				t.Errorf("%s: rukovoditelj %s ne čita stari list", x.u.FullName, d)
			}
		}
	}
}
