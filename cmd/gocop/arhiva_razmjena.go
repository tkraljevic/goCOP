package main

// Arhiva vodostaja putuje razmjenom kao .cop paketi letvi. Čvor koji izdaje
// arhivu (Uvozi → Izdavanje) za svaki paket iz kataloga zapiše u knjigu
// kazalo — letva, izdanje, otisak, razdoblje, veličina — a sam paket stavi u
// spremište sadržaja. Kazalo drže svi čvorovi; paket dohvaća samo čvor
// kojemu ga pretplata pokriva (vrsta „arhiva”, po području letve, ili čvor
// koji prati sve), i to od bilo kojeg čvora koji ga već ima. Kad paket stigne
// i prođe provjeru otiska, ugradi se kao i paket učitan ručno.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/ledger"
	"gocop/internal/peers"
	"gocop/internal/sadrzaj"
	"gocop/internal/web"
)

// EntitetArhive je kazalo jednog paketa u knjizi; ključ je letva
const EntitetArhive = "arhiva_paket"

// VrstaPaketa je vrsta .cop paketa u spremištu sadržaja
const VrstaPaketa = "application/vnd.gocop.arhiva"

// paketUKnjizi je kazalo paketa kako stoji u knjizi
type paketUKnjizi struct {
	Letva   string `json:"letva"`
	Izdanje int    `json:"izdanje"`
	Otisak  string `json:"otisak"`  // otisak podataka, kao u manifestu
	Sadrzaj string `json:"sadrzaj"` // SHA-256 datoteke, ime u spremištu
	Bajtova int64  `json:"bajtova"`
	Nizova  int    `json:"nizova"`
	Zapisa  int    `json:"zapisa"`
	Od      string `json:"od"`
	Do      string `json:"do"`
	Izdao   string `json:"izdao"`
	Kanal   string `json:"kanal"` // arhiva/područje/0
}

type razmjenaArhive struct {
	baza      *sql.DB
	rec       *ledger.Recorder
	sp        *sadrzaj.Spremiste
	ugradi    func(*arhiva.Sadrzaj) error                         // ugradnja i ponovno otvaranje arhive (Server.UgradiPaket)
	zeli      func(ctx context.Context, kanal, vrsta string) bool // pokriva li pretplata sadržaj
	arhivaPut string
	paketiDir string

	mu       sync.Mutex
	javljeno map[string]bool // što je jednom zapisano u dnevnik, da se ne ponavlja svaki krug
	potakni  chan struct{}
}

func novaRazmjenaArhive(baza *sql.DB, rec *ledger.Recorder, sp *sadrzaj.Spremiste, srv *web.Server, p *peers.Service, arhivaPut, paketiDir string) *razmjenaArhive {
	return &razmjenaArhive{baza: baza, rec: rec, sp: sp, ugradi: srv.UgradiPaket, zeli: p.ZeliSadrzaj, arhivaPut: arhivaPut, paketiDir: paketiDir,
		javljeno: map[string]bool{}, potakni: make(chan struct{}, 1)}
}

// kanalLetve je kanal sadržaja paketa: područje prve dionice uz letvu, a
// strana i meteorološka letva su područje 0
func kanalLetve(ctx context.Context, baza *sql.DB, letva string) string {
	var area int
	_ = baza.QueryRowContext(ctx, `SELECT s.area_id FROM stations st
		JOIN section_stations ss ON ss.station_id = st.id
		JOIN sections s ON s.code = ss.section_code
		WHERE st.code = ? ORDER BY s.code LIMIT 1`, letva).Scan(&area)
	return fmt.Sprintf("%s/%d/0", ledger.ChannelArhiva, area)
}

// objavi zapisuje u knjigu kazalo svakog paketa iz kataloga ovog čvora
// kojega knjiga još nema u tom izdanju. Paket ide u spremište sadržaja.
func (r *razmjenaArhive) objavi(ctx context.Context) (int, error) {
	if r.paketiDir == "" || r.sp == nil {
		return 0, nil
	}
	k, err := arhiva.UcitajKatalog(r.paketiDir)
	if err != nil {
		return 0, err
	}
	// Objavljuje samo onaj tko je katalog izdao: mapa paketa prenesena s
	// drugog računala nije ovog čvora.
	if k.Izdao != "" && k.Izdao != r.rec.Cvor() {
		return 0, nil
	}
	n := 0
	for _, p := range k.Paketi {
		if v, err := r.rec.Latest(ctx, EntitetArhive, p.Letva); err == nil {
			var bio paketUKnjizi
			if json.Unmarshal(v.Payload, &bio) == nil && bio.Izdanje == p.Izdanje && bio.Otisak == p.Otisak {
				continue
			}
		} else if !errors.Is(err, ledger.ErrNoVersion) {
			return n, err
		}
		b, err := os.ReadFile(filepath.Join(r.paketiDir, p.Datoteka))
		if err != nil {
			log.Printf("arhiva: paket %s nije pročitan: %v", p.Datoteka, err)
			continue
		}
		kanal := kanalLetve(ctx, r.baza, p.Letva)
		otisak, err := r.sp.Upisi(ctx, VrstaPaketa, b, "ovdje",
			sadrzaj.Veza{Entitet: EntitetArhive, EntitetID: p.Letva, Uloga: "paket", Kanal: kanal})
		if err != nil {
			return n, err
		}
		if _, err := r.rec.Record(ctx, r.baza, EntitetArhive, p.Letva, paketUKnjizi{
			Letva: p.Letva, Izdanje: p.Izdanje, Otisak: p.Otisak, Sadrzaj: otisak, Bajtova: int64(len(b)),
			Nizova: p.Nizova, Zapisa: p.Zapisa, Od: p.Od, Do: p.Do, Izdao: r.rec.Cvor(), Kanal: kanal,
		}); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// primi prolazi kazalo u knjizi: što ovaj čvor prati, a nema u tom izdanju,
// zatraži od drugih čvorova; što je stiglo, ugradi.
func (r *razmjenaArhive) primi(ctx context.Context) {
	if r.sp == nil || r.arhivaPut == "" {
		return
	}
	verzije, err := r.rec.LatestOf(ctx, []string{EntitetArhive})
	if err != nil {
		log.Printf("arhiva: kazalo: %v", err)
		return
	}
	var ro *sql.DB
	if _, err := os.Stat(r.arhivaPut); err == nil {
		if ro, err = sql.Open("sqlite", r.arhivaPut+"?mode=ro"); err == nil {
			defer ro.Close()
		}
	}
	trazeno, ugradjeno := 0, 0
	for _, v := range verzije {
		if v.NodeID == r.rec.Cvor() || v.Archived {
			continue
		}
		var p paketUKnjizi
		if err := json.Unmarshal(v.Payload, &p); err != nil || p.Letva == "" || p.Sadrzaj == "" {
			continue
		}
		if ro != nil {
			imamo, _ := arhiva.Primljeno(ro, p.Letva)
			if imamo != nil && imamo.Izdanje >= p.Izdanje {
				continue
			}
			if imamo == nil && izgradjenaOvdje(ro, p.Letva) {
				// Letvu je ovaj čvor sagradio iz svog stabla; paket je ne gazi.
				r.javiJednom("ovdje:"+p.Letva, "arhiva: letva %s sagrađena je na ovom čvoru, paket v%d s čvora %s se ne ugrađuje", p.Letva, p.Izdanje, p.Izdao)
				continue
			}
		}
		if !r.zeli(ctx, p.Kanal, VrstaPaketa) {
			continue
		}
		if !r.sp.Ima(ctx, p.Sadrzaj) {
			if err := r.sp.Zeli(ctx, p.Sadrzaj, VrstaPaketa, int(p.Bajtova), "arhiva", p.Kanal); err == nil {
				trazeno++
			}
			continue
		}
		kljuc := fmt.Sprintf("%s|%d", p.Letva, p.Izdanje)
		if r.javljeno["neuspjelo:"+kljuc] {
			continue
		}
		b, _, err := r.sp.Citaj(ctx, p.Sadrzaj)
		if err != nil {
			continue
		}
		s, err := arhiva.Procitaj(bytes.NewReader(b), int64(len(b)))
		if err == nil && (s.Manifest.Letva != p.Letva || s.Manifest.Izdanje != p.Izdanje || s.Manifest.Otisak != p.Otisak) {
			err = fmt.Errorf("paket je %s v%d, a kazalo kaže %s v%d", s.Manifest.Letva, s.Manifest.Izdanje, p.Letva, p.Izdanje)
		}
		if err == nil {
			err = r.ugradi(s)
		}
		if err != nil {
			r.javiJednom("neuspjelo:"+kljuc, "arhiva: paket %s v%d nije ugrađen: %v", p.Letva, p.Izdanje, err)
			continue
		}
		ugradjeno++
		if ro == nil {
			if ro, err = sql.Open("sqlite", r.arhivaPut+"?mode=ro"); err == nil {
				defer ro.Close()
			}
		}
	}
	if trazeno > 0 {
		log.Printf("arhiva: zatraženo %d paketa od drugih čvorova", trazeno)
	}
	if ugradjeno > 0 {
		log.Printf("arhiva: ugrađeno %d paketa primljenih razmjenom", ugradjeno)
	}
}

// izgradjenaOvdje javlja ima li arhiva letvu koju nije primila paketom
func izgradjenaOvdje(ro *sql.DB, letva string) bool {
	var n int
	_ = ro.QueryRow(`SELECT count(*) FROM nizovi WHERE letva = ?`, letva).Scan(&n)
	return n > 0
}

func (r *razmjenaArhive) javiJednom(kljuc, format string, args ...any) {
	if r.javljeno[kljuc] {
		return
	}
	r.javljeno[kljuc] = true
	log.Printf(format, args...)
}

// primljeno javlja da su razmjenom stigle verzije; kazalo arhive pokreće krug
func (r *razmjenaArhive) primljeno(verzije []ledger.Version) {
	for _, v := range verzije {
		if v.Entity == EntitetArhive {
			select {
			case r.potakni <- struct{}{}:
			default:
			}
			return
		}
	}
}

// vrti objavljuje i prima: odmah, na poticaj i svake dvije minute (sadržaj
// stiže u krugovima razmjene, do 48 MB po krugu).
func (r *razmjenaArhive) vrti(ctx context.Context) {
	t := time.NewTicker(2 * time.Minute)
	defer t.Stop()
	for {
		r.mu.Lock()
		if n, err := r.objavi(ctx); err != nil {
			log.Printf("arhiva: objava kazala: %v", err)
		} else if n > 0 {
			log.Printf("arhiva: u kazalo za razmjenu upisano %d paketa", n)
		}
		r.primi(ctx)
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-r.potakni:
		}
	}
}

// imaStablo javlja drži li čvor stablo izvornih datoteka arhive: barem jednu
// mapu sliva s letvama. Čvor koji je arhivu dobio paketima stablo nema.
func imaStablo(koren string) bool {
	slivovi, err := os.ReadDir(koren)
	if err != nil {
		return false
	}
	for _, s := range slivovi {
		if !s.IsDir() {
			continue
		}
		if letve, err := os.ReadDir(filepath.Join(koren, s.Name())); err == nil && len(letve) > 0 {
			return true
		}
	}
	return false
}
