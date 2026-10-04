package razmjena

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func kljuc(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// Lanac ključ mreže → ovlast primatelja → članstvo vrijedi bez pitanja
// ikoga; svaka karika koja ne valja ruši cijeli lanac
func TestLanacOvlastiPrimatelja(t *testing.T) {
	mreza, err := NewNetwork("Probna mreža")
	if err != nil {
		t.Fatal(err)
	}
	ured := kljuc(t)
	uredJavni := ured.Public().(ed25519.PublicKey)
	o, err := mreza.Ovlasti("ured-posluzitelj", uredJavni, "cop-laptop", 365*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	novi := kljuc(t)
	noviJavni := novi.Public().(ed25519.PublicKey)
	m, err := AdmitAs(ured, o, "pperic-thinkpad", noviJavni, 365*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sad := time.Now()
	if err := m.Verify(mreza.Public, noviJavni, sad); err != nil {
		t.Fatalf("ispravan lanac odbijen: %v", err)
	}
	if !m.ExpiresAt.Equal(o.ExpiresAt) && m.ExpiresAt.After(o.ExpiresAt) {
		t.Error("članstvo vrijedi dulje od ovlasti")
	}

	// tuđi ključ ne može potpisati kao primatelj
	if _, err := AdmitAs(kljuc(t), o, "x", noviJavni, time.Hour); err == nil {
		t.Error("AdmitAs s tuđim ključem")
	}
	// podmetnuta ovlast iz druge mreže
	druga, _ := NewNetwork("Druga")
	o2, _ := druga.Ovlasti("ured-posluzitelj", uredJavni, "x", time.Hour)
	m2, _ := AdmitAs(ured, o2, "pperic-thinkpad", noviJavni, time.Hour)
	if err := m2.Verify(mreza.Public, noviJavni, sad); err == nil {
		t.Error("članstvo s ovlasti druge mreže prošlo")
	}
	// zamijenjena ovlast (druga ovlast istog primatelja) ruši potpis
	o3, _ := mreza.Ovlasti("ured-posluzitelj", uredJavni, "cop-laptop", 2*time.Hour)
	zamijenjeno := m
	zamijenjeno.Primatelj = &o3
	if err := zamijenjeno.Verify(mreza.Public, noviJavni, sad); err == nil {
		t.Error("članstvo sa zamijenjenom ovlasti prošlo")
	}
	// izmijenjeno ime člana
	izmijenjeno := m
	izmijenjeno.DeviceID = "uljez"
	if err := izmijenjeno.Verify(mreza.Public, noviJavni, sad); err == nil {
		t.Error("izmijenjeno članstvo prošlo")
	}
	// izdavač u članstvu ne odgovara ovlasti
	krivIzdavac := m
	krivIzdavac.IssuedBy = "netko-drugi"
	if err := krivIzdavac.Verify(mreza.Public, noviJavni, sad); err == nil {
		t.Error("članstvo s krivim izdavačem prošlo")
	}
	// ovlast koja je istekla prije izdavanja članstva
	istekla := o
	istekla.IssuedAt, istekla.ExpiresAt = sad.Add(-2*time.Hour), sad.Add(-time.Hour)
	if _, err := AdmitAs(ured, istekla, "a", noviJavni, time.Hour); err == nil {
		t.Error("primanje s isteklom ovlasti")
	}
	// članstvo potpisano ključem mreže i dalje vrijedi (v1)
	stari, _ := mreza.Admit("stari", noviJavni, "cop-laptop", time.Hour)
	if err := stari.Verify(mreza.Public, noviJavni, sad); err != nil {
		t.Errorf("članstvo ključa mreže: %v", err)
	}
	// samo nositelj ključa mreže daje ovlast
	if _, err := PublicNetwork("x", mreza.Public).Ovlasti("a", noviJavni, "b", time.Hour); err == nil {
		t.Error("ovlast bez ključa mreže")
	}
}

// Vjerodajnice putuju u certifikatu i stariji dio certifikata ostaje isti
func TestVjerodajniceUCertifikatu(t *testing.T) {
	k := kljuc(t)
	vj := []byte(`{"clanstvo":{"deviceId":"pperic-thinkpad"}}`)
	c, err := selfSignedCert(k, "testproto", vj)
	if err != nil {
		t.Fatal(err)
	}
	if got := peerVjerodajnice(c.Certificate); !bytes.Equal(got, vj) {
		t.Fatalf("vjerodajnice: %q", got)
	}
	if pub, err := peerPublicKey(c.Certificate); err != nil || !pub.Equal(k.Public().(ed25519.PublicKey)) {
		t.Fatal("javni ključ iz certifikata s vjerodajnicama")
	}
	bez, _ := selfSignedCert(k, "testproto")
	if got := peerVjerodajnice(bez.Certificate); got != nil {
		t.Errorf("certifikat bez vjerodajnica: %q", got)
	}
}
