package main

import (
	"context"
	"database/sql"
	"log"

	"gocop/internal/repository"
)

// popraviPrilogeListova pri pokretanju dovršava fotografije uz dnevne
// listove upisane prije 0.0.38 (repository.PopraviPrilogeListova) i zapiše
// u dnevnik što je popravljeno. Zove se prije obnove površine iz knjige, a
// kad nema što popraviti, ne piše ništa.
func popraviPrilogeListova(ctx context.Context, baza *sql.DB) {
	p, err := repository.PopraviPrilogeListova(ctx, baza)
	if opis := p.String(); opis != "" {
		log.Printf("Fotografije uz dnevne listove: %s", opis)
	}
	if err != nil {
		log.Printf("Fotografije uz dnevne listove nisu do kraja popravljene, pokušat će se iznova pri idućem pokretanju: %v", err)
	}
}
