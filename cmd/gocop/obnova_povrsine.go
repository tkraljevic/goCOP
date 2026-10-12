package main

import (
	"context"
	"database/sql"
	"log"

	"gocop/internal/ledger"
	"gocop/internal/repository"
)

// primljeno je što čvor radi s verzijama koje su razmjena, COP paket ili
// arhiva upravo upisali u knjigu (OnApplied): izdanja prognoze i arhive idu
// svojim putem, a sve na površinu. Obnova površine (obnoviPovrsinu) ovdje ne
// ide: prolazi sve verzije očitanja u knjizi, pa bi usporila svaku razmjenu i
// uvoz, a paket koji ne prođe ionako čeka među nedovršenima i ponavlja se s
// idućom primjenom.
func primljeno(ctx context.Context, baza *sql.DB, rec *ledger.Recorder, arhiva *razmjenaArhive, versions []ledger.Version) error {
	primateljPrognoze.primi(versions)
	if arhiva != nil {
		arhiva.primljeno(versions)
	}
	return repository.ApplyVersions(ctx, baza, rec, versions)
}

// obnoviPovrsinu vraća u tablice zapise koji su stigli razmjenom, a ostali
// samo u knjizi jer se primjena paketa nije dovršila prije gašenja čvora.
// Zove se pri pokretanju, uz ReplaySurface, prije weba i razmjene. Kad nema
// što popraviti, ne piše ništa.
func obnoviPovrsinu(ctx context.Context, baza *sql.DB, rec *ledger.Recorder) {
	o, err := repository.ObnoviPovrsinu(ctx, baza, rec)
	if opis := o.String(); opis != "" {
		log.Printf("Obnova površine: %s", opis)
	}
	if err != nil {
		log.Printf("Obnova površine nije dovršena, pokušat će se iznova pri idućem pokretanju: %v", err)
	}
}
