package peers

// Dio karte koji druga strana pošalje, a ovaj razgovor ga nije tražio, ne
// ide nikamo, i kad mu je otisak otisak PDF-a koji čvor traži: PDF ne
// dolazi u spremište sadržaja, ostaje među željama, a dnevnik dobije jedan
// redak „karta: …”. Zaključano prije nego što karta izađe iz peers (N04,
// PR 1); poruke su bajt po bajt kao u karta_tijek_test.go. Očekivanja se
// do kraja N04 ne mijenjaju. Priprema se mijenja: test koristi unutarnje
// pomoćnike paketa i PostaviKarte, pa u N04 prelazi u peers_test, a karta
// se prijavljuje kroz vektorskakarta.

import (
	"context"
	"log"
	"strings"
	"sync"
	"testing"

	"gocop/internal/sadrzaj"
)

// dnevnikRazgovora skuplja retke dnevnika o sadržaju i dijelovima karte
// koje razgovor napiše
type dnevnikRazgovora struct {
	mu     sync.Mutex
	retci  []string
	ostalo strings.Builder
}

func (d *dnevnikRazgovora) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ostalo.Write(p)
	tekst := d.ostalo.String()
	for {
		i := strings.IndexByte(tekst, '\n')
		if i < 0 {
			break
		}
		redak := tekst[:i]
		tekst = tekst[i+1:]
		for _, pocetak := range []string{"karta: ", "sinkronizacija: odbačeno"} {
			if j := strings.Index(redak, pocetak); j >= 0 {
				d.retci = append(d.retci, "dnevnik "+redak[j:])
			}
		}
	}
	d.ostalo.Reset()
	d.ostalo.WriteString(tekst)
	return len(p), nil
}

// uzmi vrati skupljene retke i počne iznova
func (d *dnevnikRazgovora) uzmi() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := d.retci
	d.retci = nil
	return out
}

// dnevnikTijeka preusmjeri dnevnik do kraja testa
func dnevnikTijeka(t *testing.T) *dnevnikRazgovora {
	t.Helper()
	d := &dnevnikRazgovora{}
	stari := log.Writer()
	log.SetOutput(d)
	t.Cleanup(func() { log.SetOutput(stari) })
	return d
}

// PDF koji čvor traži stigne kao dio karte, a čvor ima prijavljenu kartu,
// kao goCOP (main.go uvijek zove PostaviKarte). Dio se ne upiše ni kad
// korak karte u razgovoru ne radi, ni kad radi:
//   - druga strana nema oznaku (oznake se ne slažu);
//   - druga strana ima oznaku, ali kod čvora traje obrana, a veza nije
//     ista lokalna mreža, pa čvor dijelove ne traži;
//   - druga strana ima oznaku i čvor traži dijelove, ali ne taj otisak.
//
// Za obje uloge. Čvor bez ikakve prijave karte namjerno nije ovdje: goCOP
// ga nema, a od N04 biblioteka bez prijave vrstu karta-dio ne zna.
func TestTijekDioKarteSOtiskomPDFa(t *testing.T) {
	k, _, _ := kartaUDijelovima(t, 1000, 128, 43)
	o := &opisKarte{}
	o.postavi(&k)
	a, s := cistiPar(t)
	d := dnevnikTijeka(t)
	var sp *sadrzaj.Spremiste
	kaoDio := salje(kindSadrzaj, sadrzajMsg{Stavke: []stavkaSadrzaja{
		{Otisak: otisakTudji, Vrsta: VrstaDijelaKarte, Bajtova: len(pdfTudji), Podaci: []byte(pdfTudji)},
	}})
	// prijavi: karta kojoj nedostaju svi dijelovi, uz obranu ili bez nje
	prijavi := func(obrana bool) func() {
		return func() {
			sp = sSadrzajem(t, s)
			s.PostaviKarte(t.TempDir(), o.citaj, func(context.Context) bool { return obrana })
			d.uzmi()
		}
	}
	nakon := func() []string { return append(append(stanjeSadrzaja(&sp)(), d.uzmi()...), kartaCvora(s)()...) }
	netrazeno := "dnevnik karta: 1 primljenih dijelova karte nije upisano (zadnji: dio nije tražen)"
	pdfCeka := "spremište ima tuđi=false, traži tuđi=true, zatražen=true"
	bezDijelova := "karta 0 od 8, složena false"
	sOznakom := []string{"ishod applied=1 sent=1 err=nil", sKartomDoV1, obicno, "knjiga v1 d1", pdfCeka, netrazeno, bezDijelova}
	for _, pozivatelj := range []bool{true, false} {
		provjeriSlucajeve(t, a, s, pozivatelj, []slucaj{
			{ime: "druga strana bez oznake", pripremi: prijavi(false), nakon: nakon,
				ishod:  append(nakonDelte("nil"), pdfCeka, netrazeno, bezDijelova),
				potezi: tokKarte(pozivatelj, granicaSKartomCvora(k, false), granicaDrugog, zeljeCvora, sadrzajPrazan, salje(kindZelje, zeljeMsg{}), kaoDio)},
			{ime: "obrana, druga strana s oznakom", pripremi: prijavi(true), nakon: nakon,
				ishod:  sOznakom,
				potezi: tokKarte(pozivatelj, granicaSKartomCvora(k, false), granicaSKartom, zeljeCvora, sadrzajPrazan, salje(kindZelje, zeljeMsg{}), kaoDio)},
			{ime: "druga strana s oznakom", pripremi: prijavi(false), nakon: nakon,
				ishod:  sOznakom,
				potezi: tokKarte(pozivatelj, granicaSKartomCvora(k, false), granicaSKartom, zeljeDijelovaCvora(append([]string{otisakTudji}, k.Dijelovi...)), sadrzajPrazan, salje(kindZelje, zeljeMsg{}), kaoDio)},
		})
	}
}
