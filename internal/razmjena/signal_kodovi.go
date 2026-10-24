package razmjena

// KodSignala kaže zašto računalo nije prijavljeno kod sastajališta za WEB
// sync (plan WEB synca, F2). Upisuje ga signalni klijent (F2b), a rečenice
// za ljude su u web sučelju. Prolazno stanje pločica pokaže tek kad traje
// dulje od 10 s; trajno, koje se samo ne popravi, odmah.
type KodSignala string

// Kodovi signala
const (
	KodSignalaSpajaSe     KodSignala = "spaja-se"     // prijava je u tijeku
	KodSignalaNedostupno  KodSignala = "nedostupno"   // sastajalište ne odgovara; klijent pokušava ponovno
	KodSignalaNemaRute    KodSignala = "nema-rute"    // na adresi nema sastajališta (404)
	KodSignalaKriviKljuc  KodSignala = "krivi-kljuc"  // na adresi odgovara drugo računalo
	KodSignalaNijeClan    KodSignala = "nije-clan"    // sastajalište ne prima ovo računalo
	KodSignalaKopija      KodSignala = "kopija"       // isti ključ je prijavljen s drugog računala
	KodSignalaBezPostavke KodSignala = "bez-postavke" // prijava je uključena, a sastajalište nije upisano
)

// Trajan javlja stanje koje se samo ne popravi, pa traži čovjeka
func (k KodSignala) Trajan() bool {
	return k != KodSignalaSpajaSe && k != KodSignalaNedostupno && k != ""
}
