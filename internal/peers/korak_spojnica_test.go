package peers

// Dodatni korak u razgovoru, kroz spojnicu (korak_razgovora.go), s probnim
// korakom koji bilježi svaki poziv biblioteke. Poruke čvora provjeravaju se
// bajt po bajt kao u razgovor_tijek_test.go, za obje uloge: otisci i
// stavke koraka idu iza onih iz spremišta, korak dobije točan ostatak
// ograde, a višak preko njega biblioteka odreže, stavka koraka nikad ne ide
// u spremište, a Zavrsi se zove na svakom izlazu.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
	"gocop/internal/sadrzaj"
)

// Otisci probnog koraka: k1 traži čvor, k2 drži čvor
var (
	otisakK1 = strings.Repeat("c1", 32)
	otisakK2 = strings.Repeat("c2", 32)
	podaciK1 = []byte("probna stavka jedan")
	podaciK2 = []byte("probna stavka dva")
)

// biljeznik je probni korak koji bilježi pozive biblioteke, redom
type biljeznik struct {
	mu     sync.Mutex
	pozivi []string
	ne     bool              // Zapocni vrati nil
	trazi  []string          // otisci koje tijek traži
	ima    map[string][]byte // stavke koje tijek drži, po otisku
	imaJos bool
}

func (b *biljeznik) zapisi(format string, a ...any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pozivi = append(b.pozivi, fmt.Sprintf(format, a...))
}

// uzmi vrati zabilježene pozive kao retke zapisa i isprazni ih
func (b *biljeznik) uzmi() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, 0, len(b.pozivi))
	for _, p := range b.pozivi {
		out = append(out, "korak "+p)
	}
	b.pozivi = nil
	return out
}

func (b *biljeznik) Zapocni(_ context.Context, v VezaKoraka) TijekKoraka {
	b.zapisi("Zapocni pozivatelj=%v", v.Pozivatelj())
	if b.ne {
		return nil
	}
	return &biljezeniTijek{b}
}

func (b *biljeznik) Odbaci(stavke []StavkaKoraka) { b.zapisi("Odbaci %s", opisStavki(stavke)) }

// biljezeniTijek je tijek probnog koraka
type biljezeniTijek struct{ b *biljeznik }

func (t *biljezeniTijek) Trazi(o Ostatak) []string {
	t.b.zapisi("Trazi %d %d", o.Bajtova, o.Stavki)
	return t.b.trazi
}

func (t *biljezeniTijek) Izdvoji(otisci []string) (moji, ostali []string) {
	for _, o := range otisci {
		if _, ima := t.b.ima[o]; ima {
			moji = append(moji, o)
		} else {
			ostali = append(ostali, o)
		}
	}
	t.b.zapisi("Izdvoji %s", kratko(moji))
	return moji, ostali
}

// Daj namjerno vrati krivi broj bajtova: upisuje ga biblioteka
func (t *biljezeniTijek) Daj(otisci []string, o Ostatak) []StavkaKoraka {
	t.b.zapisi("Daj %s %d %d", kratko(otisci), o.Bajtova, o.Stavki)
	var out []StavkaKoraka
	for _, ot := range otisci {
		out = append(out, StavkaKoraka{Otisak: ot, Bajtova: 999, Podaci: t.b.ima[ot]})
	}
	return out
}

func (t *biljezeniTijek) Primi(stavke []StavkaKoraka) { t.b.zapisi("Primi %s", opisStavki(stavke)) }

func (t *biljezeniTijek) ImaJos() bool {
	t.b.zapisi("ImaJos")
	return t.b.imaJos
}

func (t *biljezeniTijek) Zavrsi() { t.b.zapisi("Zavrsi") }

// kratko su prva četiri znaka svakog otiska
func kratko(otisci []string) string {
	var out []string
	for _, o := range otisci {
		out = append(out, o[:4])
	}
	return "[" + strings.Join(out, " ") + "]"
}

// opisStavki: otisak (prva četiri znaka), bajtova kako ih stavka najavi i
// stvarna duljina podataka
func opisStavki(stavke []StavkaKoraka) string {
	var out []string
	for _, st := range stavke {
		out = append(out, fmt.Sprintf("%s:%d/%d", st.Otisak[:4], st.Bajtova, len(st.Podaci)))
	}
	return "[" + strings.Join(out, " ") + "]"
}

// sProbnimKorakom: čvoru b umjesto prijave iz NewService prijavi probni
// korak koji bilježi pozive
func sProbnimKorakom(t *testing.T, s *Service, b *biljeznik) {
	t.Helper()
	s.korak.Store(nil)
	if err := s.PrijaviKorak(PrijavaKoraka{Oznaka: "probni-korak/1", Vrsta: "probna-stavka", NajviseDodatnih: 3, Korak: b}); err != nil {
		t.Fatal(err)
	}
}

// granicaDrugeSKorakom je granica novijeg čvora koji još nema ništa, s
// oznakama
func granicaDrugeSKorakom(oznake ...string) frontierMsg {
	return frontierMsg{Frontier: map[string]string{}, Wants: &Wants{All: true}, Program: "0.0.72-alfa",
		Znanje: razmjena.Znanje{Mogucnosti: oznake}}
}

// sOznakomKoraka je druga strana koja javlja oznaku probnog koraka
var sOznakomKoraka = granicaDrugeSKorakom("probni-korak/1", razmjena.OznakaRazmjena)

// granicaCvoraSKorakom je granica čvora (v1) s oznakama koje objavljuje
func granicaCvoraSKorakom(t *testing.T, s *Service) string {
	t.Helper()
	m, err := json.Marshal(s.mojeMogucnosti())
	if err != nil {
		t.Fatal(err)
	}
	return odCvora(kindFrontier, `{"frontier":{"laptop-baranja|ocitanja/16/2026":"`+tijekV1+`"},"wants":{"all":true},`+programCvora+`,"mogucnosti":`+string(m)+`}`)
}

// granicaDrugeDoV1 je granica druge strane kako je exchange vrati,
// pomaknuta do v1
func granicaDrugeDoV1(t *testing.T, g frontierMsg) string {
	t.Helper()
	g.Frontier = map[string]string{"laptop-baranja|ocitanja/16/2026": tijekV1}
	b, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	return "granica " + string(b)
}

// stavkaKoraka je stavka probnog koraka kako je čvor šalje žicom
func stavkaKoraka(otisak string, podaci []byte) string {
	return fmt.Sprintf(`{"otisak":"%s","vrsta":"probna-stavka","bajtova":%d,"podaci":"%s"}`, otisak, len(podaci), base64.StdEncoding.EncodeToString(podaci))
}

// sadrzajCvora je poruka sadržaja čvora sa stavkama kako idu žicom
func sadrzajCvora(stavke ...string) string {
	return odCvora(kindSadrzaj, `{"stavke":[`+strings.Join(stavke, ",")+`]}`)
}

// zeljeCvoraS su želje čvora s otiscima
func zeljeCvoraS(otisci ...string) string {
	return odCvora(kindZelje, `{"otisci":["`+strings.Join(otisci, `","`)+`"]}`)
}

// probneStavke: druga strana pošalje sadržaj sa stavkama
func probneStavke(stavke ...stavkaSadrzaja) potez {
	return salje(kindSadrzaj, sadrzajMsg{Stavke: stavke})
}

// probna je stavka probnog koraka od druge strane
func probna(otisak string, podaci []byte) stavkaSadrzaja {
	return stavkaSadrzaja{Otisak: otisak, Vrsta: "probna-stavka", Bajtova: len(podaci), Podaci: podaci}
}

// tokKoraka je pravilan razgovor: čvor (s v1) pošalje granicu, deltu,
// želje i sadržaj, a druga strana granicu, d1, svoje želje i sadržaj
func tokKoraka(pozivatelj bool, granicaC string, granicaD frontierMsg, zeljeC, sadrzajC string, zeljeD, sadrzajD potez) []potez {
	d1 := salje(kindDelta, deltaMsg{Versions: []ledger.Version{dolaznaD1}})
	if pozivatelj {
		return []potez{
			cita(granicaC), salje(kindFrontier, granicaD),
			cita(deltaV1), d1,
			cita(zeljeC), zeljeD,
			cita(sadrzajC), sadrzajD,
			cita(krajCvora(1)), salje(kindDone, doneMsg{Applied: 1}), cita(krajVeze),
		}
	}
	return []potez{
		salje(kindFrontier, granicaD), cita(granicaC),
		d1, cita(deltaV1),
		zeljeD, cita(zeljeC),
		sadrzajD, cita(sadrzajC),
		salje(kindDone, doneMsg{Applied: 1}), cita(krajCvora(1)), cita(krajVeze),
	}
}

// Korak u punom razgovoru: njegovi otisci idu u želje iza PDF-a, njegove
// stavke u sadržaj iza PDF-a, s vrstom koraka i stvarnim brojem bajtova, a
// korak dobije točan ostatak ograde i pozive ovim redom. Primljeni PDF ide
// u spremište, a stavka koraka koraku.
func TestKorakUZeljamaISadrzaju(t *testing.T) {
	a, s := cistiPar(t)
	b := &biljeznik{trazi: []string{otisakK1}, ima: map[string][]byte{otisakK2: podaciK2}}
	sProbnimKorakom(t, s, b)
	var sp *sadrzaj.Spremiste
	pripremi := func() { sp = sSadrzajem(t, s); b.uzmi() }
	nakon := func() []string { return append(stanjeSadrzaja(&sp)(), b.uzmi()...) }
	ishod := []string{"ishod applied=1 sent=1 err=nil", granicaDrugeDoV1(t, sOznakomKoraka), obicno, "knjiga v1 d1",
		"spremište ima tuđi=true, traži tuđi=false, zatražen=false"}
	// ograda 48 MiB: bez traženog PDF-a od 14 B, pa bez danog od 12 B
	trazi := fmt.Sprintf("korak Trazi %d 199", 48<<20-len(pdfTudji))
	daj := fmt.Sprintf("korak Daj [c2c2] %d 199", 48<<20-len(pdfCvora))
	zeljeD := salje(kindZelje, zeljeMsg{Otisci: []string{otisakCvora, otisakK2}})
	sadrzajD := probneStavke(tudjiPDF.Stavke[0], probna(otisakK1, podaciK1))
	primi := fmt.Sprintf("korak Primi [c1c1:%d/%d]", len(podaciK1), len(podaciK1))
	provjeriSlucajeve(t, a, s, true, []slucaj{{ime: "korak radi", pripremi: pripremi, nakon: nakon,
		ishod: append(slices.Clone(ishod), "korak Zapocni pozivatelj=true", trazi, "korak Izdvoji [c2c2]", daj, primi, "korak ImaJos", "korak Zavrsi"),
		potezi: tokKoraka(true, granicaCvoraSKorakom(t, s), sOznakomKoraka, zeljeCvoraS(otisakTudji, otisakK1),
			sadrzajCvora(stavkaCvora, stavkaKoraka(otisakK2, podaciK2)), zeljeD, sadrzajD)}})
	provjeriSlucajeve(t, a, s, false, []slucaj{{ime: "korak radi", pripremi: pripremi, nakon: nakon,
		ishod: append(slices.Clone(ishod), "korak Zapocni pozivatelj=false", trazi, primi, "korak Izdvoji [c2c2]", daj, "korak ImaJos", "korak Zavrsi"),
		potezi: tokKoraka(false, granicaCvoraSKorakom(t, s), sOznakomKoraka, zeljeCvoraS(otisakTudji, otisakK1),
			sadrzajCvora(stavkaCvora, stavkaKoraka(otisakK2, podaciK2)), zeljeD, sadrzajD)}})
}

// Kad spremište samo prijeđe ogradu bajtova (jedan sadržaj veći od ograde
// ide sam), korak dobije ostatak manji od nule, a stavku koju ipak da
// biblioteka odreže
func TestKorakOstatakManjiOdNule(t *testing.T) {
	staro := najviseBajtovaPoRazmjeni
	najviseBajtovaPoRazmjeni = 10
	t.Cleanup(func() { najviseBajtovaPoRazmjeni = staro })
	a, s := cistiPar(t)
	b := &biljeznik{ima: map[string][]byte{otisakK2: podaciK2}}
	sProbnimKorakom(t, s, b)
	var sp *sadrzaj.Spremiste
	ishod := []string{"ishod applied=1 sent=1 err=nil", granicaDrugeDoV1(t, sOznakomKoraka), obicno, "knjiga v1 d1",
		"spremište ima tuđi=false, traži tuđi=true, zatražen=true",
		"korak Zapocni pozivatelj=true", "korak Trazi -4 199", "korak Izdvoji [c2c2]", "korak Daj [c2c2] -2 199", "korak Primi []", "korak ImaJos", "korak Zavrsi"}
	provjeriSlucajeve(t, a, s, true, []slucaj{{ime: "ostatak manji od nule",
		pripremi: func() { sp = sSadrzajem(t, s); b.uzmi() },
		nakon:    func() []string { return append(stanjeSadrzaja(&sp)(), b.uzmi()...) },
		ishod:    ishod,
		potezi: tokKoraka(true, granicaCvoraSKorakom(t, s), sOznakomKoraka, zeljeCvora,
			sadrzajCvora(stavkaCvora),
			salje(kindZelje, zeljeMsg{Otisci: []string{otisakCvora, otisakK2}}), salje(kindSadrzaj, sadrzajMsg{}))}})
}

// Kad korak da više nego što stane u ostatak ograde, biblioteka pošalje
// njegove stavke redom dok stanu, a od prve koja ne stane ostatak odreže
func TestKorakVisakSeReze(t *testing.T) {
	staro := najviseBajtovaPoRazmjeni
	najviseBajtovaPoRazmjeni = 40
	t.Cleanup(func() { najviseBajtovaPoRazmjeni = staro })
	a, s := cistiPar(t)
	b := &biljeznik{ima: map[string][]byte{otisakK1: podaciK1, otisakK2: podaciK2}}
	sProbnimKorakom(t, s, b)
	var sp *sadrzaj.Spremiste
	// ostatak za Daj: 40 - 12 B danog PDF-a = 28; stane k1 (19 B), k2 (17 B) ne
	ishod := []string{"ishod applied=1 sent=1 err=nil", granicaDrugeDoV1(t, sOznakomKoraka), obicno, "knjiga v1 d1",
		"spremište ima tuđi=false, traži tuđi=true, zatražen=true",
		"korak Zapocni pozivatelj=true", "korak Trazi 26 199", "korak Izdvoji [c1c1 c2c2]", "korak Daj [c1c1 c2c2] 28 199", "korak Primi []", "korak ImaJos", "korak Zavrsi"}
	provjeriSlucajeve(t, a, s, true, []slucaj{{ime: "višak se reže",
		pripremi: func() { sp = sSadrzajem(t, s); b.uzmi() },
		nakon:    func() []string { return append(stanjeSadrzaja(&sp)(), b.uzmi()...) },
		ishod:    ishod,
		potezi: tokKoraka(true, granicaCvoraSKorakom(t, s), sOznakomKoraka, zeljeCvora,
			sadrzajCvora(stavkaCvora, stavkaKoraka(otisakK1, podaciK1)),
			salje(kindZelje, zeljeMsg{Otisci: []string{otisakCvora, otisakK1, otisakK2}}), salje(kindSadrzaj, sadrzajMsg{}))}})
}

// Rezanje po ostatku: otisaka najviše o.Stavki, a stavki redom dok stanu po
// broju i po bajtovima podataka (ne po onome što korak upiše u Bajtova)
func TestKorakOstatakReze(t *testing.T) {
	otisci := []string{otisakK1, otisakK2, otisakCvora}
	for _, sl := range []struct {
		o    Ostatak
		broj int
	}{{Ostatak{Stavki: 5}, 3}, {Ostatak{Stavki: 2}, 2}, {Ostatak{Stavki: 0}, 0}, {Ostatak{Stavki: -1}, 0}} {
		if got := otisciUOstatku(otisci, sl.o); !slices.Equal(got, otisci[:sl.broj]) {
			t.Errorf("otisci uz %+v: %v", sl.o, got)
		}
	}
	stavke := []StavkaKoraka{{Otisak: otisakK1, Bajtova: 1, Podaci: podaciK1}, {Otisak: otisakK2, Bajtova: 1, Podaci: podaciK2}, {Otisak: otisakCvora, Podaci: []byte("x")}}
	for _, sl := range []struct {
		o    Ostatak
		broj int
	}{
		{Ostatak{Bajtova: 1 << 20, Stavki: 200}, 3},
		{Ostatak{Bajtova: 1 << 20, Stavki: 2}, 2},
		{Ostatak{Bajtova: int64(len(podaciK1) + len(podaciK2)), Stavki: 200}, 2}, // treća ne stane, iako je mala
		{Ostatak{Bajtova: int64(len(podaciK1)), Stavki: 200}, 1},
		{Ostatak{Bajtova: 2, Stavki: 200}, 0}, // prva ne stane, pa ni manja iza nje
		{Ostatak{Bajtova: -2, Stavki: 200}, 0},
		{Ostatak{Bajtova: 1 << 20, Stavki: -1}, 0},
	} {
		if got := stavkeUOstatku(stavke, sl.o); len(got) != sl.broj {
			t.Errorf("stavke uz %+v: %d, a treba %d", sl.o, len(got), sl.broj)
		}
	}
}

// Koliko bajtova stavka koraka ima, korak dobije po podacima, a ne po
// najavi druge strane: i u Primi i u Odbaci
func TestKorakBajtovaPoPodacima(t *testing.T) {
	a, s := cistiPar(t)
	b := &biljeznik{}
	sProbnimKorakom(t, s, b)
	var sp *sadrzaj.Spremiste
	pripremi := func() { sp = sSadrzajem(t, s); b.uzmi() }
	nakon := func() []string { return append(stanjeSadrzaja(&sp)(), b.uzmi()...) }
	krivaNajava := probneStavke(
		stavkaSadrzaja{Otisak: otisakK1, Vrsta: "probna-stavka", Bajtova: 0, Podaci: podaciK1},
		stavkaSadrzaja{Otisak: otisakK2, Vrsta: "probna-stavka", Bajtova: 999, Podaci: podaciK2})
	stvarno := fmt.Sprintf("[c1c1:%d/%d c2c2:%d/%d]", len(podaciK1), len(podaciK1), len(podaciK2), len(podaciK2))
	pdfCeka := "spremište ima tuđi=false, traži tuđi=true, zatražen=true"
	granica := granicaCvoraSKorakom(t, s)
	zeljeD := salje(kindZelje, zeljeMsg{})
	provjeriSlucajeve(t, a, s, true, []slucaj{
		{ime: "korak radi", pripremi: pripremi, nakon: nakon,
			ishod: []string{"ishod applied=1 sent=1 err=nil", granicaDrugeDoV1(t, sOznakomKoraka), obicno, "knjiga v1 d1", pdfCeka,
				"korak Zapocni pozivatelj=true", fmt.Sprintf("korak Trazi %d 199", 48<<20-len(pdfTudji)), "korak Izdvoji []",
				fmt.Sprintf("korak Daj [] %d 200", 48<<20), "korak Primi " + stvarno, "korak ImaJos", "korak Zavrsi"},
			potezi: tokKoraka(true, granica, sOznakomKoraka, zeljeCvora, sadrzajPrazan, zeljeD, krivaNajava)},
		{ime: "druga strana bez oznake", pripremi: pripremi, nakon: nakon,
			ishod:  append(nakonDelte("nil"), pdfCeka, "korak Odbaci "+stvarno),
			potezi: tokKoraka(true, granica, granicaDrugog, zeljeCvora, sadrzajPrazan, zeljeD, krivaNajava)},
	})
}

// Stavka vrste koraka nikad ne ide u spremište, ni kad joj je otisak otisak
// PDF-a koji čvor traži: kad korak radi, dobije je korak, a inače Odbaci.
// Odbaci ide kad druga strana nema oznaku, kad ima samo drugi N i kad
// Zapocni vrati nil; bez primljene stavke Odbaci se ne zove.
func TestKorakStavkaNikadUSpremiste(t *testing.T) {
	a, s := cistiPar(t)
	b := &biljeznik{}
	sProbnimKorakom(t, s, b)
	var sp *sadrzaj.Spremiste
	pripremi := func(ne bool) func() {
		return func() { sp = sSadrzajem(t, s); b.ne = ne; b.uzmi() }
	}
	nakon := func() []string { return append(stanjeSadrzaja(&sp)(), b.uzmi()...) }
	kaoPDF := probneStavke(probna(otisakTudji, []byte(pdfTudji)))
	pdfCeka := "spremište ima tuđi=false, traži tuđi=true, zatražen=true"
	odbaci := "korak Odbaci [7196:14/14]"
	drugiN := granicaDrugeSKorakom("probni-korak/2", razmjena.OznakaRazmjena)
	for _, pozivatelj := range []bool{true, false} {
		zapocni := fmt.Sprintf("korak Zapocni pozivatelj=%v", pozivatelj)
		redom := []string{zapocni, fmt.Sprintf("korak Trazi %d 199", 48<<20-len(pdfTudji)), "korak Primi [7196:14/14]", "korak ImaJos", "korak Zavrsi"}
		if !pozivatelj {
			redom = []string{zapocni, redom[1], redom[2], "korak Izdvoji []", fmt.Sprintf("korak Daj [] %d 200", 48<<20), redom[3], redom[4]}
		} else {
			redom = slices.Insert(redom, 2, "korak Izdvoji []", fmt.Sprintf("korak Daj [] %d 200", 48<<20))
		}
		granica := granicaCvoraSKorakom(t, s)
		zeljeD := salje(kindZelje, zeljeMsg{})
		provjeriSlucajeve(t, a, s, pozivatelj, []slucaj{
			{ime: "korak radi", pripremi: pripremi(false), nakon: nakon,
				ishod:  append([]string{"ishod applied=1 sent=1 err=nil", granicaDrugeDoV1(t, sOznakomKoraka), obicno, "knjiga v1 d1", pdfCeka}, redom...),
				potezi: tokKoraka(pozivatelj, granica, sOznakomKoraka, zeljeCvora, sadrzajPrazan, zeljeD, kaoPDF)},
			{ime: "druga strana bez oznake", pripremi: pripremi(false), nakon: nakon,
				ishod:  append(nakonDelte("nil"), pdfCeka, odbaci),
				potezi: tokKoraka(pozivatelj, granica, granicaDrugog, zeljeCvora, sadrzajPrazan, zeljeD, kaoPDF)},
			{ime: "druga strana s drugim N", pripremi: pripremi(false), nakon: nakon,
				ishod:  []string{"ishod applied=1 sent=1 err=nil", granicaDrugeDoV1(t, drugiN), obicno, "knjiga v1 d1", pdfCeka, odbaci},
				potezi: tokKoraka(pozivatelj, granica, drugiN, zeljeCvora, sadrzajPrazan, zeljeD, kaoPDF)},
			{ime: "Zapocni vrati nil", pripremi: pripremi(true), nakon: nakon,
				ishod:  []string{"ishod applied=1 sent=1 err=nil", granicaDrugeDoV1(t, sOznakomKoraka), obicno, "knjiga v1 d1", pdfCeka, zapocni, odbaci},
				potezi: tokKoraka(pozivatelj, granica, sOznakomKoraka, zeljeCvora, sadrzajPrazan, zeljeD, kaoPDF)},
			{ime: "Zapocni vrati nil, bez stavki", pripremi: pripremi(true), nakon: nakon,
				ishod:  []string{"ishod applied=1 sent=1 err=nil", granicaDrugeDoV1(t, sOznakomKoraka), obicno, "knjiga v1 d1", pdfCeka, zapocni},
				potezi: tokKoraka(pozivatelj, granica, sOznakomKoraka, zeljeCvora, sadrzajPrazan, zeljeD, salje(kindSadrzaj, sadrzajMsg{}))},
		})
	}
}

// Kad čvor ništa ne traži, ni iz spremišta ni za korak, poruka sadržaja
// druge strane smije imati najviše 64 KiB; kad korak traži, vrijedi ograda
// sadržaja. Greška primanja i tada završi korak.
func TestKorakOgradaSadrzaja(t *testing.T) {
	a, s := cistiPar(t)
	b := &biljeznik{}
	sProbnimKorakom(t, s, b)
	velika := make([]byte, 100<<10)
	velikaStavka := probneStavke(probna(otisakK1, velika))
	trazi := fmt.Sprintf("korak Trazi %d 200", 48<<20)
	granica := granicaCvoraSKorakom(t, s)
	provjeriSlucajeve(t, a, s, true, []slucaj{
		{ime: "korak ne traži", pripremi: func() { b.trazi = nil; b.uzmi() }, nakon: b.uzmi,
			ishod: []string{"ishod applied=1 sent=1 err=poruka razmjene veća od 64 KiB", granicaDrugeDoV1(t, sOznakomKoraka), obicno, "knjiga v1 d1",
				"korak Zapocni pozivatelj=true", trazi, "korak Izdvoji []", fmt.Sprintf("korak Daj [] %d 200", 48<<20), "korak Zavrsi"},
			potezi: do(tokKoraka(true, granica, sOznakomKoraka, zeljePrazne, sadrzajPrazan, salje(kindZelje, zeljeMsg{}), velikaStavka), 8, cita(krajVeze))},
		{ime: "korak traži", pripremi: func() { b.trazi = []string{otisakK1}; b.uzmi() }, nakon: b.uzmi,
			ishod: []string{"ishod applied=1 sent=1 err=nil", granicaDrugeDoV1(t, sOznakomKoraka), obicno, "knjiga v1 d1",
				"korak Zapocni pozivatelj=true", trazi, "korak Izdvoji []", fmt.Sprintf("korak Daj [] %d 200", 48<<20),
				fmt.Sprintf("korak Primi [c1c1:%d/%d]", len(velika), len(velika)), "korak ImaJos", "korak Zavrsi"},
			potezi: tokKoraka(true, granica, sOznakomKoraka, zeljeCvoraS(otisakK1), sadrzajPrazan, salje(kindZelje, zeljeMsg{}), velikaStavka)},
	})
}

// Zavrsi se zove na svakom izlazu iz razgovora u kojem je korak počeo:
// kraj umjesto želja, greška slanja, greška primanja i zauzet upis
// sadržaja (stavka koraka tada ipak stigne koraku, jer ne ide pod bravu).
// Kad je zauzet upis verzija, korak ni ne počne.
func TestKorakZavrsiNaSvakomIzlazu(t *testing.T) {
	a, s := cistiPar(t)
	b := &biljeznik{trazi: []string{otisakK1}}
	sProbnimKorakom(t, s, b)
	granica := granicaCvoraSKorakom(t, s)
	doV1 := granicaDrugeDoV1(t, sOznakomKoraka)
	trazi := fmt.Sprintf("korak Trazi %d 200", 48<<20)
	tok := tokKoraka(true, granica, sOznakomKoraka, zeljeCvoraS(otisakK1), sadrzajPrazan, salje(kindZelje, zeljeMsg{}), salje(kindSadrzaj, sadrzajMsg{}))
	pocetak := []string{"korak Zapocni pozivatelj=true", trazi}
	ishodUz := func(greska string, pozivi ...string) []string {
		return append([]string{"ishod applied=1 sent=1 err=" + greska, doV1, obicno, "knjiga v1 d1"}, append(slices.Clone(pocetak), pozivi...)...)
	}
	pripremi := func() { b.uzmi() }
	provjeriSlucajeve(t, a, s, true, []slucaj{
		{ime: "kraj umjesto želja", pripremi: pripremi, nakon: b.uzmi, ishod: ishodUz("nil", "korak Zavrsi"),
			potezi: do(tok, 5, salje(kindDone, doneMsg{}), cita(krajCvora(1)), cita(krajVeze))},
		{ime: "greška slanja želja", pripremi: pripremi, nakon: b.uzmi, ishod: ishodUz("veza pala", "korak Zavrsi"),
			potezi: do(tok, 3, kvar(), tok[3], cita(krajVeze))},
		{ime: "greška primanja želja", pripremi: pripremi, nakon: b.uzmi, ishod: ishodUz("kraj veze", "korak Zavrsi"),
			potezi: do(tok, 5)},
		{ime: "greška primanja sadržaja", pripremi: pripremi, nakon: b.uzmi, ishod: ishodUz("kraj veze", "korak Izdvoji []", fmt.Sprintf("korak Daj [] %d 200", 48<<20), "korak Zavrsi"),
			potezi: do(tok, 7)},
	})
	provjeriSlucajeve(t, a, s, false, []slucaj{
		{ime: "kraj umjesto želja", pripremi: pripremi, nakon: b.uzmi,
			ishod:  append([]string{"ishod applied=1 sent=1 err=nil", doV1, obicno, "knjiga v1 d1"}, "korak Zapocni pozivatelj=false", trazi, "korak Zavrsi"),
			potezi: []potez{salje(kindFrontier, sOznakomKoraka), cita(granica), salje(kindDelta, deltaMsg{Versions: []ledger.Version{dolaznaD1}}), cita(deltaV1), salje(kindDone, doneMsg{}), cita(krajCvora(1)), cita(krajVeze)}},
	})

	// zauzet upis
	if err := s.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.pustiBravu)
	postaviRok(t, 20*time.Millisecond)
	var sp *sadrzaj.Spremiste
	provjeriSlucajeve(t, a, s, true, []slucaj{
		{ime: "zauzet upis verzija", pripremi: pripremi, nakon: b.uzmi,
			ishod:  []string{"ishod applied=0 sent=0 err=errUpisZauzet", doV1, obicno, "knjiga v1"},
			potezi: do(tok, 4, cita(krajCvora(0)), salje(kindDone, doneMsg{}), cita(krajVeze))},
		{ime: "zauzet upis sadržaja", pripremi: func() { sp = sSadrzajem(t, s); b.uzmi() },
			nakon: func() []string { return append(stanjeSadrzaja(&sp)(), b.uzmi()...) },
			ishod: []string{"ishod applied=0 sent=1 err=nil", doV1, obicno, "knjiga v1", "spremište ima tuđi=false, traži tuđi=true, zatražen=true",
				"korak Zapocni pozivatelj=true", fmt.Sprintf("korak Trazi %d 199", 48<<20-len(pdfTudji)), "korak Izdvoji []", fmt.Sprintf("korak Daj [] %d 200", 48<<20),
				fmt.Sprintf("korak Primi [c1c1:%d/%d]", len(podaciK1), len(podaciK1)), "korak ImaJos", "korak Zavrsi"},
			potezi: []potez{
				cita(granica), salje(kindFrontier, sOznakomKoraka),
				cita(deltaV1), salje(kindDelta, deltaMsg{}),
				cita(zeljeCvoraS(otisakTudji, otisakK1)), salje(kindZelje, zeljeMsg{}),
				cita(sadrzajPrazan), probneStavke(tudjiPDF.Stavke[0], probna(otisakK1, podaciK1)),
				cita(krajCvora(0)), salje(kindDone, doneMsg{}), cita(krajVeze),
			}},
	})
}

// Bez prijave koraka razgovor je bajt po bajt kao prije, uz iste oznake u
// granici: pravilan razgovor, pad veze i puni razgovor sa sadržajem, za
// obje uloge
func TestKorakBezPrijaveKaoPrije(t *testing.T) {
	a, s := cistiPar(t)
	s.korak.Store(nil)
	var sp *sadrzaj.Spremiste
	saPDF := func() { sp = sSadrzajem(t, s) }
	granica := frontierMsg{Frontier: map[string]string{"cop-osijek|ocitanja/16/2026": tijekD1}, Wants: &Wants{All: true}, Program: "0.0.38-alfa"}
	puniIshod := []string{"ishod applied=1 sent=1 err=nil",
		`granica {"frontier":{"cop-osijek|ocitanja/16/2026":"` + tijekD1 + `","laptop-baranja|ocitanja/16/2026":"` + tijekV1 + `"},"wants":{"all":true},"program":"0.0.38-alfa"}`,
		obicno, "knjiga v1 d1", "spremište ima tuđi=true, traži tuđi=false, zatražen=false"}
	zelje := salje(kindZelje, zeljeMsg{Otisci: []string{otisakCvora, otisakTudji}})
	tok := tokPozivatelja()
	provjeriSlucajeve(t, a, s, true, []slucaj{
		{ime: "bez kvara", potezi: do(tok, 10, cita(krajVeze)), ishod: nakonDelte("nil")},
		{ime: "nestane nakon želja", potezi: do(tok, 5), ishod: nakonDelte("kraj veze")},
		{ime: "sa sadržajem", pripremi: saPDF, nakon: stanjeSadrzaja(&sp), ishod: puniIshod,
			potezi: tokKoraka(true, granicaV1, granica, zeljeCvora, sadrzajCvora(stavkaCvora), zelje, salje(kindSadrzaj, tudjiPDF))},
	})
	tok = tokPozvanog()
	provjeriSlucajeve(t, a, s, false, []slucaj{
		{ime: "bez kvara", potezi: do(tok, 10, cita(krajVeze)), ishod: nakonDelte("nil")},
		{ime: "nestane prije sadržaja", potezi: do(tok, 6), ishod: nakonDelte("kraj veze")},
		{ime: "sa sadržajem", pripremi: saPDF, nakon: stanjeSadrzaja(&sp), ishod: puniIshod,
			potezi: tokKoraka(false, granicaV1, granica, zeljeCvora, sadrzajCvora(stavkaCvora, stavkaTudja), zelje, salje(kindSadrzaj, tudjiPDF))},
	})
}

// Korak koji uvijek ima još: pozivatelj u jednom krugu ponovi razgovor
// najviše NajviseDodatnih puta, a pozvani ne ponavlja ništa i ne pamti da
// korak teče. Bez prijave nema ponavljanja.
func TestKorakImaJosSamoPozivatelj(t *testing.T) {
	cv := clanovi(t, "cop-osijek", "laptop-baranja")
	a, l := cv[0], cv[1]
	ba, bl := &biljeznik{imaJos: true}, &biljeznik{imaJos: true}
	sProbnimKorakom(t, a, ba)
	sProbnimKorakom(t, l, bl)
	_, _, krugova, err := l.razgovoriZaRedom(ctxRub, a.node.ID)
	if err != nil || krugova != 1+3 {
		t.Fatalf("krug: %d razgovora, %v", krugova, err)
	}
	if n := brojRedaka(bl.uzmi(), "korak Zapocni pozivatelj=true"); n != 4 {
		t.Errorf("pozivatelj je počeo korak %d puta", n)
	}
	rok := time.Now().Add(5 * time.Second)
	for ba.broj("Zavrsi") < 4 && time.Now().Before(rok) {
		time.Sleep(10 * time.Millisecond)
	}
	if z := ba.uzmi(); brojRedaka(z, "korak Zapocni pozivatelj=false") != 4 || brojRedaka(z, "korak ImaJos") != 4 {
		t.Errorf("pozvani: %v", z)
	}
	for !prazno(&a.korakTece) && time.Now().Before(rok) {
		time.Sleep(10 * time.Millisecond)
	}
	if !prazno(&a.korakTece) || !prazno(&a.josKoraka) || !prazno(&l.korakTece) || !prazno(&l.josKoraka) {
		t.Error("ostao je zapis da korak teče")
	}

	l.korak.Store(nil)
	if _, _, krugova, err = l.razgovoriZaRedom(ctxRub, a.node.ID); err != nil || krugova != 1 {
		t.Errorf("bez prijave: %d razgovora, %v", krugova, err)
	}
	l.josKoraka.Store(a.node.ID, true)
	if l.dalje(a.node.ID, 0, 0, nil, new(int), new(string)) || !prazno(&l.josKoraka) {
		t.Error("bez prijave zapis koraka ponavlja razgovor ili ostaje")
	}
}

// broj je koliko zabilježenih poziva sadrži tekst
func (b *biljeznik) broj(tekst string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return brojRedaka(b.pozivi, tekst)
}

// brojRedaka je koliko redaka sadrži tekst
func brojRedaka(retci []string, tekst string) int {
	n := 0
	for _, r := range retci {
		if strings.Contains(r, tekst) {
			n++
		}
	}
	return n
}

// prazno: u mapi nema nijednog zapisa
func prazno(m *sync.Map) bool {
	n := 0
	m.Range(func(_, _ any) bool { n++; return true })
	return n == 0
}

// Kad korak traži više otisaka nego što ostane stavki, u želje ide najviše
// koliko ostane, redom kako ih je korak dao
func TestKorakTraziSeReze(t *testing.T) {
	a, s := cistiPar(t)
	var otisci []string
	for i := range najviseSadrzajaPoRazmjeni + 1 {
		otisci = append(otisci, fmt.Sprintf("%064x", i+1))
	}
	b := &biljeznik{trazi: otisci}
	sProbnimKorakom(t, s, b)
	trazi := fmt.Sprintf("korak Trazi %d 200", 48<<20)
	provjeriSlucajeve(t, a, s, true, []slucaj{{ime: "višak otisaka", pripremi: func() { b.uzmi() }, nakon: b.uzmi,
		ishod: []string{"ishod applied=1 sent=1 err=nil", granicaDrugeDoV1(t, sOznakomKoraka), obicno, "knjiga v1 d1",
			"korak Zapocni pozivatelj=true", trazi, "korak Izdvoji []", fmt.Sprintf("korak Daj [] %d 200", 48<<20), "korak Primi []", "korak ImaJos", "korak Zavrsi"},
		potezi: tokKoraka(true, granicaCvoraSKorakom(t, s), sOznakomKoraka, zeljeCvoraS(otisci[:najviseSadrzajaPoRazmjeni]...), sadrzajPrazan,
			salje(kindZelje, zeljeMsg{}), salje(kindSadrzaj, sadrzajMsg{}))}})
}
