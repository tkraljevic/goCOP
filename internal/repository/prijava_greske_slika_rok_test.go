package repository

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/sadrzaj"
)

const dan = 24 * time.Hour

var slikaZaRok = []byte("bajtovi slike zaslona")

// racunalo je čvor sa svojim spremištem. Spremište je u programu jedno, a u testu
// su računala dva, pa se postavlja prije svakog rada na računalu.
type racunalo struct {
	*cvor
	sp   *sadrzaj.Spremiste
	repo *PrijavaGreskeRepository
}

func novoRacunalo(t *testing.T, ime string) *racunalo {
	t.Helper()
	c := noviCvor(t, ime)
	return &racunalo{cvor: c, sp: spremisteZaTest(t, ime), repo: NewPrijavaGreskeRepository(c.db, c.rec)}
}

// prijavaSaSlikom upisuje prijavu sa slikom na ovom računalu
func (r *racunalo) prijavaSaSlikom(t *testing.T, opis string, slika []byte) *models.PrijavaGreske {
	t.Helper()
	SetSpremiste(r.sp)
	p := novaPrijavaGreske("osoba-1")
	p.Opis = opis
	p.SlikaOtisak, p.SlikaBajtova, p.SlikaVrsta = sadrzaj.Otisak(slika), len(slika), models.SlikaPrijavePNG
	nuzno(t, r.repo.SaveSaSlikom(ctxRaz, p, slika))
	return p
}

// primiOd prima verzije drugog računala, kao razmjena
func (r *racunalo) primiOd(t *testing.T, a *racunalo) {
	t.Helper()
	SetSpremiste(r.sp)
	posalji(t, a.cvor, r.cvor)
}

// stiglaSlika upisuje bajtove kao da ih je razmjena donijela
func (r *racunalo) stiglaSlika(t *testing.T, slika []byte) {
	t.Helper()
	nuzno(t, r.sp.UpisiProvjereno(ctxRaz, sadrzaj.Otisak(slika), models.SlikaPrijavePNG, slika, "cvor:drugo"))
}

// stanjePrije upisuje promjenu stanja kao da je nastala prije zadanog vremena:
// novu verziju u knjigu i površinu, kao administrator koji je tada radio
func (r *racunalo) stanjePrije(t *testing.T, id, stanje string, prije time.Duration) {
	t.Helper()
	p, err := r.repo.Get(ctxRaz, id)
	nuzno(t, err)
	p.Stanje, p.UpdatedAt = stanje, time.Now().Add(-prije).UTC()
	args, err := prijavaGreskeArgs(p)
	nuzno(t, err)
	tx, err := r.db.Begin()
	nuzno(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctxRaz, prijavaGreskeUpsert, args...)
	nuzno(t, err)
	_, err = r.rec.Record(ctxRaz, tx, EntityPrijaveGresaka, p.ID, p)
	nuzno(t, err)
	nuzno(t, tx.Commit())
}

// ocisti pokrene čišćenje na ovom računalu, kao program pri pokretanju
func (r *racunalo) ocisti(t *testing.T, sad time.Time) int {
	t.Helper()
	SetSpremiste(r.sp)
	n, err := r.repo.UkloniIstekleSlike(ctxRaz, sad)
	nuzno(t, err)
	return n
}

func (r *racunalo) brojVerzija(t *testing.T) int {
	t.Helper()
	verzije, err := r.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)
	return len(verzije)
}

// bezSlike tvrdi da slike nema, da je nitko ne traži i da je prijava ne drži, a da
// prijava, tekst i opis slike ostaju
func (r *racunalo) bezSlike(t *testing.T, ime string, p *models.PrijavaGreske) {
	t.Helper()
	if r.sp.Ima(ctxRaz, p.SlikaOtisak) {
		t.Errorf("%s: bajtovi su ostali", ime)
	}
	if r.sp.Zeljen(ctxRaz, p.SlikaOtisak) {
		t.Errorf("%s: slika je na popisu za dohvat", ime)
	}
	if r.sp.ImaVezu(ctxRaz, p.SlikaOtisak, sadrzaj.Veza{Entitet: EntityPrijaveGresaka, EntitetID: p.ID, Uloga: ulogaSlikePrijave}) {
		t.Errorf("%s: prijava još drži sliku", ime)
	}
	x, err := r.repo.Get(ctxRaz, p.ID)
	nuzno(t, err)
	if x == nil || x.Opis != p.Opis || x.SlikaOtisak != p.SlikaOtisak || !x.SlikaUklonjena() {
		t.Errorf("%s: prijava nakon brisanja slike: %+v", ime, x)
	}
}

// Slika ostaje do isteka 30 dana od rješavanja, i kad nakon toga stigne odgovor,
// a onda je nema. Prijava, tekst i odgovor ostaju, a u knjigu se ništa ne piše.
func TestSlikaSeBriseNakonRoka(t *testing.T) {
	ured := novoRacunalo(t, "ured")
	p := ured.prijavaSaSlikom(t, "Graf se ne crta.", slikaZaRok)
	_, err := ured.repo.Odgovori(ctxRaz, p.ID, models.StanjePrijaveRijeseno, "Popravljeno.", "Ivo Ivić")
	nuzno(t, err)
	rijesena := time.Now()
	_, err = ured.repo.Odgovori(ctxRaz, p.ID, models.StanjePrijaveRijeseno, "Još jedna napomena.", "Ivo Ivić")
	nuzno(t, err)
	verzija := ured.brojVerzija(t)

	if n := ured.ocisti(t, rijesena.Add(models.RokSlikePrijave-time.Minute)); n != 0 || !ured.sp.Ima(ctxRaz, p.SlikaOtisak) {
		t.Fatalf("prije roka: uklonjeno %d", n)
	}
	if n := ured.ocisti(t, rijesena.Add(models.RokSlikePrijave+time.Minute)); n != 1 || ured.sp.Ima(ctxRaz, p.SlikaOtisak) {
		t.Fatalf("nakon roka: uklonjeno %d", n)
	}
	if n := ured.ocisti(t, rijesena.Add(models.RokSlikePrijave+time.Hour)); n != 0 {
		t.Errorf("drugo čišćenje: %d", n)
	}
	if ured.brojVerzija(t) != verzija {
		t.Errorf("brisanje je napisalo verziju: %d, a bilo %d", ured.brojVerzija(t), verzija)
	}
	x, err := ured.repo.Get(ctxRaz, p.ID)
	nuzno(t, err)
	if x.Opis != p.Opis || x.SlikaOtisak != p.SlikaOtisak || tekstovi(x) != "Popravljeno. | Još jedna napomena." {
		t.Errorf("prijava: %+v", x)
	}
}

// Svako računalo briše samo. Razmjena poslije toga ne vraća sliku, ne stvara
// verzije, a ni obnova površine pri pokretanju ne traži sliku ponovno.
func TestBrisanjeSlikeNeKruzi(t *testing.T) {
	teren, ured := novoRacunalo(t, "teren"), novoRacunalo(t, "ured")
	p := teren.prijavaSaSlikom(t, "Graf se ne crta.", slikaZaRok)
	ured.primiOd(t, teren)
	ured.stiglaSlika(t, slikaZaRok)
	teren.stanjePrije(t, p.ID, models.StanjePrijaveRijeseno, 31*dan)

	verzija := teren.brojVerzija(t)
	if n := teren.ocisti(t, time.Now()); n != 1 {
		t.Fatalf("teren: uklonjeno %d", n)
	}
	teren.bezSlike(t, "teren", p)

	// ured još nije čistio: prva razmjena s prošlim rokom i tu briše sliku
	ured.primiOd(t, teren)
	ured.bezSlike(t, "ured", p)

	for range 3 {
		ured.primiOd(t, teren)
		teren.primiOd(t, ured)
	}
	for _, r := range []*racunalo{teren, ured} {
		SetSpremiste(r.sp)
		_, err := ReplaySurface(ctxRaz, r.db, r.rec)
		nuzno(t, err)
		r.bezSlike(t, "nakon obnove", p)
		if r.brojVerzija(t) != verzija {
			t.Errorf("verzija: %d, a bilo %d", r.brojVerzija(t), verzija)
		}
		if n := r.ocisti(t, time.Now()); n != 0 {
			t.Errorf("čišćenje poslije: %d", n)
		}
	}
}

// Računalo koje je bilo ugašeno kad je rok prošao, a riješeno stanje ima otprije,
// briše pri pokretanju, i bez razmjene
func TestUgasenoRacunaloBriseKadSeUpali(t *testing.T) {
	teren, ured := novoRacunalo(t, "teren"), novoRacunalo(t, "ured")
	p := teren.prijavaSaSlikom(t, "Graf se ne crta.", slikaZaRok)
	teren.stanjePrije(t, p.ID, models.StanjePrijaveRijeseno, 29*dan)
	ured.primiOd(t, teren)
	ured.stiglaSlika(t, slikaZaRok)

	if !ured.sp.Ima(ctxRaz, p.SlikaOtisak) {
		t.Fatal("slika mora biti tu do isteka roka")
	}
	if n := ured.ocisti(t, time.Now().Add(2*dan)); n != 1 {
		t.Errorf("uklonjeno %d", n)
	}
	if ured.sp.Ima(ctxRaz, p.SlikaOtisak) {
		t.Error("slika je ostala")
	}

	// a program pri pokretanju prvo obnovi površinu, što je dovoljno i samo po sebi
	druga := novoRacunalo(t, "ured-2")
	druga.primiOd(t, teren)
	druga.stiglaSlika(t, slikaZaRok)
	teren.stanjePrije(t, p.ID, models.StanjePrijaveRijeseno, 31*dan)
	verzije, err := teren.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)
	_, err = druga.rec.Apply(ctxRaz, verzije) // stiglo u knjigu, a program je ugašen prije površine
	nuzno(t, err)
	SetSpremiste(druga.sp)
	_, err = ReplaySurface(ctxRaz, druga.db, druga.rec)
	nuzno(t, err)
	druga.bezSlike(t, "nakon pokretanja", p)
}

// Prijava vraćena iz „riješeno” izlazi iz roka: slika ostaje, a novo rješavanje
// broji 30 dana iznova
func TestVracenaPrijavaPonistavaRok(t *testing.T) {
	teren, ured := novoRacunalo(t, "teren"), novoRacunalo(t, "ured")
	p := teren.prijavaSaSlikom(t, "Graf se ne crta.", slikaZaRok)
	teren.stanjePrije(t, p.ID, models.StanjePrijaveRijeseno, 29*dan)
	SetSpremiste(teren.sp)
	vracena, err := teren.repo.Odgovori(ctxRaz, p.ID, models.StanjePrijaveURadu, "Opet se javlja.", "Ivo Ivić")
	nuzno(t, err)
	if vracena.SlikaIstekla {
		t.Fatal("slika nije istekla, a prijava kaže da jest")
	}
	ured.primiOd(t, teren)
	ured.stiglaSlika(t, slikaZaRok)

	daleko := time.Now().Add(365 * dan)
	for _, r := range []*racunalo{teren, ured} {
		if n := r.ocisti(t, daleko); n != 0 || !r.sp.Ima(ctxRaz, p.SlikaOtisak) {
			t.Errorf("vraćena prijava: uklonjeno %d", n)
		}
	}

	SetSpremiste(teren.sp)
	_, err = teren.repo.Odgovori(ctxRaz, p.ID, models.StanjePrijaveRijeseno, "", "Ivo Ivić")
	nuzno(t, err)
	ured.primiOd(t, teren)
	sad := time.Now()
	for _, r := range []*racunalo{teren, ured} {
		if n := r.ocisti(t, sad.Add(models.RokSlikePrijave-time.Minute)); n != 0 {
			t.Errorf("novi rok: uklonjeno %d prerano", n)
		}
		if n := r.ocisti(t, sad.Add(models.RokSlikePrijave+time.Minute)); n != 1 || r.sp.Ima(ctxRaz, p.SlikaOtisak) {
			t.Errorf("novi rok: uklonjeno %d", n)
		}
	}
}

// Prijava vraćena kad je slika već obrisana ostaje bez slike na svim računalima:
// nitko je ne traži, a računalo koje ju je još imalo briše je
func TestVracenaNakonRokaOstajeBezSlike(t *testing.T) {
	teren, ured := novoRacunalo(t, "teren"), novoRacunalo(t, "ured")
	p := teren.prijavaSaSlikom(t, "Graf se ne crta.", slikaZaRok)
	ured.primiOd(t, teren)
	ured.stiglaSlika(t, slikaZaRok)
	teren.stanjePrije(t, p.ID, models.StanjePrijaveRijeseno, 31*dan)
	if n := teren.ocisti(t, time.Now()); n != 1 {
		t.Fatalf("uklonjeno %d", n)
	}

	vracena, err := teren.repo.Odgovori(ctxRaz, p.ID, models.StanjePrijaveURadu, "Opet se javlja.", "Ivo Ivić")
	nuzno(t, err)
	if !vracena.SlikaIstekla {
		t.Fatal("vraćena prijava ne kaže da slike nema")
	}
	teren.bezSlike(t, "teren", p)

	// ured je dotad bio ugašen i sliku još ima
	ured.primiOd(t, teren)
	ured.bezSlike(t, "ured", p)
	stigla, err := ured.repo.Get(ctxRaz, p.ID)
	nuzno(t, err)
	if stigla.Stanje != models.StanjePrijaveURadu || !stigla.SlikaIstekla {
		t.Errorf("na uredu: %+v", stigla)
	}

	// ni novo rješavanje ni novo vraćanje sliku ne traže
	for _, stanje := range []string{models.StanjePrijaveRijeseno, models.StanjePrijaveNova} {
		_, err = ured.repo.Odgovori(ctxRaz, p.ID, stanje, "", "Ivo Ivić")
		nuzno(t, err)
		teren.primiOd(t, ured)
		teren.bezSlike(t, "teren, "+stanje, p)
		ured.primiOd(t, teren)
		ured.bezSlike(t, "ured, "+stanje, p)
	}
}

// Računalo na kojem je prijava vraćena nakon roka još nije stiglo očistiti: slika
// se obriše idućim čišćenjem, iako prijava više nije riješena
func TestVracenaPrijeCiscenjaBriseSliku(t *testing.T) {
	ured := novoRacunalo(t, "ured")
	p := ured.prijavaSaSlikom(t, "Graf se ne crta.", slikaZaRok)
	ured.stanjePrije(t, p.ID, models.StanjePrijaveRijeseno, 31*dan)
	_, err := ured.repo.Odgovori(ctxRaz, p.ID, models.StanjePrijaveURadu, "", "Ivo Ivić")
	nuzno(t, err)
	if !ured.sp.Ima(ctxRaz, p.SlikaOtisak) {
		t.Fatal("slika mora još biti tu")
	}
	if n := ured.ocisti(t, time.Now()); n != 1 {
		t.Errorf("uklonjeno %d", n)
	}
	ured.bezSlike(t, "ured", p)
}

// Računalo je obrisalo sliku jer još nije znalo da je prijava vraćena prije roka.
// Kad to sazna, prijava je opet živa, pa sliku smije ponovno tražiti.
func TestVracenaPrijeRokaTraziSlikuPonovno(t *testing.T) {
	teren, ured := novoRacunalo(t, "teren"), novoRacunalo(t, "ured")
	p := teren.prijavaSaSlikom(t, "Graf se ne crta.", slikaZaRok)
	teren.stanjePrije(t, p.ID, models.StanjePrijaveRijeseno, 31*dan)
	ured.primiOd(t, teren)
	ured.bezSlike(t, "ured", p)

	// na terenu je vraćena dok je rok još trajao, a ured to prima tek sad
	teren.stanjePrije(t, p.ID, models.StanjePrijaveURadu, dan)
	ured.primiOd(t, teren)
	if !ured.sp.Zeljen(ctxRaz, p.SlikaOtisak) {
		t.Fatal("živa prijava ne traži svoju sliku")
	}
	ured.stiglaSlika(t, slikaZaRok)
	if !ured.repo.ImaSliku(ctxRaz, p.SlikaOtisak) {
		t.Error("slika nije stigla")
	}
}

// Ista slika u dvije prijave ostaje dok je drži druga, i na računalu koje je još
// čeka. Tek kad rok prođe i drugoj, nema je. Sadržaj koji drži drugi zapis ostaje.
func TestIstaSlikaUDvijePrijave(t *testing.T) {
	teren, ured, kasni := novoRacunalo(t, "teren"), novoRacunalo(t, "ured"), novoRacunalo(t, "kasni")
	prva := teren.prijavaSaSlikom(t, "Graf se ne crta.", slikaZaRok)
	druga := teren.prijavaSaSlikom(t, "I karta je prazna.", slikaZaRok)
	otisak := prva.SlikaOtisak
	vezaDruge := sadrzaj.Veza{Entitet: EntityPrijaveGresaka, EntitetID: druga.ID, Uloga: ulogaSlikePrijave}
	ured.primiOd(t, teren)
	ured.stiglaSlika(t, slikaZaRok)
	kasni.primiOd(t, teren) // sliku samo čeka

	teren.stanjePrije(t, prva.ID, models.StanjePrijaveRijeseno, 31*dan)
	if n := teren.ocisti(t, time.Now()); n != 1 {
		t.Errorf("uklonjeno %d", n)
	}
	ured.primiOd(t, teren)
	kasni.primiOd(t, teren)
	for ime, r := range map[string]*racunalo{"teren": teren, "ured": ured} {
		if !r.sp.Ima(ctxRaz, otisak) || !r.sp.ImaVezu(ctxRaz, otisak, vezaDruge) {
			t.Errorf("%s: nestala je slika koju drži druga prijava", ime)
		}
	}
	if !kasni.sp.Zeljen(ctxRaz, otisak) {
		t.Error("računalo koje čeka sliku druge prijave više je ne traži")
	}

	// sliku drže i drugi zapisi: fotografija iste prijave s terena
	drugiZapis := sadrzaj.Veza{Entitet: EntityPrijave, EntitetID: "s-terena", Uloga: ulogaSlike("1")}
	nuzno(t, ured.sp.Vezi(ctxRaz, otisak, drugiZapis))

	teren.stanjePrije(t, druga.ID, models.StanjePrijaveRijeseno, 31*dan)
	if n := teren.ocisti(t, time.Now()); n != 1 || teren.sp.Ima(ctxRaz, otisak) {
		t.Errorf("zadnja prijava: uklonjeno %d", n)
	}
	kasni.primiOd(t, teren)
	kasni.bezSlike(t, "kasni", druga)
	ured.primiOd(t, teren)
	if !ured.sp.Ima(ctxRaz, otisak) || !ured.sp.ImaVezu(ctxRaz, otisak, drugiZapis) {
		t.Error("ured: nestao je sadržaj drugog zapisa")
	}
	if ured.sp.ImaVezu(ctxRaz, otisak, vezaDruge) {
		t.Error("ured: druga prijava još drži sliku")
	}
}

// Računalo koje stigne kasnije prima prijavu, a sliku samo dok rok traje
func TestPrimateljKojiStigneKasnije(t *testing.T) {
	t.Run("nakon roka ništa ne traži", func(t *testing.T) {
		teren, novo := novoRacunalo(t, "teren"), novoRacunalo(t, "novo")
		p := teren.prijavaSaSlikom(t, "Graf se ne crta.", slikaZaRok)
		teren.stanjePrije(t, p.ID, models.StanjePrijaveRijeseno, 31*dan)
		novo.primiOd(t, teren)
		novo.bezSlike(t, "novo", p)
		if z, err := novo.sp.Zeljeni(ctxRaz, 10); err != nil || len(z) != 0 {
			t.Errorf("želje: %+v %v", z, err)
		}
	})
	t.Run("dok rok traje dobije sliku", func(t *testing.T) {
		teren, novo := novoRacunalo(t, "teren"), novoRacunalo(t, "novo")
		p := teren.prijavaSaSlikom(t, "Graf se ne crta.", slikaZaRok)
		teren.stanjePrije(t, p.ID, models.StanjePrijaveRijeseno, 29*dan)
		novo.primiOd(t, teren)
		if !novo.sp.Zeljen(ctxRaz, p.SlikaOtisak) {
			t.Error("slika mora čekati na popisu za dohvat")
		}
	})
	t.Run("verzije u dva paketa", func(t *testing.T) {
		teren, novo := novoRacunalo(t, "teren"), novoRacunalo(t, "novo")
		p := teren.prijavaSaSlikom(t, "Graf se ne crta.", slikaZaRok)
		prvi, err := teren.rec.Since(ctxRaz, "", 0)
		nuzno(t, err)
		SetSpremiste(novo.sp)
		primi(t, novo.cvor, prvi)
		if !novo.sp.Zeljen(ctxRaz, p.SlikaOtisak) {
			t.Fatal("prvi paket mora tražiti sliku")
		}
		teren.stanjePrije(t, p.ID, models.StanjePrijaveRijeseno, 31*dan)
		novo.primiOd(t, teren)
		novo.bezSlike(t, "novo", p)
	})
	t.Run("rok prođe dok slika još čeka", func(t *testing.T) {
		teren, novo := novoRacunalo(t, "teren"), novoRacunalo(t, "novo")
		p := teren.prijavaSaSlikom(t, "Graf se ne crta.", slikaZaRok)
		teren.stanjePrije(t, p.ID, models.StanjePrijaveRijeseno, 29*dan)
		novo.primiOd(t, teren)
		if n := novo.ocisti(t, time.Now().Add(2*dan)); n != 1 || novo.sp.Zeljen(ctxRaz, p.SlikaOtisak) {
			t.Errorf("uklonjeno %d, želja %v", n, novo.sp.Zeljen(ctxRaz, p.SlikaOtisak))
		}
	})
}

// Prijava kojoj je opis izmišljen i pokazuje na primljeni arhivski paket ne briše
// taj paket: on na računalu nema veze, a vrsta mu nije slika
func TestIstekliOpisNeBriseTudjiPaket(t *testing.T) {
	teren, novo := novoRacunalo(t, "teren"), novoRacunalo(t, "novo")
	bajtovi := []byte("arhivski paket")
	otisak, err := novo.sp.Upisi(ctxRaz, "application/vnd.gocop.arhiva", bajtovi, "cvor:drugo")
	nuzno(t, err)

	SetSpremiste(teren.sp)
	p := novaPrijavaGreske("osoba-1")
	p.SlikaOtisak, p.SlikaBajtova, p.SlikaVrsta = otisak, len(bajtovi), models.SlikaPrijavePNG
	nuzno(t, teren.repo.Save(ctxRaz, p))
	teren.stanjePrije(t, p.ID, models.StanjePrijaveRijeseno, 31*dan)

	novo.primiOd(t, teren)
	if n := novo.ocisti(t, time.Now()); n != 0 {
		t.Errorf("uklonjeno %d", n)
	}
	if !novo.sp.Ima(ctxRaz, otisak) {
		t.Error("paket je nestao")
	}
}

// Što je isteklo, a ne da se obrisati, ostaje za sljedeći pokušaj, i javlja se
func TestBrisanjeSlikeKvarovi(t *testing.T) {
	ured := novoRacunalo(t, "ured")
	p := ured.prijavaSaSlikom(t, "Graf se ne crta.", slikaZaRok)
	_, err := ured.repo.Odgovori(ctxRaz, p.ID, models.StanjePrijaveRijeseno, "", "Ivo Ivić")
	nuzno(t, err)
	daleko := time.Now().Add(31 * dan)

	sp, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "zatvoreno.db"))
	nuzno(t, err)
	nuzno(t, sp.Zatvori())
	SetSpremiste(sp)
	if _, err := ured.repo.UkloniIstekleSlike(ctxRaz, daleko); err == nil {
		t.Error("spremište ne radi, a nema greške")
	}
	istekla := novaPrijavaGreske("a")
	istekla.ID = uuid.Must(uuid.NewV7()).String()
	istekla.Stanje, istekla.UpdatedAt = models.StanjePrijaveRijeseno, time.Now().Add(-31*dan)
	istekla.SlikaOtisak, istekla.SlikaBajtova, istekla.SlikaVrsta = p.SlikaOtisak, 10, models.SlikaPrijavePNG
	teret, err := json.Marshal(istekla)
	nuzno(t, err)
	tx, err := ured.db.Begin()
	nuzno(t, err)
	defer func() { nuzno(t, tx.Rollback()) }()
	if err := primijeniPrijavuGreske(ctxRaz, tx, ledger.Version{Payload: teret}); err == nil {
		t.Error("primjena: spremište ne radi, a nema greške")
	}

	SetSpremiste(nil)
	if n, err := ured.repo.UkloniIstekleSlike(ctxRaz, daleko); err != nil || n != 0 {
		t.Errorf("bez spremišta: %d %v", n, err)
	}
}

func TestBrisanjeSlikeZatvorenaBaza(t *testing.T) {
	ured := novoRacunalo(t, "ured")
	nuzno(t, ured.db.Close())
	if _, err := ured.repo.UkloniIstekleSlike(ctxRaz, time.Now()); err == nil {
		t.Error("baza ne radi, a nema greške")
	}
}
