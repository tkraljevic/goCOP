package razmjena

// Put je put kojim je razmjena prošla. Postavlja ga peers, na strani koja
// zove i na strani koja prima. Ne ide na žicu; na pločici razmjene stoji
// njegov naziv (NazivPuta).
type Put string

// Putovi razmjene
const (
	PutLAN       Put = "lan"       // adresa iz pronalaženja u lokalnoj mreži
	PutAdresa    Put = "adresa"    // TCP na adresu iz zapisa, naučenu ili dolaznu
	PutProbijeno Put = "probijeno" // QUIC nakon probijanja NAT-a (F3)
	PutTailscale Put = "tailscale" // preko Tailscalea
	// PutKrozTunel, a ne PutTunel: odmah uz PutTunela, put "/razmjena/tunel"
	// na web sučelju, ime bi se lako zamijenilo
	PutKrozTunel Put = "tunel"
)

// Nazivi putova u tekstu programa (plan WEB synca, „Putovi i natpisi”).
// Put se uvijek zove svojim imenom; riječi „izravno” i ime sustava na
// kojem radi poslužitelj se ne pojavljuju.
const (
	NazivLAN       = "LAN sync"
	NazivServer    = "SERVER sync"
	NazivWeb       = "WEB sync"
	NazivTailscale = "Tailscale"
)

// NazivPuta je naziv puta na pločici. Za adresu i probijanje odlučuje je li
// adresa druge strane privatna, a ne je li u istoj podmreži: laptop kod kuće
// (192.168.0.x) i kućni server (192.168.1.x) su u istoj kući. Domena koju je
// upisao čovjek je javna adresa poslužitelja. Prazan put nema naziv.
func NazivPuta(p Put, privatna bool) string {
	switch {
	case p == PutLAN || (p == PutAdresa || p == PutProbijeno) && privatna:
		return NazivLAN
	case p == PutProbijeno:
		return NazivWeb
	case p == PutTailscale:
		return NazivTailscale
	case p == PutAdresa || p == PutKrozTunel:
		return NazivServer
	}
	return ""
}

// RazlogPuta kaže zašto razmjena ne ide boljim putem. Sprema se u
// peer_sync.put_razlog, pa su vrijednosti stalne; rečenice za ljude su u
// web sučelju. Nisu kodovi sa žice (KodRazloga) ni razlozi vrata
// (RazlogVrata).
type RazlogPuta string

// Razlozi puta (plan WEB synca, F1). Od F1b upisuje se samo
// RazlogPutaNijeSeJavio; ostale upisuju sastajalište (F2) i WEB sync (F3).
const (
	RazlogPutaNijeSeJavio    RazlogPuta = "nije-se-javio"   // bolji put nije odgovorio
	RazlogPutaUDPNeProlazi   RazlogPuta = "udp-ne-prolazi"  // UDP ne prolazi
	RazlogPutaNATOvisan      RazlogPuta = "nat-ovisan"      // oba NAT-a ovisna o odredištu
	RazlogPutaUDPDoDrugih    RazlogPuta = "udp-do-drugih"   // UDP do drugih računala ne prolazi
	RazlogPutaVatrozid       RazlogPuta = "vatrozid"        // vatrozid na računalu možda ne pušta UDP
	RazlogPutaNijeClan       RazlogPuta = "nije-clan"       // nije član ili je opozvan
	RazlogPutaStarijiProgram RazlogPuta = "stariji-program" // stariji program
	RazlogPutaPriprema       RazlogPuta = "priprema"        // WEB sync se priprema
	RazlogPutaPauza          RazlogPuta = "pauza"           // pauza nakon neuspjeha
	RazlogPutaIstiKljuc      RazlogPuta = "isti-kljuc"      // isti ključ na još jednom računalu
)

// Trajan javlja razlog koji se sam ne popravi, pa traži čovjeka
func (r RazlogPuta) Trajan() bool {
	return r == RazlogPutaNijeClan || r == RazlogPutaStarijiProgram || r == RazlogPutaIstiKljuc
}
