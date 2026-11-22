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
	if st := posao(); st.Greska != "" || !strings.Contains(st.Sazetak, "pouzdaniji od zadnjeg mjerenja na 2 od 3 dosega") {
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

// Nacrt s dnevnim modelom proba se dnevnom probom, s kišomjerima iz registra.
// Registar koji se ne da pročitati i greška probe su greška posla.
func TestUrednikProbaDnevnogModela(t *testing.T) {
	pb, err := prognoza.Otvori(filepath.Join(t.TempDir(), "prognoze.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer pb.Close()
	n := prognoza.NacrtLetve{Zapis: prognoza.ZapisLetve{Oblik: prognoza.OblikModelaLetve, Letva: "potok-dole", Metoda: prognoza.MetodaDnevni,
		Velicina: "vodostaj", Dnevni: json.RawMessage(`{"ulazi":["potok-vrh"],"slivovi":["Z"]}`)}}
	sadrzaj, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pb.Exec(`INSERT INTO model_nacrt (letva, sadrzaj) VALUES ('potok-dole', ?)`, string(sadrzaj)); err != nil {
		t.Fatal(err)
	}
	registar, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "registar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer registar.Close()
	if _, err := registar.Exec(`CREATE TABLE kisomjeri (code TEXT, sliv TEXT, tezina REAL, aktivan INTEGER, vrsta TEXT);
		INSERT INTO kisomjeri VALUES ('k-z-1', 'Z', 1, 1, 'izvedeni')`); err != nil {
		t.Fatal(err)
	}
	stara := probajDnevniNacrt
	t.Cleanup(func() { probajDnevniNacrt = stara })
	var tocaka int
	var greska error
	ishod := prognoza.NacrtLetve{Proba: &prognoza.ProbaNacrta{Dani: []prognoza.MjeraDana{
		{Dan: 1, Stanje: prognoza.MjeraBolje}, {Dan: 2, Stanje: prognoza.MjeraBolje}, {Dan: 3, Stanje: prognoza.MjeraIsto}}}}
	probajDnevniNacrt = func(_, _ *sql.DB, tocke []prognoza.OborinskaTocka, letva, kad string) (prognoza.NacrtLetve, error) {
		tocaka = len(tocke)
		return ishod, greska
	}
	u := &urednikModela{pb: pb, ocitanja: registar, mreza: &probneUloge{izdaje: true}}
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
	if st := posao(); st.Greska != "" || tocaka != 1 || !strings.Contains(st.Sazetak, "pouzdaniji od zadnjeg mjerenja na 2 od 3 dana") {
		t.Errorf("proba dnevnog modela: %+v, točaka %d", st, tocaka)
	}
	greska = errors.New("potok-dole: samo 900 dana sa svim ulazima")
	if st := posao(); !strings.Contains(st.Greska, "samo 900 dana") {
		t.Errorf("greška probe: %+v", st)
	}
	greska = nil
	u.ocitanja = nil // bez registra proba ide bez kišomjera
	if st := posao(); st.Greska != "" || tocaka != 0 {
		t.Errorf("proba bez registra: %+v, točaka %d", st, tocaka)
	}
	u.ocitanja = registar
	if _, err := registar.Exec(`DROP TABLE kisomjeri`); err != nil {
		t.Fatal(err)
	}
	if st := posao(); !strings.Contains(st.Greska, "kišomjeri iz registra") {
		t.Errorf("registar koji se ne da pročitati: %+v", st)
	}
	if s := opisProbeDnevnog(prognoza.NacrtLetve{}); s != "Proba je gotova." {
		t.Errorf("opis bez probe: %q", s)
	}
}

// Nacrt letve glavnog lanca proba se probom cijelog lanca, u dvije privremene
// baze; sažetak kaže na koliko se još letvi promašaj promijenio.
func TestUrednikProbaGlavneLetve(t *testing.T) {
	pb, err := prognoza.Otvori(filepath.Join(t.TempDir(), "prognoze.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer pb.Close()
	stara := probajGlavniNacrt
	t.Cleanup(func() { probajGlavniNacrt = stara })
	var baze int
	var greska error
	ishod := prognoza.NacrtLetve{Proba: &prognoza.ProbaNacrta{Lanac: &prognoza.ProbaLanca{Nizvodno: []prognoza.PromjenaNizvodno{
		{Letva: "belisce", Doseg: 24}, {Letva: "belisce", Doseg: 48}, {Letva: "osijek", Doseg: 24}}}}}
	probajGlavniNacrt = func(_, _, prije, poslije *sql.DB, letva, kad string, korak func(string)) (prognoza.NacrtLetve, error) {
		korak("mjerenje")
		if prije != nil && poslije != nil && prije != poslije {
			baze = 2
		}
		return ishod, greska
	}
	u := &urednikModela{pb: pb, mreza: &probneUloge{izdaje: true}}
	posao := func() poslovi.Pogled {
		t.Helper()
		p := poslovi.NoviRegistar().Pokreni("proba", "", "/", func(p *poslovi.Posao) error { return u.ProbajNacrt("donji-miholjac", p) })
		for range 400 {
			if !p.Traje() {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		return p.Stanje()
	}
	if st := posao(); st.Greska != "" || baze != 2 || !strings.Contains(st.Sazetak, "promašaj se promijenio na još 2 letve") {
		t.Errorf("proba letve glavnog lanca: %+v, baza %d", st, baze)
	}
	ishod.Proba.Lanac.Nizvodno = nil
	if st := posao(); !strings.Contains(st.Sazetak, "ostalim letvama se promašaj nije promijenio") {
		t.Errorf("proba bez promjena: %+v", st)
	}
	greska = errors.New("račun nije pripremljen; pripremi ga pa probaj")
	if st := posao(); !strings.Contains(st.Greska, "nije pripremljen") {
		t.Errorf("greška probe: %+v", st)
	}
	if s := opisProbeGlavnog(prognoza.NacrtLetve{}); s != "Proba je gotova." {
		t.Errorf("opis bez probe: %q", s)
	}
}

// modelSKasnjenjem je model od jednog računa donja ← gornja s zadanim kašnjenjem.
func modelSKasnjenjem(t *testing.T, pb *sql.DB, sati int) {
	t.Helper()
	if err := prognoza.Spremi(pb, []prognoza.Pojas{{Letva: "donja", Velicina: "vodostaj", Od: -1000, Do: 1000, Rasap: 3,
		Ulazi: []prognoza.Ulaz{{Letva: "gornja", Velicina: "vodostaj", PomakH: sati, Sirina: 1, Nagib: 1}}}}, "proba"); err != nil {
		t.Fatal(err)
	}
}

func kasnjenjeUModelu(t *testing.T, pb *sql.DB) int {
	t.Helper()
	in, err := prognoza.InaciceLetve(pb, "donja")
	if err != nil || len(in) != 1 {
		t.Fatalf("model: %+v, %v", in, err)
	}
	return in[0][0].Ulazi[0].PomakH
}

// Primjena na glavni lanac upiše model i odmah izda prognozu. Kad izdavanje ne
// uspije, vraća se model kakav je bio i nacrt ostaje; kad uspije, nacrt se briše.
func TestUrednikPrimjenaNaGlavniLanac(t *testing.T) {
	pb, err := prognoza.Otvori(filepath.Join(t.TempDir(), "prognoze.db"))
	if err != nil {
		t.Fatal(err)
	}
	modelSKasnjenjem(t, pb, 3)
	nacrt := func() {
		t.Helper()
		if _, err := pb.Exec(`INSERT OR REPLACE INTO model_nacrt (letva, sadrzaj) VALUES ('donji-miholjac', '{"zapis":{"letva":"donji-miholjac"}}')`); err != nil {
			t.Fatal(err)
		}
	}
	imaNacrt := func() bool {
		t.Helper()
		n, err := prognoza.Nacrti(pb)
		if err != nil {
			t.Fatal(err)
		}
		_, ima := n["donji-miholjac"]
		return ima
	}
	stara := primijeniGlavniNacrt
	t.Cleanup(func() { primijeniGlavniNacrt = stara })
	var greska error
	primijeniGlavniNacrt = func(baza, radna *sql.DB, letva string, promjena prognoza.PromjenaLetve, _ prognoza.OkvirModela) (prognoza.PaketModela, error) {
		if radna == nil || radna == baza || promjena.Tko != "Iva Proba" || promjena.Razlog != "bliže izmjerenom" || promjena.Kad == "" {
			t.Errorf("primjena: %+v", promjena)
		}
		prije, _, err := prognoza.SastaviModel(baza)
		if err != nil {
			t.Fatal(err)
		}
		modelSKasnjenjem(t, baza, 5)
		return prije, greska
	}
	u := &urednikModela{pb: pb, mreza: &probneUloge{izdaje: true}}
	primijeni := func() error { return u.PrimijeniNacrt("donji-miholjac", "Iva Proba", "bliže izmjerenom") }

	nacrt()
	var izdano []string
	u.izdaj = func(_ context.Context, letva string) error {
		izdano = append(izdano, letva)
		return nil
	}
	if err := primijeni(); err != nil || kasnjenjeUModelu(t, pb) != 5 || imaNacrt() || len(izdano) != 1 || izdano[0] != "donji-miholjac" {
		t.Errorf("uspjela primjena: %v, kašnjenje %d, izdano %v", err, kasnjenjeUModelu(t, pb), izdano)
	}
	modelSKasnjenjem(t, pb, 3)
	nacrt()
	u.izdaj = func(context.Context, string) error { return errors.New("gornja nema svježe očitanje") }
	if err := primijeni(); err == nil || !strings.Contains(err.Error(), "vraćen je prethodni") || !strings.Contains(err.Error(), "gornja nema svježe očitanje") ||
		kasnjenjeUModelu(t, pb) != 3 || !imaNacrt() {
		t.Errorf("primjena kad izdavanje ne uspije: %v, kašnjenje %d", err, kasnjenjeUModelu(t, pb))
	}
	u.izdaj = nil // bez izdavanja model ostaje primijenjen
	if err := primijeni(); err != nil || kasnjenjeUModelu(t, pb) != 5 {
		t.Errorf("primjena bez izdavanja: %v", err)
	}
	greska = errors.New("model se promijenio nakon probe")
	if err := primijeni(); err == nil || err.Error() != "model se promijenio nakon probe" {
		t.Errorf("greška primjene: %v", err)
	}
	u.mreza = &probneUloge{}
	if err := primijeni(); !errors.Is(err, errNeIzdaje) {
		t.Errorf("računalo koje ne izdaje: %v", err)
	}
	prije, _, err := prognoza.SastaviModel(pb)
	if err != nil {
		t.Fatal(err)
	}
	if err := pb.Close(); err != nil {
		t.Fatal(err)
	}
	if err := vratiModel(pb, prije, errors.New("izdavanje palo")); err == nil || !strings.Contains(err.Error(), "prethodni model se nije dao vratiti") {
		t.Errorf("povratak modela u zatvorenu bazu: %v", err)
	}
}

// Izdavanje nakon primjene računa prognozu iznova, zapiše je i objavi. Letva
// kojoj novi račun ne da prognozu je greška, kao i račun ili zapis koji ne uspije.
func TestIzdavanjePrimijenjenimModelom(t *testing.T) {
	k, objavljeno := izdavackiKrug(t)
	izdaj := izdajPrimijenjenim(k.osvjezivac, k.objavi)
	if err := izdaj(context.Background(), "donja"); err != nil || len(*objavljeno) != 1 {
		t.Fatalf("izdavanje: %v, objavljeno %d", err, len(*objavljeno))
	}
	if err := prognoza.Spremi(k.prognoze, []prognoza.Pojas{
		{Letva: "donja", Velicina: "vodostaj", Od: -1000, Do: 1000, Rasap: 3,
			Ulazi: []prognoza.Ulaz{{Letva: "gornja", Velicina: "vodostaj", PomakH: 3, Sirina: 1, Nagib: 1}}},
		{Letva: "treca", Velicina: "vodostaj", Od: -1000, Do: 1000, Rasap: 3,
			Ulazi: []prognoza.Ulaz{{Letva: "bez-ocitanja", Velicina: "vodostaj", PomakH: 3, Sirina: 1, Nagib: 1}}}}, "proba"); err != nil {
		t.Fatal(err)
	}
	if err := izdaj(context.Background(), "treca"); err == nil || len(*objavljeno) != 1 {
		t.Errorf("letva bez prognoze: %v, objavljeno %d", err, len(*objavljeno))
	}
	modelSKasnjenjem(t, k.prognoze, 3)
	zabraniZapisIzdanja(t, k.prognoze)
	if err := izdaj(context.Background(), "donja"); err == nil || len(*objavljeno) != 1 {
		t.Errorf("zapis koji ne uspije: %v, objavljeno %d", err, len(*objavljeno))
	}
	bezModela, _ := okolinaKruga(t, true)
	if err := izdajPrimijenjenim(bezModela.osvjezivac, bezModela.objavi)(context.Background(), "donja"); err == nil {
		t.Error("račun bez modela mora javiti grešku")
	}
}

// Urednik sprema, proba, primjenjuje i miče dnevni blok letve dnevnog modela
// Drave i Dunava. Računalo koje ne izdaje prognozu ne mijenja ništa.
func TestUrednikDnevnogBloka(t *testing.T) {
	pb, err := prognoza.Otvori(filepath.Join(t.TempDir(), "prognoze.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer pb.Close()
	t.Cleanup(func() { prognoza.OsvjeziDnevnuPostavu(pb) })
	stara := probajDnevniGlavni
	t.Cleanup(func() { probajDnevniGlavni = stara })
	var greska error
	probajDnevniGlavni = func(_, baza *sql.DB, tocke []prognoza.OborinskaTocka, letva, kad string) (prognoza.NacrtDnevnog, error) {
		if greska != nil {
			return prognoza.NacrtDnevnog{}, greska
		}
		// proba upiše ishod u nacrt, kao prava
		if _, err := baza.Exec(`UPDATE model_nacrt SET sadrzaj = json_set(sadrzaj, '$.proba', json('{"kad":"18:05","dani":[]}')) WHERE letva = ?`, "dnevni:"+letva); err != nil {
			t.Fatal(err)
		}
		return prognoza.NacrtDnevnog{Letva: letva, Proba: &prognoza.ProbaDnevnogBloka{Dani: []prognoza.MjeraDana{
			{Dan: 1, Stanje: prognoza.MjeraBolje}, {Dan: 2, Stanje: prognoza.MjeraIsto}}}}, nil
	}
	uloge := &probneUloge{izdaje: true}
	u := &urednikModela{pb: pb, mreza: uloge}
	blok := prognoza.DnevniLetve{Ulazi: []string{"terezino-polje", "botovo"}, Slivovi: []string{"A", "B"}}
	if err := u.SpremiDnevni("donji-miholjac", blok, "Iva Proba"); err != nil {
		t.Fatal(err)
	}
	if n, err := prognoza.DnevniNacrti(pb); err != nil || n["donji-miholjac"].Tko != "Iva Proba" || n["donji-miholjac"].Kad == "" {
		t.Fatalf("nacrt dnevnog bloka: %+v, %v", n, err)
	}
	posao := func() poslovi.Pogled {
		t.Helper()
		p := poslovi.NoviRegistar().Pokreni("proba", "", "/", func(p *poslovi.Posao) error { return u.ProbajDnevniGlavni("donji-miholjac", p) })
		for range 400 {
			if !p.Traje() {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		return p.Stanje()
	}
	if st := posao(); st.Greska != "" || !strings.Contains(st.Sazetak, "pouzdaniji od zadnjeg mjerenja na 1 od 2 dana") {
		t.Errorf("proba dnevnog bloka: %+v", st)
	}
	if err := u.PrimijeniDnevni("donji-miholjac", "Iva Proba", "bliže izmjerenom"); err != nil {
		t.Fatal(err)
	}
	if !prognoza.DnevniRacunIzZapisa("donji-miholjac") {
		t.Error("primijenjeni dnevni blok mora odmah vrijediti")
	}
	if err := u.UkloniDnevni("donji-miholjac", "Iva Proba", "natrag na program"); err != nil || prognoza.DnevniRacunIzZapisa("donji-miholjac") {
		t.Errorf("uklanjanje dnevnog bloka: %v", err)
	}
	if err := u.SpremiDnevni("donji-miholjac", blok, "Iva Proba"); err != nil {
		t.Fatal(err)
	}
	if err := u.ObrisiDnevniNacrt("donji-miholjac"); err != nil {
		t.Fatal(err)
	}
	if n, err := prognoza.DnevniNacrti(pb); err != nil || len(n) != 0 {
		t.Errorf("nacrt nakon odbacivanja: %v, %v", n, err)
	}
	greska = errors.New("donji-miholjac: samo 900 dana sa svim ulazima")
	if st := posao(); !strings.Contains(st.Greska, "samo 900 dana") {
		t.Errorf("greška probe: %+v", st)
	}
	registar, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "registar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer registar.Close()
	u.ocitanja = registar // registar bez tablice kišomjera
	if st := posao(); !strings.Contains(st.Greska, "kišomjeri iz registra") {
		t.Errorf("registar koji se ne da pročitati: %+v", st)
	}
	if s := opisProbeBloka(prognoza.NacrtDnevnog{}); s != "Proba je gotova." {
		t.Errorf("opis bez probe: %q", s)
	}
	uloge.izdaje = false
	for ime, err := range map[string]error{
		"spremi":    u.SpremiDnevni("donji-miholjac", blok, "Iva Proba"),
		"odbaci":    u.ObrisiDnevniNacrt("donji-miholjac"),
		"proba":     u.ProbajDnevniGlavni("donji-miholjac", &poslovi.Posao{}),
		"primijeni": u.PrimijeniDnevni("donji-miholjac", "Iva Proba", "razlog"),
		"ukloni":    u.UkloniDnevni("donji-miholjac", "Iva Proba", "razlog"),
	} {
		if !errors.Is(err, errNeIzdaje) {
			t.Errorf("%s na računalu koje ne izdaje: %v", ime, err)
		}
	}
}
