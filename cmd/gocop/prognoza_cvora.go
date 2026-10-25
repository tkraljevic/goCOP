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

	"gocop/internal/db"
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
	"gocop/internal/skok"
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
	pragovi     *skok.Pragovi   // pragovi lažnog skoka, isti kao pri preuzimanju

	dbPath, prognozePut, arhivaPut string
	podaci, nazivCvora             string

	ulogePostavljene bool
	ulogeErr         error
}

// pragoviSkoka su pragovi lažnog skoka iz arhive, kroz vlastitu vezu samo
// za čitanje. Veza ne otvara datoteku dok je ne zatreba: arhiva koje još
// nema daje zadani prag, a kad stigne paketom, pragovi se računaju iz nje.
func pragoviSkoka(arhivaPut string) *skok.Pragovi {
	arhiva, _ := sql.Open("sqlite", db.SamoZaCitanje(arhivaPut)) // upravljač postoji, pa ne pada; bez veze (nil) vrijedi zadani prag
	return skok.NoviPragovi(arhiva)
}

// postaviCitacPrognoza daje poslužitelju bazu prognoza za čitanje. Veza je
// samo za čitanje, pa baze koje nema ne otvori: zove se tek nakon
// pokreniPrognozu, koja bazu napravi.
func postaviCitacPrognoza(server *web.Server, put string) {
	c, err := web.OtvoriPrognoze(put)
	if err != nil {
		log.Printf("Baza prognoza nije pronađena (%s) — grafovi rade bez prognoze", put)
		return
	}
	server.SetPrognoze(c)
	log.Printf("Baza prognoza: %s", put)
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
	ocitanjaRO, err := sql.Open("sqlite", db.SamoZaCitanje(dbPath))
	if err != nil {
		log.Printf("Prognoza se neće obnavljati: očitanja: %v", err)
		return nil
	}
	arhivaRO, err := sql.Open("sqlite", db.SamoZaCitanje(o.arhivaPut))
	if err != nil {
		log.Printf("Prognoza se neće obnavljati: arhiva: %v", err)
		return nil
	}
	osvjezivac := &prognoza.Osvjezivac{Baza: pb, Ocitanja: ocitanjaRO,
		Arhiva: arhivaRO, Najdalje: 96, Model: prognoza.ModelLanac, Cvor: nazivCvora, Pragovi: o.pragovi}
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
	javniUvoznik.Nedovrseno = krug.bezIzdanja
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
		ziva := func() (map[string]prognoza.DnevniNiz, error) {
			return prognoza.OborineOkoSada(ob, time.Now().Unix()/3600, 3, 2)
		}
		oborine, izdano, err := kisaZaStanje(ctx, pb, ziva, peersService.TrenutneUloge().Izdaje)
		if err != nil {
			return nil, nil, err
		}
		// Dnevna istog izdanja kao satna; izdanje bez dnevne nema je.
		dnevne, _ := prognoza.DnevneIzdanja(pb, izdano)
		return prognoza.StanjeSlivova(oborine, pragovi), dnevne, nil
	}
}

// kisaZaStanje vraća kišu po međuslivovima i sat izdanja čija dnevna
// prognoza ide uz nju. Izvoz ranijeg izdanja uzima kišu kakvu je to izdanje
// imalo; inače čvor koji izdaje prognozu uzima živu kišu, a ostali onu iz
// zadnjeg izdanja, jer kišu preuzima izdavač i ona stiže s izdanjem.
func kisaZaStanje(ctx context.Context, pb *sql.DB, ziva func() (map[string]prognoza.DnevniNiz, error),
	izdaje bool) (map[string]prognoza.DnevniNiz, int64, error) {
	if sat, ranije := web.IzdanjeZahtjeva(ctx); ranije {
		kisa, err := prognoza.KisaIzdanja(pb, sat)
		if err == nil && len(kisa) == 0 {
			err = fmt.Errorf("uz izdanje kiša po međuslivovima nije zapisana")
		}
		return kisa, sat, err
	}
	izdano, _, _ := prognoza.ZadnjeIzdanje(pb)
	if izdaje {
		kisa, err := ziva()
		return kisa, izdano, err
	}
	kisa, _, err := prognoza.ZadnjaKisa(pb)
	return kisa, izdano, err
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

// zaboraviDnevneModele baca naučene dnevne modele, pa ih idući krug uči
// iznova; testovi ga zamjenjuju brojačem.
var zaboraviDnevneModele = prognoza.ZaboraviDnevneModele

// Namještanje lanca i provjera unatrag; testovi ih zamjenjuju, jer pravi
// traže punu arhivu
var (
	namjestiLanac   = prognoza.NamjestiLanac
	provjeriUnatrag = prognoza.ProvjeriUnatrag
)

// pripremaModela je priprema modela iz aplikacije (Prognoze → Pripremi
// model): namjesti lanac iz arhive, provjerom unatrag zapiše promašaje i odmah
// izda prognozu novim modelom. Dnevni modeli uče se iznova već idući krug, a
// ne tek iza zagrebačke ponoći.
func pripremaModela(peersService *peers.Service, arhivaRO, pb *sql.DB, javniUvoznik *javnivodostaji.Uvoznik, osvjezivac *prognoza.Osvjezivac, objaviIzdanje func(context.Context, *prognoza.Ishod)) func(ctx context.Context, p *poslovi.Posao) error {
	return func(ctx context.Context, p *poslovi.Posao) error {
		if !peersService.TrenutneUloge().Izdaje {
			return fmt.Errorf("model nije pripremljen: ovaj čvor ne izdaje prognozu, pa model prima razmjenom. Model se priprema na čvoru kojemu je uključeno „Izdaje prognozu” (Administracija → Čvor, mreža i sinkronizacija → Uloge ovog čvora)")
		}
		zaboraviDnevneModele()
		n, m, err := namjestiIProvjeri(arhivaRO, pb, p)
		if err != nil {
			return err
		}
		izdano, err := izdajNovimModelom(ctx, p, javniUvoznik.UTijeku(), osvjezivac, objaviIzdanje)
		if err != nil {
			return err
		}
		p.Zavrsi(opisPripremeModela(n, m, izdano), nil)
		return nil
	}
}

// namjestiIProvjeri namjesti lanac iz arhive i provjerom unatrag zapiše
// promašaje; vraća koliko je pojasa spremljeno i koliko promašaja zapisano
func namjestiIProvjeri(arhivaRO, pb *sql.DB, p *poslovi.Posao) (pojasa, promasaja int, err error) {
	pojasa, err = namjestiLanac(arhivaRO, pb, prognoza.OpcijeNamjestanja{Dnevnik: p, Korak: p.Korak})
	if err != nil {
		return 0, 0, err
	}
	p.Korak("provjera unatrag i zapis promašaja", 0, 0)
	o := prognoza.ZadaneOpcijeProvjere()
	o.Zapisi, o.Dnevnik = true, p
	promasaja, err = provjeriUnatrag(arhivaRO, pb, o)
	return pojasa, promasaja, err
}

// izdajNovimModelom izda prognozu novim modelom i vrati rečenicu o tome za
// završnu poruku pripreme. Ako satni krug upravo traje, novo izdanje
// pričeka idući sat: dva izdavanja istog sata ne idu jedno preko drugog.
func izdajNovimModelom(ctx context.Context, p *poslovi.Posao, krugTraje bool, osvjezivac *prognoza.Osvjezivac,
	objaviIzdanje func(context.Context, *prognoza.Ishod)) (string, error) {
	if krugTraje {
		return "novi model vrijedi od idućeg satnog kruga, jer krug upravo traje", nil
	}
	p.Korak("ponovno izdavanje prognoze", 0, 0)
	iznova := *osvjezivac
	iznova.Iznova = true
	ishod, err := iznova.Osvjezi(ctx)
	if err != nil {
		return "", fmt.Errorf("model je spremljen, ali izdavanje nije uspjelo: %w", err)
	}
	if err := iznova.Zapisi(ishod); err != nil {
		return "", fmt.Errorf("model je spremljen, ali zapis prognoze nije uspio: %w", err)
	}
	objaviIzdanje(ctx, ishod)
	return "prognoza je izdana novim modelom za " +
		time.Unix(ishod.Sada*3600, 0).In(models.Zagreb).Format("2.1. u 15:04"), nil
}
