package db

import "github.com/google/uuid"

// nsGoCOP je prostor imena za determinističke identifikatore seeda.
//
// Svaki čvor sam pokreće seed. Kad bi pritom izmišljao nove UUID-ove,
// Županja na jednom čvoru i Županja na drugom bile bi ISTA postaja s
// različitim identitetom, i prva sinkronizacija bi pukla na jedinstvenoj
// šifri. Zato identifikator seedanog zapisa slijedi iz onoga što ga
// stvarno određuje — šifre postaje, korisničkog imena — pa svaka kopija
// svijeta ima iste ključeve. Verzije koje kasnije nastaju vežu se na te
// ključeve i putuju bez sudara.
var nsGoCOP = uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://github.com/tkraljevic/goCOP"))

// StableID vraća isti UUID za isti (vrsta, ključ) na svakom čvoru
func StableID(kind, key string) uuid.UUID {
	return uuid.NewSHA1(nsGoCOP, []byte(kind+":"+key))
}
