package peers_test

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/repository"
	"gocop/internal/sadrzaj"
)

// Izdanje kanala putuje datotekom kao i mrežom: A izda .cop s prijavama i
// PDF-om, B ga ugradi i ima zapise i sadržaj; isti sadržaj daje isto
// izdanje, promijenjen sljedeće; diran paket i starije izdanje se odbijaju.
func TestCopIzdanjeKanalaPutujeDatotekom(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	a := startNodeBezRegistra(t, ctx, "cop-osijek")
	b := startNodeBezRegistra(t, ctx, "laptop-baranja")
	founder(t, ctx, a, "Hrvatske vode")
	pair(t, ctx, a, b)

	spA, _ := sadrzaj.Otvori(filepath.Join(t.TempDir(), "a.db"))
	defer spA.Zatvori()
	spB, _ := sadrzaj.Otvori(filepath.Join(t.TempDir(), "b.db"))
	defer spB.Zatvori()
	a.svc.SetSpremiste(spA)
	b.svc.SetSpremiste(spB)
	repository.SetSpremiste(spB)
	defer repository.SetSpremiste(nil)
	a.svc.SetWantsAll(true)
	b.svc.SetWantsAll(true)

	kanal := "prijave/16/2026"
	pdf := []byte("%PDF-1.4\nizvornik prijave za paket")
	otisak, _ := spA.Upisi(ctx, "application/pdf", pdf, "ovdje", sadrzaj.Veza{Entitet: repository.EntityPrijave, EntitetID: "p1", Uloga: "izvornik", Kanal: kanal})
	tx, _ := a.db.BeginTx(ctx, nil)
	if _, err := a.rec.RecordIn(ctx, tx, kanal, repository.EntityPrijaveIzvornici, "p1",
		models.IzvornikLista{ListID: "p1", Otisak: otisak, Bajtova: len(pdf), Vrsta: "application/pdf", UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	_ = tx.Commit()

	obuhvat := peers.CopObuhvat{Vrsta: "prijave", AreaID: 16, Od: 2026, Do: 2026}
	var paket bytes.Buffer
	m, err := a.svc.IzveziCop(ctx, obuhvat, []string{kanal}, true, &paket)
	if err != nil {
		t.Fatal(err)
	}
	if m.Izdanje != 1 || m.Zapisa != 1 || m.Sadrzaja != 1 || m.Ukljuceno != 1 || m.Potpis == nil {
		t.Fatalf("manifest: %+v", m)
	}
	if peers.CopIme(m) != "gocop-prijave-bp16-2026_v1.cop" {
		t.Errorf("ime: %s", peers.CopIme(m))
	}
	// isti sadržaj: isto izdanje i otisak, i bez bajtova
	var kazalo bytes.Buffer
	m2, err := a.svc.IzveziCop(ctx, obuhvat, []string{kanal}, false, &kazalo)
	if err != nil || m2.Izdanje != 1 || m2.Otisak != m.Otisak || m2.Ukljuceno != 0 {
		t.Fatalf("ponovno izdanje: %v %+v", err, m2)
	}
	if kazalo.Len() >= paket.Len() {
		t.Error("paket bez sadržaja nije manji")
	}

	put := filepath.Join(t.TempDir(), "p.cop")
	_ = os.WriteFile(put, paket.Bytes(), 0o644)
	rep, err := b.svc.UveziCop(ctx, put)
	if err != nil {
		t.Fatalf("ugradnja: %v", err)
	}
	if rep.Novih != 1 || rep.SadrzajaUpisano != 1 || !rep.PotpisValjan || !rep.IzdavacClan {
		t.Fatalf("izvještaj: %+v", rep)
	}
	var n int
	_ = b.db.QueryRow(`SELECT count(*) FROM prijave_izvornici WHERE prijava_id = 'p1' AND otisak = ?`, otisak).Scan(&n)
	if n != 1 || !spB.Ima(ctx, otisak) {
		t.Fatalf("B nema zapis ili sadržaj: %d %v", n, spB.Ima(ctx, otisak))
	}
	// ista datoteka drugi put ne mijenja ništa
	if rep, err := b.svc.UveziCop(ctx, put); err != nil || rep.Novih != 0 {
		t.Errorf("ponovljena ugradnja: %v %+v", err, rep)
	}

	// diran paket: zapis promijenjen unutar ZIP-a, manifest ostao — otisak
	// se ne slaže i paket se odbija
	putD := filepath.Join(t.TempDir(), "d.cop")
	_ = os.WriteFile(putD, prepakiraj(t, paket.Bytes(), "zapisi.jsonl", func(b []byte) []byte {
		return bytes.Replace(b, []byte(`"p1"`), []byte(`"p2"`), 1)
	}), 0o644)
	if _, err := b.svc.UveziCop(ctx, putD); err == nil {
		t.Error("diran paket je prošao")
	}
	// prepravljen manifest s preračunatim otiskom ruši potpis
	putM := filepath.Join(t.TempDir(), "m.cop")
	_ = os.WriteFile(putM, prepakiraj(t, paket.Bytes(), "manifest.json", func(b []byte) []byte {
		return bytes.Replace(b, []byte(`"izdao": "cop-osijek"`), []byte(`"izdao": "tudji-cvor"`), 1)
	}), 0o644)
	if _, err := b.svc.UveziCop(ctx, putM); err == nil {
		t.Error("paket s prepravljenim manifestom je prošao")
	}

	// nova verzija na A: sljedeće izdanje s prethodnim otiskom
	tx, _ = a.db.BeginTx(ctx, nil)
	_, _ = a.rec.RecordIn(ctx, tx, kanal, repository.EntityPrijaveIzvornici, "p1",
		models.IzvornikLista{ListID: "p1", Otisak: otisak, Bajtova: len(pdf), Vrsta: "application/pdf", Sazetak: "novi", UpdatedAt: time.Now()})
	_ = tx.Commit()
	var paket2 bytes.Buffer
	m3, err := a.svc.IzveziCop(ctx, obuhvat, []string{kanal}, true, &paket2)
	if err != nil || m3.Izdanje != 2 || m3.Prethodno != m.Otisak {
		t.Fatalf("drugo izdanje: %v %+v", err, m3)
	}
	put2 := filepath.Join(t.TempDir(), "p2.cop")
	_ = os.WriteFile(put2, paket2.Bytes(), 0o644)
	if rep, err := b.svc.UveziCop(ctx, put2); err != nil || rep.Novih != 1 {
		t.Fatalf("ugradnja drugog izdanja: %v %+v", err, rep)
	}
	// starije izdanje istog izdavača se odbija
	if _, err := b.svc.UveziCop(ctx, put); err == nil {
		t.Error("starije izdanje je prošlo")
	}
}

// prepakiraj složi isti ZIP s jednom promijenjenom datotekom
func prepakiraj(t *testing.T, paket []byte, ime string, f func([]byte) []byte) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(paket), int64(len(paket)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, zf := range zr.File {
		rc, _ := zf.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		if zf.Name == ime {
			b = f(b)
		}
		w, _ := zw.Create(zf.Name)
		_, _ = w.Write(b)
	}
	_ = zw.Close()
	return out.Bytes()
}
