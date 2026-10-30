package main

import "gocop/internal/service"

// postaviZid daje zidu događanja pravila drugih servisa: stanje obrane iz
// ovjerenih akata, a redak prijave s terena vidi samo tko smije čitati tu
// prijavu, po istom pravilu kao Dokumentacija → Prijave s terena
// (Odluke (8. 10. 2026.), pitanje 20). Izvođač na zidu ne vidi dnevnik
// COP-a ni dežurstva, kao ni u Dnevnicima (Odluke (30. 10. 2026.), 2).
func postaviZid(zid *service.ZidService, akti *service.AktService, prijave *service.PrijavaService, dnevnici *service.JournalService) {
	zid.SetStanjaSektora(akti.StanjaSektora)
	zid.SetVidiPrijavu(prijave.SmijeVidjeti)
	zid.SetIzvodjac(dnevnici.IzvodjacZaDnevnike)
}
