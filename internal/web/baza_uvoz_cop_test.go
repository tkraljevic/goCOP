package web

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/peers"
)

// cvorZaUvozCopa je čvor bez mreže s Održavanjem baze u mapi dir (ključ čvora
// je uz bazu); upisi upisuje zapis u kanal očitanja
func cvorZaUvozCopa(t *testing.T, ime, dir string) (ps *peers.Service, upisi func(id string), h *DBMaintHandler) {
	t.Helper()
	dbPath := filepath.Join(dir, "gocop.db")
	baza, err := db.BazaIzPredloska(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = baza.Close() }) // samo testna baza
	rec := ledger.New(baza, ime)
	n, err := peers.LoadNode(dbPath, ime, ime, "test")
	if err != nil {
		t.Fatal(err)
	}
	ps, err = peers.NewService(baza, rec, n, peers.Ports{})
	if err != nil {
		t.Fatal(err)
	}
	upisi = func(id string) {
		if _, err := rec.RecordIn(context.Background(), baza, "ocitanja/16/2026", "probni", id, map[string]string{"id": id}); err != nil {
			t.Fatal(err)
		}
	}
	return ps, upisi, NewDBMaintHandler(nil, rec, ps, nil, nil)
}

// ugradiPaket šalje paket na Održavanje baze i vraća odgovor iz adrese
func ugradiPaket(t *testing.T, h *DBMaintHandler, paket []byte) string {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	fw, err := mw.CreateFormFile("archive", "paket.cop")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(paket); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/administracija/baza/uvoz", &b)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.RemoteAddr, r.Host = adresaLokalno, adresaCvoraTesta // iz lokalne mreže
	r = r.WithContext(context.WithValue(r.Context(), contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: true}))
	w := httptest.NewRecorder()
	h.HandleImport(w, r)
	return mustUnescape(w.Header().Get("Location"))
}

// Računalo koje nije u mreži ugrađuje samo svoj paket; paket drugog
// računala odbija porukom koja kaže zašto.
func TestUgradnjaPaketaBezMreze(t *testing.T) {
	ctx := context.Background()
	mapaIzdavaca := t.TempDir()
	izdavac, upisi, _ := cvorZaUvozCopa(t, "laptop-izvan", mapaIzdavaca)
	upisi("r-1")
	var paket bytes.Buffer
	if _, err := izdavac.IzveziCop(ctx, peers.CopObuhvat{Vrsta: "ocitanja", AreaID: 16}, []string{"ocitanja/16/2026"}, false, &paket); err != nil {
		t.Fatal(err)
	}

	_, _, h := cvorZaUvozCopa(t, "novi-laptop", t.TempDir())
	if loc := ugradiPaket(t, h, paket.Bytes()); !strings.Contains(loc, "error=Paket je izdalo računalo laptop-izvan. Ovo računalo još nije u mreži") {
		t.Fatalf("paket drugog računala: %s", loc)
	}

	// isto računalo s novom bazom i istim ključem ugradi svoj paket
	mapaObnove := t.TempDir()
	kljuc, err := os.ReadFile(filepath.Join(mapaIzdavaca, peers.KeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mapaObnove, peers.KeyFileName), kljuc, 0o600); err != nil {
		t.Fatal(err)
	}
	obnovljen, _, hObnove := cvorZaUvozCopa(t, "laptop-izvan", mapaObnove)
	obnovljen.SetWantsAll(true)
	if loc := ugradiPaket(t, hObnove, paket.Bytes()); !strings.Contains(loc, "success=Ugrađeno izdanje 1") || !strings.Contains(loc, "1 zapis u paketu, 1 novi") {
		t.Fatalf("vlastiti paket: %s", loc)
	}
}
