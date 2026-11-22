package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/peers"
	"gocop/internal/poslovi"
	"gocop/internal/prognoza"
	"gocop/internal/web"
)

// Letve i osobe su izmišljene.

// Nacrt se sprema i odbacuje samo na računalu koje izdaje prognozu.
func TestUrednikModela(t *testing.T) {
	pb, err := prognoza.Otvori(filepath.Join(t.TempDir(), "prognoze.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer pb.Close()
	uloge := &probneUloge{}
	u := &urednikModela{pb: pb, mreza: uloge}
	n := prognoza.NacrtLetve{Tko: "Iva Proba", Zapis: prognoza.ZapisLetve{Oblik: prognoza.OblikModelaLetve, Letva: "potok-dole",
		Metoda: prognoza.MetodaLanac, Velicina: "protok", Racun: prognoza.RacunLetve{Ulazi: []string{"potok-vrh"}}}}
	if u.Izdaje() || u.SpremiNacrt(n) != errNeIzdaje || u.ObrisiNacrt("potok-dole") != errNeIzdaje {
		t.Error("računalo koje ne izdaje ne smije spremati ni odbacivati nacrt")
	}
	uloge.izdaje = true
	// bez arhive i registra provjera sektora i satnog protoka se ne radi
	if err := u.SpremiNacrt(n); err != nil || !u.Izdaje() {
		t.Fatalf("spremanje: %v", err)
	}
	if svi, err := prognoza.Nacrti(pb); err != nil || svi["potok-dole"].Tko != "Iva Proba" {
		t.Errorf("nacrt nije u bazi: %v, %v", svi, err)
	}
	n.Zapis.Racun.Ulazi = nil
	if err := u.SpremiNacrt(n); err == nil {
		t.Error("nacrt bez ulaza mora biti odbijen")
	}
	if err := u.ObrisiNacrt("potok-dole"); err != nil {
		t.Fatal(err)
	}
	if svi, err := prognoza.Nacrti(pb); err != nil || len(svi) != 0 {
		t.Errorf("nacrt nije odbačen: %v, %v", svi, err)
	}
}

// probneUloge su uloge računala koje test mijenja.
type probneUloge struct{ izdaje bool }

func (p *probneUloge) TrenutneUloge() peers.Uloge { return peers.Uloge{Izdaje: p.izdaje} }

// probniPosluzitelj pamti što mu je uključeno.
type probniPosluzitelj struct {
	priprema web.PripremaModela
	urednik  web.UrednikModela
}

func (p *probniPosluzitelj) SetPripremaModela(f web.PripremaModela) { p.priprema = f }
func (p *probniPosluzitelj) SetUrednikModela(u web.UrednikModela)   { p.urednik = u }

// Poslužitelj dobiva pripremu modela i urednika koji izdavanje pita pri svakom
// pozivu, jer se uloga računala mijenja dok program radi.
func TestUkljuciModel(t *testing.T) {
	uloge := &probneUloge{}
	p := &probniPosluzitelj{}
	ukljuciModel(p, func(context.Context, *poslovi.Posao) error { return nil }, &urednikModela{mreza: uloge})
	if p.priprema == nil || p.urednik == nil || p.urednik.Izdaje() {
		t.Fatal("poslužitelj nije dobio pripremu i urednika")
	}
	uloge.izdaje = true
	if !p.urednik.Izdaje() {
		t.Error("urednik mora vidjeti promjenu uloge")
	}
}

// Priprema nacrta ide samo na računalu koje izdaje, a posao na kraju kaže koliko
// razina računa i dokle. Greška pripreme je greška posla.
func TestUrednikPripremaNacrt(t *testing.T) {
	uloge := &probneUloge{}
	u := &urednikModela{mreza: uloge}
	stara := pripremiNacrt
	t.Cleanup(func() { pripremiNacrt = stara })
	var trazena string
	ishod := prognoza.NacrtLetve{Zapis: prognoza.ZapisLetve{Racun: prognoza.RacunLetve{Doseg: 9},
		Brojevi: prognoza.BrojeviLetve{Inacice: [][]prognoza.Pojas{{{}, {}, {}}}}}}
	var greska error
	pripremiNacrt = func(_, _ *sql.DB, letva, kad string, _ prognoza.OkvirModela) (prognoza.NacrtLetve, error) {
		trazena = letva + " " + kad
		return ishod, greska
	}
	posao := func(f func(*poslovi.Posao) error) poslovi.Pogled {
		t.Helper()
		p := poslovi.NoviRegistar().Pokreni("proba", "", "/", f)
		for range 200 {
			if !p.Traje() {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		return p.Stanje()
	}
	pripremi := func(p *poslovi.Posao) error { return u.PripremiNacrt("potok-dole", p) }
	if st := posao(pripremi); !strings.Contains(st.Greska, "računalu koje izdaje") || trazena != "" {
		t.Errorf("računalo koje ne izdaje: %+v", st)
	}
	uloge.izdaje = true
	if st := posao(pripremi); st.Greska != "" || !strings.Contains(st.Sazetak, "razina s računom 3, računa do 9 h unaprijed") ||
		!strings.HasPrefix(trazena, "potok-dole ") || len(trazena) < len("potok-dole 1. 1. 2026. 00:00") {
		t.Errorf("priprema: %+v, tražena %q", st, trazena)
	}
	greska = errors.New("namještanje modela već traje; pokušaj kad završi")
	if st := posao(pripremi); !strings.Contains(st.Greska, "već traje") {
		t.Errorf("greška pripreme: %+v", st)
	}
	if s := opisPripremeNacrta(prognoza.NacrtLetve{}); !strings.Contains(s, "razina s računom 0") {
		t.Errorf("opis praznog nacrta: %q", s)
	}
}

// Proba nacrta ide samo na računalu koje izdaje, u privremenu bazu koja se
// poslije briše. Posao na kraju kaže na koliko je dosega račun bolji.
func TestUrednikProbaNacrta(t *testing.T) {
	uloge := &probneUloge{}
	u := &urednikModela{mreza: uloge}
	stara, staroOtvaranje := probajNacrt, otvoriPrognoze
	t.Cleanup(func() { probajNacrt, otvoriPrognoze = stara, staroOtvaranje })
	var putPrivremene string
	var greska error
	ishod := prognoza.NacrtLetve{Proba: &prognoza.ProbaNacrta{Dosezi: []prognoza.MjeraDosega{
		{Doseg: 6, Stanje: prognoza.MjeraIsto}, {Doseg: 24, Stanje: prognoza.MjeraBolje}, {Doseg: 48, Stanje: prognoza.MjeraBolje}}}}
	probajNacrt = func(_, _, privremena *sql.DB, letva, kad string) (prognoza.NacrtLetve, error) {
		// privremena baza je prava, prazna baza prognoza
		if _, _, err := prognoza.SastaviModel(privremena); err != nil {
			t.Errorf("privremena baza: %v", err)
		}
		if err := privremena.QueryRow(`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&putPrivremene); err != nil {
			t.Errorf("put privremene baze: %v", err)
		}
		return ishod, greska
	}
	posao := func() poslovi.Pogled {
		t.Helper()
		p := poslovi.NoviRegistar().Pokreni("proba", "", "/", func(p *poslovi.Posao) error { return u.ProbajNacrt("potok-dole", p) })
		for range 400 {
			if !p.Traje() {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		return p.Stanje()
	}
	if st := posao(); !strings.Contains(st.Greska, "računalu koje izdaje") || putPrivremene != "" {
		t.Errorf("računalo koje ne izdaje: %+v", st)
	}
	uloge.izdaje = true
	if st := posao(); st.Greska != "" || !strings.Contains(st.Sazetak, "bolji nego bez modela na 2 od 3 dosega") {
		t.Errorf("proba: %+v", st)
	}
	if putPrivremene == "" {
		t.Fatal("proba nije dobila privremenu bazu")
	}
	if _, err := os.Stat(filepath.Dir(putPrivremene)); !os.IsNotExist(err) {
		t.Errorf("privremena baza nije obrisana: %v", err)
	}
	greska = errors.New("račun nije pripremljen; pripremi ga pa probaj")
	if st := posao(); !strings.Contains(st.Greska, "nije pripremljen") {
		t.Errorf("greška probe: %+v", st)
	}
	otvoriPrognoze = func(string) (*sql.DB, error) { return nil, errors.New("disk je pun") }
	if st := posao(); !strings.Contains(st.Greska, "privremena baza za probu: disk je pun") {
		t.Errorf("privremena baza koja se ne da otvoriti: %+v", st)
	}
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "nema-je"))
	if st := posao(); !strings.Contains(st.Greska, "privremena baza za probu") {
		t.Errorf("privremena mapa koja se ne da napraviti: %+v", st)
	}
	if s := opisProbeNacrta(prognoza.NacrtLetve{}); s != "Proba je gotova." {
		t.Errorf("opis bez probe: %q", s)
	}
}

// probniKrug je satni krug koji test pali i gasi.
type probniKrug struct{ traje bool }

func (k *probniKrug) UTijeku() bool { return k.traje }

// Model se primjenjuje i uklanja samo na računalu koje izdaje i dok satni krug ne
// traje. Primijenjeni model nosi ime, vrijeme i razlog.
func TestUrednikPrimjenaIUklanjanje(t *testing.T) {
	pb, err := prognoza.Otvori(filepath.Join(t.TempDir(), "prognoze.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer pb.Close()
	uloge, krug := &probneUloge{}, &probniKrug{traje: true}
	u := &urednikModela{pb: pb, mreza: uloge, krug: krug}
	if u.PrimijeniNacrt("potok-dole", "Iva Proba", "proba") != errNeIzdaje || u.UkloniModel("potok-dole") != errNeIzdaje ||
		u.VratiPrethodni("potok-dole", "Iva Proba", "proba") != errNeIzdaje {
		t.Error("računalo koje ne izdaje ne smije mijenjati model")
	}
	uloge.izdaje = true
	if u.PrimijeniNacrt("potok-dole", "Iva Proba", "proba") != errKrugTraje || u.UkloniModel("potok-dole") != errKrugTraje ||
		u.VratiPrethodni("potok-dole", "Iva Proba", "proba") != errKrugTraje {
		t.Error("dok satni krug traje, model se ne mijenja")
	}
	krug.traje = false
	if err := u.PrimijeniNacrt("potok-dole", "Iva Proba", "proba"); err == nil || !strings.Contains(err.Error(), "nije proban") {
		t.Errorf("primjena bez probanog nacrta: %v", err)
	}
	if err := u.UkloniModel("potok-dole"); err == nil || !strings.Contains(err.Error(), "nema primijenjen model") {
		t.Errorf("uklanjanje bez modela: %v", err)
	}
	// probani nacrt upisan izravno: račun iz jednog ulaza, s brojevima i probom
	n := prognoza.NacrtLetve{Zapis: prognoza.ZapisLetve{Oblik: prognoza.OblikModelaLetve, Letva: "potok-dole", Metoda: prognoza.MetodaLanac,
		Velicina: "vodostaj", Racun: prognoza.RacunLetve{Ulazi: []string{"potok-vrh"}, Doseg: 5},
		Brojevi: prognoza.BrojeviLetve{Inacice: [][]prognoza.Pojas{{{Letva: "potok-dole", Velicina: "vodostaj", Od: -1000, Do: 1000, Sati: 5000,
			Ulazi: []prognoza.Ulaz{{Letva: "potok-vrh", Velicina: "vodostaj", PomakH: 5, Sirina: 1, Nagib: 2}}}}}}},
		Priprema: &prognoza.PripremaNacrta{Kad: "08:01"}, Proba: &prognoza.ProbaNacrta{Kad: "08:02"}}
	sadrzaj, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pb.Exec(`INSERT INTO model_nacrt (letva, sadrzaj) VALUES ('potok-dole', ?)`, string(sadrzaj)); err != nil {
		t.Fatal(err)
	}
	u.krug = nil // bez kruga se ne pita
	if err := u.PrimijeniNacrt("potok-dole", "Iva Proba", "prvi model potoka"); err != nil {
		t.Fatal(err)
	}
	zapisi, _, err := prognoza.ZapisiLetvi(pb)
	if err != nil || len(zapisi["potok-dole"].Promjene) != 1 {
		t.Fatalf("primijenjen model: %v, %v", zapisi, err)
	}
	if p := zapisi["potok-dole"].Promjene[0]; p.Tko != "Iva Proba" || p.Razlog != "prvi model potoka" || len(p.Kad) < len("1. 1. 2026. 00:00") {
		t.Errorf("promjena %+v", p)
	}
	if err := u.VratiPrethodni("potok-dole", "Iva Proba", "nema na što"); err == nil || !strings.Contains(err.Error(), "nema prethodnu inačicu") {
		t.Errorf("povratak prvog modela: %v", err)
	}
	// druga primjena istog nacrta ostavlja prethodni, pa se na njega može vratiti
	if _, err := pb.Exec(`INSERT INTO model_nacrt (letva, sadrzaj) VALUES ('potok-dole', ?)`, string(sadrzaj)); err != nil {
		t.Fatal(err)
	}
	if err := u.PrimijeniNacrt("potok-dole", "Marko Primjer", "druga primjena"); err != nil {
		t.Fatal(err)
	}
	if err := u.VratiPrethodni("potok-dole", "Iva Proba", "povratak na prvi"); err != nil {
		t.Fatal(err)
	}
	zapisi, _, err = prognoza.ZapisiLetvi(pb)
	if err != nil || len(zapisi["potok-dole"].Promjene) != 3 || zapisi["potok-dole"].Promjene[2].Razlog != "povratak na prvi" {
		t.Errorf("vraćen model: %+v, %v", zapisi["potok-dole"].Promjene, err)
	}
	if err := u.UkloniModel("potok-dole"); err != nil {
		t.Fatal(err)
	}
	if zapisi, _, err := prognoza.ZapisiLetvi(pb); err != nil || len(zapisi) != 0 {
		t.Errorf("model nije uklonjen: %v, %v", zapisi, err)
	}
}
