package main

import (
	"context"
	"database/sql"
	"log"

	"gocop/internal/ledger"
	"gocop/internal/repository"
)

// primijeniPrimljeno stavlja primljene verzije na površinu, pa obnovi i ono
// što je ranije ostalo samo u knjizi (očitanja, dnevnici). Zove ga OnApplied,
// kroz koji idu razmjena, COP paket i arhiva.
func primijeniPrimljeno(ctx context.Context, baza *sql.DB, rec *ledger.Recorder, versions []ledger.Version) error {
	err := repository.ApplyVersions(ctx, baza, rec, versions)
	obnoviPovrsinu(ctx, baza, rec)
	return err
}

// obnoviPovrsinu vraća u tablice zapise koji su stigli razmjenom, a ostali
// samo u knjizi jer se primjena paketa nije dovršila. Zove se pri pokretanju
// (uz ReplaySurface, prije razmjene) i nakon svakog primljenog paketa
// (primijeniPrimljeno). Kad nema što popraviti, ne piše ništa.
func obnoviPovrsinu(ctx context.Context, baza *sql.DB, rec *ledger.Recorder) {
	o, err := repository.ObnoviPovrsinu(ctx, baza, rec)
	if opis := o.String(); opis != "" {
		log.Printf("Obnova površine: %s", opis)
	}
	if err != nil {
		log.Printf("Obnova površine nije dovršena, pokušat će se iznova s idućom razmjenom: %v", err)
	}
}
