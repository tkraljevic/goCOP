package main

import (
	"context"
	"log"
	"path/filepath"

	"gocop/internal/disk"
	"gocop/internal/peers"
	"gocop/internal/web"
)

// mjestaDiska su mape koje čvor puni: mapa baze (gocop.db, sadrzaj.db,
// prognoze.db, oborine.db), mapa arhive vodostaja, stablo izvornih datoteka
// arhive i mapa paketa. Prazno stablo ili mapa paketa ne prate se.
func mjestaDiska(dbPath, arhivaPut, podaci, paketi string) []disk.Mjesto {
	return []disk.Mjesto{
		{Put: apsolutna(filepath.Dir(dbPath)), Naziv: "bazom"},
		{Put: apsolutna(filepath.Dir(arhivaPut)), Naziv: "arhivom", Premjesti: "arhivu"},
		{Put: apsolutna(podaci), Naziv: "izvornim datotekama arhive", Premjesti: "izvorne datoteke arhive"},
		{Put: apsolutna(paketi), Naziv: "paketima", Premjesti: "pakete"},
	}
}

// apsolutna: Postava pokreće čvor s apsolutnom bazom, a -podaci i -pakete
// ostaju relativni. Windows relativnu putanju mjeri na disku tekuće mape, a
// svezak joj nađe na C:\, pa bi se baza i paketi na disku D: brojali kao
// dva diska. Prazno ostaje prazno: to se ne prati.
func apsolutna(put string) string {
	if a, err := filepath.Abs(put); put != "" && err == nil {
		return a
	}
	return put
}

// upozorenjaDiska su diskovi čvora na kojima je mjesta malo, za pločicu
// razmjene. Pločicu gledaju i s drugih računala, pa poruka nosi ime čvora.
func upozorenjaDiska(nd *disk.Nadzor, cvor *peers.Node) []web.DiskRazmjene {
	ime := cvor.Name
	if ime == "" {
		ime = cvor.ID
	}
	var out []web.DiskRazmjene
	for _, d := range nd.Stanje() {
		if d.Malo() {
			out = append(out, web.DiskRazmjene{Poruka: d.Poruka(ime), Savjet: d.Savjet()})
		}
	}
	return out
}

// pokreniNadzorDiska provjerava slobodno mjesto odmah i zatim svakih deset
// minuta, dok čvor radi; upozorenje se vidi na pločici razmjene
func pokreniNadzorDiska(ctx context.Context, dbPath, arhivaPut, podaci, paketi string) *disk.Nadzor {
	n := disk.NoviNadzor(disk.Sustav{}, log.Printf, mjestaDiska(dbPath, arhivaPut, podaci, paketi)...)
	go n.Vrti(ctx, disk.Svakih)
	return n
}
