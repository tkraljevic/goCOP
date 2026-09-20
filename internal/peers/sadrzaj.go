package peers

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"gocop/internal/sadrzaj"
)

// Sadržaj putuje svojim putem, nakon verzija: čvor u razmjeni kaže koje
// otiske želi, druga strana pošalje ono što od toga ima, a primatelj upiše
// tek što prođe provjeru otiska. Što čvor želi određuje pretplata (razina
// sadržaja po kanalu), a ne pošiljatelj.

// najviseBajtovaPoRazmjeni ograđuje jednu razmjenu; ostatak dođe sljedećom
const najviseBajtovaPoRazmjeni = 48 << 20

// najviseSadrzajaPoRazmjeni ograđuje broj stavki po razmjeni
const najviseSadrzajaPoRazmjeni = 200

const (
	kindZelje   = "zelje"   // otisci koje čvor traži
	kindSadrzaj = "sadrzaj" // sadržaji koje čvor daje
)

type zeljeMsg struct {
	Otisci []string `json:"otisci"`
}

type stavkaSadrzaja struct {
	Otisak  string `json:"otisak"`
	Vrsta   string `json:"vrsta"`
	Kanal   string `json:"kanal,omitempty"`
	Bajtova int    `json:"bajtova"`
	Podaci  []byte `json:"podaci"`
}

type sadrzajMsg struct {
	Stavke []stavkaSadrzaja `json:"stavke"`
}

// SetSpremiste daje razmjeni spremište sadržaja; bez njega se sadržaj ne
// razmjenjuje, samo verzije
func (s *Service) SetSpremiste(sp *sadrzaj.Spremiste) {
	s.mu.Lock()
	s.spremiste = sp
	s.mu.Unlock()
}

func (s *Service) spremisteSadrzaja() *sadrzaj.Spremiste {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.spremiste
}

// zeljeniOtisci slaže što ovaj čvor traži od drugoga: željeni sadržaji koje
// pretplata pokriva na svojoj razini, do ograde po razmjeni
func (s *Service) zeljeniOtisci(ctx context.Context, w Wants) []string {
	sp := s.spremisteSadrzaja()
	if sp == nil {
		return nil
	}
	zelje, err := sp.Zeljeni(ctx, najviseSadrzajaPoRazmjeni)
	if err != nil {
		return nil
	}
	sectorOf := s.sectorLookup(ctx)
	var out []string
	var ukupno int
	for _, z := range zelje {
		if !w.zeliSadrzaj(z.Kanal, z.Vrsta, sectorOf) {
			continue
		}
		if ukupno+z.Bajtova > najviseBajtovaPoRazmjeni && len(out) > 0 {
			break
		}
		ukupno += z.Bajtova
		out = append(out, z.Otisak)
		_ = sp.Trazeno(ctx, z.Otisak)
	}
	return out
}

// sadrzajZa slaže odgovor: od traženih otisaka ono što ovaj čvor drži i
// što druga strana po svojim pretplatama smije dobiti
func (s *Service) sadrzajZa(ctx context.Context, otisci []string, njihovi *Wants) []stavkaSadrzaja {
	sp := s.spremisteSadrzaja()
	if sp == nil || len(otisci) == 0 {
		return nil
	}
	smije := s.wantsFunc(ctx, njihovi)
	var out []stavkaSadrzaja
	var ukupno int
	for _, o := range otisci {
		if len(out) >= najviseSadrzajaPoRazmjeni {
			break
		}
		b, vrsta, kanal, err := sp.CitajSKanalom(ctx, o)
		if err != nil {
			continue
		}
		if smije != nil && kanal != "" && !smije(kanal) {
			continue
		}
		if ukupno+len(b) > najviseBajtovaPoRazmjeni && len(out) > 0 {
			break
		}
		ukupno += len(b)
		out = append(out, stavkaSadrzaja{Otisak: o, Vrsta: vrsta, Kanal: kanal, Bajtova: len(b), Podaci: b})
	}
	return out
}

// primiSadrzaj upisuje primljene sadržaje; što ne prođe otisak, ne ulazi
func (s *Service) primiSadrzaj(ctx context.Context, od string, stavke []stavkaSadrzaja) int {
	sp := s.spremisteSadrzaja()
	if sp == nil {
		return 0
	}
	n := 0
	for _, st := range stavke {
		if err := sp.UpisiProvjereno(ctx, st.Otisak, st.Vrsta, st.Podaci, "cvor:"+od, sadrzaj.Veza{Kanal: st.Kanal}); err != nil {
			log.Printf("sinkronizacija: sadržaj %.12s od %s odbijen: %v", st.Otisak, od, err)
			continue
		}
		n++
	}
	return n
}

// OtpustiStare miče s računala primljene sadržaje kojima je po pretplati
// istekao rok držanja; zapisi ostaju, sadržaj se može opet dohvatiti.
// Vlastiti sadržaj se ne dira. Vraća koliko je otpušteno i bajtova.
func (s *Service) OtpustiStare(ctx context.Context) (int, int64, error) {
	sp := s.spremisteSadrzaja()
	if sp == nil {
		return 0, 0, nil
	}
	w, err := s.CurrentWants(ctx)
	if err != nil || w.All {
		return 0, 0, err
	}
	sectorOf := s.sectorLookup(ctx)
	// najkraći rok među pravilima određuje najdalji datum koji nas zanima
	najkraci := 0
	for _, r := range w.Rules {
		if r.DrziDana > 0 && (najkraci == 0 || r.DrziDana < najkraci) {
			najkraci = r.DrziDana
		}
	}
	if najkraci == 0 {
		return 0, 0, nil
	}
	sad := time.Now()
	primljeni, err := sp.PrimljeniPrije(ctx, sad.AddDate(0, 0, -najkraci))
	if err != nil {
		return 0, 0, err
	}
	var n int
	var bajtova int64
	for _, p := range primljeni {
		dana := w.DrziDanaZa(p.Kanal, sectorOf)
		if dana == 0 || p.Primljeno.After(sad.AddDate(0, 0, -dana)) {
			continue
		}
		if err := sp.Otpusti(ctx, p.Otisak); err != nil {
			continue
		}
		n++
		bajtova += int64(p.Bajtova)
	}
	return n, bajtova, nil
}

// opisSadrzaja je za dnevnik razmjene; strings ostaje za razinu pregleda
var _ = strings.HasPrefix

// opisSadrzaja je za dnevnik razmjene
func opisSadrzaja(stavke []stavkaSadrzaja) string {
	var b int
	for _, s := range stavke {
		b += s.Bajtova
	}
	return fmt.Sprintf("%d sadržaja, %.1f MB", len(stavke), float64(b)/1e6)
}
