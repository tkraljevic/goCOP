package peers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gocop/internal/razmjena"
)

// cekanjeKojeJavljaPrekid je izmišljeno čekanje uparivanja (s.slusaj) koje
// javi kad je prekinuto
func cekanjeKojeJavljaPrekid(s *Service) <-chan struct{} {
	prekinuto := make(chan struct{}, 1)
	s.slusaj = func(ctx context.Context) (*razmjena.PairResult, error) {
		<-ctx.Done()
		prekinuto <- struct{}{}
		return nil, ctx.Err()
	}
	return prekinuto
}

func cekajPrekid(t *testing.T, prekinuto <-chan struct{}, sto string) {
	t.Helper()
	select {
	case <-prekinuto:
	case <-time.After(30 * time.Second):
		t.Fatalf("%s: čekanje uparivanja nije prekinuto", sto)
	}
}

// Čekanje uparivanja započeto prije osnivanja mreže prestaje kad čvor dobije
// ključ mreže; novo čekanje nakon toga radi
func TestOsnivanjeMrezePrekidaCekanjeUparivanja(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	prekinuto := cekanjeKojeJavljaPrekid(s)
	if err := s.StartListening(); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateNetwork(ctxRub, "Hrvatske vode"); err != nil {
		t.Fatal(err)
	}
	cekajPrekid(t, prekinuto, "osnivanje mreže")
	if st := s.PairStatus(); st.Waiting || st.Pending {
		t.Errorf("nakon osnivanja mreže: %+v", st)
	}
	if err := s.StartListening(); err != nil {
		t.Fatalf("administrator nakon osnivanja pokreće novo čekanje: %v", err)
	}
	s.StopListening()
	cekajPrekid(t, prekinuto, "novo čekanje")
}

// Isto vrijedi kad čvor uđe u mrežu potvrdom s ovlašću za primanje
func TestPotvrdaPrimanjaPrekidaCekanjeUparivanja(t *testing.T) {
	osnivac := rubnaMreza(t, "cop-osijek")
	novi := rubniCvor(t, "cop-vukovar")
	prekinuto := cekanjeKojeJavljaPrekid(novi)
	zahtjev, kod, err := novi.NapraviZahtjev()
	if err != nil {
		t.Fatal(err)
	}
	potvrda, err := osnivac.PrimiZahtjev(ctxRub, zahtjev, kod, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := novi.StartListening(); err != nil {
		t.Fatal(err)
	}
	if _, err := novi.UveziPotvrdu(ctxRub, potvrda); err != nil {
		t.Fatal(err)
	}
	cekajPrekid(t, prekinuto, "potvrda primanja")
	if n := novi.NetworkInfo(); n == nil || !n.CanAdmit || novi.PairStatus().Waiting {
		t.Errorf("nakon potvrde: %+v, %+v", n, novi.PairStatus())
	}
}

// Poziv koji je stigao prije osnivanja mreže i čeka odluku odbija se:
// računalo koje je nazvalo dobije odbijanje, a odluke više nema
func TestOsnivanjeMrezeOdbijaPozivNaCekanju(t *testing.T) {
	a, b := rubniCvor(t, "cop-osijek"), rubniCvor(t, "cop-vukovar")
	port := slobodanPort(t)
	a.slusaj = func(ctx context.Context) (*razmjena.PairResult, error) {
		return razmjena.Listen(ctx, a.node.key, a.identity(), port)
	}
	if err := a.StartListening(); err != nil {
		t.Fatal(err)
	}
	cekajPort(t, port)
	if err := b.DialPair(ctxRub, fmt.Sprintf("127.0.0.1:%d", port)); err != nil {
		t.Fatalf("nazivanje: %v", err)
	}
	kraj := time.Now().Add(30 * time.Second)
	for !a.PairStatus().Pending && time.Now().Before(kraj) {
		time.Sleep(20 * time.Millisecond)
	}
	if !a.PairStatus().Pending {
		t.Fatal("poziv nije stigao na odluku")
	}
	if err := a.CreateNetwork(ctxRub, "Hrvatske vode"); err != nil {
		t.Fatal(err)
	}
	if st := a.PairStatus(); st.Pending || st.Waiting {
		t.Errorf("nakon osnivanja mreže poziv još čeka: %+v", st)
	}
	if _, err := a.ConfirmPair(ctxRub, true, true); err == nil {
		t.Error("odluka o pozivu iz vremena prije ključa mreže još je moguća")
	}
	out, err := b.ConfirmPair(ctxRub, true, false)
	if err != nil {
		out.Message = err.Error()
	}
	if out.Paired || out.Member {
		t.Errorf("računalo koje je nazvalo: %+v", out)
	}
}
