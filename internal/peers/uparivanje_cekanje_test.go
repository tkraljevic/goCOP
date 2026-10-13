package peers

import (
	"context"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"gocop/internal/razmjena"
)

// Čekanje na uparivanje prekine se i odmah pokrene novo, prije nego što je
// prekinuto stiglo završiti. Prekinuto čekanje na kraju ne smije obrisati
// zapis novog: čvor i dalje javlja da čeka, a sljedeći prekid gasi baš novo
// čekanje. Čekanje je izmišljeno (s.slusaj), bez mreže, a synctest.Wait
// čeka da prekinuto čekanje stvarno završi.
func TestNovoCekanjeNakonPrekida(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	synctest.Test(t, func(t *testing.T) {
		pusti := make(chan struct{})
		prekinuto := make(chan struct{}, 2)
		s.slusaj = func(ctx context.Context) (*razmjena.PairResult, error) {
			<-ctx.Done()
			<-pusti // prekinuto čekanje završava tek kad novo već traje
			prekinuto <- struct{}{}
			return nil, ctx.Err()
		}
		if err := s.StartListening(); err != nil {
			t.Fatal(err)
		}
		s.StopListening()
		if err := s.StartListening(); err != nil {
			t.Fatalf("novo čekanje nakon prekida: %v", err)
		}
		close(pusti)
		synctest.Wait()
		if len(prekinuto) != 1 || !s.PairStatus().Waiting {
			t.Errorf("prekinuto čekanje obrisalo je novo: završilo %d, %+v", len(prekinuto), s.PairStatus())
			time.Sleep(time.Hour) // novo čekanje tada ne gasi ništa osim isteka roka
			return
		}
		s.StopListening()
		synctest.Wait()
		if st := s.PairStatus(); len(prekinuto) != 2 || st.Waiting || st.Error != "" {
			t.Errorf("prekid novog čekanja: završilo %d, %+v", len(prekinuto), st)
		}
	})
}

// Prekinuto čekanje baš je dočekalo poziv kad je novo čekanje već počelo.
// Taj se poziv ne nudi na potvrdu, ali računalo koje je nazvalo odmah dobije
// odbijanje i može nazvati ponovno; ne čeka dvije minute da veza istekne.
// Veza je na loopbacku, a izmišljeno čekanje (s.slusaj) drži primljeni poziv
// dok novo čekanje ne počne.
func TestPozivPrekinutogCekanjaDobijeOdbijanje(t *testing.T) {
	a, b := rubniCvor(t, "cop-osijek"), rubniCvor(t, "cop-vukovar")
	port := slobodanPort(t)
	prvo, pusti := make(chan struct{}, 1), make(chan struct{})
	prvo <- struct{}{}
	a.slusaj = func(ctx context.Context) (*razmjena.PairResult, error) {
		select {
		case <-prvo:
			res, err := razmjena.Listen(context.Background(), a.node.key, a.identity(), port)
			<-pusti // poziv je stigao, a čekanje se u međuvremenu prekida i počinje novo
			return res, err
		default:
			<-ctx.Done()
			return nil, ctx.Err()
		}
	}
	if err := a.StartListening(); err != nil {
		t.Fatal(err)
	}
	cekajPort(t, port)
	if err := b.DialPair(ctxRub, fmt.Sprintf("127.0.0.1:%d", port)); err != nil {
		t.Fatalf("nazivanje: %v", err)
	}
	a.StopListening()
	if err := a.StartListening(); err != nil {
		t.Fatalf("novo čekanje nakon prekida: %v", err)
	}
	t.Cleanup(a.StopListening)
	close(pusti)

	gotovo := make(chan PairOutcome, 1)
	go func() {
		out, err := b.ConfirmPair(ctxRub, true, false)
		if err != nil {
			out.Message = err.Error()
		}
		gotovo <- out
	}()
	select {
	case out := <-gotovo:
		if out.Paired || out.Message != "Uparivanje odbijeno ili nepotvrđeno s druge strane." {
			t.Errorf("računalo koje je nazvalo: %+v", out)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("računalo koje je nazvalo nije dobilo odbijanje")
	}
	if st := a.PairStatus(); !st.Waiting || st.Pending {
		t.Errorf("novo čekanje: %+v", st)
	}
}
