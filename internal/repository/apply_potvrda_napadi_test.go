package repository

// Potvrda promjene prava ključem mreže pod napadom i u nezgodnom redoslijedu
// (pregled grane potvrde ključem mreže, 30. 10. 2026.): podmetnuto ime
// računala s ključem, verzija iz budućnosti s drugom oznakom zapisa,
// oduzimanje s računala bez ključa i vraćanje starih prava, potvrda i
// odbijanje iste verzije, novo računalo, potpis stanja i provjera potpisa
// jednom. Računala su baze bez mreže između njih. Imena su izmišljena.

import (
	"encoding/binary"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// oznakaPoslije je oznaka verzije s vremenom za d od sada
func oznakaPoslije(t *testing.T, d time.Duration) string {
	t.Helper()
	u, err := uuid.NewV7()
	nuzno(t, err)
	var ms [8]byte
	binary.BigEndian.PutUint64(ms[:], uint64(time.Now().Add(d).UnixMilli()))
	copy(u[:6], ms[2:])
	return u.String()
}

// verzijeZapisa su verzije iz knjige računala c koje zadovoljavaju uvjet
func verzijeZapisa(t *testing.T, c *cvor, uvjet func(ledger.Version) bool) []ledger.Version {
	t.Helper()
	sve, err := c.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)
	return slices.DeleteFunc(sve, func(v ledger.Version) bool { return !uvjet(v) })
}

// cekajuVerzije su oznake verzija koje čekaju na računalu c
func cekajuVerzije(t *testing.T, c *cvor) []string {
	t.Helper()
	var out []string
	for _, p := range cekaju(t, c) {
		out = append(out, p.VersionID)
	}
	slices.Sort(out)
	return out
}

// Verzija s imenom računala s ključem koja je stigla razmjenom nije
// vlastita: na računalu s ključem čeka kao i svaka tuđa, prijava je ne
// potpiše (prijava se dok promjena čeka ni ne bilježi), a ni potpis stanja
// pri pokretanju. Drugo računalo je dobije i dalje samo na čekanju.
func TestPotvrdaKljucemPodmetnutoImeRacunala(t *testing.T) {
	m := novaMreza(t, laptopMreze)
	k, b := m.kljucem, m.ostala[0]
	u := noviRacun("mmarkic")
	nuzno(t, m.racuni(k).CreateUser(&u, nil))
	posalji(t, k, b)

	sZastavicom := u
	sZastavicom.IsGlobalAdmin = true
	lazna := tudjiRacun(t, sZastavicom)
	lazna.NodeID = racunaloSKljucem
	primi(t, k, []ledger.Version{lazna})
	if stupacRacuna(t, k, u.ID, "is_global_admin") != "0" {
		t.Fatal("verzija s imenom računala s ključem prošla je bez potvrde")
	}
	jedinaCeka(t, k, lazna)

	nuzno(t, m.racuni(k).MarkLogin(u.ID, time.Now()))
	if n, err := PotpisiStanjePrava(ctxRaz, k.db, k.rec, m.potpis); err != nil || n != 0 {
		t.Errorf("potpis stanja: %d %v", n, err)
	}
	if p := potvrdeUKnjizi(t, k, u.ID.String()); len(p) != 0 {
		t.Fatalf("podmetnuta verzija je dobila potvrdu: %+v", p)
	}
	posalji(t, k, b)
	if stupacRacuna(t, b, u.ID, "is_global_admin") != "0" {
		t.Error("podmetnuta verzija vrijedi na drugom računalu")
	}
	jedinaCeka(t, b, lazna)
}

// Verzija iz budućnosti koja u sadržaju nosi tuđi račun ne broji se: prijava
// na računalu s ključem potpisuje samo ono što je upravo upisala (ovdje
// ništa, jer prijava prava ne mijenja), pa kasnija verzija sa zastavicom i
// tuđom lozinkom čeka svugdje.
func TestPotvrdaKljucemVerzijaIzBuducnosti(t *testing.T) {
	m := novaMreza(t, laptopMreze)
	k, b := m.kljucem, m.ostala[0]
	zrtva, napadac := noviAdmin("zrtva"), noviRacun("napadac")
	zrtva.PasswordHash = "poznati-sazetak"
	nuzno(t, m.racuni(k).CreateUser(&zrtva, nil))
	nuzno(t, m.racuni(k).CreateUser(&napadac, nil))
	posalji(t, k, b)

	smece := tudjiRacun(t, zrtva)
	smece.EntityID, smece.VersionID = napadac.ID.String(), oznakaPoslije(t, 4*time.Minute)
	primi(t, k, []ledger.Version{smece})
	nuzno(t, m.racuni(k).MarkLogin(napadac.ID, time.Now()))
	if p := potvrdeUKnjizi(t, k, napadac.ID.String()); len(p) != 0 {
		t.Fatalf("prijava je potpisala: %+v", p)
	}

	preuzet := napadac
	preuzet.IsGlobalAdmin, preuzet.PasswordHash = true, zrtva.PasswordHash
	v9 := tudjiRacun(t, preuzet)
	v9.VersionID = oznakaPoslije(t, 4*time.Minute+time.Second)
	primi(t, k, []ledger.Version{v9})
	posalji(t, k, b)
	for _, c := range []*cvor{k, b} {
		if stupacRacuna(t, c, napadac.ID, "is_global_admin") != "0" || !slices.Contains(cekajuVerzije(t, c), v9.VersionID) {
			t.Errorf("%s: zastavica %q, čeka %v", c.ime, stupacRacuna(t, c, napadac.ID, "is_global_admin"), cekajuVerzije(t, c))
		}
	}
}

// Oduzimanje prava s računala bez ključa ne poništava novija verzija sa
// starim pravima: na računalu s ključem oduzimanje i dalje čeka (vidi se),
// na računalu s kojeg je oduzeto ostaje oduzeto, a kad ga administrator
// potvrdi, vraćanje starih prava čeka svugdje. Za račun s potvrdom (stanje
// iz knjige) i za račun bez nje (usporedba s površinom).
func TestPotvrdaKljucemOduzimanjeNePonistavaStaraPrava(t *testing.T) {
	for _, s := range []struct {
		naziv string
		admin bool
	}{{"potvrden_administrator", true}, {"bez_potvrde", false}} {
		t.Run(s.naziv, func(t *testing.T) {
			m := novaMreza(t, laptopMreze, treceRacunalo)
			k, n, c := m.kljucem, m.ostala[0], m.ostala[1]
			u := noviRacun("mmarkic")
			u.IsGlobalAdmin = s.admin
			nuzno(t, m.racuni(k).CreateUser(&u, nil))
			posalji(t, k, n)
			posalji(t, k, c)

			naN, err := m.racuni(n).GetUserByID(u.ID)
			nuzno(t, err)
			naN.IsGlobalAdmin = !s.admin
			nuzno(t, m.racuni(n).UpdateUser(naN))
			v2 := verzijeZapisa(t, n, func(v ledger.Version) bool { return v.Entity == EntityUsers && v.NodeID == laptopMreze })[0]
			v3 := tudjiRacun(t, u) // stara prava, novija od oduzimanja
			primi(t, n, []ledger.Version{v3})
			posalji(t, n, k)
			posalji(t, n, c)
			staro, novo := map[bool]string{true: "1", false: "0"}[s.admin], map[bool]string{true: "0", false: "1"}[s.admin]
			if stupacRacuna(t, n, u.ID, "is_global_admin") != novo {
				t.Errorf("računalo s kojeg je promjena stigla vratilo je stara prava")
			}
			for _, x := range []*cvor{k, c} {
				if stupacRacuna(t, x, u.ID, "is_global_admin") != staro || !slices.Contains(cekajuVerzije(t, x), v2.VersionID) {
					t.Fatalf("%s: zastavica %q, čeka %v, a treba čekati %s", x.ime, stupacRacuna(t, x, u.ID, "is_global_admin"),
						cekajuVerzije(t, x), v2.VersionID)
				}
			}

			nuzno(t, PotvrdiPromjenuPrava(ctxRaz, k.db, k.rec, m.potpis, v2.VersionID, tkoOdlucuje))
			posalji(t, k, n)
			posalji(t, k, c)
			for _, x := range []*cvor{k, n, c} {
				if stupacRacuna(t, x, u.ID, "is_global_admin") != novo || !slices.Contains(cekajuVerzije(t, x), v3.VersionID) {
					t.Errorf("%s nakon potvrde: zastavica %q, čeka %v", x.ime, stupacRacuna(t, x, u.ID, "is_global_admin"), cekajuVerzije(t, x))
				}
			}
		})
	}
}

// Dva računala s ključem (učitana kopija) o istoj verziji odluče
// suprotno: vrijedi odbijanje, kojim god redom odluke stigle, i na
// računalu koje je potvrdu već primijenilo.
func TestPotvrdaKljucemPotvrdaIOdbijanjeIsteVerzije(t *testing.T) {
	m := novaMreza(t, laptopMreze, treceRacunalo, "server-ured", "windows-ured")
	k, b, k2, x, y := m.kljucem, m.ostala[0], m.ostala[1], m.ostala[2], m.ostala[3]
	potpis2 := kljucTesta{cvor: treceRacunalo, kljuc: m.potpis.kljuc}
	u := noviAdmin("ppetric")
	nuzno(t, m.racuni(b).CreateUser(&u, nil))
	for _, c := range []*cvor{k, k2, x, y} {
		posalji(t, b, c)
	}
	v := cekaju(t, k)[0].VersionID
	nuzno(t, PotvrdiPromjenuPrava(ctxRaz, k.db, k.rec, m.potpis, v, tkoOdlucuje))
	nuzno(t, OdbijPromjenuPrava(ctxRaz, k2.db, k2.rec, potpis2, v, "Marko Markić (mmarkic)"))

	posalji(t, k, x)
	if stupacRacuna(t, x, u.ID, "is_global_admin") != "1" {
		t.Fatal("potvrda nije stavila račun")
	}
	posalji(t, k2, x)
	posalji(t, k2, y)
	posalji(t, k, y)
	for _, c := range []*cvor{x, y, k, k2} {
		posalji(t, x, c)
		if stupacRacuna(t, c, u.ID, "id") != "" || len(cekaju(t, c)) != 0 {
			t.Errorf("%s: račun %q, čeka %+v", c.ime, stupacRacuna(t, c, u.ID, "id"), cekaju(t, c))
		}
		if o := zadnjeOdluke(t, c); len(o) != 1 || o[0].Stanje != PravaOdbijeno {
			t.Errorf("%s: odluke %+v", c.ime, o)
		}
	}
}

// Novo računalo koje potvrđenu verziju, potvrdu i noviju verziju koja čeka
// dobije u jednom paketu ima istu površinu kao računalo koje ih je dobilo
// u dvije razmjene: račun s potvrđenim pravima, a novija promjena čeka.
// Isto i nakon odbijanja novije.
func TestPotvrdaKljucemNovoRacunaloIstaPovrsina(t *testing.T) {
	m := novaMreza(t, laptopMreze, treceRacunalo, "novo-racunalo")
	k, b, c, novo := m.kljucem, m.ostala[0], m.ostala[1], m.ostala[2]
	u := noviAdmin("ppetric")
	nuzno(t, m.racuni(b).CreateUser(&u, nil))
	posalji(t, b, k)
	nuzno(t, PotvrdiPromjenuPrava(ctxRaz, k.db, k.rec, m.potpis, cekaju(t, k)[0].VersionID, tkoOdlucuje))
	posalji(t, k, c)
	nuzno(t, m.racuni(b).ChangePassword(u.ID, "novi-sazetak"))
	posalji(t, b, k)
	posalji(t, k, c)
	posalji(t, k, novo)
	for _, x := range []*cvor{k, c, novo} {
		if stupacRacuna(t, x, u.ID, "is_global_admin") != "1" || stupacRacuna(t, x, u.ID, "password_hash") != "h" || len(cekaju(t, x)) != 1 {
			t.Errorf("%s: zastavica %q, lozinka %q, čeka %+v", x.ime, stupacRacuna(t, x, u.ID, "is_global_admin"),
				stupacRacuna(t, x, u.ID, "password_hash"), cekaju(t, x))
		}
	}
	nuzno(t, OdbijPromjenuPrava(ctxRaz, k.db, k.rec, m.potpis, cekaju(t, k)[0].VersionID, tkoOdlucuje))
	posalji(t, k, c)
	posalji(t, k, novo)
	for _, x := range []*cvor{k, c, novo} {
		if stupacRacuna(t, x, u.ID, "password_hash") != "h" || len(cekaju(t, x)) != 0 {
			t.Errorf("%s nakon odbijanja: lozinka %q, čeka %+v", x.ime, stupacRacuna(t, x, u.ID, "password_hash"), cekaju(t, x))
		}
	}
}

// Potpis stanja pri pokretanju potpisuje samo prava koja su na površini
// računala s ključem: dužnost uprave organizacije s površine dobiva
// potvrdu stanja, a podmetnuta verzija koja je seli na račun koji čeka ne
// dobiva ništa. Kad se račun potvrdi, dužnost ostaje na starom računu, a
// premještaj čeka.
func TestPotpisiStanjePravaSamoSPovrsine(t *testing.T) {
	m := novaMrezaBezPrijelaza(t, laptopMreze, treceRacunalo)
	k, b, c := m.kljucem, m.ostala[0], m.ostala[1]
	bezPotpisa := NewUserRepository(k.db, k.rec)
	a := noviRacun("aanic")
	d := upravaZa(a.ID)
	nuzno(t, bezPotpisa.CreateUser(&a, &d))
	y := noviAdmin("yyurić")
	nuzno(t, m.racuni(b).CreateUser(&y, nil))
	posalji(t, b, k)
	premjestena := d
	premjestena.UserID = y.ID
	lazna := tudja(t, EntityDuties, d.ID.String(), premjestena)
	primi(t, k, []ledger.Version{lazna})

	_, err := PotpisiStanjePrava(ctxRaz, k.db, k.rec, m.potpis)
	nuzno(t, err)
	p := potvrdeUKnjizi(t, k, d.ID.String())
	if len(p) != 1 || p[0].VersionID == lazna.VersionID || p[0].Nacin != razmjena.NacinStanje {
		t.Fatalf("potvrde dužnosti: %+v", p)
	}
	nuzno(t, PotvrdiPromjenuPrava(ctxRaz, k.db, k.rec, m.potpis, jedinaCekaRacuna(t, k, y.ID), tkoOdlucuje))
	posalji(t, k, c)
	for _, x := range []*cvor{k, c} {
		if !aktivnaDuznost(t, x, d.ID, a.ID) || !slices.Contains(cekajuVerzije(t, x), lazna.VersionID) {
			t.Errorf("%s: dužnost na starom računu %v, čeka %v", x.ime, aktivnaDuznost(t, x, d.ID, a.ID), cekajuVerzije(t, x))
		}
	}
}

// jedinaCekaRacuna je oznaka verzije koja čeka za račun id
func jedinaCekaRacuna(t *testing.T, c *cvor, id uuid.UUID) string {
	t.Helper()
	for _, p := range cekaju(t, c) {
		if p.Entitet == EntityUsers && p.Zapis == id.String() {
			return p.VersionID
		}
	}
	t.Fatalf("za račun %s ništa ne čeka", id)
	return ""
}

// Izmjena na računalu bez ključa ondje vrijedi odmah, a kad je računalo s
// ključem odbije, ni ondje više ne vrijedi
func TestPotvrdaKljucemVlastitaIzmjenaIOdbijanje(t *testing.T) {
	m := novaMreza(t, laptopMreze)
	k, b := m.kljucem, m.ostala[0]
	u := noviAdmin("mmarkic")
	nuzno(t, m.racuni(k).CreateUser(&u, nil))
	posalji(t, k, b)
	naB, err := m.racuni(b).GetUserByID(u.ID)
	nuzno(t, err)
	naB.IsActive = false
	nuzno(t, m.racuni(b).UpdateUser(naB))
	posalji(t, k, b) // ponovna primjena ne vraća vlastitu izmjenu
	if stupacRacuna(t, b, u.ID, "is_active") != "0" {
		t.Fatal("vlastita izmjena ne vrijedi na računalu bez ključa")
	}
	posalji(t, b, k)
	nuzno(t, OdbijPromjenuPrava(ctxRaz, k.db, k.rec, m.potpis, cekaju(t, k)[0].VersionID, tkoOdlucuje))
	posalji(t, k, b)
	if stupacRacuna(t, b, u.ID, "is_active") != "1" {
		t.Error("odbijena izmjena i dalje vrijedi na računalu s kojeg je stigla")
	}
}

// Novija verzija s drugom oznakom zapisa ne sprječava potvrdu: računalo s
// ključem potvrđuje, a drugo računalo potvrdu primijeni
func TestPotvrdaKljucemNovijaVerzijaSDrugomOznakom(t *testing.T) {
	m := novaMreza(t, laptopMreze, treceRacunalo)
	k, b, c := m.kljucem, m.ostala[0], m.ostala[1]
	u := noviAdmin("ppetric")
	nuzno(t, m.racuni(b).CreateUser(&u, nil))
	posalji(t, b, k)
	posalji(t, b, c)
	drugi := noviRacun("drugi")
	smece := tudjiRacun(t, drugi)
	smece.EntityID = u.ID.String()
	primi(t, k, []ledger.Version{smece})
	primi(t, c, []ledger.Version{smece})
	nuzno(t, PotvrdiPromjenuPrava(ctxRaz, k.db, k.rec, m.potpis, cekaju(t, k)[0].VersionID, tkoOdlucuje))
	posalji(t, k, c)
	for _, x := range []*cvor{k, c} {
		if stupacRacuna(t, x, u.ID, "is_global_admin") != "1" || len(cekaju(t, x)) != 0 {
			t.Errorf("%s: zastavica %q, čeka %+v", x.ime, stupacRacuna(t, x, u.ID, "is_global_admin"), cekaju(t, x))
		}
	}
}

// Potpis iste verzije zapisa potvrde provjerava se jednom; mnoštvo lažnih
// potvrda ne raste pamćenje bez kraja
func TestPotvrdaKljucemProvjeraJednom(t *testing.T) {
	m := novaMreza(t)
	k := m.kljucem
	u := noviAdmin("mmarkic")
	nuzno(t, m.racuni(k).CreateUser(&u, nil))
	provjerenePotvrde.Lock()
	provjerenePotvrde.ishodi = map[string]bool{}
	provjerenePotvrde.Unlock()
	prije := najviseProvjera
	t.Cleanup(func() { najviseProvjera = prije })
	najviseProvjera = 3

	lazne := make([]ledger.Version, 3)
	for i := range lazne {
		p := razmjena.PotvrdaPrava{Odluka: razmjena.OdlukaPotvrdeno, VersionID: vid(i + 1), Entitet: EntityUsers, Zapis: u.ID.String(),
			Mreza: razmjena.PublicKeyString(m.potpis.kljuc.Public), Potpis: "lazni"}
		p.ID = p.Oznaka()
		lazne[i] = tudja(t, EntityPotvrdePrava, p.ID, p)
	}
	primi(t, k, lazne[:1])
	broj := func() int {
		provjerenePotvrde.Lock()
		defer provjerenePotvrde.Unlock()
		return len(provjerenePotvrde.ishodi)
	}
	if n := broj(); n != 2 {
		t.Fatalf("zapamćeno %d provjera, a treba 2 (prava i lažna)", n)
	}
	primi(t, k, lazne[:1])
	if n := broj(); n != 2 {
		t.Errorf("ista provjera zapamćena dvaput: %d", n)
	}
	primi(t, k, lazne[1:])
	if n := broj(); n > najviseProvjera {
		t.Errorf("zapamćeno %d provjera, više od %d", n, najviseProvjera)
	}
	if stupacRacuna(t, k, u.ID, "is_global_admin") != "1" {
		t.Error("lažne potvrde su promijenile račun")
	}
}
