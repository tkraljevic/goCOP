package service_test

import (
	"testing"

	"gocop/internal/models"
)

// prijavi vraća sesiju administratora na čvoru
func prijavi(t *testing.T, n *cvor, username string) *models.Session {
	t.Helper()
	sess, _, err := n.auth.Login(username, "gocop2026", "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("prijava %s: %v", username, err)
	}
	return sess
}

func TestAdminGledaTudimOcimaIVidiNjegoveOvlasti(t *testing.T) {
	n := noviCvor(t, "cvor-a")
	sess := prijavi(t, n, "tomislav")

	admin, _ := n.repo.GetUserByUsername("tomislav")
	adminPerms, _ := n.auth.PermissionsFor(admin.ID)
	if !adminPerms.IsGlobalAdmin {
		t.Fatal("test traži globalnog administratora")
	}

	meta := vodocuvar(t, n)
	if err := n.auth.StartViewingAs(sess.ID, adminPerms, meta.ID); err != nil {
		t.Fatalf("pokretanje pregleda: %v", err)
	}

	view, err := n.auth.AuthenticateSessionView(sess.ID)
	if err != nil {
		t.Fatalf("čitanje sesije: %v", err)
	}
	if !view.Viewing {
		t.Fatal("sesija ne zna da se gleda tuđim očima")
	}
	if view.User.ID != meta.ID {
		t.Errorf("program gleda očima %s, očekivano %s", view.User.Username, meta.Username)
	}
	if view.RealUser.ID != admin.ID {
		t.Errorf("prijavljen je %s, očekivano %s", view.RealUser.Username, admin.Username)
	}
	if view.Perms.IsGlobalAdmin {
		t.Error("u tuđem pogledu ostale su administratorske ovlasti")
	}

	// Povratak sebi vraća i ovlasti
	if err := n.auth.StopViewingAs(sess.ID); err != nil {
		t.Fatalf("povratak: %v", err)
	}
	view, _ = n.auth.AuthenticateSessionView(sess.ID)
	if view.Viewing || !view.Perms.IsGlobalAdmin {
		t.Error("povratak sebi nije vratio administratorske ovlasti")
	}
}

// Pregled je čitanje tuđeg zaslona, a ne prijava: račun djelatnika mora ostati
// nepreuzet, inače bi testiranje samo od sebe prljalo popis.
func TestPregledNeOstavljaTragNaTudemRacunu(t *testing.T) {
	n := noviCvor(t, "cvor-a")
	sess := prijavi(t, n, "tomislav")

	admin, _ := n.repo.GetUserByUsername("tomislav")
	adminPerms, _ := n.auth.PermissionsFor(admin.ID)
	meta := vodocuvar(t, n)

	if err := n.auth.StartViewingAs(sess.ID, adminPerms, meta.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := n.auth.AuthenticateSessionView(sess.ID); err != nil {
		t.Fatal(err)
	}

	poslije, _ := n.repo.GetUserByUsername(meta.Username)
	if poslije.LastLoginAt != nil {
		t.Error("pregled je zabilježen kao prijava na tuđem računu")
	}
	if poslije.AccountState() != models.AccountPending {
		t.Errorf("stanje tuđeg računa nakon pregleda = %s, očekivano %s",
			poslije.AccountState(), models.AccountPending)
	}
}

func TestObicniDjelatnikNeMozeGledatiTudimOcima(t *testing.T) {
	n := noviCvor(t, "cvor-a")
	meta := vodocuvar(t, n)

	metaPerms, err := n.auth.PermissionsFor(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if metaPerms.IsGlobalAdmin {
		t.Skip("odabrani djelatnik je administrator")
	}

	admin, _ := n.repo.GetUserByUsername("tomislav")
	sess := prijavi(t, n, "tomislav")
	if err := n.auth.StartViewingAs(sess.ID, metaPerms, admin.ID); err == nil {
		t.Error("djelatnik bez administratorskih ovlasti pokrenuo je tuđi pregled")
	}
}

// Ovlast se provjerava pri svakom zahtjevu: administratoru kojem je pravo
// oduzeto usred pregleda, pogled se odmah zatvara.
func TestOduzimanjeAdministratorstvaZatvaraPregled(t *testing.T) {
	n := noviCvor(t, "cvor-a")
	sess := prijavi(t, n, "tomislav")

	admin, _ := n.repo.GetUserByUsername("tomislav")
	adminPerms, _ := n.auth.PermissionsFor(admin.ID)
	meta := vodocuvar(t, n)
	if err := n.auth.StartViewingAs(sess.ID, adminPerms, meta.ID); err != nil {
		t.Fatal(err)
	}

	admin.IsGlobalAdmin = false
	if err := n.repo.UpdateUser(admin); err != nil {
		t.Fatalf("oduzimanje administratorstva: %v", err)
	}
	for _, d := range admin.Duties {
		if d.Role == models.RoleGlobalAdmin {
			if err := n.repo.RevokeDuty(d.ID); err != nil {
				t.Fatalf("ukidanje dužnosti: %v", err)
			}
		}
	}

	view, err := n.auth.AuthenticateSessionView(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Viewing && view.Perms.IsGlobalAdmin {
		t.Error("pregled se nastavio s administratorskim ovlastima nakon oduzimanja prava")
	}
}

// vodocuvar vraća djelatnika koji nije administrator — pogled kakav treba
// provjeriti prije nego što dobije pojednostavljeno sučelje
func vodocuvar(t *testing.T, n *cvor) *models.User {
	t.Helper()
	svi, err := n.uslu.ListUsers("", 0, "WATER_GUARD", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := range svi {
		if !svi[i].IsGlobalAdmin {
			return &svi[i]
		}
	}
	t.Fatal("u sjemenu nema nijednog vodočuvara")
	return nil
}
