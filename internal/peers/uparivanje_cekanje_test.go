package peers

import (
	"context"
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
