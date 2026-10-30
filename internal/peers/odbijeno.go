package peers

// Zapis odbijenih i djelomično razumljenih poruka razmjene (G7, plan WEB
// synca): koliko je čega bilo po danu, vrsti, putu, razlogu i računalu, bez
// sadržaja zapisa i bez adresa, u lokalnoj tablici razmjena_odbijeno, 30
// dana. Detalj je na stranici Sinkronizacija (Odbijeno), a na pločicu
// razmjene idu samo kvarovi koji se sami ne poprave (KvaroviRazmjene).

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"log"
	"slices"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// Vrste zapisa odbijenog
const (
	// VrstaVrata: vrata razmjene nisu pustila vezu (razmjena.RazlogVrata)
	VrstaVrata = "vrata"
	// VrstaOdbiliSmo: ovo računalo je odbilo razmjenu s drugim
	VrstaOdbiliSmo = "odbili-smo"
	// VrstaOdbiliSu: drugo računalo je odbilo razmjenu s ovim
	VrstaOdbiliSu = "odbili-su"
	// VrstaDjelomicno: stiglo je nešto što ovaj program ne razumije
	VrstaDjelomicno = "djelomicno"
)

// Putovi vrata u zapisu
const (
	PutVrataPort  = "port"
	PutVrataTunel = "tunel"
	PutVrataWeb   = "web" // QUIC WEB synca (F3b)
)

// Razlozi izvan razmjena.RazlogVrata: odbijenica po kodu, oznaka mogućnosti
// i zapis koji program ne razumije. Uz zadnja tri ide i ime.
const (
	RazlogUpisZauzet        = "upis-zauzet"
	RazlogBezTailscalea     = "bez-tailscalea"
	RazlogOznaka            = "oznaka"
	RazlogNepoznatZapis     = "nepoznat-zapis"
	RazlogNovijaShema       = "novija-shema"
	cuvajOdbijenoDana       = 30
	kvaroviOdbijenogUnatrag = 24 * time.Hour
)

// ZapisOdbijenog je jedan redak zapisa: zbroj jednog dana
type ZapisOdbijenog struct {
	Dan, Vrsta, Put, Razlog, Racunalo string
	Broj                              int
	Zadnje                            time.Time
}

// zabiljeziOdbijeno doda jedan događaj u zbroj dana i jednom na dan
// obriše retke starije od 30 dana. Zapis je pomoćni, pa greška baze ide
// samo u dnevnik.
func (s *Service) zabiljeziOdbijeno(ctx context.Context, vrsta, put, razlog, racunalo string) {
	sada := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO razmjena_odbijeno (dan, vrsta, put, razlog, racunalo, broj, zadnje)
		VALUES (?, ?, ?, ?, ?, 1, ?)
		ON CONFLICT (dan, vrsta, put, razlog, racunalo) DO UPDATE SET broj = broj + 1, zadnje = excluded.zadnje`,
		sada.Format(time.DateOnly), vrsta, put, razlog, racunalo, sada); err != nil {
		log.Printf("razmjena: zapis odbijenog nije spremljen: %v", err)
		return
	}
	s.ocistiOdbijeno(ctx, sada)
}

// ocistiOdbijeno briše retke starije od 30 dana, najviše jednom na dan
func (s *Service) ocistiOdbijeno(ctx context.Context, sada time.Time) {
	dan := sada.Format(time.DateOnly)
	if prije, _ := s.odbijenoOcisceno.Swap(dan).(string); prije == dan {
		return
	}
	granica := sada.AddDate(0, 0, -cuvajOdbijenoDana).Format(time.DateOnly)
	if _, err := s.db.ExecContext(ctx, `DELETE FROM razmjena_odbijeno WHERE dan < ?`, granica); err != nil {
		log.Printf("razmjena: stari zapis odbijenog nije obrisan: %v", err)
	}
}

// naVratima je ono što ograda vrata radi kad ne pusti vezu: zapiše razlog
// i računalo s tim ključem, kad ga ovaj čvor zna
func (s *Service) naVratima(put string) func(razmjena.RazlogVrata, ed25519.PublicKey) {
	return func(r razmjena.RazlogVrata, kljuc ed25519.PublicKey) {
		s.zabiljeziOdbijeno(context.Background(), VrstaVrata, put, string(r), s.imeKljuca(kljuc))
	}
}

// imeKljuca je oznaka računala koje ovaj čvor zna pod tim ključem; "" za
// nepoznat ključ ili kad ga nema
func (s *Service) imeKljuca(kljuc ed25519.PublicKey) string {
	if kljuc == nil {
		return ""
	}
	ime, _ := imeSKljucem(s.db, razmjena.PublicKeyString(kljuc)) // nepoznat ključ ostaje bez imena
	return ime
}

// odbiliSmo zapiše razmjenu koju je ovo računalo odbilo
func (s *Service) odbiliSmo(razlog string, kljuc ed25519.PublicKey) {
	s.zabiljeziOdbijeno(context.Background(), VrstaOdbiliSmo, "", razlog, s.imeKljuca(kljuc))
}

// zabiljeziIshod upisuje ono što je razmjena s računalom rekla: odbijenicu
// druge strane i oznake mogućnosti koje ovaj program ne zna
func (s *Service) zabiljeziIshod(ctx context.Context, peer *Peer, o syncOutcome) {
	var odbijena odbijenaRazmjena
	if errors.As(o.err, &odbijena) {
		s.zabiljeziOdbijeno(ctx, VrstaOdbiliSu, "", razlogKoda(odbijena.kod), peer.NodeID)
	}
	for _, oznaka := range s.oznakeZaZapis(ctx, peer.NodeID, razmjena.NepoznateOznake(o.znanje.Ocisceno().Mogucnosti)) {
		s.zabiljeziOdbijeno(ctx, VrstaDjelomicno, "", RazlogOznaka+" "+oznaka, peer.NodeID)
	}
}

// najviseOznakaDnevno je koliko različitih nepoznatih oznaka jedno računalo
// dobije u zapisu odbijenog u jednom danu
const najviseOznakaDnevno = 32

// oznakeZaZapis su od nepoznatih oznaka one koje idu u zapis odbijenog:
// već zapisane danas i nove dok računalo danas nema najviseOznakaDnevno
// redaka oznaka. Kad se zapis ne da pročitati, ne ide nijedna.
func (s *Service) oznakeZaZapis(ctx context.Context, racunalo string, oznake []string) []string {
	if len(oznake) == 0 {
		return nil
	}
	zapisane, err := s.zapisaneOznake(ctx, racunalo)
	if err != nil {
		return nil
	}
	var out []string
	mjesta := najviseOznakaDnevno - len(zapisane)
	for _, o := range oznake {
		switch {
		case zapisane[RazlogOznaka+" "+o]:
			out = append(out, o)
		case mjesta > 0:
			out = append(out, o)
			mjesta--
		}
	}
	return out
}

// zapisaneOznake su razlozi oznaka koje računalo danas već ima u zapisu
func (s *Service) zapisaneOznake(ctx context.Context, racunalo string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT razlog FROM razmjena_odbijeno WHERE dan = ? AND vrsta = ? AND racunalo = ? AND razlog LIKE ?`,
		time.Now().UTC().Format(time.DateOnly), VrstaDjelomicno, racunalo, RazlogOznaka+" %")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var r string
		if rows.Scan(&r) == nil { // razlog je uvijek tekst
			out[r] = true
		}
	}
	return out, rows.Err()
}

// razlogKoda je razlog odbijenice u zapisu; nepoznat kod ostaje broj
func razlogKoda(kod razmjena.KodRazloga) string {
	switch kod {
	case razmjena.KodZauzet:
		return string(razmjena.VrataZauzeto)
	case razmjena.KodNijeClan:
		return string(razmjena.VrataNijeClan)
	case razmjena.KodUpisZauzet:
		return RazlogUpisZauzet
	case razmjena.KodBezTailscalea:
		return RazlogBezTailscalea
	}
	return fmt.Sprintf("kod 0x%04x", uint64(kod))
}

// PratiNovosti: ono što ovaj program u knjizi ne razumije (nepoznat zapis,
// novija shema) ide u dnevnik i u zapis odbijenog
func (s *Service) PratiNovosti(r *ledger.Recorder) { r.NaNovost(s.novost) }

func (s *Service) novost(n ledger.Novost) {
	log.Printf("razmjena: %s — nadogradite goCOP", n)
	razlog := RazlogNovijaShema
	if n.Nepoznat {
		razlog = RazlogNepoznatZapis
	}
	s.zabiljeziOdbijeno(context.Background(), VrstaDjelomicno, "", razlog+" "+n.Entity, "")
}

// Odbijeno je zapis zadnjih dana, od najnovijeg dana, u danu od najčešćeg
func (s *Service) Odbijeno(ctx context.Context, dana int) ([]ZapisOdbijenog, error) {
	od := time.Now().UTC().AddDate(0, 0, 1-dana).Format(time.DateOnly)
	rows, err := s.db.QueryContext(ctx, `SELECT dan, vrsta, put, razlog, racunalo, broj, zadnje
		FROM razmjena_odbijeno WHERE dan >= ? ORDER BY dan DESC, broj DESC, vrsta, razlog, racunalo`, od)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ZapisOdbijenog
	for rows.Next() {
		var z ZapisOdbijenog
		if err := rows.Scan(&z.Dan, &z.Vrsta, &z.Put, &z.Razlog, &z.Racunalo, &z.Broj, &z.Zadnje); err != nil {
			return nil, err
		}
		out = append(out, z)
	}
	return out, rows.Err()
}

// KvaroviRazmjene su rečenice za pločicu razmjene: kvarovi koji se sami ne
// poprave. Zasad jedan: poznato računalo u zadnja 24 sata zove ovo, a vrata
// ga ne puste jer ovdje nije član mreže.
func (s *Service) KvaroviRazmjene(ctx context.Context) []string {
	zapisi, err := s.Odbijeno(ctx, 2)
	if err != nil {
		return []string{"Zapis odbijenih razmjena se ne da pročitati: " + err.Error()}
	}
	granica := time.Now().UTC().Add(-kvaroviOdbijenogUnatrag)
	var ids []string
	for _, z := range zapisi {
		if kvarNijeClan(z, granica) && !slices.Contains(ids, z.Racunalo) {
			ids = append(ids, z.Racunalo)
		}
	}
	slices.Sort(ids)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.imeRacunala(ctx, id)+" zove ovo računalo, ali ovdje nije član mreže (članstvo je isteklo ili je opozvano), pa razmjena s njim ne ide. Javite administratoru.")
	}
	return out
}

// kvarNijeClan: vrata nakon granice nisu pustila poznato računalo jer nije član
func kvarNijeClan(z ZapisOdbijenog, granica time.Time) bool {
	return z.Vrsta == VrstaVrata && z.Razlog == string(razmjena.VrataNijeClan) && z.Racunalo != "" && !z.Zadnje.Before(granica)
}

// imeRacunala je ime poznatog računala za pločicu, inače njegova oznaka
func (s *Service) imeRacunala(ctx context.Context, id string) string {
	if p, err := s.GetPeer(ctx, id); err == nil && p != nil {
		return label(*p)
	}
	return id
}
