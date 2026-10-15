package razmjena

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"testing"
	"time"
)

// omotnica0038 je Envelope kakav je u 0.0.38-alfa (c0162ca9), bez polja
// Kod. Zamrznut je i ne mijenja se s Envelopeom: po njemu se vidi što od
// odbijenice pročita čvor 0.0.38.
type omotnica0038 struct {
	Kind     string          `json:"kind"`
	Reason   string          `json:"reason,omitempty"`
	DeviceID string          `json:"deviceId,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

// primiOdbijenicu nazove čvor koji razgovor odbije funkcijom odbij, pošalje
// granicu kao pozivatelj i odbijenicu pročita dekoderom veze (kao Receive)
// u zadano odredište
func primiOdbijenicu(t *testing.T, odbij func(*Conn), odrediste any) {
	t.Helper()
	posluzitelj, klijent := newKey(t), newKey(t)
	ctx, otkazi := context.WithTimeout(context.Background(), 10*time.Second)
	defer otkazi()
	adresa := slusalica(t, ctx, posluzitelj, novaOgrada(4, 4, time.Second), func(ed25519.PublicKey, []byte) bool { return true }, odbij)
	c, err := DialExchange(ctx, klijent, "testproto", nil, adresa, posluzitelj.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	granica, _ := NewEnvelope("frontier", map[string]string{"a": "1"})
	if err := c.Send(granica); err != nil {
		t.Fatal(err)
	}
	c.citac.postavi(c.najvise)
	if err := c.dec.Decode(odrediste); err != nil {
		t.Fatalf("odbijenica nije stigla: %v", err)
	}
}

// Odbijenica nosi kod razloga kao broj iza razloga, bez deviceId i
// payloada. Poruke bez koda ostaju bajt po bajt iste kao u 0.0.38.
func TestOdbijenicaNaZici(t *testing.T) {
	for _, p := range []struct {
		odbij     func(*Conn)
		ocekivano string
	}{
		{odbijZauzet, `{"kind":"odbijeno","reason":"čvor je zauzet, pokušajte kasnije","kod":258}`},
		{func(c *Conn) { Odbij(c, KodOdbijeno, "razmjena na ovim vratima nije dopuštena") },
			`{"kind":"odbijeno","reason":"razmjena na ovim vratima nije dopuštena","kod":259}`},
	} {
		var sirovo json.RawMessage
		primiOdbijenicu(t, p.odbij, &sirovo)
		if string(sirovo) != p.ocekivano {
			t.Errorf("odbijenica na žici:\n%s\nočekivano:\n%s", sirovo, p.ocekivano)
		}
	}
	if b, _ := json.Marshal(Odbijenica(KodUpisZauzet, "r")); string(b) != `{"kind":"odbijeno","reason":"r","kod":262}` {
		t.Errorf("odbijenica: %s", b)
	}
	e, _ := NewEnvelope("done", map[string]int{"applied": 1})
	e.DeviceID = "d"
	if b, _ := json.Marshal(e); string(b) != `{"kind":"done","deviceId":"d","payload":{"applied":1}}` {
		t.Errorf("poruka bez koda: %s", b)
	}
}

// Čvor 0.0.38 odbijenicu s kodom pročita kao i prije: njegov dekoder veze
// (json.NewDecoder bez DisallowUnknownFields) nepoznato polje preskoči, a
// vrsta i razlog ostaju. Obrnuto, odbijenica čvora 0.0.38 stiže novom
// čvoru bez koda (0).
func TestStariCvorZanemarujeKod(t *testing.T) {
	var stara omotnica0038
	primiOdbijenicu(t, odbijZauzet, &stara)
	if stara.Kind != VrstaOdbijeno || stara.Reason != RazlogZauzet || stara.DeviceID != "" || stara.Payload != nil {
		t.Errorf("0.0.38 pročita %+v", stara)
	}

	var nova Envelope
	odbij0038 := func(c *Conn) {
		defer c.Close()
		_, _ = c.Receive()
		_ = c.Send(Envelope{Kind: VrstaOdbijeno, Reason: RazlogZauzet})
	}
	primiOdbijenicu(t, odbij0038, &nova)
	if nova.Kind != VrstaOdbijeno || nova.Reason != RazlogZauzet || nova.Kod != 0 {
		t.Errorf("odbijenica čvora 0.0.38: %+v", nova)
	}
}
