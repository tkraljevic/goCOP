// Resursi za Windows: ikona, podaci o izdanju i manifest u .syso datoteci
// koju go build sam uključi u program, i .ico za instalacijski program.
//
// Zaseban modul, da winres ne uđe u ovisnosti goCOP-a: treba samo pri
// gradnji za Windows. Pokreće se iz ove mape:
//
//	go run . -program gocop -izdanje 0.0.28-alfa -izlaz ../../cmd/gocop/rsrc_windows_amd64.syso
//	go run . -program postava -izdanje 1.0.0 -izlaz ../../cmd/gocop-postava/rsrc_windows_amd64.syso -ico ../gocop.ico
//
// Podaci o izdanju (naziv proizvoda, izdanje) traži i SignPath za potpis.
// Vrijeme u resursima je nula, pa ista oznaka daje istu datoteku.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"

	"gocop/internal/ikona"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

func main() {
	program := flag.String("program", "", "gocop ili postava")
	izdanje := flag.String("izdanje", "", "izdanje, npr. 0.0.28-alfa ili 1.0.0")
	izlaz := flag.String("izlaz", "", "putanja .syso datoteke")
	ico := flag.String("ico", "", "uz to zapiši i .ico (za instalacijski program)")
	arh := flag.String("arh", "amd64", "amd64 ili arm64")
	flag.Parse()

	var naziv, opis, datoteka string
	switch *program {
	case "gocop":
		naziv, opis, datoteka = "goCOP", "goCOP — čvor centra obrane od poplava", "gocop.exe"
	case "postava":
		naziv, opis, datoteka = "goCOP Postava", "goCOP Postava — instalacija, nadogradnja i ikona u traci", "gocop-postava.exe"
	default:
		log.Fatal("-program je gocop ili postava")
	}
	brojevi, err := brojeviIzdanja(*izdanje)
	if err != nil {
		log.Fatal(err)
	}
	if *izlaz == "" {
		log.Fatal("treba -izlaz")
	}

	ikonaPrograma := ikona.IkonaPrograma()
	ic, err := winres.LoadICO(bytes.NewReader(ikonaPrograma))
	if err != nil {
		log.Fatal(err)
	}
	rs := winres.ResourceSet{}
	if err := rs.SetIcon(winres.Name("APPICON"), ic); err != nil {
		log.Fatal(err)
	}

	vi := version.Info{FileVersion: brojevi, ProductVersion: brojevi}
	for k, v := range map[string]string{
		version.ProductName:      naziv,
		version.FileDescription:  opis,
		version.ProductVersion:   *izdanje,
		version.FileVersion:      *izdanje,
		version.OriginalFilename: datoteka,
		version.InternalName:     datoteka,
		version.LegalCopyright:   "© Hrvatske vode · EUPL-1.2",
		version.Comments:         "https://github.com/tkraljevic/goCOP",
	} {
		if err := vi.Set(version.LangNeutral, k, v); err != nil {
			log.Fatal(err)
		}
	}
	rs.SetVersionInfo(vi)
	rs.SetManifest(winres.AppManifest{
		Description:         naziv,
		Compatibility:       winres.Win10AndAbove,
		ExecutionLevel:      winres.AsInvoker,
		DPIAwareness:        winres.DPIPerMonitorV2,
		LongPathAware:       true,
		UseCommonControlsV6: true,
	})

	f, err := os.Create(*izlaz)
	if err != nil {
		log.Fatal(err)
	}
	arch := winres.ArchAMD64
	if *arh == "arm64" {
		arch = winres.ArchARM64
	}
	if err := rs.WriteObject(f, arch); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
	if *ico != "" {
		if err := os.WriteFile(*ico, ikonaPrograma, 0o644); err != nil {
			log.Fatal(err)
		}
	}
	fmt.Printf("%s %s → %s\n", naziv, *izdanje, *izlaz)
}

var oblik = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(?:-(alfa|beta))?$`)

// brojeviIzdanja: 0.0.28-alfa → 0.0.28.0 (Windows prikazuje četiri broja)
func brojeviIzdanja(s string) ([4]uint16, error) {
	m := oblik.FindStringSubmatch(s)
	if m == nil {
		return [4]uint16{}, fmt.Errorf("izdanje %q nije oblika 0.0.28-alfa ili 1.0.0", s)
	}
	var out [4]uint16
	for i := 0; i < 3; i++ {
		n, err := strconv.ParseUint(m[i+1], 10, 16)
		if err != nil {
			return out, err
		}
		out[i] = uint16(n)
	}
	return out, nil
}
