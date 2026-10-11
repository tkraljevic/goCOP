package main

import "gocop/internal/service"

// postaviZid daje zidu događanja pravila drugih servisa: stanje obrane iz
// ovjerenih akata, a redak prijave s terena vidi samo tko smije čitati tu
// prijavu, po istom pravilu kao Dokumentacija → Prijave s terena
// (Odluke (8. 10. 2026.), pitanje 20)
func postaviZid(zid *service.ZidService, akti *service.AktService, prijave *service.PrijavaService) {
	zid.SetStanjaSektora(akti.StanjaSektora)
	zid.SetVidiPrijavu(prijave.SmijeVidjeti)
}
