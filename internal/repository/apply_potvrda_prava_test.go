package repository

// Potvrda promjene prava ključem mreže (apply_potvrda_prava.go). Računala su
// baze bez mreže između njih: posalji i primi rade što i razmjena. Imena su
// izmišljena.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/razmjena"
)

const (
	racunaloSKljucem = "cop-osijek"
	laptopMreze      = "laptop-dalj"
	treceRacunalo    = "windows-ured"
)

// kljucTesta potpisuje kao peers.Service na računalu s ključem mreže
type kljucTesta struct {
	cvor  string
	kljuc razmjena.NetworkKey
}

func (k kljucTesta) DrziKljucMreze() bool { return k.kljuc.CanSign() }

func (k kljucTesta) PotpisiPotvrdu(p razmjena.PotvrdaPrava) (razmjena.PotvrdaPrava, bool) {
	p.Racunalo = k.cvor
	s, err := k.kljuc.PotpisiPotvrdu(p)
	return s, err == nil
}

// mrezaRacunala su računalo s ključem mreže i druga računala iste mreže,
// sa ključem za potpis
type mrezaRacunala struct {
	kljucem *cvor
	ostala  []*cvor
	potpis  kljucTesta
}

// novaMreza: prvo računalo drži ključ mreže, ostala samo javni ključ.
// Računalo s ključem je pokrenuto (potpisan je prijelaz) i ostala su ga
// dobila.
func novaMreza(t *testing.T, imena ...string) mrezaRacunala {
	t.Helper()
	m := novaMrezaBezPrijelaza(t, imena...)
	_, err := PotpisiStanjePrava(ctxRaz, m.kljucem.db, m.kljucem.rec, m.potpis)
	nuzno(t, err)
	for _, c := range m.ostala {
		posalji(t, m.kljucem, c)
	}
	return m
}

// novaMrezaBezPrijelaza je mreža u kojoj računalo s ključem još nije
// pokrenuto novim programom
func novaMrezaBezPrijelaza(t *testing.T, imena ...string) mrezaRacunala {
	t.Helper()
	sNedovrsenima(t)
	n, err := razmjena.NewNetwork("Izmišljena mreža")
	nuzno(t, err)
	m := mrezaRacunala{potpis: kljucTesta{cvor: racunaloSKljucem, kljuc: n}}
	for i, ime := range append([]string{racunaloSKljucem}, imena...) {
		c := noviCvor(t, ime)
		uMrezu(t, c, n.Public)
		if i == 0 {
			m.kljucem = c
		} else {
			m.ostala = append(m.ostala, c)
		}
	}
	return m
}

func uMrezu(t *testing.T, c *cvor, javni []byte) {
	t.Helper()
	_, err := c.db.Exec(`INSERT INTO network (id, name, public_key, joined_at) VALUES (1, 'Izmišljena mreža', ?, ?)`,
		razmjena.PublicKeyString(javni), time.Now().UTC())
	nuzno(t, err)
}

// racuni su računi na računalu c; na računalu s ključem s potpisom
func (m mrezaRacunala) racuni(c *cvor) *UserRepository {
	r := NewUserRepository(c.db, c.rec)
	if c == m.kljucem {
		r.PostaviPotpisMreze(m.potpis)
	}
	return r
}

func noviAdmin(ime string) models.User {
	u := noviRacun(ime)
	u.IsGlobalAdmin = true
	return u
}

// potvrdeUKnjizi su potvrde ključem mreže u knjizi računala c za zapis id
func potvrdeUKnjizi(t *testing.T, c *cvor, id string) []razmjena.PotvrdaPrava {
	t.Helper()
	rows, err := c.db.Query(`SELECT payload FROM record_versions WHERE entity = ? AND entity_id LIKE ?`, EntityPotvrdePrava, "%:"+id+":%")
	nuzno(t, err)
	defer rows.Close()
	var out []razmjena.PotvrdaPrava
	for rows.Next() {
		var s string
		nuzno(t, rows.Scan(&s))
		var p razmjena.PotvrdaPrava
		nuzno(t, json.Unmarshal([]byte(s), &p))
		out = append(out, p)
	}
	return out
}

// zadnjeOdluke su zadnje odluke na računalu c
func zadnjeOdluke(t *testing.T, c *cvor) []PromjenaPrava {
	t.Helper()
	_, odluke, err := PromjenePrava(ctxRaz, c.db, c.rec, 10)
	nuzno(t, err)
	return odluke
}

// (a) Izmjena prava na računalu s ključem mreže odmah dobiva potvrdu i na
// drugom računalu vrijedi bez čekanja. Prijava istim računom ne traži novu
// potvrdu, a oduzeta zastavica dobiva svoju.
func TestPotvrdaKljucemIzmjenaNaRacunaluSKljucem(t *testing.T) {
	m := novaMreza(t, laptopMreze)
	k, b := m.kljucem, m.ostala[0]
	u := noviAdmin("mmarkic")
	nuzno(t, m.racuni(k).CreateUser(&u, nil))
	if p := potvrdeUKnjizi(t, k, u.ID.String()); len(p) != 1 || p[0].Odluka != razmjena.OdlukaPotvrdeno || p[0].Potvrdio != "" {
		t.Fatalf("potvrde nakon upisa: %+v", p)
	}
	posalji(t, k, b)
	if stupacRacuna(t, b, u.ID, "is_global_admin") != "1" || len(cekaju(t, b)) != 0 {
		t.Fatalf("zastavica na drugom računalu: %q, čeka %+v", stupacRacuna(t, b, u.ID, "is_global_admin"), cekaju(t, b))
	}
	if o := zadnjeOdluke(t, b); len(o) != 1 || !strings.Contains(o[0].Odlucio, "izmjena na računalu s ključem mreže (cop-osijek)") {
		t.Errorf("odluke na drugom računalu: %+v", o)
	}

	nuzno(t, m.racuni(k).MarkLogin(u.ID, time.Now()))
	if p := potvrdeUKnjizi(t, k, u.ID.String()); len(p) != 1 {
		t.Errorf("prijava je dobila novu potvrdu: %d", len(p))
	}
	u.IsGlobalAdmin = false
	nuzno(t, m.racuni(k).UpdateUser(&u))
	if p := potvrdeUKnjizi(t, k, u.ID.String()); len(p) != 2 {
		t.Errorf("oduzeta zastavica nema potvrdu: %d", len(p))
	}
	posalji(t, k, b)
	if stupacRacuna(t, b, u.ID, "is_global_admin") != "0" || len(cekaju(t, b)) != 0 {
		t.Error("oduzeta zastavica čeka na drugom računalu")
	}

	// računalo bez ključa izmjenu ne potpisuje
	nuzno(t, m.racuni(b).ChangePassword(u.ID, "novi"))
	if p := potvrdeUKnjizi(t, b, u.ID.String()); len(p) != 2 {
		t.Errorf("računalo bez ključa je potpisalo: %d", len(p))
	}
}

// (b) Promjena s drugog računala čeka na svim računalima dok je globalni
// administrator jednom ne potvrdi na računalu s ključem mreže. Potvrda stiže
// trećem računalu poslije verzije i stavlja na površinu račun i dužnost koja
// ga je čekala, bez nove verzije. Na računalu bez ključa odluke nema.
func TestPotvrdaKljucemPromjenaSDrugogRacunala(t *testing.T) {
	m := novaMreza(t, laptopMreze, treceRacunalo)
	k, b, c := m.kljucem, m.ostala[0], m.ostala[1]
	u := noviAdmin("ppetric")
	sektor := "B"
	d := &models.Duty{Title: "Rukovoditelj sektora", Role: models.RoleSectorLeader, ScopeType: models.ScopeSector, SectorID: &sektor, IsActive: true}
	nuzno(t, m.racuni(b).CreateUser(&u, d))
	posalji(t, b, k)
	posalji(t, b, c)
	for _, x := range []*cvor{k, c} {
		if len(cekaju(t, x)) != 1 || stupacRacuna(t, x, u.ID, "id") != "" || duznostUPovrsini(t, x, d.ID) {
			t.Fatalf("%s: račun je stigao bez potvrde: čeka %+v", x.ime, cekaju(t, x))
		}
	}
	ceka := cekaju(t, c)[0]
	if err := PotvrdiPromjenuPrava(ctxRaz, c.db, c.rec, nil, ceka.VersionID, tkoOdlucuje); !errors.Is(err, ErrOdlukaNaRacunaluSKljucem) {
		t.Fatalf("odluka na računalu bez ključa: %v", err)
	}
	verzijaPrije := brojVerzijaUKnjizi(t, c)

	nuzno(t, PotvrdiPromjenuPrava(ctxRaz, k.db, k.rec, m.potpis, cekaju(t, k)[0].VersionID, tkoOdlucuje))
	if stupacRacuna(t, k, u.ID, "is_global_admin") != "1" || !duznostUPovrsini(t, k, d.ID) {
		t.Fatal("na računalu s ključem potvrda nije stavila račun i dužnost")
	}
	p := potvrdeUKnjizi(t, k, u.ID.String())
	if len(p) != 1 || p[0].Potvrdio != tkoOdlucuje || p[0].VersionID != ceka.VersionID {
		t.Fatalf("potvrda u knjizi: %+v", p)
	}
	posalji(t, k, c)
	if stupacRacuna(t, c, u.ID, "is_global_admin") != "1" || !duznostUPovrsini(t, c, d.ID) || len(cekaju(t, c)) != 0 {
		t.Fatalf("treće računalo: zastavica %q, dužnost %v, čeka %+v", stupacRacuna(t, c, u.ID, "is_global_admin"),
			duznostUPovrsini(t, c, d.ID), cekaju(t, c))
	}
	if n := brojVerzijaUKnjizi(t, c); n != verzijaPrije+1 {
		t.Errorf("treće računalo ima %d verzija, a treba %d: samo potvrdu, bez nove verzije", n, verzijaPrije+1)
	}
	if o := zadnjeOdluke(t, c); len(o) != 1 || o[0].Stanje != PravaPotvrdeno ||
		o[0].Odlucio != tkoOdlucuje+", na računalu s ključem mreže (cop-osijek)" {
		t.Errorf("odluke na trećem računalu: %+v", o)
	}
	// i računalu s kojeg je promjena stigla
	posalji(t, k, b)
	if stupacRacuna(t, b, u.ID, "is_global_admin") != "1" {
		t.Error("računalo s kojeg je promjena stigla je izgubilo zastavicu")
	}
}

// Potvrda koja stigne prije verzije vrijedi kad verzija stigne; novo
// računalo dobije i račune s prije potvrđenim pravima bez čekanja, i kad
// stigne samo zadnja verzija računa (prijava na drugom računalu).
func TestPotvrdaKljucemPrijeVerzije(t *testing.T) {
	m := novaMreza(t, laptopMreze, treceRacunalo)
	k, b, novo := m.kljucem, m.ostala[0], m.ostala[1]
	u := noviAdmin("iivic")
	nuzno(t, m.racuni(k).CreateUser(&u, nil))
	posalji(t, k, b)
	nuzno(t, m.racuni(b).MarkLogin(u.ID, time.Now()))

	sve, err := b.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)
	var potvrde, racun []ledger.Version
	for _, v := range sve {
		switch v.Entity {
		case EntityPotvrdePrava:
			if strings.HasPrefix(v.EntityID, EntityUsers+":") { // bez potvrde prijelaza
				potvrde = append(potvrde, v)
			}
		case EntityUsers:
			racun = append(racun, v)
		}
	}
	zadnja := racun[len(racun)-1]
	if zadnja.NodeID != laptopMreze || len(potvrde) != 1 {
		t.Fatalf("zadnja verzija računa %s, potvrda %d", zadnja.NodeID, len(potvrde))
	}
	primi(t, novo, potvrde)
	if stupacRacuna(t, novo, u.ID, "id") != "" {
		t.Fatal("račun bez verzije")
	}
	primi(t, novo, []ledger.Version{zadnja})
	if stupacRacuna(t, novo, u.ID, "is_global_admin") != "1" || len(cekaju(t, novo)) != 0 {
		t.Fatalf("novo računalo: zastavica %q, čeka %+v", stupacRacuna(t, novo, u.ID, "is_global_admin"), cekaju(t, novo))
	}
}

// Krivotvorena potvrda ne vrijedi: drugi ključ, izmijenjen sadržaj
// verzije, potvrda druge verzije s drugim pravima, ni potvrda kojoj je
// odluka promijenjena nakon potpisa.
func TestPotvrdaKljucemKrivotvorena(t *testing.T) {
	tudjiKljuc, err := razmjena.NewNetwork("Druga mreža")
	nuzno(t, err)
	for _, s := range []struct {
		naziv   string
		pokvari func(t *testing.T, m mrezaRacunala, v ledger.Version, p razmjena.PotvrdaPrava) (ledger.Version, razmjena.PotvrdaPrava)
	}{
		{"drugi_kljuc", func(t *testing.T, _ mrezaRacunala, v ledger.Version, p razmjena.PotvrdaPrava) (ledger.Version, razmjena.PotvrdaPrava) {
			p, err := tudjiKljuc.PotpisiPotvrdu(p)
			nuzno(t, err)
			return v, p
		}},
		{"izmijenjen_sadrzaj", func(t *testing.T, _ mrezaRacunala, v ledger.Version, p razmjena.PotvrdaPrava) (ledger.Version, razmjena.PotvrdaPrava) {
			var uv userVersion
			nuzno(t, json.Unmarshal(v.Payload, &uv))
			uv.PasswordHash = "podmetnuti-sazetak"
			v.Payload, err = json.Marshal(uv)
			nuzno(t, err)
			return v, p
		}},
		{"druga_verzija", func(t *testing.T, _ mrezaRacunala, v ledger.Version, p razmjena.PotvrdaPrava) (ledger.Version, razmjena.PotvrdaPrava) {
			var uv userVersion
			nuzno(t, json.Unmarshal(v.Payload, &uv))
			uv.PasswordHash = "podmetnuti-sazetak"
			druga := tudja(t, EntityUsers, v.EntityID, uv)
			return druga, p
		}},
		{"promijenjena_odluka", func(t *testing.T, _ mrezaRacunala, v ledger.Version, p razmjena.PotvrdaPrava) (ledger.Version, razmjena.PotvrdaPrava) {
			p.Prava = `{"zastavica":true,"aktivan":true,"lozinka":"podmetnuti-sazetak","obaveznaPromjena":false}`
			return v, p
		}},
	} {
		t.Run(s.naziv, func(t *testing.T) {
			m := novaMreza(t, laptopMreze, treceRacunalo)
			k, b, c := m.kljucem, m.ostala[0], m.ostala[1]
			u := noviAdmin("ppetric")
			nuzno(t, m.racuni(b).CreateUser(&u, nil))
			posalji(t, b, k)
			ceka := cekaju(t, k)[0]
			nuzno(t, PotvrdiPromjenuPrava(ctxRaz, k.db, k.rec, m.potpis, ceka.VersionID, tkoOdlucuje))
			v, err := k.rec.Latest(ctxRaz, EntityUsers, u.ID.String())
			nuzno(t, err)
			prava := potvrdeUKnjizi(t, k, u.ID.String())[0]
			lazna, laznaPotvrda := s.pokvari(t, m, *v, prava)
			p, err := json.Marshal(laznaPotvrda)
			nuzno(t, err)
			pv := tudja(t, EntityPotvrdePrava, laznaPotvrda.ID+"-lazna", nil)
			pv.Payload = p
			pv.EntityID = laznaPotvrda.Oznaka()
			if laznaPotvrda.Potpis == prava.Potpis && laznaPotvrda.Prava == prava.Prava {
				pv.EntityID += "-kopija" // ista potvrda pod drugom oznakom zapisa
			}
			primi(t, c, []ledger.Version{lazna, pv})
			// prava koja vrijede su samo ona koja je potpisalo računalo s
			// ključem: uz pravu potvrdu verzije koja nije stigla to je
			// njezina zastavica i lozinka, a podmetnuta verzija čeka
			zastavica := map[bool]string{true: "1", false: ""}[s.naziv == "druga_verzija"]
			if stupacRacuna(t, c, u.ID, "is_global_admin") != zastavica || stupacRacuna(t, c, u.ID, "password_hash") == "podmetnuti-sazetak" {
				t.Fatalf("krivotvorena potvrda: zastavica %q, lozinka %q", stupacRacuna(t, c, u.ID, "is_global_admin"),
					stupacRacuna(t, c, u.ID, "password_hash"))
			}
			if ceka := cekaju(t, c); len(ceka) != 1 || ceka[0].VersionID != lazna.VersionID {
				t.Errorf("čeka %+v", ceka)
			}
		})
	}
}

// (e) Odbijanje na računalu s ključem mreže vrijedi svugdje; odbijena
// verzija ostaje samo u knjizi, a novija s istim pravima ostaje odbijena.
// Potvrda novije verzije s drugim pravima opet vrijedi.
func TestPotvrdaKljucemOdbijanje(t *testing.T) {
	m := novaMreza(t, laptopMreze, treceRacunalo)
	k, b, c := m.kljucem, m.ostala[0], m.ostala[1]
	u := noviRacun("aanic")
	nuzno(t, m.racuni(k).CreateUser(&u, nil))
	posalji(t, k, b)
	posalji(t, k, c)
	naB, err := m.racuni(b).GetUserByID(u.ID)
	nuzno(t, err)
	naB.IsGlobalAdmin = true
	nuzno(t, m.racuni(b).UpdateUser(naB))
	posalji(t, b, k)
	posalji(t, b, c)
	nuzno(t, OdbijPromjenuPrava(ctxRaz, k.db, k.rec, m.potpis, cekaju(t, k)[0].VersionID, tkoOdlucuje))
	posalji(t, k, c)
	if stupacRacuna(t, c, u.ID, "is_global_admin") != "0" || len(cekaju(t, c)) != 0 {
		t.Fatalf("odbijeno na trećem računalu: zastavica %q, čeka %+v", stupacRacuna(t, c, u.ID, "is_global_admin"), cekaju(t, c))
	}
	if o := zadnjeOdluke(t, c); len(o) != 1 || o[0].Stanje != PravaOdbijeno {
		t.Errorf("odluke: %+v", o)
	}

	// ista prava u novijoj verziji s računala bez ključa ostaju odbijena
	naB.Phone = "031 1"
	v := tudjiRacun(t, *naB)
	primi(t, c, []ledger.Version{v})
	if stupacRacuna(t, c, u.ID, "is_global_admin") != "0" || stupacRacuna(t, c, u.ID, "phone") != "031 1" || len(cekaju(t, c)) != 0 {
		t.Errorf("novija verzija odbijenih prava: zastavica %q, telefon %q, čeka %+v",
			stupacRacuna(t, c, u.ID, "is_global_admin"), stupacRacuna(t, c, u.ID, "phone"), cekaju(t, c))
	}

	// računalo s ključem kasnije samo dâ zastavicu: to vrijedi svugdje
	naK, err := m.racuni(k).GetUserByID(u.ID)
	nuzno(t, err)
	naK.IsGlobalAdmin = true
	nuzno(t, m.racuni(k).UpdateUser(naK))
	posalji(t, k, c)
	if stupacRacuna(t, c, u.ID, "is_global_admin") != "1" {
		t.Error("izmjena na računalu s ključem nakon odbijanja ne vrijedi")
	}
}

// Potvrda stara jednu odluku ne vraća prava koja je računalo s ključem
// poslije oduzelo: verzija s prije potvrđenim pravima, a novija od
// oduzimanja, čeka.
func TestPotvrdaKljucemStaraPravaNeVrijede(t *testing.T) {
	m := novaMreza(t, laptopMreze)
	k, c := m.kljucem, m.ostala[0]
	u := noviAdmin("mmarkic")
	nuzno(t, m.racuni(k).CreateUser(&u, nil))
	sAdminom := u
	u.IsGlobalAdmin = false
	nuzno(t, m.racuni(k).UpdateUser(&u))
	posalji(t, k, c)
	sAdminom.Phone = "099 9"
	v := tudjiRacun(t, sAdminom)
	primi(t, c, []ledger.Version{v})
	if stupacRacuna(t, c, u.ID, "is_global_admin") != "0" {
		t.Fatal("stara potvrda je vratila zastavicu")
	}
	jedinaCeka(t, c, v)
}

// (d) Obnova površine pri pokretanju: potvrda koja je u knjizi, a promjena
// i dalje čeka (npr. računalo je palo nakon upisa u knjigu), stavi promjenu
// na površinu.
func TestPotvrdaKljucemObnovaPovrsine(t *testing.T) {
	m := novaMreza(t, laptopMreze, treceRacunalo)
	k, b, c := m.kljucem, m.ostala[0], m.ostala[1]
	u := noviAdmin("ppetric")
	nuzno(t, m.racuni(b).CreateUser(&u, nil))
	posalji(t, b, k)
	posalji(t, b, c)
	nuzno(t, PotvrdiPromjenuPrava(ctxRaz, k.db, k.rec, m.potpis, cekaju(t, k)[0].VersionID, tkoOdlucuje))
	verzije, err := k.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)
	_, err = c.rec.Apply(ctxRaz, verzije) // samo u knjigu, kao da je primjena pala
	nuzno(t, err)
	if len(cekaju(t, c)) != 1 {
		t.Fatal("promjena ne čeka prije obnove")
	}
	_, err = ReplaySurface(ctxRaz, c.db, c.rec)
	nuzno(t, err)
	if stupacRacuna(t, c, u.ID, "is_global_admin") != "1" || len(cekaju(t, c)) != 0 {
		t.Errorf("nakon obnove: zastavica %q, čeka %+v", stupacRacuna(t, c, u.ID, "is_global_admin"), cekaju(t, c))
	}
}

// Stanje od prije potvrda ključem mreže: računalo s ključem ga potpiše
// jednom (pri pokretanju), samo ondje gdje bi drugdje bez potpisa ispalo
// drukčije: stari globalni administrator dobiva potvrdu, a obrisan običan
// račun i promjena koja ovdje čeka ne dobivaju. Novo računalo zatim dobije
// račune globalnih administratora bez čekanja.
func TestPotpisiStanjePrava(t *testing.T) {
	m := novaMrezaBezPrijelaza(t, laptopMreze, treceRacunalo)
	k, b, novo := m.kljucem, m.ostala[0], m.ostala[1]
	bezPotpisa := NewUserRepository(k.db, k.rec)
	admin := noviAdmin("staradmin")
	nuzno(t, bezPotpisa.CreateUser(&admin, nil))
	obrisan := noviRacun("obrisan")
	nuzno(t, bezPotpisa.CreateUser(&obrisan, nil))
	nuzno(t, bezPotpisa.DeleteUser(obrisan.ID))
	ceka := noviAdmin("ceka")
	nuzno(t, m.racuni(b).CreateUser(&ceka, nil))
	posalji(t, b, k)
	if len(cekaju(t, k)) != 1 {
		t.Fatalf("na računalu s ključem čeka %+v", cekaju(t, k))
	}

	if n, err := PotpisiStanjePrava(ctxRaz, b.db, b.rec, nil); err != nil || n != 0 {
		t.Errorf("bez ključa: %d %v", n, err)
	}
	n, err := PotpisiStanjePrava(ctxRaz, k.db, k.rec, m.potpis)
	nuzno(t, err)
	if n != 1 || len(potvrdeUKnjizi(t, k, ceka.ID.String())) != 0 || len(potvrdeUKnjizi(t, k, obrisan.ID.String())) != 0 {
		t.Fatalf("potpisano %d, za račun koji čeka %d", n, len(potvrdeUKnjizi(t, k, ceka.ID.String())))
	}
	if n, err := PotpisiStanjePrava(ctxRaz, k.db, k.rec, m.potpis); err != nil || n != 0 {
		t.Errorf("drugi put: %d %v", n, err)
	}
	posalji(t, k, novo)
	if stupacRacuna(t, novo, admin.ID, "is_global_admin") != "1" || stupacRacuna(t, novo, obrisan.ID, "id") != "" {
		t.Error("novo računalo nema starog administratora ili ima obrisan račun")
	}
	if p := cekaju(t, novo); len(p) != 1 || p[0].Racun != "Ana Anić (ceka)" {
		t.Errorf("na novom računalu čeka %+v", p)
	}
}

// Gdje se odlučuje: računalo bez mreže samo za sebe, računalo s ključem
// mreže za cijelu mrežu, a ostala računala mreže nigdje
func TestNacinOdlukePrava(t *testing.T) {
	m := novaMreza(t, laptopMreze)
	sam := noviCvor(t, "sam")
	for _, s := range []struct {
		c      *cvor
		potpis PotpisMreze
		nacin  string
	}{{sam, nil, OdlukaOvdje}, {m.kljucem, m.potpis, OdlukaKljucem}, {m.ostala[0], nil, OdlukaDrugdje},
		{m.ostala[0], kljucTesta{kljuc: razmjena.PublicNetwork("x", nil)}, OdlukaDrugdje}} {
		if n, err := NacinOdlukePrava(ctxRaz, s.c.db, s.potpis); err != nil || n != s.nacin {
			t.Errorf("%s: %q %v, treba %q", s.c.ime, n, err, s.nacin)
		}
	}
	_, err := sam.db.Exec(`INSERT INTO network (id, name, public_key, joined_at) VALUES (1, 'x', 'nije-kljuc', ?)`, time.Now())
	nuzno(t, err)
	if _, err := NacinOdlukePrava(ctxRaz, sam.db, nil); err == nil {
		t.Error("neispravan ključ mreže bez greške")
	}
}

// Greške baze se javljaju, a ne gutaju
func TestPotvrdaKljucemGreskeBaze(t *testing.T) {
	m := novaMreza(t, laptopMreze)
	k, b := m.kljucem, m.ostala[0]
	u := noviAdmin("ppetric")
	nuzno(t, m.racuni(b).CreateUser(&u, nil))
	posalji(t, b, k)
	bezPotpisa := noviRacun("bezpotpisa")
	nuzno(t, NewUserRepository(k.db, k.rec).CreateUser(&bezPotpisa, nil))
	id := cekaju(t, k)[0].VersionID
	_, err := k.db.Exec(`DROP TABLE network`)
	nuzno(t, err)
	if err := PotvrdiPromjenuPrava(ctxRaz, k.db, k.rec, m.potpis, id, tkoOdlucuje); err == nil {
		t.Error("odluka bez tablice mreže")
	}
	if _, err := PotpisiStanjePrava(ctxRaz, k.db, k.rec, m.potpis); err == nil {
		t.Error("potpis stanja bez tablice mreže")
	}
	bezPotpisa.Phone = "031 2"
	if err := m.racuni(k).UpdateUser(&bezPotpisa); err == nil {
		t.Error("izmjena bez tablice mreže")
	}
	if _, err := NacinOdlukePrava(ctxRaz, k.db, m.potpis); err == nil {
		t.Error("način odluke bez tablice mreže")
	}
}

// losPotpis kaže da drži ključ mreže, a ne potpisuje
type losPotpis struct{}

func (losPotpis) DrziKljucMreze() bool { return true }

func (losPotpis) PotpisiPotvrdu(p razmjena.PotvrdaPrava) (razmjena.PotvrdaPrava, bool) {
	return p, false
}

// Rubovi: potpis koji ne prođe ne ostavlja izmjenu, zapis potvrda koji se
// ne da pročitati ostaje samo u knjizi, a potvrđena promjena koja ne prođe
// na površinu i dalje čeka, bez greške primjene ostalog
func TestPotvrdaKljucemRubovi(t *testing.T) {
	m := novaMreza(t, laptopMreze, treceRacunalo)
	k, b, c := m.kljucem, m.ostala[0], m.ostala[1]

	r := NewUserRepository(k.db, k.rec)
	r.PostaviPotpisMreze(losPotpis{})
	u := noviAdmin("bezpotpisa")
	if err := r.CreateUser(&u, nil); !errors.Is(err, razmjena.ErrBezKljucaMreze) {
		t.Errorf("upis bez potpisa: %v", err)
	}
	if stupacRacuna(t, k, u.ID, "id") != "" {
		t.Error("račun je upisan bez potvrde")
	}

	pokvarena := tudja(t, EntityPotvrdePrava, "users:x:y:potvrdeno", nil)
	pokvarena.Payload = []byte(`"nije potvrda"`)
	primi(t, c, []ledger.Version{pokvarena})
	for _, o := range ogradePrava {
		if _, citljivo := o.prava(ledger.Version{Payload: []byte("{")}); citljivo {
			t.Error("prava sadržaja koji nije zapis")
		}
	}

	// na trećem računalu isto korisničko ime već ima drugi račun
	isti := noviAdmin("ppetric")
	nuzno(t, m.racuni(b).CreateUser(&isti, nil))
	posalji(t, b, k)
	posalji(t, b, c)
	drugi := noviRacun("ppetric")
	nuzno(t, NewUserRepository(c.db, c.rec).CreateUser(&drugi, nil))
	nuzno(t, PotvrdiPromjenuPrava(ctxRaz, k.db, k.rec, m.potpis, cekaju(t, k)[0].VersionID, tkoOdlucuje))
	verzije, err := k.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)
	_, err = c.rec.Apply(ctxRaz, verzije)
	nuzno(t, err)
	var nedovrseno *greskaNedovrsenih
	if err := ApplyVersions(ctxRaz, c.db, c.rec, verzije); !errors.As(err, &nedovrseno) {
		t.Errorf("račun koji ne prolazi na površinu: %v", err)
	}
	if stupacRacuna(t, c, isti.ID, "id") != "" || !nedovrsen(EntityUsers+"|"+isti.ID.String()) {
		t.Error("račun koji ne prolazi na površinu nije među nedovršenima")
	}
	if o := zadnjeOdluke(t, c); len(o) != 1 || o[0].Stanje != PravaPotvrdeno {
		t.Errorf("odluka na računalu na kojem račun ne prolazi: %+v", o)
	}
}
