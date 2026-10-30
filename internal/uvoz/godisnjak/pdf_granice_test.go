package godisnjak

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// postavi mijenja granicu za jedan test i vraća je poslije.
func postavi[T any](t *testing.T, granica *T, v T) {
	t.Helper()
	staro := *granica
	*granica = v
	t.Cleanup(func() { *granica = staro })
}

// Čitanje koje traje dulje od roka prekida se i pdftotext se gasi.
func TestTekstPDFRok(t *testing.T) {
	lazniPdftotext(t)
	postavi(t, &rokPDF, 200*time.Millisecond)
	pocetak := time.Now()
	_, err := TekstPDF(context.Background(), godisnjakNaDisku(t, "sporo.pdf", "x"))
	if err == nil || !strings.Contains(err.Error(), "traje dulje od") {
		t.Errorf("sporo čitanje: %v", err)
	}
	if trajalo := time.Since(pocetak); trajalo > 4*time.Second {
		t.Errorf("prekid je trajao %v", trajalo)
	}
}

// Prekinut zahtjev prekida i čitanje.
func TestTekstPDFPrekid(t *testing.T) {
	lazniPdftotext(t)
	ctx, prekini := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer prekini()
	_, err := TekstPDF(ctx, godisnjakNaDisku(t, "sporo.pdf", "x"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("prekinut zahtjev: %v", err)
	}
}

// Tekst veći od granice i PDF s previše stranica ne čitaju se kao godišnjak;
// godišnjak ispod obje granice čita se cijeli.
func TestTekstPDFGranice(t *testing.T) {
	lazniPdftotext(t)
	postavi(t, &najviseTeksta, 64)
	postavi(t, &najviseStranica, 3)
	if _, err := TekstPDF(context.Background(), godisnjakNaDisku(t, "velik.pdf", strings.Repeat("x", 65))); err == nil || !strings.Contains(err.Error(), "veći od") {
		t.Errorf("prevelik tekst: %v", err)
	}
	if _, err := TekstPDF(context.Background(), godisnjakNaDisku(t, "dug.pdf", "a\fb\fc\f")); err == nil || !strings.Contains(err.Error(), "3 ili više stranica") {
		t.Errorf("previše stranica: %v", err)
	}
	tekst, err := TekstPDF(context.Background(), godisnjakNaDisku(t, "1991.pdf", "a\fb\f"+strings.Repeat("y", 60)))
	if err != nil || len(tekst) != 64 {
		t.Errorf("godišnjak unutar granica: %d znakova, %v", len(tekst), err)
	}
}

// Čitanje koje čeka red odustaje kad se zahtjev prekine.
func TestTekstPDFCekaRed(t *testing.T) {
	lazniPdftotext(t)
	for range cap(citaciPDF) {
		citaciPDF <- struct{}{}
	}
	defer func() {
		for range cap(citaciPDF) {
			<-citaciPDF
		}
	}()
	ctx, prekini := context.WithCancel(context.Background())
	prekini()
	if _, err := TekstPDF(ctx, godisnjakNaDisku(t, "1991.pdf", "x")); !errors.Is(err, context.Canceled) {
		t.Errorf("čekanje reda: %v", err)
	}
}

// Zadatak bez konteksta čita bez prekida, samo uz rok; zadani kontekst se
// koristi kakav jest.
func TestKontekstZadatka(t *testing.T) {
	if (Zadatak{}).kontekst() != context.Background() {
		t.Error("zadatak bez konteksta nema pozadinski kontekst")
	}
	ctx, prekini := context.WithCancel(context.Background())
	defer prekini()
	if (Zadatak{Ctx: ctx}).kontekst() != ctx {
		t.Error("zadani kontekst nije upotrijebljen")
	}
}
