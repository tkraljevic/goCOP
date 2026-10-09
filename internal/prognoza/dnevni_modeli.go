package prognoza

// Naučeni dnevni modeli. Učenje iz arhive traje sekundu-dvije, pa se ne
// ponavlja svaki sat: naučeno vrijedi do kraja zagrebačkog dana ili dok se
// arhiva ne promijeni. Inačica koja se nije dala naučiti pokušava se opet
// idući sat; dosad je greška stajala do ponoći i popravljalo ju je samo
// ponovno pokretanje programa, a uvoz u arhivu tijekom dana nije se vidio do
// sutra. Odluke (8. 10. 2026.), pitanje 15.

import (
	"database/sql"
	"log"
	"os"
	"sync"
	"time"

	"gocop/internal/models"
)

// sadaUcenja je sat po kojem se zna dan i sat učenja; testovi ga pomiču.
var sadaUcenja = time.Now

// zalihaDnevnih drži naučene dnevne modele i ono po čemu se zna vrijede li
// još.
type zalihaDnevnih struct {
	sync.Mutex
	dan    string                    // zagrebački dan učenja
	zig    zigArhive                 // otisak arhive kad je učeno
	sat    int64                     // sat zadnjeg učenja: neuspjelo se ponavlja tek idući sat
	modeli map[string][]*DnevniModel // po letvi: glavni pa rezerve; nil gdje učenje nije prošlo
	greske map[string]error          // letve kojima se nijedna inačica nije dala naučiti
}

var dnevniModeli zalihaDnevnih

// ZaboraviDnevneModele baca naučene dnevne modele, pa ih idući krug uči
// iznova iz arhive. Zove ga priprema modela, da novi model ne čeka ponoć.
func ZaboraviDnevneModele() {
	dnevniModeli.Lock()
	defer dnevniModeli.Unlock()
	dnevniModeli.dan = ""
}

// osvjezi baca naučeno kad je počeo novi zagrebački dan ili se arhiva
// promijenila, pa uči ono čega nema: nakon bacanja sve, inače samo inačice
// koje se nisu dale naučiti — i to najviše jednom na sat, jer jedno izdanje
// modele traži dvaput (vrhovi lanca i dnevna prognoza).
func (z *zalihaDnevnih) osvjezi(arhiva *sql.DB, oborine *OborinskiIzvor, sada time.Time) {
	dan, zig, sat := danUcenja(sada), otisakArhive(arhiva), sada.Unix()/3600
	if z.dan != dan || z.zig.promijenjen(zig) {
		z.modeli = nil
	}
	z.dan, z.zig = dan, zig
	if z.modeli != nil && (z.sat == sat || !nedostaje(z.modeli)) {
		return
	}
	z.sat = sat
	z.modeli, z.greske = nauciDnevne(arhiva, oborine, z.modeli)
}

// danUcenja je zagrebački dan, kakav god bio sat računala: dnevni nizovi
// arhive stoje po lokalnim danima, a ponoć mora biti ista na svakom čvoru.
func danUcenja(t time.Time) string { return t.In(models.Zagreb).Format("2006-01-02") }

// nedostaje javlja ima li dnevni cilj inačicu koja nije naučena.
func nedostaje(modeli map[string][]*DnevniModel) bool {
	for _, c := range DnevniCiljevi {
		in := modeli[c.Letva]
		if len(in) < len(c.Inacice()) {
			return true
		}
		for _, m := range in {
			if m == nil {
				return true
			}
		}
	}
	return false
}

// nauciDnevne uči inačice dnevnih ciljeva kojih u staro nema; naučene iz
// staro ostaju kakve jesu. Mape i popisi su novi, jer stare još čita tko ih
// je dobio prošli put.
func nauciDnevne(arhiva *sql.DB, oborine *OborinskiIzvor, staro map[string][]*DnevniModel) (map[string][]*DnevniModel, map[string]error) {
	u := &ucenjeDnevnih{arhiva: arhiva, oborine: oborine}
	modeli, greske := map[string][]*DnevniModel{}, map[string]error{}
	for _, c := range DnevniCiljevi {
		in, err := u.inacice(c, staro[c.Letva])
		modeli[c.Letva] = in
		if err != nil {
			greske[c.Letva] = err
		}
	}
	return modeli, greske
}

// ucenjeDnevnih je jedno učenje: dnevni nizovi čitaju se iz arhive jednom, i
// tek kad ih neka inačica treba.
type ucenjeDnevnih struct {
	arhiva  *sql.DB
	oborine *OborinskiIzvor
	nizovi  map[string]DnevniNiz // nil dok ništa nije pročitano
}

// inacice uči inačice cilja c kojih u staro nema. Greška je greška prve
// inačice koja nije prošla, i to samo kad nijedna nije naučena: letvi koju
// računa rezerva greška glavnog modela ne smije stajati u BezDnevne.
func (u *ucenjeDnevnih) inacice(c DnevniCilj, staro []*DnevniModel) ([]*DnevniModel, error) {
	ciljevi := c.Inacice()
	in := make([]*DnevniModel, len(ciljevi))
	var prva error
	for i, cilj := range ciljevi {
		m, err := u.inacica(cilj, staro, i)
		in[i] = m
		if prva == nil {
			prva = err
		}
	}
	for _, m := range in {
		if m != nil {
			return in, nil
		}
	}
	return in, prva
}

// inacica vraća i-tu inačicu iz staro kad je naučena, a inače je uči.
func (u *ucenjeDnevnih) inacica(cilj DnevniCilj, staro []*DnevniModel, i int) (*DnevniModel, error) {
	if i < len(staro) && staro[i] != nil {
		return staro[i], nil
	}
	nizovi, err := u.ulazi(cilj)
	if err != nil {
		return nil, err
	}
	return NamjestiDnevni(nizovi, cilj, 0)
}

// ulazi su dnevni nizovi za cilj: oborina po međuslivovima i dnevni
// srednjaci njegovih letvi.
func (u *ucenjeDnevnih) ulazi(cilj DnevniCilj) (map[string]DnevniNiz, error) {
	if u.nizovi == nil {
		u.nizovi = oborineZaUcenje(u.arhiva, u.oborine)
	}
	for _, l := range cilj.letve() {
		if _, ima := u.nizovi[l]; ima {
			continue
		}
		n, err := DnevniIzArhive(u.arhiva, l)
		if err != nil {
			return nil, err
		}
		u.nizovi[l] = n
	}
	return u.nizovi, nil
}

// oborineZaUcenje je dnevna oborina po međuslivovima iz arhive. Bez
// kišomjera, ili kad se ne da pročitati, prazna je: inačice s kišom tada ne
// uče i pokušavaju se opet idući sat.
func oborineZaUcenje(arhiva *sql.DB, oborine *OborinskiIzvor) map[string]DnevniNiz {
	if oborine == nil || len(oborine.Tocke) == 0 {
		return map[string]DnevniNiz{}
	}
	ob, err := DnevneOborine(arhiva, oborine.Tocke)
	if err != nil {
		log.Printf("dnevni model: oborina iz arhive: %v", err)
		return map[string]DnevniNiz{}
	}
	return ob
}

// zigArhive je otisak datoteke arhive: veličina i vrijeme izmjene baze i
// njezina zapisnika (-wal). Svaki upis mijenja barem jedno od toga — gradnja
// letve, ugradnja paketa, ulaganje očitanja, kišomjeri, izvor ili niz
// promijenjen na stranici, izdvojen kvar telemetrije, ali i upis alatom iz
// drugog programa — a čitanje ništa. Prazan put znači da se otisak nije dao
// uzeti (npr. arhiva u memoriji).
type zigArhive struct {
	put       string
	baza, wal stanjeDatoteke
}

type stanjeDatoteke struct {
	velicina, izmjena int64 // izmjena u nanosekundama
}

// otisakArhive uzima otisak datoteke iz koje arhiva čita.
func otisakArhive(arhiva *sql.DB) zigArhive {
	var z zigArhive
	var redni int
	var ime string
	if err := arhiva.QueryRow(`PRAGMA database_list`).Scan(&redni, &ime, &z.put); err != nil || z.put == "" {
		return zigArhive{}
	}
	z.baza, z.wal = stanjeNa(z.put), stanjeNa(z.put+"-wal")
	return z
}

// stanjeNa je veličina i vrijeme izmjene datoteke; nule kad je nema.
func stanjeNa(put string) stanjeDatoteke {
	fi, err := os.Stat(put)
	if err != nil {
		return stanjeDatoteke{}
	}
	return stanjeDatoteke{velicina: fi.Size(), izmjena: fi.ModTime().UnixNano()}
}

// promijenjen javlja je li se arhiva promijenila od otiska z. Otisak bez puta
// ne zna ništa, pa ni ne javlja.
func (z zigArhive) promijenjen(sada zigArhive) bool {
	return z.put != "" && z != sada
}
