package repository

import (
	"context"
	"time"

	"gocop/internal/ledger"
)

// pravilaSazimanja čuvaju pri sažimanju knjige i verzije koje nisu zadnje, a
// površina ih treba. Akt zadržava zadnju verziju svake svoje ovjere: na
// površini stoji najnovija ovjerena verzija i kad je poslije nje stigao
// nacrt s drugog čvora (aktNaPovrsini), a druga ovjera istog nacrta stoji
// samo u knjizi (drugeOvjereAkata). Bez toga bi obnova površine pri
// pokretanju vratila ovjeren akt u nacrt, a sukob oznake nestao bez odluke
// (Odluke (8. 10. 2026.), pitanje 4).
//
// Članstvo ostaje svako (po potpisu): razmjena ključ računala bira iz svih
// članstava u knjizi, a vrijedi prvo izdano (peers, clanstvo_oznake.go).
// Kad bi ostala samo zadnja verzija, tuđe članstvo pod istim imenom, koje
// je stiglo poslije, nakon sažimanja zauzelo bi to ime.
var pravilaSazimanja = []ledger.ZadrziPo{
	{Entity: EntityAkti, Kljuc: "$.ovjera_kod"},
	{Entity: "memberships", Kljuc: "$.signature"},
}

// ZaSazimanje broji verzije koje bi SazmiKnjigu obrisala, za pregled prije
// radnje
func ZaSazimanje(ctx context.Context, rec *ledger.Recorder, rok time.Time) (int, error) {
	return rec.Compactable(ctx, rok, pravilaSazimanja...)
}
