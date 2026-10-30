package peers

// Ograde onoga što razgovor prima od druge strane: veličina poruke po
// vrsti, oznake i vrste u znanju, duljina razloga, izdanja i greške koja se
// pamti, broj ključeva zapamćene granice i sadržaj koji se prima samo kad
// je tražen. Uz svaku ogradu i ispravan razgovor koji njome ne smije zapeti.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
	"gocop/internal/sadrzaj"
)

// Ograda svake vrste poruke pušta najveću poruku koju šalje ispravan
// pošiljatelj od 0.0.25: deltu do najviseBajtovaDelte (uz omotnicu i prvu
// veću verziju), sadržaj do najveciSadrzajUPoruci kao base64 i 200 otisaka
// želja. Vrsta koju razgovor ne zna prima se do razmjena.NajvecaPoruka.
func TestOgradaPorukePoVrsti(t *testing.T) {
	if ogradaPoruke(kindDelta) < 2*int64(najviseBajtovaDelte) {
		t.Errorf("delta %d, pošiljatelj šalje do %d", ogradaPoruke(kindDelta), najviseBajtovaDelte)
	}
	if base64 := int64(najveciSadrzajUPoruci) * 4 / 3; ogradaPoruke(kindSadrzaj) < base64+base64/4 {
		t.Errorf("sadržaj %d, najveća stavka kao base64 %d", ogradaPoruke(kindSadrzaj), base64)
	}
	zelje, err := json.Marshal(zeljeMsg{Otisci: slicesRepeat(strings.Repeat("a", 64), 2*najviseSadrzajaPoRazmjeni)})
	if err != nil {
		t.Fatal(err)
	}
	if ogradaPoruke(kindZelje) < 4*int64(len(zelje)) {
		t.Errorf("želje %d, dvostruko više otisaka od ograde je %d bajtova", ogradaPoruke(kindZelje), len(zelje))
	}
	for _, kind := range []string{kindFrontier, kindGranicaOdbijenih, kindRupe, kindZelje, kindDone} {
		if ogradaPoruke(kind) > 8<<20 {
			t.Errorf("%s: %d", kind, ogradaPoruke(kind))
		}
	}
	if ogradaPoruke("nova-vrsta") != razmjena.NajvecaPoruka {
		t.Error("nepoznata vrsta ima ogradu manju od najveće poruke")
	}
}

func slicesRepeat(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

// Granica i želje veće od ograde svoje vrste prekidaju razgovor jasnom
// greškom, za obje strane, a delta veća od tih ograda (ispod svoje) prolazi
// kao i prije.
func TestRazgovorOgradaPoVrstiPoruke(t *testing.T) {
	a, b := cistiPar(t)
	velikaGranica := frontierMsg{Frontier: map[string]string{"cop-osijek|": strings.Repeat("0", 9<<20)}, Program: "0.0.38-alfa"}
	velikeZelje := zeljeMsg{Otisci: slicesRepeat(strings.Repeat("b", 64), 20000)}
	velikaD1 := dolaznaD1
	velikaD1.Payload = json.RawMessage(`{"cm":130,"opis":"` + strings.Repeat("x", 12<<20) + `"}`)
	sVelikom := func(tok []potez, i int) []potez {
		return zamijeni(tok, i, salje(kindDelta, deltaMsg{Versions: []ledger.Version{velikaD1}}))
	}
	tok := tokPozivatelja()
	provjeriSlucajeve(t, a, b, true, []slucaj{
		{ime: "granica preko ograde", potezi: do(tok, 1, salje(kindFrontier, velikaGranica), cita(krajVeze)), ishod: prijeGranice("poruka razmjene veća od 8 MiB")},
		{ime: "želje preko ograde", potezi: do(tok, 5, salje(kindZelje, velikeZelje), cita(krajVeze)), ishod: nakonDelte("poruka razmjene veća od 1 MiB")},
		{ime: "delta od 12 MiB", potezi: do(sVelikom(tok, 3), 10, cita(krajVeze)), ishod: nakonDelte("nil")},
	})
	tok = tokPozvanog()
	provjeriSlucajeve(t, a, b, false, []slucaj{
		{ime: "granica preko ograde", potezi: do(tok, 0, salje(kindFrontier, velikaGranica), cita(krajVeze)), ishod: prijeGranice("poruka razmjene veća od 8 MiB")},
		{ime: "želje preko ograde", potezi: do(tok, 4, salje(kindZelje, velikeZelje), cita(krajVeze)), ishod: nakonDelte("poruka razmjene veća od 1 MiB")},
		{ime: "delta od 12 MiB", potezi: do(sVelikom(tok, 2), 10, cita(krajVeze)), ishod: nakonDelte("nil")},
	})
}

// Greška razgovora s predugim tekstom (ovdje broj od 5000 znamenki u
// granici) vraća se skraćena, a kratka ostaje ista
func TestGreskaRazgovoraSkracena(t *testing.T) {
	a, b := cistiPar(t)
	dug := razmjena.Envelope{Kind: kindFrontier, Payload: json.RawMessage(`{"frontier":{},"cekaNadogradnju":` + strings.Repeat("9", 5000) + `}`)}
	for _, pozivatelj := range []bool{true, false} {
		samoV1(t, b)
		potezi := []potez{saljeSirovo("granica s dugim brojem", dug), cita(krajVeze)}
		if pozivatelj {
			potezi = append([]potez{cita(granicaV1)}, potezi...)
		}
		zapis := snimiTijek(t, b, a, razgovor{pozivatelj: pozivatelj, potezi: potezi})
		ishod := zapis[len(potezi)]
		if !strings.HasPrefix(ishod, "ishod applied=0 sent=0 err=json: cannot unmarshal number 999") || len(ishod) > najduljaGreska+64 || !strings.HasSuffix(ishod, "…") {
			t.Errorf("%s: %.200s… (%d bajtova)", uloga(pozivatelj), ishod, len(ishod))
		}
	}
	kratka := errors.New("kratka greška")
	if ograniciGresku(kratka) != kratka || ograniciGresku(nil) != nil {
		t.Error("kratka greška je promijenjena")
	}
	duga := ograniciGresku(fmt.Errorf("%s: %w", strings.Repeat("ž", 2000), errUpisZauzet))
	if !errors.Is(duga, errUpisZauzet) || len(duga.Error()) > najduljaGreska+len("…") {
		t.Errorf("omotana greška: %d bajtova, errors.Is %v", len(duga.Error()), errors.Is(duga, errUpisZauzet))
	}
}

// Razlog odbijenice, izdanje, gradnja i greška pamte se skraćeni, a
// prevelika granica se ne pamti; ispravne vrijednosti ostaju iste
func TestStanjeRazmjenePamtiOgradeno(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	if _, err := s.db.Exec(`INSERT INTO peers (node_id, name, public_key, created_at) VALUES ('ured-vukovar', 'Ured Vukovar', 'kljuc', CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	velik := strings.Repeat("A", 8<<20)
	odbijena := neocekivanaPoruka(kindFrontier, razmjena.Envelope{Kind: razmjena.VrstaOdbijeno, Reason: velik})
	var o odbijenaRazmjena
	if !errors.As(odbijena, &o) || len(o.razlog) != najduljiRazlog {
		t.Fatalf("razlog odbijenice: %d bajtova", len(o.razlog))
	}
	if g := neocekivanaPoruka(kindFrontier, razmjena.Envelope{Kind: velik}); len(g.Error()) > 100 {
		t.Errorf("nepoznata vrsta poruke: %d bajtova", len(g.Error()))
	}
	granica := map[string]string{}
	for i := range najviseKljucevaGranice + 1 {
		granica[fmt.Sprintf("k%d|", i)] = tijekD1
	}
	if granicaZaPamcenje(granica) != nil {
		t.Error("granica s previše ključeva se pamti")
	}
	granica = map[string]string{"cop-osijek|": strings.Repeat("z", 7<<20), strings.Repeat("k", 300) + "|": tijekD1, "": tijekD1}
	ishod := ishodRazmjene(0, 0, frontierMsg{Frontier: granica, Program: velik, Gradnja: velik}, time.Second, odbijena)
	s.recordSyncState(ctxRub, "ured-vukovar", ishod)
	s.zapisiBiljesku(ctxRub, "ured-vukovar", ishod)
	ishod.err = nil
	s.recordSyncState(ctxRub, "ured-vukovar", ishod)
	var stanje, biljeska int
	if err := s.db.QueryRow(`SELECT length(their_frontier) + length(last_error) + length(program) + length(gradnja) FROM peer_sync WHERE node_id = 'ured-vukovar'`).Scan(&stanje); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT length(last_sync_note) FROM peers WHERE node_id = 'ured-vukovar'`).Scan(&biljeska); err != nil {
		t.Fatal(err)
	}
	if stanje > 2*najduljiProgram+16 || biljeska > najduljiRazlog+64 {
		t.Errorf("zapamćeno %d bajtova stanja i %d bilješke", stanje, biljeska)
	}

	// ispravna granica i izdanje ostaju kakvi su stigli
	ispravan := ishodRazmjene(1, 2, frontierMsg{Frontier: map[string]string{"ured-vukovar|": tijekD1}, Program: "0.0.47-alfa", Gradnja: "0.0.47-alfa (606a83d5)"}, time.Second, nil)
	s.recordSyncState(ctxRub, "ured-vukovar", ispravan)
	st, err := s.syncStates(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	if z := st["ured-vukovar"]; z.Program != "0.0.47-alfa" || z.Gradnja != "0.0.47-alfa (606a83d5)" || z.Frontier["ured-vukovar|"] != tijekD1 {
		t.Errorf("ispravno stanje: %+v", z)
	}
	if skrati("čćž", 3) != "č" || skrati("abc", 0) != "" {
		t.Error("skraćivanje reže znak")
	}
}

// Dnevnik javlja drugo izdanje drugog čvora jednom dok se ne promijeni i
// najviše jednom na sat, a pamti samo zadnje izdanje po čvoru
func TestDrugoIzdanjeSePamtiPoCvoru(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	s.PostaviProgram("0.0.47-alfa", "0.0.47-alfa (abc)")
	var dnevnik bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&dnevnik)
	t.Cleanup(func() { log.SetOutput(stari) })
	p := &Peer{NodeID: "ured-vukovar", Name: "Ured Vukovar"}
	for i := range 50 {
		s.javiDrugiProgram(p, syncOutcome{program: fmt.Sprintf("0.0.%d-alfa", i)})
	}
	n := 0
	s.javljenaInacica.Range(func(_, _ any) bool { n++; return true })
	if redaka := strings.Count(dnevnik.String(), "radi na"); n != 1 || redaka != 1 {
		t.Errorf("zapamćeno %d izdanja, %d redaka u dnevniku", n, redaka)
	}
	// sat poslije novo izdanje se javi, isto izdanje ne
	s.javljenaInacica.Store(p.NodeID, javljenoIzdanje{program: "0.0.0-alfa", kad: time.Now().Add(-razmakJavljanjaIzdanja)})
	s.javiDrugiProgram(p, syncOutcome{program: "0.0.0-alfa"})
	s.javiDrugiProgram(p, syncOutcome{program: "0.0.46-alfa"})
	s.javiDrugiProgram(p, syncOutcome{program: "0.0.47-alfa"})
	if redaka := strings.Count(dnevnik.String(), "radi na"); redaka != 2 || !strings.Contains(dnevnik.String(), "radi na 0.0.46-alfa") {
		t.Errorf("nakon sata %d redaka: %s", redaka, dnevnik.String())
	}
}

// Od oznaka druge strane zapis odbijenog dobiva najviše 32 nepoznate na dan
// po računalu, i kad svaki razgovor donese nove, a ishod razmjene čuva samo
// očišćeno znanje; jedna nepoznata se zapiše
func TestNepoznateOznakeOgradjene(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	p := &Peer{NodeID: "ured-vukovar"}
	oznake := make([]string, 20000)
	for i := range oznake {
		oznake[i] = fmt.Sprintf("x%d/1", i)
	}
	vrste := map[string]int{}
	for i := range 5000 {
		vrste[fmt.Sprintf("vrsta_%d", i)] = 1
	}
	o := ishodRazmjene(0, 0, frontierMsg{Program: "x", Znanje: razmjena.Znanje{Mogucnosti: append(oznake, razmjena.OznakaRupe), Vrste: vrste}}, 0, nil)
	if len(o.znanje.Mogucnosti) != 32 || len(o.znanje.Vrste) != 1024 {
		t.Errorf("u ishodu %d oznaka i %d vrsta", len(o.znanje.Mogucnosti), len(o.znanje.Vrste))
	}
	for krug := range 5 {
		s.zabiljeziIshod(ctxRub, p, syncOutcome{program: "x", znanje: razmjena.Znanje{Mogucnosti: oznake[32*krug : 32*krug+32]}})
	}
	s.zabiljeziIshod(ctxRub, p, syncOutcome{program: "x", znanje: razmjena.Znanje{Mogucnosti: oznake[:32]}})
	var broj int
	if err := s.db.QueryRow(`SELECT sum(broj) FROM razmjena_odbijeno`).Scan(&broj); err != nil {
		t.Fatal(err)
	}
	if n := redakaOdbijenog(t, s); n != najviseOznakaDnevno || broj != 2*najviseOznakaDnevno {
		t.Errorf("%d redaka zapisa odbijenog, %d događaja", n, broj)
	}
	if got := s.oznakeZaZapis(ctxRub, p.NodeID, nil); got != nil {
		t.Errorf("bez oznaka: %v", got)
	}
	pokvariTablicu(t, s, "razmjena_odbijeno")
	if got := s.oznakeZaZapis(ctxRub, p.NodeID, oznake[:1]); got != nil {
		t.Errorf("bez zapisa: %v", got)
	}
	k := rubniCvor(t, "laptop-baranja")
	k.zabiljeziIshod(ctxRub, p, syncOutcome{program: "x", znanje: razmjena.Znanje{Mogucnosti: []string{razmjena.OznakaRazmjena, "stranice/2"}}})
	zapis, err := k.Odbijeno(ctxRub, 1)
	if err != nil || len(zapis) != 1 || zapis[0].Razlog != RazlogOznaka+" stranice/2" {
		t.Errorf("jedna nepoznata oznaka: %+v %v", zapis, err)
	}
}

func redakaOdbijenog(t *testing.T, s *Service) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM razmjena_odbijeno`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Sadržaj se prima samo kad ga je razgovor tražio: netražena stavka s
// ispravnim otiskom i ponovljena stavka se ne upisuju, a vrsta i kanal
// uzimaju se iz vlastite želje. Primljeno se upisuje bez veze, za obje
// strane; pozvani zatim vrati i upravo primljeno, kao u punom razgovoru.
func TestPrimaSeSamoTrazeniSadrzaj(t *testing.T) {
	a, b := cistiPar(t)
	var sp *sadrzaj.Spremiste
	netrazen := make([]byte, 64<<10)
	if _, err := rand.Read(netrazen); err != nil {
		t.Fatal(err)
	}
	drugaVrsta := stavkaSadrzaja{Otisak: otisakTudji, Vrsta: "text/html", Kanal: "dnevnici/3/2026", Bajtova: len(pdfTudji), Podaci: []byte(pdfTudji)}
	stiglo := sadrzajMsg{Stavke: []stavkaSadrzaja{drugaVrsta, drugaVrsta,
		{Otisak: sadrzaj.Otisak(netrazen), Vrsta: "application/pdf", Bajtova: len(netrazen), Podaci: netrazen}}}
	granica := frontierMsg{Frontier: map[string]string{"cop-osijek|ocitanja/16/2026": tijekD1}, Wants: &Wants{All: true}, Program: "0.0.38-alfa"}
	delta := deltaMsg{Versions: []ledger.Version{dolaznaD1}}
	zelje := zeljeMsg{Otisci: []string{otisakCvora, otisakTudji}}
	ishod := []string{"ishod applied=1 sent=1 err=nil",
		`granica {"frontier":{"cop-osijek|ocitanja/16/2026":"` + tijekD1 + `","laptop-baranja|ocitanja/16/2026":"` + tijekV1 + `"},"wants":{"all":true},"program":"0.0.38-alfa"}`,
		obicno, "knjiga v1 d1", "tuđi=true vrsta=application/pdf kanal=prijave/16/2026, netraženi=false, siročadi=1", "<nil>"}
	nakon := func() []string {
		_, vrsta, kanal, err := sp.CitajSKanalom(ctxRub, otisakTudji)
		st, errStanja := sp.Stanje(ctxRub)
		return []string{fmt.Sprintf("tuđi=%v vrsta=%s kanal=%s, netraženi=%v, siročadi=%d", err == nil, vrsta, kanal, sp.Ima(ctxRub, sadrzaj.Otisak(netrazen)), st.Sirocadi), fmt.Sprint(errStanja)}
	}
	pripremi := func() { sp = sSadrzajem(t, b) }
	provjeriSlucajeve(t, a, b, true, []slucaj{{ime: "netraženi sadržaj", pripremi: pripremi, nakon: nakon, ishod: ishod, potezi: []potez{
		cita(granicaV1), salje(kindFrontier, granica),
		cita(deltaV1), salje(kindDelta, delta),
		cita(zeljeCvora), salje(kindZelje, zelje),
		cita(odCvora(kindSadrzaj, `{"stavke":[`+stavkaCvora+`]}`)), salje(kindSadrzaj, stiglo),
		cita(krajCvora(1)), salje(kindDone, doneMsg{Applied: 1}),
		cita(krajVeze),
	}}})
	// tražena stavka čiji se bajtovi ne slažu s najavom ne upiše se
	krivaNajava := drugaVrsta
	krivaNajava.Bajtova++
	bezUpisa := slices.Clone(ishod)
	bezUpisa[4] = "tuđi=false vrsta= kanal=, netraženi=false, siročadi=0"
	provjeriSlucajeve(t, a, b, true, []slucaj{{ime: "kriva najava bajtova", pripremi: pripremi, nakon: nakon, ishod: bezUpisa, potezi: []potez{
		cita(granicaV1), salje(kindFrontier, granica),
		cita(deltaV1), salje(kindDelta, delta),
		cita(zeljeCvora), salje(kindZelje, zelje),
		cita(odCvora(kindSadrzaj, `{"stavke":[`+stavkaCvora+`]}`)), salje(kindSadrzaj, sadrzajMsg{Stavke: []stavkaSadrzaja{krivaNajava}}),
		cita(krajCvora(1)), salje(kindDone, doneMsg{Applied: 1}),
		cita(krajVeze),
	}}})
	provjeriSlucajeve(t, a, b, false, []slucaj{{ime: "netraženi sadržaj", pripremi: pripremi, nakon: nakon, ishod: ishod, potezi: []potez{
		salje(kindFrontier, granica), cita(granicaV1),
		salje(kindDelta, delta), cita(deltaV1),
		salje(kindZelje, zelje), cita(zeljeCvora),
		salje(kindSadrzaj, stiglo), cita(odCvora(kindSadrzaj, `{"stavke":[`+stavkaCvora+`,`+stavkaTudja+`]}`)),
		salje(kindDone, doneMsg{Applied: 1}), cita(krajCvora(1)),
		cita(krajVeze),
	}}})
}

// Stavka čiji se bajtovi ne slažu s najavom ne prima se ni kad je tražena;
// bez ičega traženog ne prima se ništa
func TestTrazeniSadrzajSBajtovimaIzNajave(t *testing.T) {
	r := &razgovorSCvorom{mojiSadrzaji: map[string]sadrzaj.Zelja{"o1": {Otisak: "o1", Vrsta: "application/pdf"}, "o2": {Otisak: "o2"}}}
	got := r.trazeniSadrzaji([]stavkaSadrzaja{
		{Otisak: "o1", Vrsta: "image/jpeg", Bajtova: 99, Podaci: []byte("abc")},
		{Otisak: "o2", Vrsta: "image/jpeg", Bajtova: 3, Podaci: []byte("abc")},
	})
	if len(got) != 1 || got[0].Otisak != "o2" || got[0].Vrsta != "image/jpeg" {
		t.Errorf("primljeno %+v", got)
	}
	if got := (&razgovorSCvorom{}).trazeniSadrzaji([]stavkaSadrzaja{{Otisak: "o1", Bajtova: 3, Podaci: []byte("abc")}}); len(got) != 0 {
		t.Errorf("bez želja primljeno %+v", got)
	}
}

// postaviNajvecuVerziju zada manju ogradu verzije za test
func postaviNajvecuVerziju(t *testing.T, n int) {
	t.Helper()
	stara := najvecaVerzija
	najvecaVerzija = n
	t.Cleanup(func() { najvecaVerzija = stara })
}

// Verzija koju ovaj čvor ne bi mogao poslati dalje, jer je JSON za slanje
// veći od ograde (znakovi < idu kao <), ne upisuje se, nego ide u
// granicu odbijenih; manja verzija ide kao i prije
func TestPrevelikaVerzijaSeNePrima(t *testing.T) {
	postaviNajvecuVerziju(t, 64<<10)
	b := rubniCvor(t, "laptop-baranja")
	r := &razgovorSCvorom{s: b, myWants: Wants{All: true}}
	velika := stalnaVerzija("0190a000-0000-7000-8000-000000000501", "cop-osijek", "ocitanja/16/2026", "r-51", `{"opis":"`+strings.Repeat("<", 20<<10)+`"}`)
	mala := stalnaVerzija("0190a000-0000-7000-8000-000000000502", "cop-osijek", "ocitanja/16/2026", "r-52", `{"opis":"`+strings.Repeat("a", 20<<10)+`"}`)
	upis := r.zaUpis(ctxRub, []ledger.Version{velika, mala})
	if len(upis) != 1 || upis[0].VersionID != mala.VersionID {
		t.Errorf("za upis: %d verzija", len(upis))
	}
	if r.odbijeno["cop-osijek|ocitanja/16/2026"] != velika.VersionID {
		t.Errorf("odbijeno: %v", r.odbijeno)
	}
	if prevelikaVerzija(dolaznaD1) || !prevelikaVerzija(velika) {
		t.Error("ograda verzije mjeri krivo")
	}
}

// Pošiljatelj verziju veću od ograde ne šalje ni samu, nego je vraća kao
// neispravnu, pa granica primatelja prijeđe preko nje; ostale idu
func TestPrevelikaVerzijaSeNeSalje(t *testing.T) {
	postaviNajvecuVerziju(t, 64<<10)
	velika := stalnaVerzija("0190a000-0000-7000-8000-000000000501", "cop-osijek", "ocitanja/16/2026", "r-51", `{"opis":"`+strings.Repeat("&", 20<<10)+`"}`)
	poslati, neispravne, skraceno := ogradiDeltu([]ledger.Version{velika, dolaznaD1, dolaznaD2}, najviseBajtovaDelte)
	if len(poslati) != 2 || len(neispravne) != 1 || neispravne[0].VersionID != velika.VersionID || skraceno {
		t.Errorf("poslati %d, neispravne %d, skraćeno %v", len(poslati), len(neispravne), skraceno)
	}
	if _, err := zaSlanje(velika); !errors.Is(err, errPrevelika) {
		t.Errorf("zaSlanje: %v", err)
	}
}

// cekajuBravu broji gorutine koje čekaju bravu upisa
func cekajuBravu() int {
	buf := make([]byte, 8<<20)
	return strings.Count(string(buf[:runtime.Stack(buf, true)]), "peers.(*Service).uzmiBravu(")
}

// cekajBiljeske čeka da čvor upiše bilješke dolaznih razmjena koje su
// čekale bravu upisa
func cekajBiljeske(t *testing.T, s *Service) {
	t.Helper()
	cekajDa(t, "bilješke na čekanju uzmu bravu", func() bool {
		s.biljeske.mu.Lock()
		defer s.biljeske.mu.Unlock()
		return len(s.biljeske.ceka) == 0
	})
	if err := s.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	s.pustiBravu()
}

// Dok je upis zauzet (npr. dugi uvoz), dolazne razmjene istog računala
// ostave jednu bilješku koja čeka bravu, a ne po jednu za svaku razmjenu;
// kad se brava pusti, upiše se zadnja
func TestBiljeskaDolazneRazmjeneCekaJednom(t *testing.T) {
	c := clanovi(t, "ured-vukovar", "laptop-baranja")
	ured, laptop := c[0], c[1]
	uskladi(t, c...)
	cekajBiljeske(t, ured)
	prije := cekajuBravu()
	if err := ured.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if _, _, err := laptop.SyncWith(ctxRub, ured.node.ID); err != nil {
			ured.pustiBravu()
			t.Fatal(err)
		}
	}
	cekajDa(t, "bilješka čeka bravu", func() bool { return cekajuBravu() > prije })
	time.Sleep(200 * time.Millisecond) // bilješke ostalih razmjena stignu do brave
	ceka := cekajuBravu() - prije
	ured.pustiBravu()
	if ceka != 1 {
		t.Errorf("bravu čeka %d bilješki", ceka)
	}
	cekajBiljeske(t, ured)
	if p, err := ured.GetPeer(ctxRub, laptop.node.ID); err != nil || p.LastSyncNote != "primljeno 0, poslano 0" {
		t.Errorf("bilješka: %+v %v", p, err)
	}
	ured.zabiljeziKasnije(ctxRub, nil, nil, syncOutcome{}) // nepoznato računalo nema bilješke
	ured.noteSync(ctxRub, nil, syncOutcome{})

	// bilješka čija je razmjena prekinuta (gašenje) odustane od brave
	if err := ured.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	otkazan, otkazi := context.WithCancel(ctxRub)
	ured.zabiljeziKasnije(otkazan, &Peer{NodeID: "cop-osijek"}, nil, syncOutcome{applied: 7})
	otkazi()
	cekajDa(t, "prekinuta bilješka odustane", func() bool {
		ured.biljeske.mu.Lock()
		defer ured.biljeske.mu.Unlock()
		return len(ured.biljeske.ceka) == 0
	})
	ured.pustiBravu()
	if st, err := ured.syncStates(ctxRub); err != nil || st["cop-osijek"].Applied != 0 {
		t.Errorf("prekinuta bilješka je upisana: %+v %v", st["cop-osijek"], err)
	}
}

// Čvor koji nije tražio nijedan sadržaj prima samo kratku poruku sadržaja,
// za obje strane; prazan sadržaj prolazi kao i prije (tokPozivatelja)
func TestSadrzajBezZeljaOgradjen(t *testing.T) {
	a, b := cistiPar(t)
	velik := sadrzajMsg{Stavke: []stavkaSadrzaja{{Otisak: otisakTudji, Vrsta: "application/pdf", Bajtova: 100 << 10, Podaci: make([]byte, 100<<10)}}}
	provjeriSlucajeve(t, a, b, true, []slucaj{
		{ime: "sadržaj bez želja", potezi: do(zamijeni(tokPozivatelja(), 7, salje(kindSadrzaj, velik)), 8, cita(krajVeze)), ishod: nakonDelte("poruka razmjene veća od 64 KiB")},
	})
	provjeriSlucajeve(t, a, b, false, []slucaj{
		{ime: "sadržaj bez želja", potezi: do(zamijeni(tokPozvanog(), 6, salje(kindSadrzaj, velik)), 7, cita(krajVeze)), ishod: nakonDelte("poruka razmjene veća od 64 KiB")},
	})
	if r := (&razgovorSCvorom{mojiDijelovi: []string{"d1"}}); r.ogradaSadrzaja() != ogradaPoruke(kindSadrzaj) {
		t.Error("uz tražene dijelove karte ograda sadržaja je manja")
	}
}
