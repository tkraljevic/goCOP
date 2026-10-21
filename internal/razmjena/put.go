package razmjena

// Put je put kojim je razmjena prošla. Postavlja ga peers, na strani koja
// zove i na strani koja prima. Ne ide na žicu; na pločici razmjene
// prikazuje se u F1.
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
