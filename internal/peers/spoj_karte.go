package peers

// PRIVREMENO, dok prijenos karte ne drže goCOP (main.go) i web sami: Service
// drži prijenos karte bez interneta (PrijenosKarte) za PostaviKarte, za
// stranicu karte (StanjeKarte) i za pločicu razmjene (Status). Prijenos
// karte razmjenu koristi samo kroz njezino javno sučelje
// (PrijenosKarte.Prijavi).

import (
	"context"
	"log"

	"gocop/internal/models"
	"gocop/internal/razmjena"
)

// PostaviKarte daje razmjeni mapu karata, opis zadane karte i javlja li
// ovo računalo obranu. Prvi poziv napravi prijenos karte i prijavi ga kao
// dodatni korak razgovora (PrijenosKarte.Prijavi); bez toga se dijelovi
// karte ne traže ni ne daju, a granica nema polje karta. Svaki sljedeći
// poziv zamijeni mapu, opis i obranu (PrijenosKarte.Postavi). Zove se
// poslije PostaviProtokol, a prije Serve, Announce i RunAutoSync.
//
// Ne uzima s.mu: prijenos čita mapu s diska i zove opis, pa se složi
// izvan brave i spremi s CompareAndSwap. Kad dva prva poziva idu
// istodobno, prijenos spremi samo jedan; drugi na njemu zove Postavi, kao
// i svaki sljedeći poziv.
func (s *Service) PostaviKarte(mapa string, opis func() *models.VektorskaKarta, uObrani func(context.Context) bool) {
	if p := s.karte.Load(); p != nil {
		p.Postavi(mapa, opis, uObrani)
		return
	}
	p := NoviPrijenosKarte(mapa, opis, uObrani)
	if !s.karte.CompareAndSwap(nil, p) {
		s.karte.Load().Postavi(mapa, opis, uObrani)
		return
	}
	if err := p.Prijavi(s); err != nil {
		log.Printf("karta bez interneta: dijelovi karte ne idu razmjenom: %v", err)
	}
}

// StanjeKarte je stanje prijenosa zadane karte; bez opisa s dijelovima ili
// bez prijenosa (PostaviKarte) prazno
func (s *Service) StanjeKarte(ctx context.Context) StanjeKarte {
	if p := s.karte.Load(); p != nil {
		return p.Stanje(ctx)
	}
	return StanjeKarte{}
}

// znanjeKarte je ono što pločica ovog računala pokazuje o njegovoj karti
// (PrijenosKarte.Znanje); nil kad prijenos karata nije postavljen
func (s *Service) znanjeKarte() *razmjena.ZnanjeKarte {
	if p := s.karte.Load(); p != nil {
		return p.Znanje()
	}
	return nil
}
