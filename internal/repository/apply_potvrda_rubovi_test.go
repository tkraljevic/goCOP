package repository

// Rubovi potvrde promjene prava ključem mreže: prijelaz s promjenom koja
// čeka, stanje promijenjeno mimo programa, dvije promjene koje čekaju,
// potvrda koja stigne poslije verzije koja ne prolazi, i greške baze.
// Imena su izmišljena.

import (
	"context"
	"errors"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/razmjena"
)

// Prije prijelaza na računalu s ključem čeka oduzimanje zastavice s drugog
// računala. Prijelaz potpiše stanje s površine uz verziju prije te
// promjene, pa promjena i dalje čeka, i na računalu s ključem i na novom
// računalu, koje ima administratora.
func TestPrijelazSPromjenomKojaCeka(t *testing.T) {
	m := novaMrezaBezPrijelaza(t, laptopMreze, treceRacunalo)
	k, b, novo := m.kljucem, m.ostala[0], m.ostala[1]
	u := noviAdmin("mmarkic")
	nuzno(t, NewUserRepository(k.db, k.rec).CreateUser(&u, nil))
	posalji(t, k, b)
	bezZastavice := u
	bezZastavice.IsGlobalAdmin = false
	v := tudjiRacun(t, bezZastavice)
	primi(t, k, []ledger.Version{v})
	jedinaCeka(t, k, v)

	n, err := PotpisiStanjePrava(ctxRaz, k.db, k.rec, m.potpis)
	nuzno(t, err)
	p := potvrdeUKnjizi(t, k, u.ID.String())
	if n != 1 || len(p) != 1 || p[0].VersionID == v.VersionID {
		t.Fatalf("potpisano %d: %+v", n, p)
	}
	posalji(t, k, novo)
	for _, c := range []*cvor{k, novo} {
		if stupacRacuna(t, c, u.ID, "is_global_admin") != "1" {
			t.Errorf("%s: zastavica %q", c.ime, stupacRacuna(t, c, u.ID, "is_global_admin"))
		}
		jedinaCeka(t, c, v)
	}
}

// Sažetak lozinke promijenjen mimo programa (izravno u bazi) nema verziju s
// tim pravima: prijelaz potpiše stanje s površine uz zadnju verziju, pa
// novo računalo dobije prava iz potvrde, ne iz verzije
func TestPrijelazStanjeMimoProgramaPravaIzPotvrde(t *testing.T) {
	m := novaMrezaBezPrijelaza(t, laptopMreze)
	k, novo := m.kljucem, m.ostala[0]
	u := noviAdmin("mmarkic")
	nuzno(t, NewUserRepository(k.db, k.rec).CreateUser(&u, nil))
	_, err := k.db.Exec(`UPDATE users SET password_hash = 'oporavljeno' WHERE id = ?`, u.ID.String())
	nuzno(t, err)
	n, err := PotpisiStanjePrava(ctxRaz, k.db, k.rec, m.potpis)
	nuzno(t, err)
	if n != 1 {
		t.Fatalf("potpisano %d", n)
	}
	posalji(t, k, novo)
	if stupacRacuna(t, novo, u.ID, "password_hash") != "oporavljeno" || stupacRacuna(t, novo, u.ID, "is_global_admin") != "1" {
		t.Errorf("novo računalo: lozinka %q, zastavica %q", stupacRacuna(t, novo, u.ID, "password_hash"),
			stupacRacuna(t, novo, u.ID, "is_global_admin"))
	}
}

// Dvije promjene istog računa čekaju; potvrda novije zamjenjuje stariju, pa
// više ne čeka nijedna, a odbijanje novije vraća stariju na popis
func TestPotvrdaKljucemDvijePromjeneCekaju(t *testing.T) {
	for _, odluka := range []string{PravaPotvrdeno, PravaOdbijeno} {
		t.Run(odluka, func(t *testing.T) {
			m := novaMreza(t, laptopMreze)
			k, b := m.kljucem, m.ostala[0]
			u := noviRacun("mmarkic")
			nuzno(t, m.racuni(k).CreateUser(&u, nil))
			posalji(t, k, b)
			prva, druga := u, u
			prva.IsGlobalAdmin = true
			druga.IsGlobalAdmin, druga.PasswordHash = true, "drugi-sazetak"
			v1, v2 := tudjiRacun(t, prva), tudjiRacun(t, druga)
			primi(t, k, []ledger.Version{v1, v2})
			if len(cekaju(t, k)) != 2 {
				t.Fatalf("čeka %+v", cekaju(t, k))
			}
			odluci := PotvrdiPromjenuPrava
			if odluka == PravaOdbijeno {
				odluci = OdbijPromjenuPrava
			}
			nuzno(t, odluci(ctxRaz, k.db, k.rec, m.potpis, v2.VersionID, tkoOdlucuje))
			ceka := cekajuVerzije(t, k)
			if odluka == PravaPotvrdeno && (len(ceka) != 0 || stupacRacuna(t, k, u.ID, "password_hash") != "drugi-sazetak") {
				t.Errorf("nakon potvrde novije: čeka %v, lozinka %q", ceka, stupacRacuna(t, k, u.ID, "password_hash"))
			}
			if odluka == PravaOdbijeno && (len(ceka) != 1 || ceka[0] != v1.VersionID) {
				t.Errorf("nakon odbijanja novije čeka %v", ceka)
			}
		})
	}
}

// Potvrda koja stigne u zasebnoj razmjeni, poslije verzije koja na
// površinu ne prolazi (isto korisničko ime), ne ruši primjenu ostalog:
// zapis ide među nedovršene
func TestPotvrdaKljucemPoslijeVerzijeKojaNeProlazi(t *testing.T) {
	m := novaMreza(t, laptopMreze, treceRacunalo)
	k, b, c := m.kljucem, m.ostala[0], m.ostala[1]
	isti := noviAdmin("ppetric")
	nuzno(t, m.racuni(b).CreateUser(&isti, nil))
	posalji(t, b, k)
	posalji(t, b, c)
	drugi := noviRacun("ppetric")
	nuzno(t, NewUserRepository(c.db, c.rec).CreateUser(&drugi, nil))
	nuzno(t, PotvrdiPromjenuPrava(ctxRaz, k.db, k.rec, m.potpis, cekaju(t, k)[0].VersionID, tkoOdlucuje))
	primi(t, c, verzijeZapisa(t, k, func(v ledger.Version) bool { return v.Entity == EntityPotvrdePrava }))
	if stupacRacuna(t, c, isti.ID, "id") != "" || !nedovrsen(EntityUsers+"|"+isti.ID.String()) {
		t.Error("račun koji ne prolazi na površinu nije među nedovršenima")
	}
}

// Pomoćne funkcije za prava: sadržaj koji se ne da pročitati, vrijeme s
// djelićima sekunde i ishod verzije koje nema
func TestPravaPoljaRubovi(t *testing.T) {
	if otisakPrava("") != "" || len(otisakPrava(pravaObrisanog)) != 64 {
		t.Error("otisak prava")
	}
	sad := time.Now()
	s := sad.Unix()
	if trenutak(nil, &sad) != nil || trenutak(&s, &sad) != &sad || !trenutak(&s, nil).Equal(time.Unix(s, 0)) {
		t.Error("trenutak iz sekundi")
	}
	pokvarena := ledger.Version{Entity: EntityDuties, Payload: []byte("{")}
	if _, err := duznostSPravima(pokvarena, pravaDuznosti(models.Duty{})); err == nil {
		t.Error("dužnost iz sadržaja koji nije zapis")
	}
	d := ledger.Version{Entity: EntityDuties, Payload: []byte(`{"id":"` + vid(1) + `"}`)}
	if _, err := duznostSPravima(d, pravaObrisanog); !errors.Is(err, errNemaPrava) {
		t.Errorf("dužnost s pravima obrisane: %v", err)
	}
	if _, err := racunSPravima(ledger.Version{Payload: []byte(`{}`)}, "{"); err == nil {
		t.Error("račun s pravima koja nisu zapis")
	}
	if _, ima := (stanjePrava{}).ishod(vid(1)); ima {
		t.Error("ishod verzije koje nema")
	}
	if p := promjenaRacunaProtiv(racunNaPovrsini{}, pokvarena); len(p.promjene) != 0 {
		t.Error("promjena iz sadržaja koji nije zapis")
	}
}

// Greške: potpis koji ne prođe pri prijelazu, sadržaj bez prava, zapis
// potvrde koji nije JSON, i baza bez tablica koje potvrda treba
func TestPotvrdaKljucemGreskePrijelaza(t *testing.T) {
	m := novaMrezaBezPrijelaza(t, laptopMreze)
	k := m.kljucem
	if _, err := PotpisiStanjePrava(ctxRaz, k.db, k.rec, losPotpis{}); !errors.Is(err, razmjena.ErrBezKljucaMreze) {
		t.Errorf("prijelaz bez potpisa: %v", err)
	}
	tx, err := k.db.BeginTx(ctxRaz, nil)
	nuzno(t, err)
	if err := potpisiOdluku(ctxRaz, tx, k.rec, m.potpis, ledger.Version{Entity: EntityUsers, Payload: []byte("{")},
		razmjena.OdlukaPotvrdeno, razmjena.NacinIzmjena, ""); !errors.Is(err, errNemaPrava) {
		t.Errorf("potpis sadržaja bez prava: %v", err)
	}
	if _, err := zadnjaIspravnaTx(ctxRaz, tx, EntityUsers, "nema"); !errors.Is(err, ledger.ErrNoVersion) {
		t.Errorf("zadnja verzija zapisa kojeg nema: %v", err)
	}
	nuzno(t, tx.Rollback())

	u := noviAdmin("mmarkic")
	nuzno(t, m.racuni(k).CreateUser(&u, nil))
	pokvarena := tudja(t, EntityPotvrdePrava, razmjena.PrefiksPotvrda(EntityUsers, u.ID.String())+"x:potvrdeno", nil)
	pokvarena.Payload = []byte(`"nije potvrda"`)
	primi(t, k, []ledger.Version{pokvarena})
	if stupacRacuna(t, k, u.ID, "is_global_admin") != "1" {
		t.Error("zapis potvrde koji nije JSON promijenio je račun")
	}

	_, err = k.db.Exec(`DROP TABLE prava_vlastite`)
	nuzno(t, err)
	u.Phone = "031 1"
	if err := m.racuni(k).UpdateUser(&u); err == nil {
		t.Error("izmjena bez tablice vlastitih verzija")
	}
	if _, err := PotpisiStanjePrava(ctxRaz, k.db, k.rec, m.potpis); err == nil {
		t.Error("prijelaz bez tablice vlastitih verzija")
	}
	_, err = k.db.Exec(`DROP TABLE record_versions`)
	nuzno(t, err)
	if _, err := PotpisiStanjePrava(ctxRaz, k.db, k.rec, m.potpis); err == nil {
		t.Error("prijelaz bez knjige")
	}
}

// Greške izračuna i savepointa se javljaju: knjiga i tablica dužnosti koje
// se ne daju pročitati, savepoint u prekinutom zahtjevu i savepoint koji je
// u međuvremenu nestao, i ključ mreže koji nije ključ
func TestStanjePravaGreskeIzracuna(t *testing.T) {
	m := novaMreza(t)
	k := m.kljucem
	u := noviAdmin("mmarkic")
	nuzno(t, m.racuni(k).CreateUser(&u, nil))
	tx, err := k.db.BeginTx(ctxRaz, nil)
	nuzno(t, err)
	defer tx.Rollback()
	o := ogradePrava[EntityUsers]
	prekinut, prekini := context.WithCancel(ctxRaz)
	prekini()
	if err := uSavepointu(prekinut, tx, func() error { return nil }, func(error) {}); err == nil {
		t.Error("savepoint u prekinutom zahtjevu")
	}
	nestao := func() error {
		_, err := tx.ExecContext(ctxRaz, `RELEASE SAVEPOINT stanje_prava`)
		return errors.Join(err, errNemaPrava)
	}
	if err := uSavepointu(ctxRaz, tx, nestao, func(error) {}); err == nil {
		t.Error("povratak na savepoint koji je nestao")
	}
	_, err = tx.ExecContext(ctxRaz, `DROP TABLE duties`)
	nuzno(t, err)
	if _, err := izracunajStanje(ctxRaz, tx, o, EntityUsers, u.ID.String(), odlukeZapisa{}, nil); err == nil {
		t.Error("izračun bez tablice dužnosti")
	}
	_, err = tx.ExecContext(ctxRaz, `DROP TABLE record_versions`)
	nuzno(t, err)
	if _, err := izracunajStanje(ctxRaz, tx, o, EntityUsers, u.ID.String(), odlukeZapisa{}, nil); err == nil {
		t.Error("izračun bez knjige")
	}

	prazno := novaMrezaBezPrijelaza(t)
	_, err = prazno.kljucem.db.Exec(`UPDATE network SET public_key = 'nije-kljuc'`)
	nuzno(t, err)
	if _, err := PotpisiStanjePrava(ctxRaz, prazno.kljucem.db, prazno.kljucem.rec, prazno.potpis); err == nil {
		t.Error("prijelaz s ključem mreže koji nije ključ")
	}
}

// Prije prijelaza u mreži novija verzija bez promjene prava ne zamjenjuje
// promjenu koja čeka: računalo s ključem je i dalje vidi i može potvrditi
func TestPrijePrijelazaVerzijaBezPromjeneNeZamjenjuje(t *testing.T) {
	m := novaMrezaBezPrijelaza(t, laptopMreze)
	k := m.kljucem
	u := noviRacun("mmarkic")
	nuzno(t, NewUserRepository(k.db, k.rec).CreateUser(&u, nil))
	sZastavicom, telefon := u, u
	sZastavicom.IsGlobalAdmin = true
	telefon.Phone = "031 1"
	v1, v2 := tudjiRacun(t, sZastavicom), tudjiRacun(t, telefon)
	primi(t, k, []ledger.Version{v1})
	primi(t, k, []ledger.Version{v2})
	jedinaCeka(t, k, v1)
	if stupacRacuna(t, k, u.ID, "phone") != "031 1" {
		t.Error("verzija bez promjene prava nije prošla")
	}
	nuzno(t, PotvrdiPromjenuPrava(ctxRaz, k.db, k.rec, m.potpis, v1.VersionID, tkoOdlucuje))
	if err := PotvrdiPromjenuPrava(ctxRaz, k.db, k.rec, m.potpis, v1.VersionID, tkoOdlucuje); !errors.Is(err, ErrNemaPromjenePrava) {
		t.Errorf("druga odluka o istoj promjeni: %v", err)
	}
}

// Izmjena na računalu s ključem upisana bez potpisa (npr. prije nego što je
// dobilo ključ) dobiva potvrdu pri pokretanju, i kad je poslije nje stigla
// tuđa verzija; ponovno pokretanje i izmjena bez promjene prava ne potpisuju
// ništa
func TestPotpisiVlastituIzmjenu(t *testing.T) {
	m := novaMreza(t, laptopMreze)
	k, b := m.kljucem, m.ostala[0]
	bezPotpisa := NewUserRepository(k.db, k.rec)
	x := noviAdmin("xxanic")
	nuzno(t, bezPotpisa.CreateUser(&x, nil))
	telefon := x
	telefon.Phone = "031 2"
	primi(t, k, []ledger.Version{tudjiRacun(t, telefon)})
	if n, err := PotpisiStanjePrava(ctxRaz, k.db, k.rec, m.potpis); err != nil || n != 1 {
		t.Fatalf("potpis vlastite izmjene: %d %v", n, err)
	}
	posalji(t, k, b)
	if stupacRacuna(t, b, x.ID, "is_global_admin") != "1" || len(cekaju(t, b)) != 0 {
		t.Errorf("drugo računalo: zastavica %q, čeka %+v", stupacRacuna(t, b, x.ID, "is_global_admin"), cekaju(t, b))
	}
	x.Phone = "031 3"
	nuzno(t, bezPotpisa.UpdateUser(&x))
	if n, err := PotpisiStanjePrava(ctxRaz, k.db, k.rec, m.potpis); err != nil || n != 0 {
		t.Errorf("ponovno pokretanje: %d %v", n, err)
	}
}

// Potvrda s pravima koja se ne daju upisati u verziju ne mijenja površinu
func TestPotvrdaKljucemPravaKojaNisuZapis(t *testing.T) {
	m := novaMreza(t, laptopMreze)
	k, b := m.kljucem, m.ostala[0]
	u := noviRacun("mmarkic")
	nuzno(t, m.racuni(k).CreateUser(&u, nil))
	posalji(t, k, b)
	v, err := k.rec.Latest(ctxRaz, EntityUsers, u.ID.String())
	nuzno(t, err)
	p, ok := m.potpis.PotpisiPotvrdu(razmjena.PotvrdaPrava{Odluka: razmjena.OdlukaPotvrdeno, VersionID: v.VersionID, Entitet: EntityUsers,
		Zapis: u.ID.String(), Otisak: razmjena.OtisakVerzije(v.Archived, v.Payload), Prava: "{", Nacin: razmjena.NacinOdluka})
	if !ok {
		t.Fatal("potpis")
	}
	primi(t, b, []ledger.Version{tudja(t, EntityPotvrdePrava, p.ID, p)})
	if stupacRacuna(t, b, u.ID, "username") != "mmarkic" {
		t.Error("potvrda s pokvarenim pravima promijenila je račun")
	}
}
