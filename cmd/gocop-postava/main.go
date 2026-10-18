// goCOP Postava: instalira goCOP, nadograđuje ga, pali i gasi čvor i drži
// ikonu u traci (docs/postava.md).
//
// Mali, stabilan program s vlastitim izdanjima (oznake postava-v*). Mijenja
// samo gocop(.exe), nikad sebe, pa na Windowsu ne mora prepisivati program
// koji radi. Na Windowsu se gradi s -H=windowsgui: nema prozora konzole, pa
// unos za pokretanje pri prijavi može pokazivati izravno na nju.
//
//	gocop-postava                       ikona u traci (i pri prijavi)
//	gocop-postava -instaliraj [-pri-prijavi=false] [-iz-mape D:\goCOP] [-ime-cvora pperic-thinkpad] [-prvi nova|postojeca]
//	gocop-postava -zaustavi             ugasi Postavu i čvor (prije zamjene datoteka)
//	gocop-postava -ukloni               uz to makni pokretanje pri prijavi i PATH
//	gocop-postava -ikona PUT            zapiši ikonu goCOP-a (PNG 256×256), za izbornik na Linuxu
//	gocop-postava -version
//
// Na Linuxu, kad je čvor usluga sustava (build/linux/instaliraj.sh), Postava
// ne pokreće vlastiti čvor, nego upravlja uslugom gocop (docs/linux.md).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"gocop/internal/ikona"
	"gocop/internal/postava"
)

// verzija Postave; CI je postavlja iz oznake postava-vX.Y.Z, a Postavi za
// Linux, koja ide uz izdanje goCOP-a, ovu zadanu
var verzija = "1.2.0"

const naslov = "goCOP Postava"

func main() {
	ispisi := flag.Bool("version", false, "ispiši izdanje Postave i završi")
	instaliraj := flag.Bool("instaliraj", false, "preuzmi najnovije izdanje goCOP-a, stavi ga u PATH i u pokretanje pri prijavi")
	priPrijavi := flag.Bool("pri-prijavi", true, "uz -instaliraj: pokreni Postavu pri prijavi u sustav")
	izMape := flag.String("iz-mape", "", "uz -instaliraj: izdanje bez interneta (mapa s programom, SHA256SUMS i SHA256SUMS.sig)")
	imeCvora := flag.String("ime-cvora", "", "uz -instaliraj: ime čvora u mreži (npr. pperic-thinkpad); prazno: čvor ga izabere sam")
	prvi := flag.String("prvi", "", "uz -instaliraj: nova ili postojeca — što otvoriti pri prvom pokretanju svježeg čvora")
	zaustavi := flag.Bool("zaustavi", false, "ugasi Postavu i čvor koji rade")
	ukloni := flag.Bool("ukloni", false, "za deinstalaciju: ugasi, makni pokretanje pri prijavi i PATH")
	ikonaU := flag.String("ikona", "", "zapiši ikonu goCOP-a (PNG 256×256) u zadanu datoteku i završi; za izbornik programa na Linuxu")
	flag.Parse()

	if *ispisi {
		fmt.Println("goCOP Postava " + verzija)
		return
	}
	if *ikonaU != "" {
		if err := zapisiIkonu(*ikonaU); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	exe, err := putPostave()
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
		if err := p.Instaliraj(context.Background(), postava.Opcije{PriPrijavi: *priPrijavi, IzMape: *izMape,
			ImeCvora: *imeCvora, Prvi: *prvi}); err != nil {
			p.Pisi("Instalacija: %v", err)
			postava.Poruka(naslov, "goCOP nije preuzet: "+err.Error()+
				"\n\nPostava se ipak instalira; goCOP možete preuzeti kasnije iz ikone u traci.", true)
			os.Exit(1)
		}
	default:
		pokreniTraku(postava.Nova(exe, verzija))
	}
}

// putPostave je stvarno mjesto ovog programa, bez poveznica
func putPostave() (string, error) {
	exe, err := os.Executable()
	if err == nil {
		if p, e := filepath.EvalSymlinks(exe); e == nil {
			exe = p
		}
	}
	return exe, err
}

// zapisiIkonu zapisuje znak goCOP-a za izbornik programa; instaliraj.sh ga
// stavlja u /usr/share/icons/hicolor/256x256/apps/gocop.png
func zapisiIkonu(put string) error {
	return os.WriteFile(put, ikona.PNG(ikona.Slika(256, ikona.Radi, false)), 0o644)
}
