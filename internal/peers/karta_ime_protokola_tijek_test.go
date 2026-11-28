package peers_test

// Odluke (28. 11. 2026.): uz ime protokola koje je dio oznake
// dijelovi-karte/1 razgovor s dijelovima karte ide kao uz zadano gocop.
// Poruke i ishod su oni iz TestTijekDioKarteSOtiskomPDFa: granica nosi
// polje karta, želje nose dijelove karte, a dio karte s otiskom traženog
// PDF-a ne ide u spremište sadržaja.

import (
	"context"
	"testing"

	"gocop/internal/peers"
	"gocop/internal/sadrzaj"
)

func TestTijekKarteUzImeProtokolaIzOznake(t *testing.T) {
	k, _, _ := kartaUDijelovima(t, 1000, 128, 43)
	o := &opisKarte{}
	o.postavi(&k)
	kaoDio := salje(kindSadrzaj, sadrzajMsg{Stavke: []stavkaSadrzaja{
		{Otisak: otisakTudji, Vrsta: peers.VrstaDijelaKarte, Bajtova: len(pdfTudji), Podaci: []byte(pdfTudji)},
	}})
	netrazeno := "dnevnik karta: 1 primljenih dijelova karte nije upisano (zadnji: dio nije tražen)"
	pdfCeka := "spremište ima tuđi=false, traži tuđi=true, zatražen=true"
	ishod := []string{"ishod applied=1 sent=1 err=nil", sKartomDoV1, obicno, "knjiga v1 d1", pdfCeka, netrazeno, "karta 0 od 8, složena false"}
	for _, ime := range []string{peers.Protocol, "karte", "dijelovi", "e", "1", "dijelovi-karte"} {
		a, s := cistiPar(t)
		s.PostaviProtokol(ime)
		d := dnevnikTijeka(t)
		var sp *sadrzaj.Spremiste
		pripremi := func() {
			sp = sSadrzajem(t, s)
			s.PostaviKarte(t.TempDir(), o.citaj, func(context.Context) bool { return false })
			d.uzmi()
		}
		nakon := func() []string { return append(append(stanjeSadrzaja(&sp)(), d.uzmi()...), kartaCvora(s)()...) }
		for _, pozivatelj := range []bool{true, false} {
			provjeriSlucajeve(t, a, s, pozivatelj, []slucaj{
				{ime: "protokol " + ime, pripremi: pripremi, nakon: nakon, ishod: ishod,
					potezi: tokKarte(pozivatelj, granicaSKartomCvora(k, false), granicaSKartom, zeljeDijelovaCvora(append([]string{otisakTudji}, k.Dijelovi...)), sadrzajPrazan, salje(kindZelje, zeljeMsg{}), kaoDio)},
			})
		}
	}
}
