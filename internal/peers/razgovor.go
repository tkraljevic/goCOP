package peers

// Razgovor razmjene s drugim čvorom, korak po korak. Onaj tko je nazvao
// (pozivatelj) u svakom koraku prvi šalje, a pozvani prvo prima; poruke su u
// obje uloge iste, osim kad je upis zauzet (zavrsiZauzet):
//
//	granice  obje strane kažu dokle znaju, što prate i koje im je izdanje
//	delte    obje pošalju verzije koje druga prati, a nema
//	upis     od primljenoga se pod bravom (upis.go) upiše ono što ovaj čvor prati
//	želje    obje kažu koje otiske sadržaja traže
//	sadržaj  obje daju što od traženoga imaju
//	kraj     obje potvrde koliko su upisale
//
// Stariji čvor bez sadržaja umjesto želja pošalje kraj, i to je kraj
// razgovora bez sadržaja. Isto pošalje pozivatelj kojem je upis zauzet.
//
// Poruke, njihov redoslijed, povrat na svakom izlazu i greške zaključava
// razgovor_tijek_test.go za obje uloge. Red čitanja baze unutar koraka drži
// ono što ostaje zapisano dok drugi razgovori teku usporedo, a test ga ne
// hvata u cijelosti (odluka o Tailscaleu prije granice iz baze prolazi), pa
// ga pregled provjerava okom. Novo ponašanje dodaje se kao novi korak koji
// exchange, ili nakon upisa sadrzajIKraj iza provjere kraja, zove bez
// uvjeta, a ne kao grana u postojećem koraku.

import (
	"context"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// razgovorSCvorom je stanje jednog razgovora i živi samo dok exchange traje.
// Ne sprema se u Service, jer čvor istodobno vodi do osam razgovora.
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
	// neispravno, uz Tailscale adrese iz njezine delte
	theirs frontierMsg
	// applied je upisano, sent poslano; exchange ih vraća tek kad je upis
	// prošao, a do tada 0, 0
	applied, sent int
}

// zapocniRazgovor pročita sve što razgovor treba prije prve poruke: granicu
// i pretplate ovog čvora, izdanje programa i ide li veza preko Tailscalea.
// Kad baza ne odgovori, druga strana ne dobije ništa.
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
	r.moja = r.mojaGranica(mine)
	r.prekoTailscalea = s.prekoTailscalea(c.LocalAddr(), c.RemoteAddr())
	return r, nil
}

// mojaGranica je prva poruka ovog čvora. Wants ide uvijek, i kad čvor prati
// sve: granica bez njega drugoj strani znači da se šalje sve.
func (r *razgovorSCvorom) mojaGranica(mine map[string]string) frontierMsg {
	program, gradnja := r.s.Program()
	return frontierMsg{Frontier: mine, Wants: &r.myWants, Program: program, Gradnja: gradnja}
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
// greška; odbijenica tada nosi razlog druge strane (neocekivanaPoruka).
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

// granice: obje strane kažu dokle znaju. Tuđa granica dekodira se ravno u
// theirs, pa i djelomično dekodirana ide u bilješku o razmjeni.
func (r *razgovorSCvorom) granice() error {
	return r.razmijeni(kindFrontier, r.moja, &r.theirs)
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
func (r *razgovorSCvorom) mojaDelta(ctx context.Context) (deltaMsg, []ledger.Version, error) {
	delta, err := r.s.deltaZa(ctx, r.theirs)
	if err != nil {
		return deltaMsg{}, nil, err
	}
	delta, neispravne, skraceno := ogradiDeltu(delta, najviseBajtovaDelte)
	r.s.zabiljeziNeispravne(neispravne)
	return deltaMsg{Versions: delta, Skraceno: skraceno, Tailscale: r.s.tailscaleZaDrugoga(r.prekoTailscalea)}, neispravne, nil
}

// zapamtiDelte bilježi što su delte donijele. Ide prije upisa: i razgovor
// kojem upis ne uspije vraća pomaknutu granicu i Tailscale adrese.
func (r *razgovorSCvorom) zapamtiDelte(moja, stigla deltaMsg, neispravne []ledger.Version) {
	r.sent = len(moja.Versions)
	r.theirs.tailscale = primljeniTailscale(r.prekoTailscalea, stigla.Tailscale)
	// Bilo koja strana je skratila deltu: ima još, razgovor se ponavlja odmah
	if moja.Skraceno || stigla.Skraceno {
		r.s.skracene.Store(r.c, true)
	}
	// Zapamćena granica druge strane uključuje i ono što joj je upravo
	// poslano; inače bi ploča do sljedeće razmjene brojala poslane verzije
	// kao da još čekaju. Isto vrijedi za verzije koje se ne mogu poslati:
	// one ne čekaju, nego stoje na ploči kao upozorenje.
	r.theirs.Frontier = pomakniGranicu(pomakniGranicu(r.theirs.Frontier, moja.Versions), neispravne)
}

// upisiPrimljeno upiše primljene verzije pod bravom čvora (upis.go). Kad je
// upis predugo zauzet, razgovor završi bez upisa (zavrsiZauzet), a primljeno
// stigne sljedećom razmjenom.
func (r *razgovorSCvorom) upisiPrimljeno(ctx context.Context, stiglo []ledger.Version) error {
	applied, err := r.s.primijeniAkoStigne(ctx, r.zaUpis(ctx, stiglo))
	if err != nil {
		r.s.zavrsiZauzet(r.c, r.initiator, err)
		return err
	}
	r.applied = applied
	return nil
}

// zaUpis su primljene verzije čiji kanal ovaj čvor prati i koje prihvaća
// (Accept). Odabire se prije upisa, jer razgovor bez ičega za upis ne čeka
// bravu.
func (r *razgovorSCvorom) zaUpis(ctx context.Context, stiglo []ledger.Version) []ledger.Version {
	myFilter := r.s.wantsFunc(ctx, &r.myWants)
	wanted := make([]ledger.Version, 0, len(stiglo))
	for _, v := range stiglo {
		if myFilter != nil && !myFilter(v.Channel) {
			continue
		}
		if r.s.accept != nil && !r.s.accept(v) {
			continue
		}
		wanted = append(wanted, v)
	}
	return wanted
}

// sadrzajIKraj: svaka strana kaže koje otiske želi, druga da što od toga
// ima, i obje potvrde kraj. Stariji čvor bez sadržaja umjesto želja pošalje
// kraj; tada je razgovor gotov bez sadržaja. Novi korak nakon upisa ide
// ovamo, iza provjere gotovo, jer druga strana koja je poslala kraj više
// ništa ne prima.
func (r *razgovorSCvorom) sadrzajIKraj(ctx context.Context) error {
	njihove, gotovo, err := r.zelje(ctx)
	if err != nil || gotovo {
		return err
	}
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
