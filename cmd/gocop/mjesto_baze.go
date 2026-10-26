package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// Čvor instaliran Postavom (docs/postava.md) ima program u <instalacija>/program,
// koji je u PATH-u, a bazu u <instalacija>/data. Postava ga pokreće s -db.
// Pokrenut ručno bez -db, gocop bazu traži u data/ ispod trenutne mape i
// ondje napravi novu, s novim ključem i imenom (kućni Windows, 26. 10. 2026.:
// čvor thinkcentre-5-z6ju u program\data). Zato tada ne kreće i kaže gdje
// su podaci čvora.

// bazaInstalacije je baza čvora instalacije Postave u kojoj stoji program
// exe; false kad program nije iz takve instalacije ili ona još nema baze
func bazaInstalacije(exe string) (string, bool) {
	program := filepath.Dir(exe)
	if filepath.Base(program) != "program" {
		return "", false
	}
	instalacija := filepath.Dir(program)
	if fi, err := os.Stat(filepath.Join(instalacija, "postava")); err != nil || !fi.IsDir() {
		return "", false
	}
	baza := filepath.Join(instalacija, "data", "gocop.db")
	if _, err := os.Stat(baza); err != nil {
		return "", false
	}
	return baza, true
}

// mjestoBazePrograma je provjeriMjestoBaze za program koji upravo radi
func mjestoBazePrograma(zadane map[string]bool, db string) error {
	exe, _ := os.Executable() // bez putanje programa ("") nije iz instalacije Postave
	return provjeriMjestoBaze(zadane, db, exe)
}

// provjeriMjestoBaze: program iz instalacije Postave bez -db i -config
// smije otvoriti samo bazu te instalacije
func provjeriMjestoBaze(zadane map[string]bool, db, exe string) error {
	if zadane["db"] || zadane["config"] {
		return nil
	}
	baza, ok := bazaInstalacije(exe)
	if !ok || istaDatoteka(db, baza) {
		return nil
	}
	ovdje, err := filepath.Abs(db)
	if err != nil {
		ovdje = db
	}
	return fmt.Errorf("podaci čvora ove instalacije su u %s, a bez -db gocop bi otvorio drugu bazu, %s, i u mreži se javio kao novo računalo, pa nije pokrenut. "+
		"Čvor pali i gasi Postava (ikona u traci). Za ručno pokretanje zaustavite ga u Postavi i pokrenite: gocop -db \"%s\"",
		filepath.Dir(baza), ovdje, baza)
}

// istaDatoteka: obje putanje postoje i vode do iste datoteke
func istaDatoteka(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	return err == nil && os.SameFile(fa, fb)
}
