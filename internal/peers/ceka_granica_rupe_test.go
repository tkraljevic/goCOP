package peers

// Verzija koja čeka članstvo ili potpisanu inačicu ide u trazi zapisa rupa
// (rupe.go, rasponCekanja), nikad u odbijeno, pa je razmjene traže
// popunom. Ovi testovi tvrde da je drugi ne popune mimo provjere potpisa,
// da ostaje tražena dok ne prođe, i kad čeka zauvijek, a da zbog nje ne
// zapne traženje drugih verzija istog ključa i da se ne šalje cijeli ključ
// iznova; da treće računalo koje od laptopa dobije noviju verziju i nju
// traži; da je granica odbijenih ne preskače; i da rupe nema pod vlastitim
// imenom ni kad granica knjige ne prelazi preko nje.

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// trazi javlja da čvor u ključu k traži verziju id
func trazi(t *testing.T, s *Service, k, id string) bool {
	t.Helper()
	tr := rupeCvora(t, s)[k].trazi
	return !tr.prazan() && tr.Od < id && id <= tr.Do
}

// skracenih je broj razgovora čvora sa skraćenom deltom
func skracenih(s *Service) int {
	n := 0
	s.skracene.Range(func(_, _ any) bool { n++; return true })
	return n
}

// Nepotpisana verzija prava od autora koji potpisuje čeka na laptopu, a
// novija verzija istog ključa prođe. Laptop je traži, posrednik s oznakom
// potpisa mu je popunom daje opet nepotpisanu, pa i dalje čeka i ne ulazi
// u knjigu, a razgovori ne padaju i delta se ne skraćuje. Treće računalo
// od laptopa dobije noviju verziju, a laptop mu za verziju koja čeka ne
// jamči, pa je i ono traži i dobije od autora. Kad laptop razgovara s
// autorom, dobije je i on.
func TestCekanjeTrazenaDokNeStigneOdAutora(t *testing.T) {
	cv := clanoviMreze(t, false, "cop-osijek", "stari-posrednik", "posrednik", "laptop-baranja", "dezurni-treci")
	a, o, p, c, d := cv[0], cv[1], cv[2], cv[3], cv[4]
	for _, s := range cv {
		s.SetWantsAll(true)
	}
	o.mogucnosti = bezOznakePotpisa
	c.PostaviStrogePotpise("users")
	a.rec.PostaviPotpisnika(a.node.PotpisnikVerzija(Protocol))

	upisiOcitanja(t, a, "prijave/16/2026", "p0")
	razgovaraj(t, c, a)
	if _, err := a.rec.Record(ctxRub, a.db, "users", "u1", map[string]string{"id": "u1"}); err != nil {
		t.Fatal(err)
	}
	prva := zadnjaVerzija(t, a, "users", "u1")
	razgovaraj(t, o, a)
	razgovaraj(t, p, o)
	if _, err := a.rec.Record(ctxRub, a.db, "users", "u2", map[string]string{"id": "u2"}); err != nil {
		t.Fatal(err)
	}
	druga := zadnjaVerzija(t, a, "users", "u2")
	razgovaraj(t, p, a)
	k := ledger.FrontierKey(a.node.ID, prva.Channel)

	razgovaraj(t, c, p)
	if redakKnjige(t, c, prva.VersionID) != nil || redakKnjige(t, c, druga.VersionID) == nil {
		t.Fatal("postava: prva mora čekati, a druga proći")
	}
	if !trazi(t, c, k, prva.VersionID) {
		t.Fatalf("laptop ne traži verziju koja čeka: %+v", rupeCvora(t, c)[k])
	}

	// posrednik popunom šalje opet nepotpisanu: strogo vrijedi i za popunu
	for i := range 3 {
		cekaju := brojiDnevnik(t, "a upisat će se kad stigne članstvo autora ili potpisana inačica")
		razgovaraj(t, c, p)
		if n, _ := cekaju.broj(); n == 0 {
			t.Errorf("%d. razgovor: posrednik nije popunio rupu verzijom koja čeka", i+1)
		}
		if redakKnjige(t, c, prva.VersionID) != nil {
			t.Fatalf("%d. razgovor: nepotpisana verzija prava ušla je u knjigu popunom", i+1)
		}
		if !trazi(t, c, k, prva.VersionID) {
			t.Fatalf("%d. razgovor: laptop više ne traži verziju koja čeka: %+v", i+1, rupeCvora(t, c)[k])
		}
	}
	if n, m := skracenih(c), skracenih(p); n != 0 || m != 0 {
		t.Errorf("delta skraćena: laptop %d, posrednik %d", n, m)
	}

	// treće računalo: od laptopa dobije drugu i rupu, a prvu od autora
	razgovaraj(t, d, c)
	if redakKnjige(t, d, druga.VersionID) == nil || redakKnjige(t, d, prva.VersionID) != nil {
		t.Fatal("treće računalo: druga mora stići od laptopa, a prva ne")
	}
	if !trazi(t, d, k, prva.VersionID) {
		t.Fatalf("treće računalo ne traži verziju koju laptop nema: %+v", rupeCvora(t, d)[k])
	}
	razgovaraj(t, d, a)
	if u := redakKnjige(t, d, prva.VersionID); u == nil || u.Potpis == "" {
		t.Errorf("treće računalo nije dobilo verziju od autora: %+v", u)
	}

	razgovaraj(t, c, a)
	if u := redakKnjige(t, c, prva.VersionID); u == nil || u.Potpis == "" {
		t.Errorf("laptop nije dobio verziju od autora: %+v", u)
	}
	if trazi(t, c, k, prva.VersionID) {
		t.Errorf("laptop i dalje traži verziju koja je stigla: %+v", rupeCvora(t, c)[k])
	}
}

// potpisanaU je potpisanaProbe u kanalu kanal
func potpisanaU(t *testing.T, kljuc ed25519.PrivateKey, autor, kanal, zapis string) ledger.Version {
	t.Helper()
	v := potpisanaProbe(t, nil, autor, entitetProbe, zapis, `{"cm":1}`)
	v.Channel = kanal
	if kljuc != nil {
		var err error
		if v.Potpis, v.KljucAutora, err = razmjena.PotpisiVerziju(kljuc, Protocol, PodaciVerzije(v)); err != nil {
			t.Fatal(err)
		}
	}
	return v
}

// razgovorZaCekanje je razgovor laptopa koji prati samo očitanja
// područja 16, sa zapisom rupa i granicom s početka
func razgovorZaCekanje(t *testing.T, b *Service) *razgovorSCvorom {
	t.Helper()
	g, err := b.rec.Frontier(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	r := &razgovorSCvorom{s: b, myWants: Wants{Rules: []Subscription{{Kind: "ocitanja", AreaID: 16}}}, moja: frontierMsg{Frontier: g}}
	r.rupe = b.pocetakRupa(ctxRub)
	r.trazeno = r.rupe.trazim(b.wantsFunc(ctxRub, &r.myWants))
	return r
}

// uKnjiguPrimatelja upiše verzije čvoru kao uvoz, da ključevi imaju granicu
func uKnjiguPrimatelja(t *testing.T, b *Service, verzije ...ledger.Version) {
	t.Helper()
	if n, err := b.primijeniPodBravom(ctxRub, verzije, nil); err != nil || n != len(verzije) {
		t.Fatalf("upis u knjigu: %d od %d, %v", n, len(verzije), err)
	}
}

// Verzija koja čeka stvara rupu samo pod tuđim imenom i samo kad granica
// prelazi preko nje. Uz svaku stiže i novija verzija istog ključa koja
// prolazi, osim u ključu sama, gdje stiže samo verzija koja čeka: granica
// ostaje ispod nje, pa je druga strana šalje opet, i rupe nema. U kanalu
// koji laptop ne prati novija verzija se ne upisuje (zaUpis), pa ni tu
// granica ne prelazi preko nje. Bez rupe ostaju i ključ bez granice u koji
// ništa ne prolazi (ime koje je netko izmislio) i verzija ispod granice
// knjige koju nitko ne traži (ispod): niz je tu prijeđen i prije, a inače
// bi jedna verzija sa starom oznakom tražila cijelu povijest ključa.
func TestCekanjeBezRupeIzvanPracenogIVlastito(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := primateljOd(t, a)
	_, tudji := noviKljuc(t)
	praceni, nepraceni, sama, ispod := "ocitanja/16/2026", "ocitanja/3/2026", "ocitanja/16/2025", "ocitanja/16/2024"
	cekaIspod := potpisanaU(t, tudji, a.node.ID, ispod, "z9")
	uKnjiguPrimatelja(t, b, potpisanaU(t, a.node.key, a.node.ID, sama, "z0"), potpisanaU(t, a.node.key, a.node.ID, ispod, "z8"))
	cekaPraceni := potpisanaU(t, tudji, a.node.ID, praceni, "z1")
	cekaNepraceni := potpisanaU(t, tudji, a.node.ID, nepraceni, "z2")
	cekaVlastita := potpisanaU(t, tudji, b.node.ID, praceni, "z3")
	cekaSama := potpisanaU(t, tudji, a.node.ID, sama, "z7")
	cekaIzmisljen := potpisanaU(t, tudji, "0-l00000", praceni, "z10")
	poruka := []ledger.Version{cekaPraceni, cekaNepraceni, cekaVlastita, cekaSama, cekaIzmisljen, cekaIspod,
		potpisanaU(t, a.node.key, a.node.ID, praceni, "z4"), potpisanaU(t, a.node.key, a.node.ID, nepraceni, "z5"),
		potpisanaU(t, b.node.key, b.node.ID, praceni, "z6")}

	if err := razgovorZaCekanje(t, b).upisiPrimljeno(ctxRub, poruka); err != nil {
		t.Fatal(err)
	}
	for _, v := range []ledger.Version{cekaPraceni, cekaNepraceni, cekaVlastita, cekaSama, cekaIzmisljen, cekaIspod} {
		if redakKnjige(t, b, v.VersionID) != nil {
			t.Errorf("verzija koja čeka %s ušla je u knjigu", v.EntityID)
		}
	}
	rupe := rupeCvora(t, b)
	if !trazi(t, b, ledger.FrontierKey(a.node.ID, praceni), cekaPraceni.VersionID) {
		t.Errorf("praćeni kanal: verzija koja čeka nije tražena: %+v", rupe)
	}
	for _, k := range []string{ledger.FrontierKey(a.node.ID, nepraceni), ledger.FrontierKey(b.node.ID, praceni),
		ledger.FrontierKey("0-l00000", praceni), ledger.FrontierKey(a.node.ID, ispod), ledger.FrontierKey(a.node.ID, sama)} {
		if rk, ima := rupe[k]; ima {
			t.Errorf("%s ima rupu: %+v", k, rk)
		}
	}
}

// Druga provjera potpisa (primijeniAkoStigne): članstvo novog člana je u
// poruci, pa njegova verzija u prvoj provjeri prođe, ali verzija članstva
// sama čeka (potpisana je ključem koji nije vezan uz ime autora) i ne ide
// u upis. U drugoj provjeri verzija novog člana zato čeka, a novija verzija
// istog ključa (nepotpisana) prođe. Verzija koja čeka mora biti tražena.
// Isto vrijedi za seriju uvoza (uveziSRupama, primijeniPodBravom), osim u
// ključu u kojem se ništa novije ne upisuje, a traženje tu počinje od
// granice knjige prije uvoza.
func TestCekanjeUDrugojProvjeri(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	noviPub, noviPriv := noviKljuc(t)
	m, err := a.network.Admit("novi-clan", noviPub, a.node.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sadrzajClanstva, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	_, tudji := noviKljuc(t)
	kanal := "ocitanja/16/2026"

	t.Run("razmjena", func(t *testing.T) {
		b := primateljOd(t, a)
		clanstvo := potpisanaProbe(t, tudji, a.node.ID, EntityMemberships, "novi-clan", string(sadrzajClanstva))
		odNovog := potpisanaU(t, noviPriv, "novi-clan", kanal, "n1")
		novija := potpisanaU(t, nil, "novi-clan", kanal, "n2")
		if err := razgovorZaCekanje(t, b).upisiPrimljeno(ctxRub, []ledger.Version{clanstvo, odNovog, novija}); err != nil {
			t.Fatal(err)
		}
		if redakKnjige(t, b, odNovog.VersionID) != nil || redakKnjige(t, b, novija.VersionID) == nil {
			t.Fatal("postava: verzija novog člana mora čekati u drugoj provjeri, a novija proći")
		}
		if !trazi(t, b, ledger.FrontierKey("novi-clan", kanal), odNovog.VersionID) {
			t.Errorf("verzija koja čeka u drugoj provjeri nije tražena: %+v", rupeCvora(t, b))
		}
	})

	t.Run("serija uvoza", func(t *testing.T) {
		b := primateljOd(t, a)
		rani := potpisanaU(t, nil, "novi-clan", kanal, "n0")
		uKnjiguPrimatelja(t, b, rani)
		odNovog := potpisanaU(t, noviPriv, "novi-clan", kanal, "n1")
		novija := potpisanaU(t, nil, "novi-clan", kanal, "n2")
		samaUKljucu := potpisanaU(t, noviPriv, "novi-clan", "ocitanja/3/2026", "n3")
		if _, err := b.uveziSRupama(ctxRub, []ledger.Version{odNovog, novija, samaUKljucu}, promjenaRupa{}); err != nil {
			t.Fatal(err)
		}
		if redakKnjige(t, b, odNovog.VersionID) != nil || redakKnjige(t, b, novija.VersionID) == nil {
			t.Fatal("postava: verzija novog člana mora čekati, a novija proći")
		}
		k := ledger.FrontierKey("novi-clan", kanal)
		if !trazi(t, b, k, odNovog.VersionID) {
			t.Errorf("verzija koja čeka u seriji uvoza nije tražena: %+v", rupeCvora(t, b))
		}
		// traži se od granice knjige prije uvoza, a ne od početka niza
		if od := rupeCvora(t, b)[k].trazi.Od; od != rani.VersionID {
			t.Errorf("traženje počinje od %q, a granica prije uvoza je %q", od, rani.VersionID)
		}
		if rk, ima := rupeCvora(t, b)[ledger.FrontierKey("novi-clan", "ocitanja/3/2026")]; ima {
			t.Errorf("ključ u kojem se ništa novije ne upisuje ima rupu: %+v", rk)
		}
	})
}

// Netko pod imenom autora pošalje verziju potpisanu svojim ključem, iznad
// granice laptopa, u ključu u koji autor više ne piše (zatvorena godina).
// Granica ne prelazi preko nje, pa rupe nema: inače je nitko ne bi mogao
// zatvoriti, jer za ono iznad svih granica nitko ne jamči. Razgovor zato ne
// treba ni bravu (promjenaRupa.nista). Kad ista verzija stigne uz noviju
// verziju istog ključa koja prolazi, granica prelazi preko nje, pa se
// traži.
func TestCekanjeKadNistaNeProlazi(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := primateljOd(t, a)
	_, tudji := noviKljuc(t)
	kanal := "ocitanja/16/2025"
	uKnjiguPrimatelja(t, b, potpisanaU(t, a.node.key, a.node.ID, kanal, "z0"))
	izmisljena := potpisanaU(t, tudji, a.node.ID, kanal, "z1")
	k := ledger.FrontierKey(a.node.ID, kanal)

	// brava zauzeta: upis koji ništa ne mijenja ne čeka je
	if err := b.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	staro := rokCekanjaUpisa
	rokCekanjaUpisa = 10 * time.Millisecond
	t.Cleanup(func() { rokCekanjaUpisa = staro })
	for i := range 3 {
		if err := razgovorZaCekanje(t, b).upisiPrimljeno(ctxRub, []ledger.Version{izmisljena}); err != nil {
			t.Fatalf("%d. put: %v", i+1, err)
		}
	}
	b.pustiBravu()
	if rk, ima := rupeCvora(t, b)[k]; ima {
		t.Fatalf("verzija iznad granice koju granica ne prelazi ostavila je rupu: %+v", rk)
	}
	if got := b.NedostajeDulje(ctxRub); len(got) != 0 {
		t.Errorf("pločica: %v", got)
	}

	novija := potpisanaU(t, a.node.key, a.node.ID, kanal, "z2")
	if err := razgovorZaCekanje(t, b).upisiPrimljeno(ctxRub, []ledger.Version{izmisljena, novija}); err != nil {
		t.Fatal(err)
	}
	if redakKnjige(t, b, novija.VersionID) == nil || !trazi(t, b, k, izmisljena.VersionID) {
		t.Errorf("uz noviju verziju koja prolazi verzija koja čeka nije tražena: %+v", rupeCvora(t, b)[k])
	}
}

// Popuna od računala koje jamči (rupe/1) donese potpisanu verziju koja i
// dalje čeka, jer članstvo njezina ključa ne stiže s njom. Ona ostaje u
// trazi, a ne u odbijeno: članstvo može stići kasnije. Laptop traži cijeli
// ključ (od početka do v5), a popuna ga pokrije do v3, pa se traženje
// smanji na (v1, v5]: počinje od najveće verzije ispod one koja čeka, a ne
// od početka. Iduća popuna pokrije ostatak, pa se traži samo (v1, v2].
// Kad zatim stigne članstvo, verzija prođe i traženje nestane.
func TestCekanjeUPopuniOstajeTrazena(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := primateljOd(t, a)
	tudjiPub, tudji := noviKljuc(t)
	kanal := "ocitanja/16/2026"
	v1 := potpisanaU(t, a.node.key, a.node.ID, kanal, "v1")
	ceka := potpisanaU(t, tudji, a.node.ID, kanal, "v2")
	v3 := potpisanaU(t, a.node.key, a.node.ID, kanal, "v3")
	v5 := potpisanaU(t, a.node.key, a.node.ID, kanal, "v5")
	uKnjiguPrimatelja(t, b, v1, v3, v5)
	k := ledger.FrontierKey(a.node.ID, kanal)
	upisiRupuTrazi(t, b, k, raspon{Do: v5.VersionID})
	popuni := func(popuna raspon, verzije ...ledger.Version) rupaKljuca {
		t.Helper()
		r := razgovorZaCekanje(t, b)
		r.rupe.dogovor = true
		r.njihovaPopuna = map[string]raspon{k: popuna}
		if err := r.upisiPrimljeno(ctxRub, verzije); err != nil {
			t.Fatal(err)
		}
		return rupeCvora(t, b)[k]
	}

	rk := popuni(raspon{Do: v3.VersionID}, v1, ceka, v3)
	if redakKnjige(t, b, ceka.VersionID) != nil {
		t.Fatal("verzija s ključem koji nije vezan ušla je u knjigu")
	}
	if rk.trazi != (raspon{Od: v1.VersionID, Do: v5.VersionID}) || !rk.odbijeno.prazan() {
		t.Errorf("nakon prve popune %+v, a treba trazi (v1, v5] i ništa u odbijenom", rk)
	}
	rk = popuni(raspon{Od: v1.VersionID, Do: v5.VersionID}, ceka, v3, v5)
	if rk.trazi != (raspon{Od: v1.VersionID, Do: ceka.VersionID}) || !rk.odbijeno.prazan() {
		t.Errorf("nakon druge popune %+v, a treba trazi (v1, v2] i ništa u odbijenom", rk)
	}

	m, err := a.network.Admit(a.node.ID, tudjiPub, a.node.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.saveMembership(ctxRub, m); err != nil {
		t.Fatal(err)
	}
	rk = popuni(raspon{Od: v1.VersionID, Do: ceka.VersionID}, ceka)
	if redakKnjige(t, b, ceka.VersionID) == nil || !rk.prazna() {
		t.Errorf("kad je stiglo članstvo, verzija nije prošla ili se još traži: %+v", rk)
	}
}

// Autor je users/u1 napisao prije nego što je počeo potpisivati, pa ga
// nitko nema potpisanog. Laptop prava prima strogo i ima potpisanu verziju
// autora, pa u1 od posrednika s oznakom potpisa čeka zauvijek, a novije
// verzije istog ključa prođu. Laptop ne može znati da potpisane inačice
// nema, pa u1 traži (nije u odbijenom), ali ga to ne prikiva: popunom dobije
// i druge verzije istog ključa koje mu nedostaju, a poslije svaki razgovor
// šalje samo u1, ne cijeli ključ. To vrijedi kad popuna od početka
// traženja stane u jedan razgovor (ovdje desetak verzija); kad ne stane,
// traženje ostaje prikovano (TestCekanjeOdrezanaPopunaDrziTrazenje,
// invarijanta u rupe.go). Traženje ne nestaje, pa je i upozorenje pločice
// za autora trajno (NedostajeDulje).
func TestCekanjeZauvijekNePrikujeTrazenje(t *testing.T) {
	cv := clanoviMreze(t, false, "cop-osijek", "posrednik", "laptop-baranja")
	a, p, c := cv[0], cv[1], cv[2]
	for _, s := range cv {
		s.SetWantsAll(true)
	}
	c.PostaviStrogePotpise("users")

	// prije potpisivanja: očitanja u zajedničkom kanalu, pa u1
	var rani []string
	for i := range 20 {
		rani = append(rani, fmt.Sprintf("x%02d", i))
	}
	upisiOcitanja(t, a, "", rani...)
	zadnjiRani := zadnjaVerzija(t, a, "probni", rani[len(rani)-1])
	if _, err := a.rec.Record(ctxRub, a.db, "users", "u1", map[string]string{"id": "u1"}); err != nil {
		t.Fatal(err)
	}
	stara := zadnjaVerzija(t, a, "users", "u1")
	if zadnjiRani.Channel != stara.Channel || zadnjiRani.VersionID >= stara.VersionID {
		t.Fatalf("postava: očitanja moraju biti u ključu u1, ispod njega: %+v / %+v", zadnjiRani, stara)
	}
	a.rec.PostaviPotpisnika(a.node.PotpisnikVerzija(Protocol))
	kanal := "prijave/16/2026"
	upisiOcitanja(t, a, kanal, "p0")
	_, put := izvezi(t, a, CopObuhvat{Vrsta: "prijave", AreaID: 16, Od: 2026, Do: 2026}, []string{kanal}, false)
	if _, err := c.UveziCop(ctxRub, put); err != nil {
		t.Fatal(err)
	}
	var nove []ledger.Version
	for i := 2; i <= 12; i++ {
		id := fmt.Sprintf("u%d", i)
		if _, err := a.rec.Record(ctxRub, a.db, "users", id, map[string]string{"id": id}); err != nil {
			t.Fatal(err)
		}
		nove = append(nove, zadnjaVerzija(t, a, "users", id))
	}
	razgovaraj(t, p, a)
	k := ledger.FrontierKey(a.node.ID, stara.Channel)

	razgovaraj(t, c, p)
	if redakKnjige(t, c, stara.VersionID) != nil || redakKnjige(t, c, nove[len(nove)-1].VersionID) == nil {
		t.Fatal("postava: u1 mora čekati, a novije proći")
	}
	if rk := rupeCvora(t, c)[k]; !trazi(t, c, k, stara.VersionID) || rk.odbijeno.drziVerziju(stara.VersionID) {
		t.Fatalf("u1 mora biti tražen, a ne u odbijenom: %+v", rk)
	}

	// laptopu nedostaju neke novije verzije istog ključa, i zna to: traži
	// cijeli ključ
	nedostaju := nove[2:6]
	for _, v := range nedostaju {
		if _, err := c.db.Exec(`DELETE FROM record_versions WHERE version_id = ?`, v.VersionID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.db.Exec(`DELETE FROM rupe_knjige WHERE node_id = ? AND channel = ?`, a.node.ID, stara.Channel); err != nil {
		t.Fatal(err)
	}
	upisiRupuTrazi(t, c, k, raspon{Do: nove[len(nove)-1].VersionID})

	razgovaraj(t, c, p)
	for _, v := range nedostaju {
		if redakKnjige(t, c, v.VersionID) == nil {
			t.Errorf("verzija %s istog ključa nije stigla popunom", v.EntityID)
		}
	}
	trazeno := raspon{Od: zadnjiRani.VersionID, Do: stara.VersionID}
	if rk := rupeCvora(t, c)[k]; rk.trazi != trazeno || !rk.odbijeno.prazan() {
		t.Errorf("nakon popune %+v, a treba trazi (zadnje očitanje, u1] i ništa u odbijenom", rk)
	}
	for i := range 3 {
		cekaju := brojiDnevnik(t, "a upisat će se kad stigne članstvo autora ili potpisana inačica")
		_, z := razgovaraj(t, c, p)
		if z.sent != 1 {
			t.Errorf("%d. razgovor: posrednik je poslao %d verzija, a čeka samo u1", i+1, z.sent)
		}
		if n, _ := cekaju.broj(); n == 0 {
			t.Errorf("%d. razgovor: posrednik nije popunom poslao u1", i+1)
		}
		if rk := rupeCvora(t, c)[k]; rk.trazi != trazeno || !rk.odbijeno.prazan() {
			t.Errorf("%d. razgovor: %+v, a treba trazi (zadnje očitanje, u1]", i+1, rk)
		}
	}
	if redakKnjige(t, c, stara.VersionID) != nil {
		t.Error("nepotpisana verzija prava ušla je u knjigu")
	}
	c.satOdbijenih = func() time.Time { return time.Now().Add(najduljeTrazi + time.Hour) }
	if got := c.NedostajeDulje(ctxRub); len(got) != 1 || !strings.Contains(got[0], "čekaju provjeru potpisa") {
		t.Errorf("pločica nakon dana: %q", got)
	}
}

// Granica odbijenih ne preskače verziju koja čeka. U istom primanju, u
// istom ključu, verzija čeka (ključ nije vezan), a novija se odbije (kriv
// potpis) i ništa ne prođe. Laptop pošiljatelju za taj ključ ne javlja
// granicu odbijenih, pa ga pošiljatelj iduće razmjene šalje opet od
// granice knjige, s verzijom koja čeka. Inače bi je preskočio, a kad bi
// prošla novija, granica knjige prešla bi preko nje bez rupe. Zapis od
// ranije koji bi je preskočio laptop zaboravi, a ključ bez verzije koja
// čeka pamti kao i prije.
func TestCekanjeIspodOdbijenih(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := primateljOd(t, a)
	_, tudji := noviKljuc(t)
	kanal, drugi := "ocitanja/16/2026", "ocitanja/16/2025"
	uKnjiguPrimatelja(t, b, potpisanaU(t, a.node.key, a.node.ID, kanal, "z0"), potpisanaU(t, a.node.key, a.node.ID, drugi, "y0"))
	k, kDrugi := ledger.FrontierKey(a.node.ID, kanal), ledger.FrontierKey(a.node.ID, drugi)
	ceka := potpisanaU(t, tudji, a.node.ID, kanal, "z1")
	kriva := potpisanaU(t, a.node.key, a.node.ID, kanal, "z2")
	kriva.Payload = json.RawMessage(`{"cm":2}`)
	krivaDrugi := potpisanaU(t, a.node.key, a.node.ID, drugi, "y1")
	krivaDrugi.Payload = json.RawMessage(`{"cm":2}`)
	const posiljatelj = "kljuc-posiljatelja"
	primi := func(verzije ...ledger.Version) {
		t.Helper()
		r := razgovorZaCekanje(t, b)
		r.odbijene = b.pocetakOdbijenih(posiljatelj, r.moja.Frontier)
		if err := r.upisiPrimljeno(ctxRub, verzije); err != nil {
			t.Fatal(err)
		}
	}
	odbijene := func() map[string]string {
		_, z := b.odbijeneOd(posiljatelj, b.ogradaSada(), b.sadOdbijenih())
		return z.granica
	}

	primi(ceka, kriva, krivaDrugi)
	if redakKnjige(t, b, ceka.VersionID) != nil || redakKnjige(t, b, kriva.VersionID) != nil {
		t.Fatal("postava: verzija koja čeka i kriva verzija ušle su u knjigu")
	}
	if g, ima := odbijene()[k]; ima {
		t.Errorf("granica odbijenih %s preskače verziju koja čeka", g)
	}
	if g := odbijene()[kDrugi]; g != krivaDrugi.VersionID {
		t.Errorf("ključ bez verzije koja čeka: granica odbijenih %q", g)
	}

	// zapis od ranije preskočio bi verziju koja čeka
	b.zapamtiOdbijene(b.pocetakOdbijenih(posiljatelj, nil), map[string]string{k: kriva.VersionID})
	primi(ceka)
	if g, ima := odbijene()[k]; ima {
		t.Errorf("zapis od ranije %s i dalje preskače verziju koja čeka", g)
	}
	if g := odbijene()[kDrugi]; g != krivaDrugi.VersionID {
		t.Errorf("ključ bez verzije koja čeka zaboravljen: %q", g)
	}

	// pošiljatelj šalje opet od granice, uz noviju verziju koja prolazi
	novija := potpisanaU(t, a.node.key, a.node.ID, kanal, "z3")
	primi(ceka, kriva, novija)
	if redakKnjige(t, b, novija.VersionID) == nil || !trazi(t, b, k, ceka.VersionID) {
		t.Errorf("uz noviju verziju verzija koja čeka nije tražena: %+v", rupeCvora(t, b)[k])
	}

	// verzija koja čeka tek u drugoj provjeri potpisa: članstvo novog člana
	// je u poruci, ali samo čeka, pa verzija novog člana prođe prvu
	// provjeru i čeka u drugoj, a novija verzija istog ključa ima kriv potpis
	noviPub, noviPriv := noviKljuc(t)
	m, err := a.network.Admit("novi-clan", noviPub, a.node.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sadrzajClanstva, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	clanstvo := potpisanaProbe(t, tudji, a.node.ID, EntityMemberships, "novi-clan", string(sadrzajClanstva))
	odNovog := potpisanaU(t, noviPriv, "novi-clan", kanal, "n1")
	krivaNovog := potpisanaU(t, noviPriv, "novi-clan", kanal, "n2")
	krivaNovog.Payload = json.RawMessage(`{"cm":2}`)
	kNovog := ledger.FrontierKey("novi-clan", kanal)
	primi(clanstvo, odNovog, krivaNovog)
	if redakKnjige(t, b, odNovog.VersionID) != nil {
		t.Fatal("postava: verzija novog člana mora čekati")
	}
	if g, ima := odbijene()[kNovog]; ima {
		t.Errorf("granica odbijenih %s preskače verziju koja čeka tek u drugoj provjeri", g)
	}
}

// Bez oznake rupe/1 druga strana za popunu ne jamči. Verzija koja čeka, a
// stigne u takvoj popuni ispod granice knjige, zato se ne bilježi, a zapis
// rupa ostaje kakav jest: što se traži, traži se i dalje.
func TestCekanjePopunaBezDogovora(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := primateljOd(t, a)
	_, tudji := noviKljuc(t)
	kanal := "ocitanja/16/2026"
	v1 := potpisanaU(t, a.node.key, a.node.ID, kanal, "v1")
	v3 := potpisanaU(t, a.node.key, a.node.ID, kanal, "v3")
	ceka := potpisanaU(t, tudji, a.node.ID, kanal, "v4")
	v5 := potpisanaU(t, a.node.key, a.node.ID, kanal, "v5")
	uKnjiguPrimatelja(t, b, v1, v5)
	k := ledger.FrontierKey(a.node.ID, kanal)
	trazeno := raspon{Od: v3.VersionID, Do: v5.VersionID}
	upisiRupuTrazi(t, b, k, trazeno)

	r := razgovorZaCekanje(t, b)
	r.njihovaPopuna = map[string]raspon{k: trazeno}
	if err := r.upisiPrimljeno(ctxRub, []ledger.Version{ceka, v5}); err != nil {
		t.Fatal(err)
	}
	if rk := rupeCvora(t, b)[k]; rk.trazi != trazeno || !rk.odbijeno.prazan() {
		t.Errorf("popuna bez jamstva promijenila je zapis rupa: %+v, a treba trazi %+v", rk, trazeno)
	}
}

// uveziKaoCop uveze verzije kao UveziCop, bez paketa: provjera potpisa,
// Accept, rupe uz uvoz i upis u serijama
func uveziKaoCop(t *testing.T, b *Service, verzije ...ledger.Version) {
	t.Helper()
	ispravne, _, cekaju := b.preskociNeispravne(ctxRub, prijem{put: "uvoz"}, verzije)
	wanted, odluke := b.zaUvoz(ispravne, nil)
	odluke.cekaju = cekaju
	if _, err := b.uveziSRupama(ctxRub, wanted, b.promjenaUvoza(ctxRub, wanted, odluke, nil)); err != nil {
		t.Fatal(err)
	}
}

// Uvoz u ključ koji već ima granicu: kad paket donese samo verziju koja
// čeka, granica ostaje ispod nje, pa rupe nema (verzija stiže opet). Kad
// donese i noviju verziju istog ključa koja prolazi, granica prelazi preko
// verzije koja čeka, pa se traži od granice prije uvoza.
func TestCekanjeUvozUKljucSGranicom(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := primateljOd(t, a)
	_, tudji := noviKljuc(t)
	kanal := "ocitanja/16/2026"
	rani := potpisanaU(t, a.node.key, a.node.ID, kanal, "z0")
	uKnjiguPrimatelja(t, b, rani)
	ceka := potpisanaU(t, tudji, a.node.ID, kanal, "z1")
	k := ledger.FrontierKey(a.node.ID, kanal)

	uveziKaoCop(t, b, ceka)
	if rk, ima := rupeCvora(t, b)[k]; ima || redakKnjige(t, b, ceka.VersionID) != nil {
		t.Fatalf("samo verzija koja čeka: rupa %+v", rk)
	}
	novija := potpisanaU(t, a.node.key, a.node.ID, kanal, "z2")
	uveziKaoCop(t, b, ceka, novija)
	if redakKnjige(t, b, novija.VersionID) == nil {
		t.Fatal("postava: novija verzija nije uvezena")
	}
	if rk := rupeCvora(t, b)[k]; rk.trazi != (raspon{Od: rani.VersionID, Do: ceka.VersionID}) {
		t.Errorf("uz noviju verziju trazi %+v, a treba (granica prije uvoza, verzija koja čeka]", rk.trazi)
	}
}

// Starija nepotpisana verzija prava autora koji danas potpisuje već je u
// knjizi laptopa (stigla je od računala bez oznake potpisa, dok strogo nije
// vrijedilo), a laptop u njezinu ključu nešto traži. Posrednik s oznakom
// potpisa popunom pošalje i nju: kod laptopa ona opet čeka, ali je ima, pa
// je ne smije tražiti dalje ni drugima uskratiti jamstvo za nju (zaCekanje
// gleda knjigu), nego popuna to traženje makne.
func TestCekanjeVecUKnjiziNeOstajeTrazena(t *testing.T) {
	cv := clanoviMreze(t, false, "cop-osijek", "stari-posrednik", "posrednik", "laptop-baranja")
	a, o, p, c := cv[0], cv[1], cv[2], cv[3]
	for _, s := range cv {
		s.SetWantsAll(true)
	}
	o.mogucnosti = bezOznakePotpisa
	c.PostaviStrogePotpise("users")
	a.rec.PostaviPotpisnika(a.node.PotpisnikVerzija(Protocol))

	if _, err := a.rec.Record(ctxRub, a.db, "users", "u1", map[string]string{"id": "u1"}); err != nil {
		t.Fatal(err)
	}
	stara := zadnjaVerzija(t, a, "users", "u1")
	razgovaraj(t, o, a)
	razgovaraj(t, c, o)
	razgovaraj(t, p, o)
	if u := redakKnjige(t, c, stara.VersionID); u == nil || u.Potpis != "" {
		t.Fatalf("postava: laptop mora imati staru verziju bez potpisa: %+v", u)
	}
	if _, err := a.rec.Record(ctxRub, a.db, "users", "u2", map[string]string{"id": "u2"}); err != nil {
		t.Fatal(err)
	}
	razgovaraj(t, c, a)
	k := ledger.FrontierKey(a.node.ID, stara.Channel)
	if rk, ima := rupeCvora(t, c)[k]; ima {
		t.Fatalf("postava: laptop već ima rupu %+v", rk)
	}
	upisiRupuTrazi(t, c, k, raspon{Do: stara.VersionID})

	cekaju := brojiDnevnik(t, "a upisat će se kad stigne članstvo autora ili potpisana inačica")
	razgovaraj(t, c, p)
	if n, _ := cekaju.broj(); n == 0 {
		t.Fatal("postava: posrednik nije popunom poslao staru verziju bez potpisa")
	}
	if u := redakKnjige(t, c, stara.VersionID); u == nil || u.Potpis != "" {
		t.Errorf("stara verzija u knjizi laptopa se promijenila: %+v", u)
	}
	if rk := rupeCvora(t, c)[k]; rk.nejamci().drziVerziju(stara.VersionID) {
		t.Errorf("laptop ne jamči za verziju koju već ima (traži je ili je u odbijenom): %+v", rk)
	}
}

// Arhiva knjige (ImportFile): verzija koja čeka članstvo ostavi rupu, a
// novija istog ključa prođe. Arhiva upisuje i kanal koji laptop ne prati,
// pa i tamo ostaje rupa, a treće računalo, koje kanal od laptopa dobije,
// zato verziju koja čeka traži i dobije je od autora. U kanalu koji se
// prati laptop je dobije razmjenom s posrednikom.
func TestCekanjeUvozArhive(t *testing.T) {
	for _, prati := range []bool{true, false} {
		t.Run(map[bool]string{true: "kanal se prati", false: "kanal se ne prati"}[prati], func(t *testing.T) {
			cv := clanoviBez(t, 3, 0, "novi-clan", "stari-posrednik", "posrednik", "laptop-baranja", "dezurni-treci")
			x, o, p, c, d := cv[0], cv[1], cv[2], cv[3], cv[4]
			for _, s := range cv {
				s.SetWantsAll(true)
			}
			c.SetWantsAll(prati)
			o.mogucnosti = bezOznakePotpisa
			x.rec.PostaviPotpisnika(x.node.PotpisnikVerzija(Protocol))
			kanal := "prijave/16/2026"

			upisiOcitanja(t, x, kanal, "p1")
			prva := zadnjaVerzija(t, x, "probni", "p1")
			razgovaraj(t, p, x)
			upisiOcitanja(t, x, kanal, "p2")
			druga := zadnjaVerzija(t, x, "probni", "p2")
			razgovaraj(t, o, x)
			razgovaraj(t, p, o)

			put := filepath.Join(t.TempDir(), "arhiva.db")
			if _, err := p.ExportFile(ctxRub, put, []string{kanal}); err != nil {
				t.Fatal(err)
			}
			if _, err := c.ImportFile(ctxRub, put); err != nil {
				t.Fatal(err)
			}
			if redakKnjige(t, c, prva.VersionID) != nil || redakKnjige(t, c, druga.VersionID) == nil {
				t.Fatal("postava: prva mora čekati članstvo, a druga proći")
			}
			k := ledger.FrontierKey(x.node.ID, kanal)
			if !trazi(t, c, k, prva.VersionID) {
				t.Fatalf("uvoz arhive nije ostavio rupu za verziju koja čeka: %+v", rupeCvora(t, c)[k])
			}

			if !prati {
				razgovaraj(t, d, c)
				if redakKnjige(t, d, druga.VersionID) == nil || !trazi(t, d, k, prva.VersionID) {
					t.Fatalf("treće računalo: druga mora stići od laptopa, a prva biti tražena: %+v", rupeCvora(t, d)[k])
				}
				razgovaraj(t, d, x)
				if redakKnjige(t, d, prva.VersionID) == nil {
					t.Error("treće računalo nije dobilo verziju od autora")
				}
				return
			}
			razgovaraj(t, c, p)
			if redakKnjige(t, c, prva.VersionID) == nil {
				t.Error("razmjena s posrednikom nije donijela verziju koja je čekala")
			}
			if trazi(t, c, k, prva.VersionID) {
				t.Errorf("laptop i dalje traži verziju koja je stigla: %+v", rupeCvora(t, c)[k])
			}
		})
	}
}

// Dva razgovora počnu s istom granicom laptopa. U prvom stigne verzija
// koja čeka i novija koja prođe, pa se verzija koja čeka traži, a granica
// knjige prijeđe preko nje. U drugom stigne opet ona (čeka), novija s
// krivim potpisom i nešto u drugom ključu. Drugi razgovor po svojoj
// granici s početka vidi verziju koja čeka iznad granice i bez novijeg
// upisa u ključu, pa je ne bilježi, ali je druga strana nije ni upisala ni
// odbila: jamstvo pošiljatelja (pokriveno) zato ne smije maknuti traženje
// koje je zapisao prvi razgovor.
func TestCekanjeDvaRazgovoraIsteGranice(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := primateljOd(t, a)
	_, tudji := noviKljuc(t)
	kanal, drugi := "ocitanja/16/2026", "ocitanja/16/2025"
	uKnjiguPrimatelja(t, b, potpisanaU(t, a.node.key, a.node.ID, kanal, "z0"))
	k := ledger.FrontierKey(a.node.ID, kanal)
	ceka := potpisanaU(t, tudji, a.node.ID, kanal, "z1")
	kriva := potpisanaU(t, a.node.key, a.node.ID, kanal, "z2")
	kriva.Payload = json.RawMessage(`{"cm":2}`)
	novija := potpisanaU(t, a.node.key, a.node.ID, kanal, "z3")
	uDrugom := potpisanaU(t, a.node.key, a.node.ID, drugi, "y1")

	r1, r2 := razgovorZaCekanje(t, b), razgovorZaCekanje(t, b)
	r1.rupe.dogovor, r2.rupe.dogovor = true, true
	if err := r1.upisiPrimljeno(ctxRub, []ledger.Version{ceka, novija}); err != nil {
		t.Fatal(err)
	}
	if redakKnjige(t, b, novija.VersionID) == nil || !trazi(t, b, k, ceka.VersionID) {
		t.Fatalf("postava: novija mora proći, a verzija koja čeka biti tražena: %+v", rupeCvora(t, b)[k])
	}
	if err := r2.upisiPrimljeno(ctxRub, []ledger.Version{ceka, kriva, uDrugom}); err != nil {
		t.Fatal(err)
	}
	if redakKnjige(t, b, ceka.VersionID) != nil || redakKnjige(t, b, uDrugom.VersionID) == nil {
		t.Fatal("postava: verzija koja čeka ušla je u knjigu ili drugi ključ nije upisan")
	}
	if !trazi(t, b, k, ceka.VersionID) {
		t.Errorf("drugi razgovor maknuo je traženje verzije koja čeka: %+v", rupeCvora(t, b)[k])
	}
}

// Verzija koja čeka ispod granice knjige laptopa stiže samo popunom, a
// granica odbijenih je ne preskače: pošiljatelj preskače samo ono iznad
// granice knjige primatelja. Kad laptop u istom ključu iznad granice odbije
// verziju (kriv potpis), granicu odbijenih tog ključa zato pamti, iz
// razgovora u razgovor, iako verzija koja čeka svakom popunom stiže opet.
func TestCekanjeIspodGraniceNeBriseOdbijene(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := primateljOd(t, a)
	_, tudji := noviKljuc(t)
	kanal := "ocitanja/16/2026"
	v1 := potpisanaU(t, a.node.key, a.node.ID, kanal, "v1")
	ceka := potpisanaU(t, tudji, a.node.ID, kanal, "v2")
	v3 := potpisanaU(t, a.node.key, a.node.ID, kanal, "v3")
	kriva := potpisanaU(t, a.node.key, a.node.ID, kanal, "v4")
	kriva.Payload = json.RawMessage(`{"cm":2}`)
	uKnjiguPrimatelja(t, b, v1, v3)
	k := ledger.FrontierKey(a.node.ID, kanal)
	upisiRupuTrazi(t, b, k, raspon{Od: v1.VersionID, Do: v3.VersionID})
	const posiljatelj = "kljuc-posiljatelja"
	for i := range 2 {
		r := razgovorZaCekanje(t, b)
		r.rupe.dogovor = true
		r.odbijene = b.pocetakOdbijenih(posiljatelj, r.moja.Frontier)
		r.njihovaPopuna = map[string]raspon{k: r.trazeno[k]}
		if err := r.upisiPrimljeno(ctxRub, []ledger.Version{ceka, kriva}); err != nil {
			t.Fatal(err)
		}
		if redakKnjige(t, b, ceka.VersionID) != nil || !trazi(t, b, k, ceka.VersionID) {
			t.Fatalf("%d. razgovor: verzija koja čeka ušla je u knjigu ili nije tražena: %+v", i+1, rupeCvora(t, b)[k])
		}
		_, z := b.odbijeneOd(posiljatelj, b.ogradaSada(), b.sadOdbijenih())
		if g := z.granica[k]; g != kriva.VersionID {
			t.Errorf("%d. razgovor: granica odbijenih ključa %q, a treba %q", i+1, g, kriva.VersionID)
		}
	}
}

// Poznato ograničenje (invarijanta u rupe.go): kad traženje ključa ne stane
// u jednu popunu, verzija koja čeka drži njegov početak tik ispod sebe.
// Svaka odrezana popuna zato počinje opet od nje, a ono iza dosega popune
// ostaje traženo, ali ne stiže dok verzija čeka. Ovdje je rez popune
// zadan rukom (njihovaPopuna), umjesto NajviseVerzijaPoRazmjeni verzija.
func TestCekanjeOdrezanaPopunaDrziTrazenje(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := primateljOd(t, a)
	_, tudji := noviKljuc(t)
	kanal := "ocitanja/16/2026"
	v1 := potpisanaU(t, a.node.key, a.node.ID, kanal, "v1")
	ceka := potpisanaU(t, tudji, a.node.ID, kanal, "v2")
	v3 := potpisanaU(t, a.node.key, a.node.ID, kanal, "v3")
	v5 := potpisanaU(t, a.node.key, a.node.ID, kanal, "v5")
	v7 := potpisanaU(t, a.node.key, a.node.ID, kanal, "v7")
	uKnjiguPrimatelja(t, b, v1, v3, v5, v7)
	k := ledger.FrontierKey(a.node.ID, kanal)
	upisiRupuTrazi(t, b, k, raspon{Do: v7.VersionID})
	popuni := func(popuna raspon, verzije ...ledger.Version) raspon {
		t.Helper()
		r := razgovorZaCekanje(t, b)
		r.rupe.dogovor = true
		r.njihovaPopuna = map[string]raspon{k: popuna}
		if err := r.upisiPrimljeno(ctxRub, verzije); err != nil {
			t.Fatal(err)
		}
		return rupeCvora(t, b)[k].trazi
	}
	drzi := raspon{Od: v1.VersionID, Do: v7.VersionID}
	if tr := popuni(raspon{Do: v3.VersionID}, v1, ceka, v3); tr != drzi {
		t.Fatalf("nakon prve odrezane popune trazi %+v, a treba (v1, v7]", tr)
	}
	for i := range 2 {
		if tr := popuni(raspon{Od: v1.VersionID, Do: v5.VersionID}, ceka, v3, v5); tr != drzi {
			t.Errorf("%d. odrezana popuna od verzije koja čeka: trazi %+v, a treba ostati (v1, v7]", i+1, tr)
		}
	}
}
