package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"gocop/internal/sadrzaj"
)

// pospremanjeSadrzajaSvakih je razmak između dva pospremanja sadržaja
const pospremanjeSadrzajaSvakih = 6 * time.Hour

// odgodaSeljenja je koliko program nakon pokretanja čeka prije seljenja.
// Postava novom izdanju daje 90 s da se javi i inače vraća prethodni
// program, a bazu ne vraća. Dok seljenje nije počelo, prethodno izdanje u
// bazi sadržaja nalazi sve kao prije.
const odgodaSeljenja = 5 * time.Minute

// preseliSadrzaj seli PDF-ove i slike iz vremena kad su bili u bazi u
// datoteke. Ide u pozadini i tek nakon čekanja (odgodaSeljenja): svaka
// datoteka ide na disk posebno, a nadogradnja čeka da se program javi.
func preseliSadrzaj(cekaj func(), sp *sadrzaj.Spremiste) {
	cekaj()
	uDnevnik(porukaSeljenja(sp.PreseliIzBaze(context.Background())))
}

func porukaSeljenja(n int, err error) string {
	if err != nil {
		return fmt.Sprintf("seljenje sadržaja iz baze u datoteke (nastavlja se pri sljedećem pokretanju): %v", err)
	}
	if n > 0 {
		return fmt.Sprintf("sadržaj preseljen iz baze u datoteke: %d", n)
	}
	return ""
}

// pospremajSadrzaj pospremla sadržaj dok dalje ne kaže da je dosta; dalje
// i čeka do sljedećeg pospremanja
func pospremajSadrzaj(dalje func() bool, otpusti func(context.Context) (int, int64, error), sp *sadrzaj.Spremiste) {
	for dalje() {
		for _, p := range pospremiSadrzaj(context.Background(), otpusti, sp) {
			uDnevnik(p)
		}
	}
}

// pospremiSadrzaj otpušta primljeni sadržaj kojem je po pretplati istekao
// rok držanja (zapisi ostaju i sadržaj se može opet dohvatiti) i miče
// datoteke sadržaja koje baza više ne navodi (prekinut upis, datoteka koju
// Windows nije dao obrisati dok se čitala). Vraća poruke za dnevnik.
func pospremiSadrzaj(ctx context.Context, otpusti func(context.Context) (int, int64, error), sp *sadrzaj.Spremiste) []string {
	var out []string
	if n, b, err := otpusti(ctx); err != nil {
		out = append(out, fmt.Sprintf("otpuštanje starog sadržaja: %v", err))
	} else if n > 0 {
		out = append(out, fmt.Sprintf("otpušteno %d primljenih sadržaja (%.1f MB) po roku pretplate", n, float64(b)/1e6))
	}
	if n, err := sp.PocistiDatoteke(ctx); err != nil {
		out = append(out, fmt.Sprintf("čišćenje datoteka sadržaja: %v", err))
	} else if n > 0 {
		out = append(out, fmt.Sprintf("maknuto %d datoteka sadržaja koje baza više ne navodi", n))
	}
	return out
}

// uDnevnik upisuje poruku u dnevnik; prazna se ne upisuje
func uDnevnik(p string) {
	if p != "" {
		log.Print(p)
	}
}
