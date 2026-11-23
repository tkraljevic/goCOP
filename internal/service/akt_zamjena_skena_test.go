package service_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/razmjena"
	"gocop/internal/service"
)

// Sken ovjerenog akta zamjenjuje se uz razlog: novi sken postaje izvornik,
// prvi ostaje u potpisu akta, pa potpis ovjere i dalje vrijedi, a zamjena
// nosi vlastiti potpis ključem čvora. Druga zamjena polazi od prve. Nacrt,
// akt ovjeren u goCOP-u i poništen akt nemaju sken koji bi se zamijenio, a
// zamjenu ne radi tko akt ne smije ovjeriti ni tko gleda tuđim očima.
func TestZamjenaSkenaOvjerenogAkta(t *testing.T) {
	o := novaOkolinaAkta(t)
	o.akti.SetKljuc(kljucCvora(7))
	ctx := context.Background()
	prvi := []byte("%PDF-1.4 prvi sken")
	drugi := []byte("%PDF-1.4 drugi sken, obje stranice")
	treci := []byte("%PDF-1.4 treci sken")
	ruk := o.rukovod.ID.String()

	nacrt := o.nacrtDezurnog(t, models.AktUspostava, time.Now().Add(-2*time.Hour).Truncate(time.Minute))
	if _, err := o.akti.ZamijeniSken(ctx, o.ovlasti, o.rukovod, nacrt.ID, drugi, "nečitko"); err == nil || !strings.Contains(err.Error(), "samo na ovjerenom") {
		t.Errorf("nacrt: %v", err)
	}
	if o.akti.SmijeZamijenitiSken(ctx, o.ovlasti, o.rukovod, nacrt) {
		t.Error("nacrt nudi zamjenu skena")
	}
	a, _, err := o.akti.UcitajSkenirani(ctx, o.ovlasti, o.rukovod, nacrt.ID, prvi, ruk, potvrdaPrimatelja)
	if err != nil {
		t.Fatal(err)
	}
	if service.ProvjeriPotpis(a) != service.PotpisVrijedi {
		t.Fatal("potpis ovjere skenom ne vrijedi")
	}
	nitko, nitkoOvl := bezOvlasti()
	for _, c := range []struct {
		opis   string
		perms  *models.UserPermissions
		u      *models.User
		id     string
		sken   []byte
		razlog string
		greska string
	}{
		{"akt kojeg nema", o.ovlasti, o.rukovod, uuid.NewString(), drugi, "nečitko", "akt ne postoji"},
		{"bez prava", nitkoOvl, nitko, a.ID, drugi, "nečitko", service.ErrUnauthorized.Error()},
		{"bez razloga", o.ovlasti, o.rukovod, a.ID, drugi, "  ", "upišite zašto"},
		{"nije sken", o.ovlasti, o.rukovod, a.ID, []byte("nije PDF"), "nečitko", "PDF"},
		{"isti sken", o.ovlasti, o.rukovod, a.ID, prvi, "nečitko", "isti sken"},
	} {
		if _, err := o.akti.ZamijeniSken(ctx, c.perms, c.u, c.id, c.sken, c.razlog); err == nil || !strings.Contains(err.Error(), c.greska) {
			t.Errorf("%s: %v", c.opis, err)
		}
	}
	if _, err := o.akti.ZamijeniSken(service.TudjimOcima(ctx), o.ovlasti, o.rukovod, a.ID, drugi, "nečitko"); err == nil {
		t.Error("tuđim očima se sken ne zamjenjuje")
	}
	if !o.akti.SmijeZamijenitiSken(ctx, o.ovlasti, o.rukovod, a) || o.akti.SmijeZamijenitiSken(ctx, nitkoOvl, nitko, a) {
		t.Error("zamjenu skena smije onaj tko akt smije ovjeriti")
	}

	a, err = o.akti.ZamijeniSken(ctx, o.ovlasti, o.rukovod, a.ID, drugi, " fali druga stranica ")
	if err != nil {
		t.Fatal(err)
	}
	if pdf, err := o.akti.Izvornik(ctx, a.ID); err != nil || !bytes.Equal(pdf, drugi) {
		t.Errorf("izvornik nije novi sken: %v", err)
	}
	if a, err = o.akti.Get(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if len(a.ZamjeneSkena) != 1 || a.ZamjeneSkena[0].Razlog != "fali druga stranica" || a.ZamjeneSkena[0].Zamijenio != o.rukovod.FullName ||
		a.ZamjeneSkena[0].StariSazetak != a.Rucno.Sazetak || a.ZamjeneSkena[0].StariBajtova != len(prvi) || a.SazetakSkena() == a.Rucno.Sazetak {
		t.Fatalf("zapis zamjene: %+v", a.ZamjeneSkena)
	}
	if service.ProvjeriPotpis(a) != service.PotpisVrijedi {
		t.Error("zamjena skena pokvarila je potpis ovjere")
	}
	z := a.ZamjeneSkena[0]
	pub, err := razmjena.ParsePublicKey(z.KljucCvora)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := base64.StdEncoding.DecodeString(z.Potpis)
	if err != nil || !ed25519.Verify(pub, a.PorukaZamjene(z), sig) {
		t.Error("potpis zamjene ne vrijedi")
	}
	if b := o.akti.ZamijenjeniSken(ctx, a, z.StariSazetak); !bytes.Equal(b, prvi) {
		t.Error("prvi sken nije ostao")
	}
	if o.akti.ZamijenjeniSken(ctx, a, z.NoviSazetak) != nil || o.akti.ZamijenjeniSken(ctx, nil, z.StariSazetak) != nil {
		t.Error("sken koji nije zamijenjen ne daje se kao zamijenjeni")
	}

	a, err = o.akti.ZamijeniSken(ctx, o.ovlasti, o.rukovod, a.ID, treci, "krivi papir")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.ZamjeneSkena) != 2 || a.ZamjeneSkena[1].StariSazetak != z.NoviSazetak {
		t.Errorf("druga zamjena: %+v", a.ZamjeneSkena)
	}

	if _, _, err := o.akti.Storniraj(ctx, o.ovlasti, o.rukovod, a.ID, "izdan greškom"); err != nil {
		t.Fatal(err)
	}
	if _, err := o.akti.ZamijeniSken(ctx, o.ovlasti, o.rukovod, a.ID, drugi, "nečitko"); err == nil || !strings.Contains(err.Error(), "poništen") {
		t.Errorf("poništen akt: %v", err)
	}

	goCOP := o.nacrtDezurnog(t, models.AktUspostava, time.Now().Add(-time.Hour).Truncate(time.Minute))
	goCOP, _, err = o.akti.Ovjeri(ctx, o.ovlasti, o.rukovod, goCOP.ID, potvrdaPrimatelja)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.akti.ZamijeniSken(ctx, o.ovlasti, o.rukovod, goCOP.ID, drugi, "nečitko"); err == nil || !strings.Contains(err.Error(), "nema sken") {
		t.Errorf("akt ovjeren u goCOP-u: %v", err)
	}
}

// Kad se izvornik ne da upisati, zamjena se ne bilježi: akt ostaje sa
// starim skenom
func TestZamjenaSkenaKadBazaOdbije(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	a := o.nacrtDezurnog(t, models.AktUspostava, time.Now().Add(-time.Hour).Truncate(time.Minute))
	a, _, err := o.akti.UcitajSkenirani(ctx, o.ovlasti, o.rukovod, a.ID, []byte("%PDF-1.4 prvi"), o.rukovod.ID.String(), potvrdaPrimatelja)
	if err != nil {
		t.Fatal(err)
	}
	o.izvrsi(t, `CREATE TRIGGER zabrana BEFORE UPDATE ON akti_izvornici BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`)
	if _, err := o.akti.ZamijeniSken(ctx, o.ovlasti, o.rukovod, a.ID, []byte("%PDF-1.4 drugi"), "nečitko"); err == nil || !strings.Contains(err.Error(), "zabranjeno") {
		t.Errorf("izvornik koji baza ne primi: %v", err)
	}
	if x, err := o.akti.Get(ctx, a.ID); err != nil || len(x.ZamjeneSkena) != 0 {
		t.Error("zamjena zabilježena iako izvornik nije upisan")
	}
}
