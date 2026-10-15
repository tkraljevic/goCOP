package peers

// Izdavač paketa izdanja (.cop) je čvor čiji ključ paket potpisuje. Član
// mreže koji potpiše paket pod imenom drugog čvora, s visokim izdanjem, ne
// smije zauzeti mjesto tog čvora: primatelj bi inače prave pakete tog čvora
// odbijao kao starija izdanja. Imena čvorova su izmišljena.

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// podmetniIzdavaca: čvor s izda paket kanala svojim ključem, ali u
// manifest upiše tuđe ime i izdanje i potpiše ga iznova
func podmetniIzdavaca(t *testing.T, s *Service, obuhvat CopObuhvat, kanal, izdao string, izdanje int) string {
	t.Helper()
	m, put := izvezi(t, s, obuhvat, []string{kanal}, false)
	m.Izdao, m.Izdanje = izdao, izdanje
	m.Potpis = &CopPotpis{Kljuc: base64.StdEncoding.EncodeToString(s.javni()),
		Potpis: base64.StdEncoding.EncodeToString(ed25519.Sign(s.node.key, kanonskiCop(m)))}
	paket, err := os.ReadFile(put)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(paket), int64(len(paket)))
	if err != nil {
		t.Fatal(err)
	}
	manifest, _ := json.Marshal(m)
	dat := []datotekaCopa{{ime: "manifest.json", b: manifest}}
	for _, f := range zr.File {
		if f.Name == "manifest.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		dat = append(dat, datotekaCopa{ime: f.Name, b: b})
	}
	novi := filepath.Join(t.TempDir(), "podmetnut-"+CopIme(m))
	if err := os.WriteFile(novi, zipCopa(t, dat...), 0o600); err != nil {
		t.Fatal(err)
	}
	return novi
}

func TestUvozCopaPodTudjimImenom(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	vukovar := rubniCvor(t, "ured-vukovar")
	if err := b.saveMembership(ctxRub, primiIzravno(t, a, vukovar)); err != nil {
		t.Fatal(err)
	}
	obuhvat, kanal := CopObuhvat{Vrsta: "ocitanja", AreaID: 16}, "ocitanja/16/2026"
	upisiOcitanja(t, vukovar, kanal, "r-vukovar")

	for _, izdao := range []string{a.node.ID, "nepoznati-cvor"} {
		rep, err := b.UveziCop(ctxRub, podmetniIzdavaca(t, vukovar, obuhvat, kanal, izdao, 99))
		if err == nil || !strings.Contains(err.Error(), "izdao čvor "+izdao) || !rep.PotpisValjan || rep.IzdavacClan {
			t.Errorf("paket pod imenom %s: %+v %v", izdao, rep, err)
		}
	}
	if n, _ := b.rec.CountByChannel(ctxRub); n[kanal] != 0 {
		t.Errorf("paket pod tuđim imenom je ugrađen: %d verzija", n[kanal])
	}

	// pravi paket osječkog čvora i paket Vukovara pod svojim imenom prolaze
	upisiOcitanja(t, a, kanal, "r-osijek")
	_, put := izvezi(t, a, obuhvat, []string{kanal}, false)
	if rep, err := b.UveziCop(ctxRub, put); err != nil || rep.Novih != 1 || !rep.IzdavacClan {
		t.Fatalf("pravi paket izdavača: %+v %v", rep, err)
	}
	_, put = izvezi(t, vukovar, obuhvat, []string{kanal}, false)
	if rep, err := b.UveziCop(ctxRub, put); err != nil || rep.Novih != 1 || !rep.IzdavacClan {
		t.Fatalf("paket člana pod svojim imenom: %+v %v", rep, err)
	}
}
