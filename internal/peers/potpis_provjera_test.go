package peers

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// potpisanaProbe je nova verzija autora, potpisana ključem kljuc pod imenom
// protokola Protocol
func potpisanaProbe(t *testing.T, kljuc ed25519.PrivateKey, autor, entitet, zapis, sadrzaj string) ledger.Version {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	v := ledger.Version{VersionID: id.String(), Entity: entitet, EntityID: zapis, NodeID: autor,
		Payload: json.RawMessage(sadrzaj), CreatedAt: time.Now().UTC(), SchemaVersion: 1}
	if kljuc != nil {
		v.Potpis, v.KljucAutora, err = razmjena.PotpisiVerziju(kljuc, Protocol, PodaciVerzije(v))
		if err != nil {
			t.Fatal(err)
		}
	}
	return v
}

// redakKnjige je verzija iz knjige s tom oznakom, ili nil
func redakKnjige(t *testing.T, s *Service, id string) *ledger.Version {
	t.Helper()
	var v ledger.Version
	var payload string
	err := s.db.QueryRow(`SELECT potpis, kljuc_autora, payload FROM record_versions WHERE version_id = ?`, id).Scan(&v.Potpis, &v.KljucAutora, &payload)
	if err != nil {
		return nil
	}
	v.Payload = json.RawMessage(payload)
	return &v
}

func noviKljuc(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

// Primljena verzija s potpisom ulazi u knjigu, s potpisom, samo kad potpis
// vrijedi i kad je ključ imao članstvo pod imenom autora (i ono iz iste
// poruke). Kriv potpis je odbijen i broji se; ključ koji još nije vezan uz
// ime čeka i ne broji se. Pod imenom ovog računala vrijedi samo njegov
// ključ ili ključ koji je pod tim imenom imao članstvo (Odluke
// (26. 11. 2026.), 3, 4 i 12).
func TestProvjeraPotpisaPriPrimanju(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := primateljOd(t, a)
	cekaju := brojiDnevnik(t, "a upisat će se kad stigne članstvo autora")

	dobra := potpisanaProbe(t, a.node.key, a.node.ID, entitetProbe, "z1", `{"cm":1}`)
	izmijenjena := potpisanaProbe(t, a.node.key, a.node.ID, entitetProbe, "z2", `{"cm":2}`)
	izmijenjena.Payload = json.RawMessage(`{"cm":3}`)
	bezKljuca := potpisanaProbe(t, a.node.key, a.node.ID, entitetProbe, "z3", `{"cm":4}`)
	bezKljuca.KljucAutora = ""
	_, tudji := noviKljuc(t)
	nevezana := potpisanaProbe(t, tudji, a.node.ID, entitetProbe, "z4", `{"cm":5}`)
	podMojimImenom := potpisanaProbe(t, tudji, b.node.ID, entitetProbe, "z5", `{"cm":6}`)
	moja := potpisanaProbe(t, b.node.key, b.node.ID, entitetProbe, "z6", `{"cm":7}`)

	// novi član: članstvo stiže u istoj poruci kao njegova verzija
	noviPub, noviPriv := noviKljuc(t)
	m, err := a.network.Admit("novi-clan", noviPub, a.node.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sadrzajClanstva, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	clanstvo := potpisanaProbe(t, a.node.key, a.node.ID, EntityMemberships, "novi-clan", string(sadrzajClanstva))
	odNovog := potpisanaProbe(t, noviPriv, "novi-clan", entitetProbe, "z7", `{"cm":8}`)
	// lažno članstvo (nije potpisano ključem mreže) ne veže ključ
	lazniPub, lazniPriv := noviKljuc(t)
	lazno := m
	lazno.DeviceID, lazno.DeviceKey = "lazni-clan", razmjena.PublicKeyString(lazniPub)
	sadrzajLaznog, err := json.Marshal(lazno)
	if err != nil {
		t.Fatal(err)
	}
	laznoClanstvo := potpisanaProbe(t, nil, "lazni-clan", EntityMemberships, "lazni-clan", string(sadrzajLaznog))
	odLaznog := potpisanaProbe(t, lazniPriv, "lazni-clan", entitetProbe, "z8", `{"cm":9}`)

	poruka := []ledger.Version{dobra, izmijenjena, bezKljuca, nevezana, podMojimImenom, moja, clanstvo, odNovog, laznoClanstvo, odLaznog}
	ispravne, krive := b.preskociNeispravne(ctxRub, prijem{put: "razmjena", od: a.javni()}, poruka)
	if len(krive) != 2 || krive[0].EntityID != "z2" || krive[1].EntityID != "z3" {
		t.Errorf("krive: %v", krive)
	}
	proslo := map[string]bool{}
	for _, v := range ispravne {
		proslo[v.EntityID] = true
	}
	for zapis, treba := range map[string]bool{"z1": true, "z2": false, "z3": false, "z4": false, "z5": false, "z6": true,
		"novi-clan": true, "z7": true, "lazni-clan": true, "z8": false} {
		if proslo[zapis] != treba {
			t.Errorf("%s: prošlo %v, a treba %v", zapis, proslo[zapis], treba)
		}
	}
	if n, _ := cekaju.broj(); n != 3 {
		t.Errorf("redaka o verzijama koje čekaju: %d", n)
	}
	zapisi, err := b.Odbijeno(ctxRub, 1)
	if err != nil || len(zapisi) != 1 || zapisi[0].Vrsta != VrstaVerzija || zapisi[0].Razlog != RazlogKrivPotpis ||
		zapisi[0].Broj != 2 || zapisi[0].Racunalo != a.node.ID {
		t.Errorf("zapis odbijenog: %+v, %v", zapisi, err)
	}

	// u knjigu ide s potpisom
	if n, err := b.primijeniPodBravom(ctxRub, poruka); err != nil || n != 5 {
		t.Fatalf("upis: %d, %v", n, err)
	}
	for _, v := range []ledger.Version{dobra, moja, odNovog} {
		if u := redakKnjige(t, b, v.VersionID); u == nil || u.Potpis != v.Potpis || u.KljucAutora != v.KljucAutora {
			t.Errorf("%s u knjizi: %+v", v.EntityID, u)
		}
	}
	for _, v := range []ledger.Version{izmijenjena, nevezana, podMojimImenom, odLaznog} {
		if u := redakKnjige(t, b, v.VersionID); u != nil {
			t.Errorf("%s je u knjizi", v.EntityID)
		}
	}

	// bez mreže ključ nije vezan ni uz koje ime osim vlastitog
	c := rubniCvor(t, "bez-mreze")
	if ispravne, _ := c.preskociNeispravne(ctxRub, prijem{put: "uvoz"}, []ledger.Version{dobra}); len(ispravne) != 0 {
		t.Error("računalo bez mreže primilo je potpisanu verziju tuđeg autora")
	}
}

// Kad je ista verzija prvo stigla bez potpisa (kroz starije računalo), a
// poslije potpisana, potpis se dopiše u redak, ali samo kad vrijedi nad
// spremljenim retkom. Redak s potpisom se ne mijenja (Odluke
// (26. 11. 2026.), 6).
func TestDopunaPotpisa(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := primateljOd(t, a)

	potpisana := potpisanaProbe(t, a.node.key, a.node.ID, entitetProbe, "z1", `{"cm":1}`)
	bez := potpisana
	bez.Potpis, bez.KljucAutora = "", ""
	// druga verzija: u knjizi je s drugim sadržajem nego što ga potpis pokriva
	druga := potpisanaProbe(t, a.node.key, a.node.ID, entitetProbe, "z2", `{"cm":2}`)
	podmetnuta := druga
	podmetnuta.Potpis, podmetnuta.KljucAutora, podmetnuta.Payload = "", "", json.RawMessage(`{"cm":99}`)
	if n, err := b.primijeniPodBravom(ctxRub, []ledger.Version{bez, podmetnuta}); err != nil || n != 2 {
		t.Fatalf("nepotpisane: %d, %v", n, err)
	}

	if n, err := b.primijeniPodBravom(ctxRub, []ledger.Version{potpisana, druga}); err != nil || n != 0 {
		t.Fatalf("potpisane: %d, %v", n, err)
	}
	if u := redakKnjige(t, b, potpisana.VersionID); u == nil || u.Potpis != potpisana.Potpis || u.KljucAutora != potpisana.KljucAutora {
		t.Errorf("potpis nije dopisan: %+v", u)
	}
	if u := redakKnjige(t, b, druga.VersionID); u == nil || u.Potpis != "" || string(u.Payload) != `{"cm":99}` {
		t.Errorf("potpis dopisan retku koji ne pokriva: %+v", u)
	}

	// potpis koji je već u retku se ne zamjenjuje, i kad stigne drugi koji vrijedi
	_, drugiKljuc := noviKljuc(t)
	drugiPotpis := potpisana
	var err error
	if drugiPotpis.Potpis, drugiPotpis.KljucAutora, err = razmjena.PotpisiVerziju(drugiKljuc, Protocol, PodaciVerzije(potpisana)); err != nil {
		t.Fatal(err)
	}
	b.dopuniPotpise(ctxRub, []ledger.Version{drugiPotpis})
	if u := redakKnjige(t, b, potpisana.VersionID); u == nil || u.Potpis != potpisana.Potpis {
		t.Errorf("postojeći potpis je zamijenjen: %+v", u)
	}

	// knjiga koja se ne da čitati: dopuna samo ide u dnevnik
	dnevnik := brojiDnevnik(t, "dopuna potpisa nije uspjela")
	b.db.Close()
	b.dopuniPotpise(ctxRub, []ledger.Version{potpisana})
	if n, _ := dnevnik.broj(); n != 1 {
		t.Errorf("redaka u dnevniku: %d", n)
	}
}

// Nepotpisana verzija prava ili mreže tuđeg autora koji već potpisuje čeka,
// kad ju je poslalo računalo koje potpis prenosi (oznaka potpis-verzije/1).
// Od računala bez oznake, od autora koji još ne potpisuje, pod vlastitim
// imenom i za ostale entitete prolazi kao i prije (Odluke (26. 11. 2026.), 7).
func TestStrogoZaPravaIMrezu(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := primateljOd(t, a)
	b.PostaviStrogePotpise("users")
	if n, err := b.primijeniPodBravom(ctxRub, []ledger.Version{potpisanaProbe(t, a.node.key, a.node.ID, entitetProbe, "z0", `{}`)}); err != nil || n != 1 {
		t.Fatalf("potpisana: %d, %v", n, err)
	}
	korisnik := potpisanaProbe(t, nil, a.node.ID, "users", "u1", `{"id":"u1"}`)
	drugiKorisnik := potpisanaProbe(t, nil, a.node.ID, "users", "u4", `{"id":"u4"}`)
	ostalo := potpisanaProbe(t, nil, a.node.ID, entitetProbe, "z1", `{}`)
	odStarog := potpisanaProbe(t, nil, "stari-cvor", "users", "u2", `{"id":"u2"}`)
	vlastiti := potpisanaProbe(t, nil, b.node.ID, "users", "u3", `{"id":"u3"}`)
	poruka := []ledger.Version{korisnik, drugiKorisnik, ostalo, odStarog, vlastiti}

	ispravne, krive := b.preskociNeispravne(ctxRub, prijem{put: "razmjena", strogo: true}, poruka)
	if len(ispravne) != 3 || len(krive) != 0 {
		t.Fatalf("strogo: prošlo %v, krive %v", ispravne, krive)
	}
	for _, v := range ispravne {
		if v.EntityID == "u1" || v.EntityID == "u4" {
			t.Error("nepotpisana verzija prava od autora koji potpisuje je prošla")
		}
	}
	if ispravne, _ := b.preskociNeispravne(ctxRub, prijem{put: "razmjena"}, poruka); len(ispravne) != 5 {
		t.Errorf("bez oznake kod pošiljatelja prošlo %d od 5", len(ispravne))
	}
}

// U razgovoru kriv potpis ide u granicu odbijenih (pošiljatelj ga ne šalje
// iznova), a verzija koja čeka članstvo ne ide
func TestKrivPotpisUGraniciOdbijenih(t *testing.T) {
	rz := razgovorZaRupe(t, nil, nil)
	rz.myWants = Wants{All: true}
	_, kljuc := noviKljuc(t)
	kriva := potpisanaProbe(t, kljuc, "ured", entitetProbe, "z1", `{"cm":1}`)
	kriva.Channel = "ocitanja/16/2026"
	kriva.Payload = json.RawMessage(`{"cm":2}`)
	ceka := potpisanaProbe(t, kljuc, "ured", entitetProbe, "z2", `{"cm":3}`)
	ceka.Channel = "ocitanja/16/2025" // kanal je u potpisanom, pa se potpisuje iznova
	var err error
	ceka.Potpis, ceka.KljucAutora, err = razmjena.PotpisiVerziju(kljuc, Protocol, PodaciVerzije(ceka))
	if err != nil {
		t.Fatal(err)
	}
	if err := rz.upisiPrimljeno(ctxRub, []ledger.Version{kriva, ceka}); err != nil {
		t.Fatal(err)
	}
	if got := rz.odbijeno[ledger.FrontierKey("ured", kriva.Channel)]; got != kriva.VersionID {
		t.Errorf("odbijeno %q, a treba %q", got, kriva.VersionID)
	}
	if got, ima := rz.odbijeno[ledger.FrontierKey("ured", ceka.Channel)]; ima {
		t.Errorf("verzija koja čeka članstvo je u granici odbijenih: %q (kriva %q, čeka %q)", got, kriva.VersionID, ceka.VersionID)
	}
}

// Članstva koja se ne daju pročitati: potpisana verzija čeka, a ne odbija se
func TestProvjeraPotpisaBezClanstava(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := primateljOd(t, a)
	dnevnik := brojiDnevnik(t, "članstva za provjeru potpisa ne daju se pročitati")
	if _, err := b.db.Exec(`DROP TABLE memberships`); err != nil {
		t.Fatal(err)
	}
	v := potpisanaProbe(t, a.node.key, a.node.ID, entitetProbe, "z1", `{}`)
	ispravne, krive := b.preskociNeispravne(ctxRub, prijem{put: "razmjena"}, []ledger.Version{v})
	if len(ispravne) != 0 || len(krive) != 0 {
		t.Errorf("prošlo %v, krive %v", ispravne, krive)
	}
	if n, _ := dnevnik.broj(); n != 1 {
		t.Errorf("redaka u dnevniku: %d", n)
	}
	if !ceka(errNepotpisanaOdPotpisnika) || ceka(errKrivPotpis) {
		t.Error("razlozi čekanja")
	}
}

// Mješovita mreža: posrednik bez oznake potpis-verzije/1 skine potpis, pa
// treće računalo verziju primi i spremi bez potpisa. Kad ista verzija
// poslije stigne potpisana (COP paket autora), potpis se dopiše u redak
// (Odluke (26. 11. 2026.), 6).
func TestDopunaKrozStarogPosrednika(t *testing.T) {
	cv := clanoviMreze(t, false, "cop-osijek", "posrednik", "laptop-baranja")
	a, p, c := cv[0], cv[1], cv[2]
	for _, s := range cv {
		s.SetWantsAll(true)
	}
	p.mogucnosti = func() []string { return []string{razmjena.OznakaRazmjena} }
	a.rec.PostaviPotpisnika(a.node.PotpisnikVerzija(Protocol))
	kanal := "prijave/16/2026"
	upisiOcitanja(t, a, kanal, "p1")
	potpisana, err := a.rec.Latest(ctxRub, "probni", "p1")
	if err != nil {
		t.Fatal(err)
	}

	razgovaraj(t, p, a)
	razgovaraj(t, c, p)
	if u := redakKnjige(t, c, potpisana.VersionID); u == nil || u.Potpis != "" {
		t.Fatalf("kroz starog posrednika: %+v", u)
	}

	_, put := izvezi(t, a, CopObuhvat{Vrsta: "prijave", AreaID: 16, Od: 2026, Do: 2026}, []string{kanal}, false)
	if _, err := c.UveziCop(ctxRub, put); err != nil {
		t.Fatal(err)
	}
	if v, err := c.rec.Latest(ctxRub, "probni", "p1"); err != nil {
		t.Fatal(err)
	} else {
		vrijediPotpis(t, a, Protocol, *v)
	}
}

// Uvoz u serijama: članstva idu prva, ostale verzije istim redom
func TestClanstvaNaPocetak(t *testing.T) {
	v := func(entitet, id string) ledger.Version { return ledger.Version{Entity: entitet, EntityID: id} }
	got := clanstvaNaPocetak([]ledger.Version{v(entitetProbe, "z1"), v(EntityMemberships, "m1"), v(entitetProbe, "z2"), v(EntityMemberships, "m2")})
	ids := ""
	for _, x := range got {
		ids += x.EntityID + " "
	}
	if ids != "m1 m2 z1 z2 " {
		t.Errorf("redoslijed: %q", ids)
	}
}
