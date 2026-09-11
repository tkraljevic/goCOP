package models

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Bilješka uz arhivsku vrijednost.
//
// Ispravak kaže da je vrijednost pogrešna i mijenja ju. Bilješka ne mijenja
// ništa — govori što ta vrijednost jest. „Očitan maksimum" uz 772 cm u 11:11
// nije ispravak nego ono najvrednije što o toj vrijednosti postoji: čovjek je
// stajao pred letvom i vidio vrh.
//
// Stoji odvojeno od arhive, kao i ispravak: arhiva je obnovljiva iz datoteka i
// mora ostati onakva kakvu su je izvori dali. Bilješka je naša, ide kroz knjigu
// verzija i preživljava ulaganje očitanja u arhivu — inače bi se pri
// pospremanju baze izgubilo upravo ono zbog čega se očitanje i pamti.
type ArhivaBiljeska struct {
	ID       uuid.UUID `json:"id"`
	Letva    string    `json:"letva"`
	Velicina string    `json:"velicina"`
	Korak    string    `json:"korak"` // satni | dnevni
	Vrijeme  time.Time `json:"vrijeme"`
	Tekst    string    `json:"tekst"`
	// Tko je bilješku ostavio: promatrač s terena, ili korisnik koji je upisao.
	Tko       string    `json:"tko,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// oznakeVrha su izrazi kojima promatrač kaže da je uhvatio kulminaciju.
// Traže se u malim slovima, bez dijakritike ne — ljudi pišu i „maksimum" i
// „maximum", ali „vrh vala" i „kulminacija" jednako često.
var oznakeVrha = []string{"maksimum", "maximum", "kulminacij", "vrh vala", "vrhunac", "najviš"}

// JeVrh javlja tvrdi li bilješka da je to bila kulminacija. Takva tvrdnja
// vrijedi više od svakog nagađanja iz razilaženja dojava: program iz njihova
// neslaganja tek pokušava zaključiti ono što je ovdje netko vidio.
func (b ArhivaBiljeska) JeVrh() bool {
	t := strings.ToLower(b.Tekst)
	for _, o := range oznakeVrha {
		if strings.Contains(t, o) {
			return true
		}
	}
	return false
}
