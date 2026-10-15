package main

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"gocop/internal/config"
	"gocop/internal/peers"
	"gocop/internal/web"
)

// provjeriKljuc je provjera ključa čvora pri pokretanju
// (peers.ProvjeriKljucCvora) uz ono što zna samo pokretanje: nedostaje li
// uz bazu i gocop.toml, a s njim i ime čvora (Unraid, 5. 10. 2026.: mover
// je odnio oboje). Tada odbijanje kaže i za postavke, a novi identitet uz
// zastavicu javlja pod kojim imenom čvor kreće.
func provjeriKljuc(cfg config.Config, cfgFrom string, dopustiNovi bool) error {
	noviIdentitet, err := peers.ProvjeriKljucCvora(cfg.DB, dopustiNovi)
	if cfgFrom != "" {
		return err
	}
	postavke := filepath.Join(filepath.Dir(cfg.DB), config.FileName)
	var nema *peers.KljucNedostaje
	if errors.As(err, &nema) {
		nema.Postavke = postavke
	}
	if noviIdentitet {
		log.Printf("Ime čvora: uz bazu nema ni postavki %s, pa čvor s novim ključem kreće pod imenom %s. Ako je u mreži bio pod drugim imenom, odmah ga zaustavite, vratite postavke ili ih napišite s tim imenom (redak id = \"…\" pod [node]) i ponovno ga pokrenite, prije ponovnog primanja u mrežu.",
			postavke, cfg.Node.ID)
	}
	return err
}

// odbijPokretanje završava pokretanje koje provjera nije pustila (npr.
// postojeća baza bez ključa čvora): razlog ide u dnevnik, a izlazni kod je
// 1. Pod Postavom (-upravitelj) čvor najprije ostane upaljen dok ga ona ne
// zaustavi (cekajOdbijen); vidi web.OdbijenoPokretanje.
func odbijPokretanje(ctx context.Context, podPostavom bool, addr string, razlog error, ulaz io.Reader) int {
	log.Print(razlog)
	if podPostavom {
		cekajOdbijen(ctx, addr, razlog.Error(), ulaz)
	}
	return 1
}

// cekajOdbijen drži odbijeni čvor upaljenim dok Postava ne zatvori ulaz ili
// dok ctx traje: na portu web sučelja, samo na ovom računalu, /zdravlje
// odgovara izdanjem, a ploča kaže zašto čvor ne radi. Baza se ne otvara i
// ništa se ne zapisuje. Kad ni port nije slobodan, odmah se vraća.
func cekajOdbijen(ctx context.Context, addr, razlog string, ulaz io.Reader) {
	portovi, err := portoviOdbijenog(addr)
	var ln net.Listener
	if err == nil {
		ln, err = slusajNaOvomRacunalu(portovi)
	}
	if err != nil {
		log.Printf("Postava će čvor vidjeti kao pao: port web sučelja %q nije dostupan (%v)", addr, err)
		return
	}
	srv := &http.Server{Handler: web.OdbijenoPokretanje(razlog), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	log.Printf("Čvor ne radi, ali pod Postavom ostaje upaljen dok ga ona ne zaustavi, da ga pri nadogradnji ne zamijeni prethodnim izdanjem: ono bi uz ovu bazu napravilo novi ključ, dakle novi identitet. Razlog piše i na http://localhost:%d/ na ovom računalu.",
		ln.Addr().(*net.TCPAddr).Port)
	stop := make(chan os.Signal, 1)
	go cekajZatvaranjeUlaza(ulaz, stop)
	select {
	case <-ctx.Done():
	case <-stop:
	}
	_ = srv.Close() // čvor ionako izlazi; greška zatvaranja ništa ne mijenja
	// Close ne zatvara slušatelja kojeg Serve još nije preuzeo (gašenje odmah
	// nakon pokretanja), pa bi port ostao otvoren
	_ = ln.Close()
}

// portoviOdbijenog su portovi na kojima Postava pita /zdravlje: port iz
// adrese web sučelja, a uz zadani 80 i rezervni 8080, na koji i čvor sam
// prelazi kad je 80 zauzet
func portoviOdbijenog(addr string) ([]string, error) {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if port == "80" {
		return []string{"80", "8080"}, nil
	}
	return []string{port}, nil
}

// slusajNaOvomRacunalu otvara prvi slobodan od portova na 127.0.0.1, gdje
// ga pita Postava; iz mreže odbijeni čvor nije dostupan
func slusajNaOvomRacunalu(portovi []string) (ln net.Listener, err error) {
	for _, port := range portovi {
		if ln, err = net.Listen("tcp", net.JoinHostPort("127.0.0.1", port)); err == nil {
			return ln, nil
		}
	}
	return nil, err
}
