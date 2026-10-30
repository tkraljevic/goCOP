package peers

// Razgovor razmjene s drugim čvorom, korak po korak. Onaj tko je nazvao
// (pozivatelj) u svakom koraku prvi šalje, a pozvani prvo prima:
//
//	granice  obje strane kažu dokle znaju, što prate, koje im je izdanje
//	         i što razumiju (razmjena/mogucnosti.go), a uz zajedničke
//	         oznake i dokle su odbile poslano (granica_odbijenih.go) i
//	         što traže (rupe.go)
//	delte    obje pošalju verzije koje druga prati, a nema, i popunu
//	         onoga što traži, uz to za što ne jamče
//	upis     od primljenoga se pod bravom (upis.go) upiše ono što ovaj čvor prati
//	želje    obje kažu koje otiske sadržaja traže
//	sadržaj  obje daju što od traženoga imaju
//	kraj     obje potvrde koliko su upisale
//
// Obje uloge šalju iste poruke, osim u dva slučaja. Prvi je kad druga
// strana umjesto želja pošalje kraj: to radi stariji čvor bez sadržaja i
// pozivatelj kojem je upis zauzet, i razgovor je tada gotov bez sadržaja.
// Drugi je kad je upis zauzet ovom čvoru (zavrsiZauzet).
//
// Poruke, njihov redoslijed, povrat na svakom izlazu i greške zaključava
// razgovor_tijek_test.go za obje uloge. Redoslijed čitanja baze test ne
// zaključava u cijelosti: granica i pretplate čitaju se prije prve poruke,
// delta tek nakon granica, želje nakon upisa verzija, a sadržaj pozvanog
// tek nakon upisa primljenog sadržaja. Premještanje toga pregled mora
// uhvatiti okom.
//
// Novi korak dodaje se kao novi poziv u exchange (ili u sadrzajIKraj, iza
// provjere gotovo), bez uvjeta, a ne kao grana u postojećem koraku. Granica
// odbijenih (G4) i rupe (G4b) su dio koraka granica, a ne novi poziv u
// exchange: idu odmah iza njih (nakonGranica) i samo uz oznake koje objave
// obje strane, pa razgovor sa starijim čvorom ostaje isti.

import (
	"context"
	"log"
	"slices"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// razgovorSCvorom je stanje jednog razgovora i živi samo dok exchange traje.
// Ne sprema se u Service, jer čvor istodobno vodi više razgovora, dolaznih
// i odlaznih.
type razgovorSCvorom struct {
	s *Service
	// c je baš veza koju je exchange dobio: s.skracene je ključan njome, a
	// SyncWith i primiRazmjenu brišu zapis istim ključem
	c         *razmjena.Conn
	initiator bool // ovaj čvor je nazvao, pa u svakom koraku prvi šalje
	// myWants je ono što ovaj čvor prati: ide u granicu, odabire primljeno
	// za upis i bira želje
	myWants Wants
	moja    frontierMsg // granica ovog čvora, složena prije prve poruke
	// prekoTailscalea se odluči jednom, prije prve poruke
	prekoTailscalea bool
	// theirs je granica druge strane; nakon delti pomaknuta za poslano i
	// neispravno i za ono što je druga strana javila da je od ovog čvora
	// odbila, najviše do vlastite granice (doVlastite), uz Tailscale adrese
	// iz njezine delte
	theirs frontierMsg
	// applied je upisano, sent poslano; exchange ih vraća tek kad je upis
	// prošao, a do tada 0, 0
	applied, sent int
	// odbijene je granica odbijenih uzeta prije prve poruke, njihoveOdbijene
	// ono što je druga strana javila da je od ovog čvora odbila, a odbijeno
	// ono što je ovaj čvor odbio u ovom razgovoru (granica_odbijenih.go);
	// pokriveno je ono što je delta ovog čvora nosila (granica_nakon_uvoza.go)
	odbijene        pocetakOdbijenih
	njihoveOdbijene map[string]string
	odbijeno        map[string]string
	pokriveno       map[string]string
	// rupe u nizu (rupe.go): rupe je zapis s početka razgovora, trazeno
	// ono što ovaj čvor traži, a njihovaTrazenja ono što traži druga
	// strana; popunjavam je po čemu je delta složena i što je u njoj
	// popuna; njihoveRupe i njihovaPopuna su iz delte druge strane; bacio
	// je po ključu najmanja i najveća verzija koju je ovaj čvor odbio, a
	// preskoceni ključevi koje bezPreskocenog nije upisao
	rupe                       pocetakRupa
	trazeno, njihovaTrazenja   map[string]raspon
	popunjavam                 popunaDelte
	njihoveRupe, njihovaPopuna map[string]raspon
	bacio                      map[string]baceno
	preskoceni                 map[string]string
}

// zapocniRazgovor pročita sve što razgovor treba prije prve poruke: granicu
// i pretplate ovog čvora, izdanje programa, što čvor zna, ide li veza
// preko Tailscalea, granicu odbijenih od druge strane i, nakon granice,
// zapis rupa (rupe.go). Kad granica ili pretplate ne odgovore, druga strana
// ne dobije ništa.
func (s *Service) zapocniRazgovor(ctx context.Context, c *razmjena.Conn, initiator bool) (*razgovorSCvorom, error) {
	mine, err := s.rec.Frontier(ctx)
	if err != nil {
		return nil, err
	}
	myWants, err := s.CurrentWants(ctx)
	if err != nil {
		return nil, err
	}
	r := &razgovorSCvorom{s: s, c: c, initiator: initiator, myWants: myWants}
	r.moja = r.mojaGranica(mine, s.mojeZnanje(ctx))
	r.prekoTailscalea = s.prekoTailscalea(c.LocalAddr(), c.RemoteAddr())
	r.odbijene = s.pocetakOdbijenih(razmjena.PublicKeyString(c.PeerKey), mine)
	r.rupe = s.pocetakRupa(ctx)
	r.trazeno = r.rupe.trazim(s.wantsFunc(ctx, &myWants))
	return r, nil
}

// mojaGranica je prva poruka ovog čvora. Wants ide uvijek, i kad čvor prati
// sve: granica bez njega drugoj strani znači da se šalje sve.
func (r *razgovorSCvorom) mojaGranica(mine map[string]string, znanje razmjena.Znanje) frontierMsg {
	program, gradnja := r.s.Program()
	return frontierMsg{Frontier: mine, Wants: &r.myWants, Program: program, Gradnja: gradnja, Znanje: znanje}
}

// mojeZnanje je ono što ovaj čvor javlja u granici: svoje oznake, vrste
// zapisa koje razumije i koliko zapisa kod njega čeka nadogradnju. Broj je
// samo obavijest, pa greška brojanja nije greška razgovora: broja tada nema,
// a razlog ide u dnevnik.
func (s *Service) mojeZnanje(ctx context.Context) razmjena.Znanje {
	ceka, err := s.rec.CekaNadogradnju(ctx)
	if err != nil {
		log.Printf("razmjena: koliko zapisa čeka nadogradnju nije izbrojeno: %v", err)
	}
	return razmjena.Znanje{Mogucnosti: s.mojeMogucnosti(), Vrste: s.mojKatalog(), CekaNadogradnju: ceka,
		Uloge: s.TrenutneUloge().Oznake()}
}

// mojeMogucnosti su oznake koje ovaj program objavljuje
// (razmjena.NaseMogucnosti), uz web/1 dok računalo ima uključen WEB sync
// (priprema_weba.go), ili oznake koje je čvoru dao test (s.mogucnosti)
func (s *Service) mojeMogucnosti() []string {
	if s.mogucnosti != nil {
		return s.mogucnosti()
	}
	return s.uzWeb(razmjena.NaseMogucnosti())
}

// uzWeb doda oznaku web/1 dok računalo ima uključen WEB sync, poredano.
// Uz web_sync = false oznake nema (Odluke (25. 10. 2026.)): nitko ga tada
// ne zove, a računalo s 0.0.42 ne vidi novu oznaku dok proba ne krene.
func (s *Service) uzWeb(oznake []string) []string {
	if ws := s.web.Load(); ws == nil || !ws.ukljucen {
		return oznake
	}
	oznake = append(oznake, razmjena.OznakaWeb)
	slices.Sort(oznake)
	return oznake
}

// mojKatalog su vrste zapisa sa shemama koje ovaj program razumije
// (ledger.Katalog), ili katalog koji je čvoru dao test (s.katalog)
func (s *Service) mojKatalog() map[string]int {
	if s.katalog != nil {
		return s.katalog()
	}
	return ledger.Katalog()
}

// posalji pošalje poruku s oznakom ovog čvora (deviceId). Jedino odbijenica
// pozvanog kad je upis zauzet ide bez nje (zavrsiZauzet).
func (r *razgovorSCvorom) posalji(kind string, v any) error {
	e, err := razmjena.NewEnvelope(kind, v)
	if err != nil {
		return err
	}
	e.DeviceID = r.s.node.ID
	return r.c.Send(e)
}

// ocekuj primi sljedeću poruku i dekodira je u v. Druga vrsta poruke je
// greška; odbijenica tada nosi razlog i kod druge strane (neocekivanaPoruka).
func (r *razgovorSCvorom) ocekuj(kind string, v any) error {
	e, err := r.c.Receive()
	if err != nil {
		return err
	}
	if e.Kind != kind {
		return neocekivanaPoruka(kind, e)
	}
	return e.Decode(v)
}

// redom odradi slanje i primanje istog koraka redom uloge: pozivatelj prvo
// šalje, pozvani prvo prima. Kad prvo ne uspije, drugo se ne radi.
func (r *razgovorSCvorom) redom(salje, prima func() error) error {
	prvo, drugo := salje, prima
	if !r.initiator {
		prvo, drugo = prima, salje
	}
	if err := prvo(); err != nil {
		return err
	}
	return drugo()
}

// razmijeni pošalje svoju i primi tuđu poruku iste vrste, redom uloge
func (r *razgovorSCvorom) razmijeni(kind string, moja, njihova any) error {
	return r.redom(
		func() error { return r.posalji(kind, moja) },
		func() error { return r.ocekuj(kind, njihova) },
	)
}

// granice: obje strane kažu dokle znaju, a uz zajedničke oznake odmah zatim
// i dokle su odbile poslano i što traže (nakonGranica). Tuđa granica
// dekodira se ravno u theirs, pa i djelomično dekodirana ide u bilješku o
// razmjeni.
func (r *razgovorSCvorom) granice() error {
	if err := r.razmijeni(kindFrontier, r.moja, &r.theirs); err != nil {
		return err
	}
	return r.nakonGranica()
}

// delte: svaka strana pošalje ono što druga prati, a nema, i vrati primljene
// verzije za upis
func (r *razgovorSCvorom) delte(ctx context.Context) ([]ledger.Version, error) {
	moja, neispravne, err := r.mojaDelta(ctx)
	if err != nil {
		return nil, err
	}
	var stigla deltaMsg
	if err = r.razmijeni(kindDelta, moja, &stigla); err != nil {
		return nil, err
	}
	r.zapamtiDelte(moja, stigla, neispravne)
	return stigla.Versions, nil
}

// mojaDelta slaže deltu za drugu stranu, i kod pozvanog prije nego što
// stigne tuđa: neispravne verzije zabilježe se i kad razmjena delti padne.
// Šalje se samo što drugi prati; što ovaj čvor ne prati, ne prima ni
// omaškom (zaUpis), jer bi ostalo bez granice i vraćalo se svakom razmjenom.
// Ne šalje se ni ono što je druga strana javila da je odbila (granicaZaDeltu).
// Na kraju ide popuna rupa koje je druga strana zatražila, a uz deltu što
// ovaj čvor o poslanome jamči (deltaSPopunom, jamstvoDelte).
func (r *razgovorSCvorom) mojaDelta(ctx context.Context) (deltaMsg, []ledger.Version, error) {
	delta, err := r.deltaSPopunom(ctx, r.granicaZaDeltu())
	if err != nil {
		return deltaMsg{}, nil, err
	}
	delta, neispravne, skraceno := ogradiDeltu(delta, najviseBajtovaDelte)
	r.s.zabiljeziNeispravne(neispravne)
	rupe, popuna := r.jamstvoDelte(delta, neispravne)
	return deltaMsg{Versions: delta, Skraceno: skraceno, Tailscale: r.s.tailscaleZaDrugoga(r.prekoTailscalea), Rupe: rupe, Popuna: popuna}, neispravne, nil
}

// zapamtiDelte bilježi što su delte donijele. Ide prije upisa: i razgovor
// kojem upis ne uspije vraća pomaknutu granicu i Tailscale adrese.
func (r *razgovorSCvorom) zapamtiDelte(moja, stigla deltaMsg, neispravne []ledger.Version) {
	r.sent = len(moja.Versions)
	r.pokriveno = pokriveno(moja, neispravne)
	r.theirs.tailscale = primljeniTailscale(r.prekoTailscalea, stigla.Tailscale)
	r.zapamtiJamstvo(stigla)
	// Bilo koja strana je skratila deltu: ima još, razgovor se ponavlja odmah
	if moja.Skraceno || stigla.Skraceno {
		r.s.skracene.Store(r.c, true)
	}
	// Zapamćena granica druge strane uključuje i ono što joj je upravo
	// poslano; inače bi ploča do sljedeće razmjene brojala poslane verzije
	// kao da još čekaju. Isto vrijedi za verzije koje se ne mogu poslati:
	// one ne čekaju, nego stoje na ploči kao upozorenje.
	r.theirs.Frontier = pomakniGranicu(pomakniGranicu(r.theirs.Frontier, moja.Versions), neispravne)
	// Ne čeka ni ono što je druga strana javila da je odbila, ali samo do
	// vlastite granice (doVlastite). To je samo za ploču: u knjigu i
	// drugima ne ide.
	r.theirs.Frontier = spojiGranice(r.theirs.Frontier, doVlastite(r.njihoveOdbijene, r.moja.Frontier))
}

// upisiPrimljeno upiše primljene verzije pod bravom čvora (upis.go). Kad je
// upis predugo zauzet, razgovor završi bez upisa (zavrsiZauzet), a primljeno
// stigne sljedećom razmjenom. Ne upisuje se ni ono što bi granicu knjige
// pomaknulo preko verzija koje je druga strana preskočila po granici
// odbijenih, a ovaj čvor ih sad možda drži (bezPreskocenog). Pod istom
// bravom, prije upisa, trajno se zapiše što je odbijeno i stiglo s rupom
// (promjenaRupa). Tek kad je upis prošao, ovaj čvor zapamti što je odbio
// (zapamtiOdbijene), i to prije kraja razgovora, pa ga već sljedeći
// razgovor javlja.
func (r *razgovorSCvorom) upisiPrimljeno(ctx context.Context, stiglo []ledger.Version) error {
	wanted := r.bezPreskocenog(r.zaUpis(ctx, stiglo))
	applied, err := r.s.primijeniAkoStigne(ctx, wanted, r.promjenaRupa(wanted))
	if err != nil {
		r.s.zavrsiZauzet(r.c, r.initiator, err)
		return err
	}
	r.applied = applied
	r.s.zapamtiOdbijene(r.odbijene, r.odbijeno)
	return nil
}

// zaUpis su primljene verzije čiji kanal ovaj čvor prati i koje prihvaća
// (Accept). Odabire se prije upisa, jer razgovor bez ičega za upis ne čeka
// bravu. Što odbije Accept, zapamti se za granicu odbijenih (odbaci); što
// otpadne na pretplatama, ne: kanal koji se počne pratiti mora dobiti sve.
func (r *razgovorSCvorom) zaUpis(ctx context.Context, stiglo []ledger.Version) []ledger.Version {
	myFilter := r.s.wantsFunc(ctx, &r.myWants)
	wanted := make([]ledger.Version, 0, len(stiglo))
	for _, v := range stiglo {
		if myFilter != nil && !myFilter(v.Channel) {
			continue
		}
		if r.s.accept != nil && !r.s.accept(v) {
			r.odbaci(v)
			continue
		}
		wanted = append(wanted, v)
	}
	return wanted
}

// sadrzajIKraj: svaka strana kaže koje otiske želi, druga da što od toga
// ima, i obje potvrde kraj. Stariji čvor bez sadržaja umjesto želja pošalje
// kraj; tada je razgovor gotov bez sadržaja. Novi korak nakon upisa ide
// ovamo, iza provjere gotovo, jer druga strana koja je poslala kraj čeka još
// samo naš kraj. Želje druga strana šalje tek kad je upisala primljeno, pa
// se tek tada pamti što je nakon uvoza dobila (zapamtiUvoz).
func (r *razgovorSCvorom) sadrzajIKraj(ctx context.Context) error {
	njihove, gotovo, err := r.zelje(ctx)
	if err != nil || gotovo {
		return err
	}
	r.s.zapamtiUvoz(r.odbijene, r.pokriveno)
	if err = r.sadrzaj(ctx, njihove); err != nil {
		return err
	}
	return r.kraj()
}

// zelje razmijeni želje i javi je li druga strana umjesto njih poslala kraj.
// Primjena verzija je već upisala želje, pa se ovdje traže i one koje su
// upravo nastale. Želje se slože prije nego što stigne tuđa poruka, pa i
// kad druga strana umjesto želja pošalje kraj ili pukne: odabrani otisci
// označe se zatraženima, a to mijenja poredak želja sljedeće razmjene.
func (r *razgovorSCvorom) zelje(ctx context.Context) (zeljeMsg, bool, error) {
	moje := zeljeMsg{r.s.zeljeniOtisci(ctx, r.myWants)}
	if r.initiator {
		if err := r.posalji(kindZelje, moje); err != nil {
			return zeljeMsg{}, false, err
		}
		return r.zeljeIliKraj()
	}
	njihove, gotovo, err := r.zeljeIliKraj()
	if err != nil || gotovo {
		return njihove, gotovo, err
	}
	return njihove, false, r.posalji(kindZelje, moje)
}

// zeljeIliKraj prima želje druge strane. Kraj umjesto želja šalje stariji
// čvor bez sadržaja (pozivatelj nakon delte, pozvani na primljene želje) i
// pozivatelj kojem je upis bio zauzet (zavrsiZauzet). Na kraj se odgovori
// krajem i razgovor je uspješno gotov; na tome stoji TestZauzetUpisPozivatelja.
func (r *razgovorSCvorom) zeljeIliKraj() (zeljeMsg, bool, error) {
	var njihove zeljeMsg
	e, err := r.c.Receive()
	if err != nil {
		return njihove, false, err
	}
	if e.Kind == kindDone {
		// broj upisanih kod druge strane nitko ne čita, a druga strana je
		// svoje završila, pa razgovor uspije i kad odgovor ne stigne
		var njihovKraj doneMsg
		_ = e.Decode(&njihovKraj)
		_ = r.posalji(kindDone, doneMsg{r.applied})
		return njihove, true, nil
	}
	if e.Kind != kindZelje {
		return njihove, false, neocekivanaPoruka(kindZelje, e)
	}
	// pokvarene želje ne prekidaju razgovor; daje se što se iz njih da pročitati
	_ = e.Decode(&njihove)
	return njihove, false, nil
}

// sadrzaj: svaka strana da što od traženoga ima, po pretplatama druge
// strane. Pozivatelj svoje složi i pošalje prije nego primi tuđe; pozvani
// prvo upiše primljeno, a tek onda složi svoje, jer se sadržaj slaže tek u
// koraku slanja.
func (r *razgovorSCvorom) sadrzaj(ctx context.Context, njihove zeljeMsg) error {
	return r.redom(
		func() error {
			return r.posalji(kindSadrzaj, sadrzajMsg{r.s.sadrzajZa(ctx, njihove.Otisci, r.theirs.Wants)})
		},
		func() error { return r.primiNjihovSadrzaj(ctx) },
	)
}

// primiNjihovSadrzaj prima sadržaj druge strane i upiše ga kad brava stigne
// na vrijeme; što nije upisano, ostaje među željama i traži se sljedećom
// razmjenom
func (r *razgovorSCvorom) primiNjihovSadrzaj(ctx context.Context) error {
	var stigao sadrzajMsg
	if err := r.ocekuj(kindSadrzaj, &stigao); err != nil {
		return err
	}
	r.s.primiSadrzajAkoStigne(ctx, razmjena.PublicKeyString(r.c.PeerKey), stigao.Stavke)
	return nil
}

// kraj: obje strane potvrde koliko su upisale. Pozivatelju je neuspjelo
// slanje kraja greška razgovora. Pozvani je tada već sve poslao, pa mu kraj
// ne može pasti; tuđi kraj ne čita nitko.
func (r *razgovorSCvorom) kraj() error {
	var njihovKraj doneMsg
	if r.initiator {
		if err := r.posalji(kindDone, doneMsg{r.applied}); err != nil {
			return err
		}
		_ = r.ocekuj(kindDone, &njihovKraj)
		return nil
	}
	_ = r.ocekuj(kindDone, &njihovKraj)
	_ = r.posalji(kindDone, doneMsg{r.applied})
	return nil
}
