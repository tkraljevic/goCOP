package peers

// Potvrda promjene prava ključem mreže kroz pravu razmjenu (Odluke
// (30. 10. 2026.), dopuna 16): promjena napravljena na računalu s ključem
// mreže vrijedi na drugima odmah, a promjena s drugog računala čeka dok je
// globalni administrator jednom ne potvrdi na računalu s ključem. Stariji
// program potvrdu ne razumije, ali je čuva i prosljeđuje. Imena su izmišljena.

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/razmjena"
	"gocop/internal/repository"
)

// mrezaSKljucem: prvo računalo osniva mrežu i drži ključ, ostala prima;
// svi znaju jedni za druge, prate sve, a primljeno ide na površinu
func mrezaSKljucem(t *testing.T, stari func(*Service), ids ...string) []*Service {
	t.Helper()
	k := rubnaMreza(t, ids[0])
	cv := []*Service{k}
	clanstva := []razmjena.Membership{*k.myMembership(ctxRub)}
	for _, id := range ids[1:] {
		s := rubniCvor(t, id)
		clanstva = append(clanstva, primiIzravno(t, k, s))
		cv = append(cv, s)
	}
	for i, a := range cv {
		a.SetWantsAll(true)
		sPovrsinom(a)
		for j, b := range cv {
			if i != j {
				if err := a.saveMembership(ctxRub, clanstva[j]); err != nil {
					t.Fatal(err)
				}
				upoznaj(t, a, b)
			}
		}
	}
	if stari != nil {
		stari(cv[1])
	}
	// računalo s ključem pokrenuto je novim programom: potpisalo je prijelaz
	if _, err := repository.PotpisiStanjePrava(ctxRub, k.db, k.rec, k); err != nil {
		t.Fatal(err)
	}
	return cv
}

// racuniNa su računi računala s potpisom ključem mreže, kao u programu
func racuniNa(s *Service) *repository.UserRepository {
	r := repository.NewUserRepository(s.db, s.rec)
	r.PostaviPotpisMreze(s)
	return r
}

func adminRacun(ime string) models.User {
	return models.User{ID: uuid.New(), Username: ime, FullName: "Ana Anić", PasswordHash: "h", IsActive: true,
		IsGlobalAdmin: true, OrgType: models.OrgHrvatskeVode, CreatedAt: time.Now(), UpdatedAt: time.Now()}
}

// zastavica je zastavica globalnog administratora računa na površini;
// false i kad računa nema
func zastavica(t *testing.T, s *Service, id uuid.UUID) bool {
	t.Helper()
	u, err := repository.NewUserRepository(s.db, s.rec).GetUserByID(id)
	if err != nil {
		t.Fatal(err)
	}
	return u != nil && u.IsGlobalAdmin
}

func cekaNa(t *testing.T, s *Service) []repository.PromjenaPrava {
	t.Helper()
	ceka, _, err := repository.PromjenePrava(ctxRub, s.db, s.rec, 10)
	if err != nil {
		t.Fatal(err)
	}
	return ceka
}

// (a) i (b) kroz razmjenu triju računala: izmjena na računalu s ključem
// vrijedi odmah, a promjena s laptopa čeka na serveru i na Windowsu dok je
// globalni administrator ne potvrdi na računalu s ključem; Windows zatim
// potvrdu dobije razmjenom i promjena vrijedi bez odluke na njemu.
func TestPotvrdaPravaKrozRazmjenu(t *testing.T) {
	cv := mrezaSKljucem(t, nil, "cop-osijek", "laptop-baranja", "windows-ured")
	k, b, c := cv[0], cv[1], cv[2]
	if !k.DrziKljucMreze() || b.DrziKljucMreze() {
		t.Fatal("ključ mreže drži samo osnivač")
	}

	naK := adminRacun("mmarkic")
	if err := racuniNa(k).CreateUser(&naK, nil); err != nil {
		t.Fatal(err)
	}
	razgovaraj(t, b, k)
	razgovaraj(t, c, b)
	if !zastavica(t, b, naK.ID) || !zastavica(t, c, naK.ID) || len(cekaNa(t, b)) != 0 || len(cekaNa(t, c)) != 0 {
		t.Fatal("izmjena na računalu s ključem čeka na drugima")
	}

	naB := adminRacun("ppetric")
	if err := racuniNa(b).CreateUser(&naB, nil); err != nil {
		t.Fatal(err)
	}
	razgovaraj(t, b, k)
	razgovaraj(t, c, b)
	for _, s := range []*Service{k, c} {
		if zastavica(t, s, naB.ID) || len(cekaNa(t, s)) != 1 {
			t.Fatalf("%s: promjena s laptopa ne čeka", s.node.ID)
		}
	}
	if err := repository.PotvrdiPromjenuPrava(ctxRub, c.db, c.rec, c, cekaNa(t, c)[0].VersionID, "Ivan Ivić (iivic)"); err == nil {
		t.Fatal("Windows bez ključa mreže je odlučio")
	}
	if err := repository.PotvrdiPromjenuPrava(ctxRub, k.db, k.rec, k, cekaNa(t, k)[0].VersionID, "Ivan Ivić (iivic)"); err != nil {
		t.Fatal(err)
	}
	razgovaraj(t, c, k)
	if !zastavica(t, c, naB.ID) || len(cekaNa(t, c)) != 0 {
		t.Fatalf("Windows nakon potvrde: zastavica %v, čeka %+v", zastavica(t, c, naB.ID), cekaNa(t, c))
	}
	razgovaraj(t, b, c)
	if !zastavica(t, b, naB.ID) {
		t.Error("laptop je izgubio vlastitu izmjenu")
	}
}

// (h) Stariji program (0.0.46) potvrdu ne poznaje: čuva je u knjizi,
// javlja da čeka nadogradnju i prosljeđuje je, pa Windows, koji s računalom
// s ključem nikad ne razgovara, dobije izmjenu bez čekanja. Na žici se ništa
// ne mijenja. Popis poznatih (ledger.PoznatiEntiteti) se ne da vratiti, pa
// test radi u zasebnom procesu.
func TestPotvrdaPravaKrozStarijiProgram(t *testing.T) {
	if os.Getenv(procesStarogPrograma) == "" {
		uZasebnomProcesu(t)
		return
	}
	ledger.PoznatiEntiteti(slices.DeleteFunc(repository.PoznatiEntiteti(), func(e string) bool { return e == repository.EntityPotvrdePrava })...)
	stari := func(s *Service) {
		s.OnApplied(func(ctx context.Context, vv []ledger.Version) error {
			zna := slices.DeleteFunc(slices.Clone(vv), func(v ledger.Version) bool { return v.Entity == repository.EntityPotvrdePrava })
			return repository.ApplyVersions(ctx, s.db, s.rec, zna)
		})
	}
	cv := mrezaSKljucem(t, stari, "cop-osijek", "server-ured", "windows-ured")
	k, srednji, c := cv[0], cv[1], cv[2]
	u := adminRacun("mmarkic")
	if err := racuniNa(k).CreateUser(&u, nil); err != nil {
		t.Fatal(err)
	}
	razgovaraj(t, srednji, k)
	if n, err := srednji.rec.CekaNadogradnju(ctxRub); err != nil || n < 2 {
		t.Fatalf("stariji program: čeka nadogradnju %d (%v), a treba potvrda prijelaza i potvrda računa", n, err)
	}
	razgovaraj(t, c, srednji)
	if !zastavica(t, c, u.ID) || len(cekaNa(t, c)) != 0 {
		t.Errorf("Windows iza starijeg programa: zastavica %v, čeka %+v", zastavica(t, c, u.ID), cekaNa(t, c))
	}
}

// Potpis i ključ: računalo bez mreže i bez ključa ne potpisuje; osnivanje
// mreže i učitana kopija ključa javljaju da je računalo dobilo ključ
func TestPotvrdaPravaPotpisIKljuc(t *testing.T) {
	brziScrypt(t)
	sam := rubniCvor(t, "laptop-ilok")
	if _, ok := sam.PotpisiPotvrdu(razmjena.PotvrdaPrava{}); ok || sam.DrziKljucMreze() {
		t.Error("računalo bez mreže potpisuje")
	}
	javljeno := map[string]int{}
	sam.NakonKljucaMreze(func(context.Context) { javljeno[sam.node.ID]++ })
	if err := sam.CreateNetwork(ctxRub, "Hrvatske vode"); err != nil {
		t.Fatal(err)
	}
	p, ok := sam.PotpisiPotvrdu(razmjena.PotvrdaPrava{Odluka: razmjena.OdlukaPotvrdeno, VersionID: "v", Entitet: "users", Zapis: "z"})
	if !ok || p.Racunalo != sam.node.ID || !p.Vrijedi(sam.network.Public) {
		t.Errorf("potpis osnivača: %+v %v", p, ok)
	}

	drugi := rubniCvor(t, "server-ured")
	primiIzravno(t, sam, drugi)
	if _, ok := drugi.PotpisiPotvrdu(razmjena.PotvrdaPrava{}); ok {
		t.Error("član bez ključa potpisuje")
	}
	drugi.javiKljucMreze(ctxRub) // bez zadanog ne radi ništa
	drugi.NakonKljucaMreze(func(context.Context) { javljeno[drugi.node.ID]++ })
	raw, _, err := sam.KopijaKljucaMreze(ctxRub, lozinkaKopije)
	if err != nil {
		t.Fatal(err)
	}
	if err := drugi.UcitajKopijuKljuca(ctxRub, raw, lozinkaKopije); err != nil {
		t.Fatal(err)
	}
	if !drugi.DrziKljucMreze() || javljeno[sam.node.ID] != 1 || javljeno[drugi.node.ID] != 1 {
		t.Errorf("javljeno %v, drži ključ %v", javljeno, drugi.DrziKljucMreze())
	}
}
