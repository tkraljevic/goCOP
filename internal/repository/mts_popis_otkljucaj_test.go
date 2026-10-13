package repository

import (
	"encoding/json"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Otključavanje inventure (Odluke (13. 10. 2026.)): zaključena inventura
// vraća se u nacrt, a usklađenja koja je zaključenje upisalo miču se iz
// prometa. Oboje u jednoj transakciji i oboje u knjizi, da drugi čvor
// dobije istu sliku.

// zakljucenaInventura upisuje skladište, početno stanje i zaključenu
// inventuru s jednim usklađenjem, kako je ostavlja ZakljuciPopis: 46 lopata
// u knjizi, 44 prebrojane, usklađenje −2
func zakljucenaInventura(t *testing.T, n *cvor) (*MtsRepository, *models.Popis) {
	t.Helper()
	repo := NewMtsRepository(n.db, n.rec)
	sk := &models.Skladiste{Sektor: "B", AreaID: 16, Naziv: "Skladište Ribnjak", Aktivno: true}
	nuzno(t, repo.SaveSkladiste(ctxRaz, sk))
	dan := time.Date(2026, 9, 30, 0, 0, 0, 0, models.Zagreb)
	nuzno(t, repo.SavePromet(ctxRaz, []models.Promet{{Datum: dan.AddDate(0, 0, -5), VrstaID: "lopata", Oblik: models.OblikOsnovni,
		Kolicina: 46, Vrsta: models.PrometPocetno, Sektor: "B", SkladisteID: sk.ID, UserName: "Petra Perić"}}))
	zakljuceno := time.Date(2026, 10, 1, 9, 0, 0, 0, models.Zagreb)
	p := &models.Popis{SkladisteID: sk.ID, Sektor: "B", Dan: dan, Godina: 2026, IzradioID: "skladistar", Izradio: "Petra Perić",
		IzradenoAt: zakljuceno, ZakljucenoAt: &zakljuceno, Napomena: "brojala Petra",
		Stavke: []models.PopisnaStavka{{VrstaID: "lopata", Oblik: models.OblikOsnovni, Utvrdjeno: 44, Knjizno: 46}}}
	nuzno(t, repo.SavePopis(ctxRaz, p))
	nuzno(t, repo.SavePromet(ctxRaz, []models.Promet{{Datum: dan, VrstaID: "lopata", Oblik: models.OblikOsnovni, Kolicina: -2,
		Vrsta: models.PrometPopis, Sektor: "B", SkladisteID: sk.ID, PopisID: p.ID, UserName: "Petra Perić",
		Napomena: "usklađenje po inventuri na dan 30.9.2026."}}))
	return repo, p
}

// stanjeLopata javlja koliko lopata knjiga pokazuje u skladištu
func stanjeLopata(t *testing.T, repo *MtsRepository, skladisteID string) float64 {
	t.Helper()
	st, err := repo.Stanje(ctxRaz, skladisteID, "", nil)
	nuzno(t, err)
	var k float64
	for _, s := range st {
		if s.VrstaID == "lopata" {
			k += s.Kolicina
		}
	}
	return k
}

// verzijaUKnjizi broji sve verzije u knjizi čvora
func verzijaUKnjizi(t *testing.T, n *cvor) int {
	t.Helper()
	po, err := n.rec.Count(ctxRaz)
	nuzno(t, err)
	ukupno := 0
	for _, k := range po {
		ukupno += k
	}
	return ukupno
}

func TestOtkljucajPopisVracaNacrtIBriseUskladjenja(t *testing.T) {
	n := noviCvor(t, "ured")
	repo, p := zakljucenaInventura(t, n)
	uskladjenja, err := repo.UskladjenjaPopisa(ctxRaz, p.ID)
	nuzno(t, err)
	if len(uskladjenja) != 1 || uskladjenja[0].Kolicina != -2 || uskladjenja[0].PopisID != p.ID {
		t.Fatalf("usklađenja zaključene inventure: %+v", uskladjenja)
	}
	if k := stanjeLopata(t, repo, p.SkladisteID); k != 44 {
		t.Fatalf("stanje poslije zaključenja: %v", k)
	}
	// početno stanje nije usklađenje, iako je istog skladišta
	if sve, _ := repo.ListPromet(ctxRaz, FiltarPrometa{SkladisteID: p.SkladisteID}); len(sve) != 2 {
		t.Fatalf("promet skladišta: %d redaka", len(sve))
	}

	p.Napomena = "brojala Petra. Otključao: Marko Marić, 13. 10. 2026. u 11:16; 1 usklađenje je obrisano."
	nuzno(t, repo.OtkljucajPopis(ctxRaz, p, uskladjenja))

	nacrt, err := repo.GetPopis(ctxRaz, p.ID)
	nuzno(t, err)
	if nacrt.Zakljucen() || nacrt.Napomena != p.Napomena || len(nacrt.Stavke) != 1 || nacrt.Stavke[0].Utvrdjeno != 44 {
		t.Errorf("popis poslije otključavanja: %+v", nacrt)
	}
	if ostala, _ := repo.UskladjenjaPopisa(ctxRaz, p.ID); len(ostala) != 0 {
		t.Errorf("usklađenja poslije otključavanja: %+v", ostala)
	}
	if k := stanjeLopata(t, repo, p.SkladisteID); k != 46 {
		t.Errorf("stanje poslije otključavanja: %v, a knjiga bez usklađenja ima 46", k)
	}
	// u knjizi: usklađenje kao spomenik, popis bez zaključenja
	v, err := n.rec.Latest(ctxRaz, EntityMtsPromet, uskladjenja[0].ID)
	nuzno(t, err)
	if !v.Archived {
		t.Errorf("usklađenje u knjizi nije obrisano: %+v", v)
	}
	vp, err := n.rec.Latest(ctxRaz, EntityMtsPopisi, p.ID)
	nuzno(t, err)
	var uKnjizi models.Popis
	nuzno(t, json.Unmarshal(vp.Payload, &uKnjizi))
	if vp.Archived || uKnjizi.Zakljucen() || uKnjizi.Napomena != p.Napomena {
		t.Errorf("popis u knjizi: %+v", uKnjizi)
	}
}

// Ako upis popisa ne uspije, ne mijenja se ništa: usklađenja ostaju u
// prometu i inventura ostaje zaključena. Inače bi nastao zaključeni popis
// bez usklađenja ili nacrt koji ih nosi.
func TestOtkljucajPopisSveIliNista(t *testing.T) {
	kvarovi := []struct {
		ime     string
		pokvari func(t *testing.T, n *cvor, p *models.Popis)
	}{
		{"upis popisa", func(t *testing.T, n *cvor, _ *models.Popis) {
			_, err := n.db.Exec(`CREATE TRIGGER pukni BEFORE UPDATE ON mts_popisi BEGIN SELECT RAISE(ABORT, 'pukni'); END`)
			nuzno(t, err)
		}},
		{"popis koji se ne da zapisati", func(_ *testing.T, _ *cvor, p *models.Popis) {
			p.Stavke[0].Utvrdjeno = math.NaN()
		}},
		{"brisanje usklađenja", func(t *testing.T, n *cvor, _ *models.Popis) {
			_, err := n.db.Exec(`CREATE TRIGGER pukni BEFORE DELETE ON mts_promet BEGIN SELECT RAISE(ABORT, 'pukni'); END`)
			nuzno(t, err)
		}},
		{"spomenik usklađenja u knjizi", func(t *testing.T, n *cvor, _ *models.Popis) {
			_, err := n.db.Exec(`CREATE TRIGGER pukni BEFORE INSERT ON record_versions WHEN NEW.entity = 'mts_promet' BEGIN SELECT RAISE(ABORT, 'pukni'); END`)
			nuzno(t, err)
		}},
		{"knjiga", func(t *testing.T, n *cvor, _ *models.Popis) {
			_, err := n.db.Exec(`CREATE TRIGGER pukni BEFORE INSERT ON record_versions WHEN NEW.entity = 'mts_popisi' BEGIN SELECT RAISE(ABORT, 'pukni'); END`)
			nuzno(t, err)
		}},
	}
	for _, k := range kvarovi {
		t.Run(k.ime, func(t *testing.T) {
			n := noviCvor(t, "ured")
			repo, p := zakljucenaInventura(t, n)
			uskladjenja, err := repo.UskladjenjaPopisa(ctxRaz, p.ID)
			nuzno(t, err)
			prije := verzijaUKnjizi(t, n)
			k.pokvari(t, n, p)
			if err := repo.OtkljucajPopis(ctxRaz, p, uskladjenja); err == nil {
				t.Fatal("otključavanje je prošlo, a dio upisa nije")
			}
			if ostala, _ := repo.UskladjenjaPopisa(ctxRaz, p.ID); len(ostala) != 1 {
				t.Errorf("usklađenja poslije neuspjeha: %+v", ostala)
			}
			if cur, _ := repo.GetPopis(ctxRaz, p.ID); cur == nil || !cur.Zakljucen() {
				t.Errorf("popis poslije neuspjeha: %+v", cur)
			}
			if poslije := verzijaUKnjizi(t, n); poslije != prije {
				t.Errorf("knjiga: %d verzija prije, %d poslije", prije, poslije)
			}
		})
	}
}

// Zatvorena baza: otključavanje, spremanje popisa i brisanje prometa vraćaju
// grešku, ne pola posla
func TestMtsZatvorenaBazaVracaGresku(t *testing.T) {
	n := noviCvor(t, "ured")
	repo, p := zakljucenaInventura(t, n)
	uskladjenja, err := repo.UskladjenjaPopisa(ctxRaz, p.ID)
	nuzno(t, err)
	n.db.Close()
	if err := repo.OtkljucajPopis(ctxRaz, p, uskladjenja); err == nil {
		t.Error("otključavanje na zatvorenoj bazi")
	}
	if err := repo.SavePopis(ctxRaz, p); err == nil {
		t.Error("spremanje popisa na zatvorenoj bazi")
	}
	if err := repo.ArhivirajPromet(ctxRaz, uskladjenja); err == nil {
		t.Error("brisanje prometa na zatvorenoj bazi")
	}
	if _, err := repo.UskladjenjaPopisa(ctxRaz, p.ID); err == nil {
		t.Error("čitanje usklađenja na zatvorenoj bazi")
	}
}

// Redak usklađenja koji se ne da pročitati javlja grešku umjesto praznog
// popisa; inače bi otključavanje ostavilo usklađenje u prometu
func TestUskladjenjaPopisaJavljajuNecitljivRedak(t *testing.T) {
	n := noviCvor(t, "ured")
	repo, p := zakljucenaInventura(t, n)
	_, err := n.db.Exec(`UPDATE mts_promet SET created_at = 'nije vrijeme' WHERE popis_id = ?`, p.ID)
	nuzno(t, err)
	if _, err := repo.UskladjenjaPopisa(ctxRaz, p.ID); err == nil {
		t.Error("nečitljiv redak usklađenja nije javljen")
	}
}

// SavePopis i ArhivirajPromet idu kroz iste korake kao otključavanje;
// neuspjeh u njima ne ostavlja ništa
func TestSavePopisIArhivirajPrometSveIliNista(t *testing.T) {
	n := noviCvor(t, "ured")
	repo, p := zakljucenaInventura(t, n)
	prije := verzijaUKnjizi(t, n)
	kvar := *p
	kvar.Stavke = []models.PopisnaStavka{{VrstaID: "lopata", Utvrdjeno: math.Inf(1)}}
	if err := repo.SavePopis(ctxRaz, &kvar); err == nil || !strings.Contains(err.Error(), p.ID) {
		t.Errorf("popis koji se ne da zapisati: %v", err)
	}
	_, err := n.db.Exec(`CREATE TRIGGER pukni BEFORE DELETE ON mts_promet BEGIN SELECT RAISE(ABORT, 'pukni'); END`)
	nuzno(t, err)
	uskladjenja, err := repo.UskladjenjaPopisa(ctxRaz, p.ID)
	nuzno(t, err)
	if err := repo.ArhivirajPromet(ctxRaz, uskladjenja); err == nil || !strings.Contains(err.Error(), uskladjenja[0].ID) {
		t.Errorf("brisanje retka koje ne uspije: %v", err)
	}
	if poslije := verzijaUKnjizi(t, n); poslije != prije {
		t.Errorf("knjiga: %d verzija prije, %d poslije", prije, poslije)
	}
	_, err = n.db.Exec(`DROP TRIGGER pukni`)
	nuzno(t, err)
	nuzno(t, repo.ArhivirajPromet(ctxRaz, uskladjenja))
	if ostala, _ := repo.UskladjenjaPopisa(ctxRaz, p.ID); len(ostala) != 0 {
		t.Errorf("obrisani redak još je u prometu: %+v", ostala)
	}
	if v, err := n.rec.Latest(ctxRaz, EntityMtsPromet, uskladjenja[0].ID); err != nil || !v.Archived {
		t.Errorf("obrisani redak u knjizi: %+v, %v", v, err)
	}
}

// slikaInventure je ono što čvor pokazuje o inventuri: popis, njegova
// usklađenja i stanje skladišta
type slikaInventure struct {
	Zakljucen   bool
	Napomena    string
	Stavke      []models.PopisnaStavka
	Uskladjenja []string
	Lopata      float64
}

func slikaNa(t *testing.T, n *cvor, popisID string) slikaInventure {
	t.Helper()
	repo := NewMtsRepository(n.db, n.rec)
	p, err := repo.GetPopis(ctxRaz, popisID)
	nuzno(t, err)
	if p == nil {
		t.Fatalf("%s nema popis %s", n.ime, popisID)
	}
	u, err := repo.UskladjenjaPopisa(ctxRaz, popisID)
	nuzno(t, err)
	s := slikaInventure{Zakljucen: p.Zakljucen(), Napomena: p.Napomena, Stavke: p.Stavke, Lopata: stanjeLopata(t, repo, p.SkladisteID)}
	for _, x := range u {
		s.Uskladjenja = append(s.Uskladjenja, x.ID)
	}
	return s
}

// Drugi čvor koji primi otključavanje dobiva istu sliku: nacrt bez
// usklađenja i stanje bez njih. Primjena primljenog ne stvara verzije, a
// isti skup verzija daje isto i čvoru koji ih primi odjednom, obrnutim
// redom. Ponovno zaključenje na prvom čvoru stiže kao i svako.
func TestOtkljucavanjeStizeDrugomCvoru(t *testing.T) {
	ured, teren := noviCvor(t, "ured"), noviCvor(t, "teren")
	repo, p := zakljucenaInventura(t, ured)
	posalji(t, ured, teren)
	if s := slikaNa(t, teren, p.ID); !s.Zakljucen || len(s.Uskladjenja) != 1 || s.Lopata != 44 {
		t.Fatalf("zaključena inventura na drugom čvoru: %+v", s)
	}

	uskladjenja, err := repo.UskladjenjaPopisa(ctxRaz, p.ID)
	nuzno(t, err)
	p.Napomena += " Otključao: Marko Marić, 13. 10. 2026. u 11:16; 1 usklađenje je obrisano."
	nuzno(t, repo.OtkljucajPopis(ctxRaz, p, uskladjenja))
	posalji(t, ured, teren)

	naUredu := slikaNa(t, ured, p.ID)
	if naUredu.Zakljucen || len(naUredu.Uskladjenja) != 0 || naUredu.Lopata != 46 {
		t.Fatalf("otključana inventura na prvom čvoru: %+v", naUredu)
	}
	if naTerenu := slikaNa(t, teren, p.ID); !reflect.DeepEqual(naTerenu, naUredu) {
		t.Errorf("drugi čvor:\n%+v\na prvi:\n%+v", naTerenu, naUredu)
	}
	if a, b := verzijaUKnjizi(t, ured), verzijaUKnjizi(t, teren); a != b {
		t.Errorf("primjena je stvorila verzije: prvi čvor %d, drugi %d", a, b)
	}

	// novi čvor dobiva sve odjednom, obrnutim redom nastanka
	novi := noviCvor(t, "novi")
	sve, err := ured.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)
	slices.Reverse(sve)
	primi(t, novi, sve)
	if naNovom := slikaNa(t, novi, p.ID); !reflect.DeepEqual(naNovom, naUredu) {
		t.Errorf("novi čvor:\n%+v\na prvi:\n%+v", naNovom, naUredu)
	}

	// ponovno zaključenje: novo usklađenje stiže, obrisano se ne vraća
	zakljuceno := time.Date(2026, 10, 13, 11, 30, 0, 0, models.Zagreb)
	p.ZakljucenoAt = &zakljuceno
	nuzno(t, repo.SavePromet(ctxRaz, []models.Promet{{Datum: p.Dan, VrstaID: "lopata", Oblik: models.OblikOsnovni, Kolicina: -2,
		Vrsta: models.PrometPopis, Sektor: "B", SkladisteID: p.SkladisteID, PopisID: p.ID, UserName: "Petra Perić"}}))
	nuzno(t, repo.SavePopis(ctxRaz, p))
	posalji(t, ured, teren)
	naTerenu := slikaNa(t, teren, p.ID)
	if !naTerenu.Zakljucen || len(naTerenu.Uskladjenja) != 1 || naTerenu.Uskladjenja[0] == uskladjenja[0].ID || naTerenu.Lopata != 44 {
		t.Errorf("ponovno zaključena inventura na drugom čvoru: %+v", naTerenu)
	}
}

// verzijeEntiteta vraća verzije jednog entiteta iz knjige čvora
func verzijeEntiteta(t *testing.T, n *cvor, entitet string) []ledger.Version {
	t.Helper()
	sve, err := n.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)
	var out []ledger.Version
	for _, v := range sve {
		if v.Entity == entitet {
			out = append(out, v)
		}
	}
	return out
}

// Verzija otključanog popisa ne nosi zaključenje, pa ga ni stariji čvor ne
// prepisuje iz prethodne verzije kao polje koje ne poznaje
func TestOtkljucaniPopisUKnjiziNemaZakljucenja(t *testing.T) {
	n := noviCvor(t, "ured")
	repo, p := zakljucenaInventura(t, n)
	uskladjenja, err := repo.UskladjenjaPopisa(ctxRaz, p.ID)
	nuzno(t, err)
	nuzno(t, repo.OtkljucajPopis(ctxRaz, p, uskladjenja))
	verzije := verzijeEntiteta(t, n, EntityMtsPopisi)
	if len(verzije) != 2 {
		t.Fatalf("verzije popisa: %d", len(verzije))
	}
	if strings.Contains(string(verzije[1].Payload), "zakljuceno_at") || !strings.Contains(string(verzije[0].Payload), "zakljuceno_at") {
		t.Errorf("zaključenje u verzijama:\n%s\n%s", verzije[0].Payload, verzije[1].Payload)
	}
	if verzije[1].Supersedes != verzije[0].VersionID {
		t.Errorf("otključavanje ne nastavlja zaključenu verziju: %+v", verzije[1])
	}
}
