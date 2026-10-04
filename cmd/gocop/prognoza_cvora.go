package main

// Obnova prognoze na čvoru koji je izdaje: satni krug uz javne vodostaje,
// oborine i kišomjeri, kiša po slivovima, pričuvni Excel i priprema modela iz
// aplikacije.

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"gocop/internal/hidroview"
	"gocop/internal/javnivodostaji"
	"gocop/internal/kisomjeri"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/oborine"
	"gocop/internal/peers"
	"gocop/internal/poslovi"
	"gocop/internal/prognoza"
	"gocop/internal/repository"
	"gocop/internal/web"
)

// ovisnostiPrognoze je ono što obnova prognoze treba od ostatka čvora
type ovisnostiPrognoze struct {
	baza        *sql.DB
	knjiga      *ledger.Recorder
	mreza       *peers.Service
	cvor        *peers.Node
	posluzitelj *web.Server
	uvoznik     *javnivodostaji.Uvoznik
	razmjenaArh *razmjenaArhive
	ctx         context.Context // razmjena; gasi prorjeđivanje izdanja

	dbPath, prognozePut, arhivaPut string
	podaci, nazivCvora             string

	ulogePostavljene bool
	ulogeErr         error
}

// pokreniPrognozu: prognoza se obnavlja čim stignu novi vodostaji, a ne po
// vlastitom satu — inače bi pola vremena stajala na starim brojkama a
// izgledala kao da je današnja. Kad baze prognoza nema ili je prazna,
// poslužitelj radi kao i dosad, samo bez prognoze, i vraća se nil; inače
// baza prognoza.
func pokreniPrognozu(o ovisnostiPrognoze) *sql.DB {
	database, recorder, peersService, node, server := o.baza, o.knjiga, o.mreza, o.cvor, o.posluzitelj
	javniUvoznik, razmjenaArh, syncCtx := o.uvoznik, o.razmjenaArh, o.ctx
	dbPath, nazivCvora, ulogePostavljene, ulogeErr := o.dbPath, o.nazivCvora, o.ulogePostavljene, o.ulogeErr
	pb, err := prognoza.Otvori(o.prognozePut)
	if err != nil {
		log.Printf("Prognoza se neće obnavljati: %v", err)
		return nil
	}
	ocitanjaRO, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		log.Printf("Prognoza se neće obnavljati: očitanja: %v", err)
		return nil
	}
	arhivaRO, err := sql.Open("sqlite", o.arhivaPut+"?mode=ro")
	if err != nil {
		log.Printf("Prognoza se neće obnavljati: arhiva: %v", err)
		return nil
	}
	osvjezivac := &prognoza.Osvjezivac{Baza: pb, Ocitanja: ocitanjaRO,
		Arhiva: arhivaRO, Najdalje: 96, Model: prognoza.ModelLanac, Cvor: nazivCvora}
	// Izdanje putuje razmjenom: ostali čvorovi ga upišu u svoju bazu
	// prognoza, a namješteni model ide uz njega kad se promijeni.
	var oborineDB *sql.DB // baza oborina; kiša ide uz izdanje
	objaviIzdanje := func(ctx context.Context, ishod *prognoza.Ishod) {
		if err := objaviPrognozu(ctx, database, recorder, pb, oborineDB, ishod, nazivCvora); err != nil {
			log.Printf("prognoza: slanje razmjenom: %v", err)
		}
	}
	if !ulogePostavljene && ulogeErr == nil {
		zadrziUlogeOdPrije(pb, peersService)
	}
	primateljPrognoze.postavi(pb, recorder, func() bool { return peersService.TrenutneUloge().Izdaje })
	peersService.NaPromjenuUloga(func(u peers.Uloge) {
		if !u.Izdaje {
			primateljPrognoze.nadoknadi()
		}
	})
	go prorjedjujIzdanja(syncCtx, recorder)
	// Pričuvni Excel: veze naših letvi s mađarskim i srpskim iz arhive, uz
	// predupis zadnjih tuđih prognoza — za dan kad naša prognoza ne radi.
	server.SetPricuvno(func(ctx context.Context) prognoza.PricuvniPodaci {
		return prognoza.PricuvniIzracun(ctx, arhivaRO, ocitanjaRO, pb, time.Now(), models.Zagreb)
	})
	server.SetKisaSlivova(kisaSlivova(osvjezivac, arhivaRO, pb, peersService))
	oborineUvoznik, kisUvoznik, ob := pripremiOborine(database, dbPath, node, server, osvjezivac)
	oborineDB = ob
	krug := &satniKrug{
		uvoznik: javniUvoznik, mreza: peersService, baza: database, prognoze: pb,
		osvjezivac: osvjezivac, oborine: oborineUvoznik, kisomjeri: kisUvoznik,
		posluzitelj: server, razmjenaArh: razmjenaArh, podaci: o.podaci, objavi: objaviIzdanje,
	}
	javniUvoznik.NakonPreuzimanja = krug.vrti
	server.SetPripremaModela(pripremaModela(peersService, arhivaRO, pb, javniUvoznik, osvjezivac, objaviIzdanje))
	return pb
}

// zadrziUlogeOdPrije: čvor od prije uloga zadrži što je radio — ako je
// zadnjih tjedan dana izdavao prognozu, i dalje preuzima i izdaje. Novi čvor
// ne radi ni jedno dok mu se uloga ne uključi u Postavkama.
func zadrziUlogeOdPrije(pb *sql.DB, peersService *peers.Service) {
	var zadnje sql.NullInt64
	_ = pb.QueryRow(`SELECT max(nastalo) FROM izdanja WHERE knjiga = ''`).Scan(&zadnje)
	radio := zadnje.Valid && time.Since(time.Unix(zadnje.Int64, 0)) < 7*24*time.Hour
	if err := peersService.PostaviUloge(context.Background(), peers.Uloge{Preuzima: radio, Izdaje: radio}); err != nil {
		log.Printf("Uloge čvora nisu zapisane: %v", err)
	}
}

// kisaSlivova je kiša po slivovima na naslovnoj: palo i očekivano prema
// uobičajenom za međusliv (ERA5), s letvama na kojima će porasti voda.
func kisaSlivova(osvjezivac *prognoza.Osvjezivac, arhivaRO, pb *sql.DB, peersService *peers.Service) func(ctx context.Context) ([]prognoza.StanjeSliva, map[string][]prognoza.DnevnaIzdana, error) {
	return func(ctx context.Context) ([]prognoza.StanjeSliva, map[string][]prognoza.DnevnaIzdana, error) {
		ob := osvjezivac.Oborine
		if ob == nil {
			return nil, nil, fmt.Errorf("oborina po međuslivovima se na ovom čvoru ne preuzima")
		}
		pragovi, err := prognoza.PragoviSlivova(arhivaRO, ob.Tocke)
		if err != nil {
			return nil, nil, err
		}
		var oborine map[string]prognoza.DnevniNiz
		if peersService.TrenutneUloge().Izdaje {
			oborine, err = prognoza.OborineOkoSada(ob, time.Now().Unix()/3600, 3, 2)
		} else {
			// Kišu preuzima čvor koji izdaje prognozu; stiže s izdanjem.
			oborine, _, err = prognoza.ZadnjaKisa(pb)
		}
		if err != nil {
			return nil, nil, err
		}
		_, dnevne, _ := prognoza.ZadnjeDnevno(pb)
		return prognoza.StanjeSlivova(oborine, pragovi), dnevne, nil
	}
}

// pripremiOborine otvori bazu oborina uz bazu prognoza: kišomjeri iz
// registra slivova, živi sati s Open-Meteo i mjerenja stvarnih kišomjera.
// Bez nje dnevni model radi kao i prije, bez oborine, a vraća se nil.
func pripremiOborine(database *sql.DB, dbPath string, node *peers.Node, server *web.Server, osvjezivac *prognoza.Osvjezivac) (oborineUvoznik *oborine.Uvoznik, kisUvoznik *kisomjeri.Uvoznik, oborineDB *sql.DB) {
	if ob, err := oborine.Otvori(filepath.Join(filepath.Dir(dbPath), "oborine.db")); err != nil {
		log.Printf("Oborine se neće preuzimati: %v", err)
	} else {
		oborineUvoznik = &oborine.Uvoznik{DB: ob, Tocke: oborine.TockeIzRegistra(database)}
		oborineDB = ob
		primateljPrognoze.postaviOborine(ob)
		// Stvarni kišomjeri: mjerenja se skupljaju u istu radnu bazu, a
		// jednom dnevno ulažu u arhivsko stablo. pljusak.com je javan;
		// DHMZ se čita s letva.voda.hr, koja traži prijavu računom domene
		// Hrvatskih voda — istim računom čvora kao mletva.voda.hr
		// (ime kao za e-poštu, bez "@voda.hr"), zaključanim ključem čvora.
		kisSpremiste := &kisomjeri.Spremiste{DB: ob}
		letvaRacun := racunSustava(repository.NewRacuniSustavaRepository(database),
			hidroview.Kljuc(node.PrivateKey().Seed()), "letva.voda.hr")
		if err := kisSpremiste.Pripremi(); err != nil {
			log.Printf("Stvarni kišomjeri se neće skupljati: %v", err)
		} else {
			kisUvoznik = &kisomjeri.Uvoznik{Spremiste: kisSpremiste, Postaje: kisomjeri.PostajeIzRegistra(database),
				Citaci: map[string]kisomjeri.Citac{"pljusak": &kisomjeri.Pljusak{}, "dhmz": &kisomjeri.Letva{Racun: letvaRacun}}}
			server.SetKisomjeriMjerenja(kisSpremiste)
		}
		if tocke, err := prognoza.OborinskeTocke(database); err != nil {
			log.Printf("Oborine: registar: %v", err)
		} else if len(tocke) > 0 {
			osvjezivac.Oborine = &prognoza.OborinskiIzvor{Tocke: tocke, Satne: oborineUvoznik.Satne}
		}
	}
	return oborineUvoznik, kisUvoznik, oborineDB
}

// pripremaModela je priprema modela iz aplikacije (Prognoze → Pripremi
// model): namjesti lanac iz arhive, provjerom unatrag zapiše promašaje i odmah
// izda prognozu novim modelom. Ako satni krug upravo traje, novo izdanje
// pričeka idući sat — dva izdavanja istog sata ne idu jedno preko drugog.
func pripremaModela(peersService *peers.Service, arhivaRO, pb *sql.DB, javniUvoznik *javnivodostaji.Uvoznik, osvjezivac *prognoza.Osvjezivac, objaviIzdanje func(context.Context, *prognoza.Ishod)) func(ctx context.Context, p *poslovi.Posao) error {
	return func(ctx context.Context, p *poslovi.Posao) error {
		if !peersService.TrenutneUloge().Izdaje {
			return fmt.Errorf("model priprema čvor koji izdaje prognozu (Postavke → Čvor); ovaj ga prima razmjenom")
		}
		n, err := prognoza.NamjestiLanac(arhivaRO, pb, prognoza.OpcijeNamjestanja{Dnevnik: p, Korak: p.Korak})
		if err != nil {
			return err
		}
		p.Korak("provjera unatrag i zapis promašaja", 0, 0)
		o := prognoza.ZadaneOpcijeProvjere()
		o.Zapisi, o.Dnevnik = true, p
		m, err := prognoza.ProvjeriUnatrag(arhivaRO, pb, o)
		if err != nil {
			return err
		}
		izdano := "novi model vrijedi od idućeg satnog kruga, jer krug upravo traje"
		if !javniUvoznik.UTijeku() {
			p.Korak("ponovno izdavanje prognoze", 0, 0)
			iznova := *osvjezivac
			iznova.Iznova = true
			ishod, err := iznova.Osvjezi(ctx)
			if err != nil {
				return fmt.Errorf("model je spremljen, ali izdavanje nije uspjelo: %w", err)
			}
			if err := iznova.Zapisi(ishod); err != nil {
				return fmt.Errorf("model je spremljen, ali zapis prognoze nije uspio: %w", err)
			}
			objaviIzdanje(ctx, ishod)
			izdano = "prognoza je izdana novim modelom za " +
				time.Unix(ishod.Sada*3600, 0).In(models.Zagreb).Format("2.1. u 15:04")
		}
		p.Zavrsi(fmt.Sprintf("Namješteno %d pojasa, zapisano %d promašaja; %s.", n, m, izdano), nil)
		return nil
	}
}
