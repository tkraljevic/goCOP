package izdanje

// Dopuštenje za starije izdanje (Odluke (30. 10. 2026.)): globalni
// administrator na čvoru dopusti Postavi da postavi starije izdanje, a čvor
// to javi u /zdravlje. /zdravlje odgovara svakome na ovom računalu, pa i
// drugi program koji zauzme port može javiti lažno dopuštenje. Zato
// vlastitom čvoru Postava pri svakom pokretanju da novi ključ kroz okolinu,
// a čvor dopuštenje potpiše tim ključem (PotpisDopustenja). Usluzi sustava
// Postava ključ ne daje, pa dopuštenje prima samo s porta ispod 1024
// (internal/postava/usluga.go).

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// OkolinaKljucaPostave je varijabla okoline kroz koju Postava vlastitom
// čvoru (gocop -upravitelj) daje ključ za potpis dopuštenja
const OkolinaKljucaPostave = "GOCOP_POSTAVA_KLJUC"

// NajstarijeVracanje je najstarije izdanje na koje Postava vraća goCOP. Od
// njega izdanja imaju isti instaliraj.sh i gocop.service kao danas, čvor
// koji pod Postavom ne smije krenuti ne izlazi (od 0.0.35), a program u
// mapi program je prava datoteka (od 0.0.36). Na starije se vraća ručno.
const NajstarijeVracanje = "0.0.37-alfa"

// PrestaroZaVracanje: na izdanje v Postava ne vraća, jer je starije od
// NajstarijeVracanje
func PrestaroZaVracanje(v Verzija) bool {
	n, _ := ParsirajOznaku(NajstarijeVracanje) // stalna oznaka, uvijek ispravna (test)
	return v.Usporedi(n) < 0
}

// PotpisDopustenja je HMAC-SHA256 ključem koji je Postava dala čvoru, nad
// izdanjem koje radi, dopuštenim izdanjem i rokom (Unix sekunde)
func PotpisDopustenja(kljuc, izd, dopusteno string, do int64) string {
	m := hmac.New(sha256.New, []byte(kljuc))
	_, _ = fmt.Fprintf(m, "goCOP dopušteno izdanje v1\n%s\n%s\n%d\n", izd, dopusteno, do) // HMAC ne javlja grešku pisanja
	return hex.EncodeToString(m.Sum(nil))
}
