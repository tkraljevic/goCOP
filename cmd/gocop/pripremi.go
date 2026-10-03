package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gocop/internal/config"
	"gocop/internal/imecvora"
)

// pripremiPostavke je gocop -pripremi (ugovor s Postavom): prije prvog
// pokretanja zapiše gocop.toml uz bazu s imenom čvora iz instalacijskog
// programa. Bazu ne otvara. Ime koje postavke već imaju ostaje: pod njim su
// zapisi čvora, pa se ponovnom instalacijom ne smije promijeniti.
func pripremiPostavke(cfg config.Config, cfgFrom, imeIzDatoteke string, out io.Writer) int {
	if err := imecvora.Provjeri(cfg.Node.ID); err != nil {
		fmt.Fprintf(out, "Ime čvora %q: %v\n", cfg.Node.ID, err)
		return 2
	}
	put := filepath.Join(filepath.Dir(cfg.DB), config.FileName)
	_, bazaPostoji := os.Stat(cfg.DB)
	switch {
	case cfgFrom == "" && bazaPostoji == nil:
		// baza bez postavki: radila je pod starim zadanim imenom
		primjer := config.Default()
		primjer.Node.ID = imecvora.Stari
		if _, err := config.WriteExample(put, primjer); err != nil {
			fmt.Fprintf(out, "Postavke se ne mogu zapisati: %v\n", err)
			return 1
		}
		fmt.Fprintf(out, "Ime čvora ostaje %s: baza već postoji\n", imecvora.Stari)
		return 0
	case cfgFrom == "":
		primjer := config.Default()
		primjer.Node.ID = cfg.Node.ID
		primjer.Node.Name = cfg.Node.Name
		if _, err := config.WriteExample(put, primjer); err != nil {
			fmt.Fprintf(out, "Postavke se ne mogu zapisati: %v\n", err)
			return 1
		}
	case imeIzDatoteke != "":
		fmt.Fprintf(out, "Ime čvora ostaje %s (%s)\n", imeIzDatoteke, cfgFrom)
		return 0
	default:
		if bazaPostoji == nil {
			// baza postoji, a ime nije upisano: radila je pod starim zadanim
			fmt.Fprintf(out, "Ime čvora ostaje %s: baza već postoji\n", imecvora.Stari)
			return 0
		}
		put = cfgFrom
		if err := config.UpisiIme(put, cfg.Node.ID); err != nil {
			fmt.Fprintf(out, "Ime čvora se ne može upisati: %v\n", err)
			return 1
		}
	}
	fmt.Fprintf(out, "Ime čvora %s upisano u %s\n", cfg.Node.ID, put)
	return 0
}
