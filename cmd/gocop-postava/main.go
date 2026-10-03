// goCOP Postava: instalira goCOP, nadograđuje ga, pali i gasi čvor i drži
// ikonu u traci (docs/plan-instalacija.md).
//
// Mali, stabilan program s vlastitim izdanjima (oznake postava-v*). Mijenja
// samo gocop(.exe), nikad sebe, pa na Windowsu ne mora prepisivati program
// koji radi. Na Windowsu se gradi s -H=windowsgui: nema prozora konzole, pa
// unos za pokretanje pri prijavi može pokazivati izravno na nju.
//
//	gocop-postava                       ikona u traci (i pri prijavi)
//	gocop-postava -instaliraj [-pri-prijavi=false] [-iz-mape D:\goCOP]
//	gocop-postava -zaustavi             ugasi Postavu i čvor (prije zamjene datoteka)
//	gocop-postava -ukloni               uz to makni pokretanje pri prijavi i PATH
//	gocop-postava -version
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"gocop/internal/postava"
)

// verzija Postave; CI je postavlja iz oznake postava-vX.Y.Z
var verzija = "1.0.0"

const naslov = "goCOP Postava"

func main() {
	ispisi := flag.Bool("version", false, "ispiši izdanje Postave i završi")
	instaliraj := flag.Bool("instaliraj", false, "preuzmi najnovije izdanje goCOP-a, stavi ga u PATH i u pokretanje pri prijavi")
	priPrijavi := flag.Bool("pri-prijavi", true, "uz -instaliraj: pokreni Postavu pri prijavi u sustav")
	izMape := flag.String("iz-mape", "", "uz -instaliraj: izdanje bez interneta (mapa s programom, SHA256SUMS i SHA256SUMS.sig)")
	zaustavi := flag.Bool("zaustavi", false, "ugasi Postavu i čvor koji rade")
	ukloni := flag.Bool("ukloni", false, "za deinstalaciju: ugasi, makni pokretanje pri prijavi i PATH")
	flag.Parse()

	if *ispisi {
		fmt.Println("goCOP Postava " + verzija)
		return
	}
	exe, err := os.Executable()
	if err == nil {
		if p, err := filepath.EvalSymlinks(exe); err == nil {
			exe = p
		}
	}
	if err != nil {
		postava.Poruka(naslov, "Ne znam gdje stojim: "+err.Error(), true)
		os.Exit(1)
	}
	m := postava.OdrediMjesta(exe)

	switch {
	case *zaustavi:
		if err := postava.Zaustavi(m); err != nil {
			postava.Poruka(naslov, "goCOP se ne da zaustaviti: "+err.Error(), true)
			os.Exit(1)
		}
	case *ukloni:
		if err := postava.Ukloni(m); err != nil {
			postava.Poruka(naslov, "Uklanjanje nije potpuno: "+err.Error(), true)
			os.Exit(1)
		}
	case *instaliraj:
		p := postava.Nova(exe, verzija)
		if err := p.Instaliraj(context.Background(), postava.Opcije{PriPrijavi: *priPrijavi, IzMape: *izMape}); err != nil {
			p.Pisi("Instalacija: %v", err)
			postava.Poruka(naslov, "goCOP nije preuzet: "+err.Error()+
				"\n\nPostava se ipak instalira; goCOP možete preuzeti kasnije iz ikone u traci.", true)
			os.Exit(1)
		}
	default:
		pokreniTraku(postava.Nova(exe, verzija))
	}
}
