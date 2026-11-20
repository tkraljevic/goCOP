package peers

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/ledger"
)

// Pregled kaže što bi ugradnja donijela, a ne upisuje ništa: nakon pregleda
// knjiga je ista i ista se ugradnja može napraviti; nakon ugradnje isti
// paket u pregledu nema novih, a starije izdanje bi se odbilo (Odluke
// (18. 11. 2026.), Administracija, novo ponašanje 9)
func TestPregledCopaBezUpisa(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	upisiOcitanja(t, a, "ocitanja/16/2026", "o-1")
	_, prvi := izvezi(t, a, CopObuhvat{Vrsta: "ocitanja", AreaID: 16}, []string{"ocitanja/16/2026"}, true)

	brojVerzija := func() int {
		var n int
		if err := b.db.QueryRow(`SELECT COUNT(*) FROM record_versions`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	prije := brojVerzija()
	pr, err := b.PregledCop(ctxRub, prvi)
	if err != nil || pr.Odbija != "" || pr.Verzija != 1 || pr.Prima != 1 || pr.Novih != 1 || len(pr.Kanali) != 1 || pr.Kanali[0].Kanal != "ocitanja/16/2026" || pr.Manifest.Izdao == "" {
		t.Fatalf("pregled: %+v %v", pr, err)
	}
	if brojVerzija() != prije {
		t.Fatal("pregled je upisao u knjigu")
	}
	if rep, err := b.UveziCop(ctxRub, prvi); err != nil || rep.Novih != 1 {
		t.Fatalf("ugradnja nakon pregleda: %+v %v", rep, err)
	}
	if pr, err := b.PregledCop(ctxRub, prvi); err != nil || pr.Novih != 0 || pr.Prima != 1 || pr.Odbija != "" {
		t.Errorf("isti paket nakon ugradnje: %+v %v", pr, err)
	}

	// novije izdanje istog obuhvata, pa je prvo starije
	upisiOcitanja(t, a, "ocitanja/16/2026", "o-2")
	_, drugi := izvezi(t, a, CopObuhvat{Vrsta: "ocitanja", AreaID: 16}, []string{"ocitanja/16/2026"}, true)
	if _, err := b.UveziCop(ctxRub, drugi); err != nil {
		t.Fatal(err)
	}
	if pr, err := b.PregledCop(ctxRub, prvi); err != nil || !strings.Contains(pr.Odbija, "starije se ne ugrađuje") {
		t.Errorf("starije izdanje: %+v %v", pr, err)
	}
}

// Paket ključa izvan mreže pregled odbija s razlogom, a paket koji se ne da
// pročitati javlja grešku
func TestPregledCopaOdbijenIPokvaren(t *testing.T) {
	_, b := izdavacIPrimatelj(t)
	_, strani, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	g := novaGradnjaCopa(t)
	potpisiGradnju(g, strani)
	manifest, errM := json.Marshal(g.m)
	popis, errP := json.Marshal(g.sadrzaji)
	if errM != nil || errP != nil {
		t.Fatal(errM, errP)
	}
	put := filepath.Join(t.TempDir(), "strani.cop")
	if err := os.WriteFile(put, zipCopa(t, datotekaCopa{ime: "manifest.json", b: manifest},
		datotekaCopa{ime: "zapisi.jsonl", b: []byte("\n")}, datotekaCopa{ime: "sadrzaji.json", b: popis}), 0o600); err != nil {
		t.Fatal(err)
	}
	if pr, err := b.PregledCop(ctxRub, put); err != nil || !strings.Contains(pr.Odbija, "nije član naše mreže") {
		t.Errorf("strani paket: %+v %v", pr, err)
	}
	pokvaren := filepath.Join(t.TempDir(), "pokvaren.cop")
	if err := os.WriteFile(pokvaren, []byte("nije zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.PregledCop(ctxRub, pokvaren); err == nil {
		t.Error("pokvaren paket bez greške")
	}
	if _, err := b.PregledCop(ctxRub, filepath.Join(t.TempDir(), "nema.cop")); err == nil {
		t.Error("paket kojeg nema bez greške")
	}
}

// Paket člana s valjanim potpisom, a zapisima koji ne odgovaraju otisku,
// pregled javlja kao grešku, kao i ugradnja
func TestPregledCopaKriviZapisi(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	g := novaGradnjaCopa(t)
	g.m.Izdao = a.node.ID
	potpisiGradnju(g, a.node.key)
	g.zapisi = bytes.Replace(g.zapisi, []byte(`"p-1"`), []byte(`"p-9"`), 1)
	put := filepath.Join(t.TempDir(), "paket.cop")
	if err := os.WriteFile(put, g.slozi(t), 0o600); err != nil {
		t.Fatal(err)
	}
	if pr, err := b.PregledCop(ctxRub, put); err == nil || !strings.Contains(err.Error(), "otisak paketa se ne slaže") || pr.Manifest.Izdao != a.node.ID {
		t.Errorf("krivi zapisi: %+v %v", pr, err)
	}
}

func TestPorukaIzdanja(t *testing.T) {
	m := CopManifest{Izdanje: 3, Izdao: "ured", Otisak: "a"}
	for _, c := range []struct {
		primljeno int
		otisak    string
		zeli      string
	}{{4, "x", "starije se ne ugrađuje"}, {3, "b", "s drugim otiskom"}, {3, "a", ""}, {2, "x", ""}} {
		got := porukaIzdanja(m, c.primljeno, c.otisak)
		if c.zeli == "" && got != "" || c.zeli != "" && !strings.Contains(got, c.zeli) {
			t.Errorf("primljeno %d %q: %q", c.primljeno, c.otisak, got)
		}
	}
}

// Kad se knjiga ne da pročitati, pregled javlja grešku, a ne nulu
func TestPregledCopaKnjigaSeNeDaProcitati(t *testing.T) {
	_, b := izdavacIPrimatelj(t)
	v := []ledger.Version{{VersionID: "v-1", Channel: "ocitanja/16/2026"}}
	if ima, err := b.uKnjizi(ctxRub, v); err != nil || len(ima) != 0 {
		t.Fatalf("prazna knjiga: %v %v", ima, err)
	}
	b.SetWantsAll(false) // pretplate se tada čitaju iz baze
	b.db.Close()
	if _, err := b.uKnjizi(ctxRub, v); err == nil {
		t.Error("zatvorena baza bez greške")
	}
	if _, _, err := b.zaPregled(ctxRub, v); err == nil {
		t.Error("pretplate iz zatvorene baze bez greške")
	}
	var pr CopPregled
	if err := b.prebrojiPregled(ctxRub, &pr, v); err == nil {
		t.Error("prebrojavanje nad zatvorenom bazom bez greške")
	}
}

func TestKanaliPregleda(t *testing.T) {
	v := func(id, kanal string) ledger.Version { return ledger.Version{VersionID: id, Channel: kanal} }
	sve := []ledger.Version{v("1", "prijave/16/2026"), v("2", "ocitanja/16/2026"), v("3", "ocitanja/16/2026")}
	kanali, prima, novih := kanaliPregleda(sve, sve[1:], map[string]bool{"2": true})
	if prima != 2 || novih != 1 || len(kanali) != 2 || kanali[0] != (KanalPregleda{"ocitanja/16/2026", 2, 2, 1}) || kanali[1] != (KanalPregleda{"prijave/16/2026", 1, 0, 0}) {
		t.Errorf("%+v %d %d", kanali, prima, novih)
	}
}
