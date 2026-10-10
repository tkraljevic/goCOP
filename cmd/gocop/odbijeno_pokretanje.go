package main

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"gocop/internal/web"
)

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
	defer srv.Close()
	log.Printf("Čvor ne radi, ali pod Postavom ostaje upaljen dok ga ona ne zaustavi, da ga pri nadogradnji ne zamijeni prethodnim izdanjem: ono bi uz ovu bazu napravilo novi ključ, dakle novi identitet. Razlog piše i na http://localhost:%d/ na ovom računalu.",
		ln.Addr().(*net.TCPAddr).Port)
	stop := make(chan os.Signal, 1)
	go cekajZatvaranjeUlaza(ulaz, stop)
	select {
	case <-ctx.Done():
	case <-stop:
	}
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
