package prognoza

// Naučeni dnevni modeli. Učenje iz arhive traje desetak sekundi (9 s za svih
// 13 ciljeva na arhivi od 9. 10. 2026.), pa se ne ponavlja svaki sat:
// naučeno vrijedi do kraja zagrebačkog dana ili dok se arhiva ne promijeni.
// Inačica koja se nije dala naučiti pokušava se opet idući sat; dosad je
// greška stajala do ponoći i popravljalo ju je samo ponovno pokretanje
// programa, a uvoz u arhivu tijekom dana nije se vidio do sutra. Odluke
// (8. 10. 2026.), pitanje 15. Inačica s kišom na čvoru bez kišomjera ne
// pokušava se ni jednom, jer se ne može naučiti.

import (
	"context"
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
	// javljeno je dan i greška čitanja kiše koja je zadnja otišla u
	// dnevnik: učenje se ponavlja svaki sat, a ista greška ide jednom na dan.
	javljeno string
}

var dnevniModeli zalihaDnevnih

// krugDnevnih su dnevni modeli jednog izdanja. Izdanje ih traži dvaput —
// za vrhove lanca i za dnevnu prognozu — a oba puta moraju biti isti i
// učiti se najviše jednom. Dosad se zaliha pitala dvaput: paket arhive
// ugrađen između dva traženja (razmjena arhive stiže svake dvije minute)
// davao je izdanju dva skupa modela, a krug koji prijeđe puni sat dok
// inačica nedostaje učio je dvaput.
type krugDnevnih struct {
	sync.Once
	modeli map[string][]*DnevniModel
	greske map[string]error
}

// kljucKruga je ključ krugDnevnih u kontekstu izdanja.
type kljucKruga struct{}

// uKrugu daje kontekst jednog izdanja: dnevni modeli uzimaju se iz zalihe
// jednom, pri prvom traženju.
func uKrugu(ctx context.Context) context.Context {
	return context.WithValue(ctx, kljucKruga{}, &krugDnevnih{})
}

// ZaboraviDnevneModele baca naučene dnevne modele, pa ih idući krug uči
// iznova iz arhive. Zove ga priprema modela, da novi model ne čeka ponoć.
func ZaboraviDnevneModele() {
	dnevniModeli.Lock()
	defer dnevniModeli.Unlock()
	dnevniModeli.dan = ""
}

// osvjezi baca naučeno kad je počeo novi zagrebački dan ili se arhiva
// promijenila, pa uči ono čega nema: nakon bacanja sve, inače samo inačice
// koje se nisu dale naučiti — i to najviše jednom na sat, u idućem satnom
// krugu, a ne pri svakom izdanju (i Generiraj izdaje), jer pokušaj traje
// desetak sekundi.
func (z *zalihaDnevnih) osvjezi(arhiva *sql.DB, oborine *OborinskiIzvor, sada time.Time) {
	dan, zig, sat := danUcenja(sada), otisakArhive(arhiva), sada.Unix()/3600
	if z.dan != dan || z.zig.promijenjen(zig) {
		z.modeli = nil
	}
	z.dan, z.zig = dan, zig
	if z.modeli != nil && (z.sat == sat || !nedostaje(z.modeli, oborine)) {
		return
	}
	z.sat = sat
	u := &ucenjeDnevnih{arhiva: arhiva, oborine: oborine}
	z.modeli, z.greske = u.nauci(z.modeli)
	z.javi(u.greskaKise)
}

// javi zapisuje u dnevnik grešku čitanja kiše iz arhive, istu jednom na dan:
// dok se arhiva ne da pročitati, učenje se ponavlja svaki sat.
func (z *zalihaDnevnih) javi(err error) {
	if err == nil || z.javljeno == z.dan+" "+err.Error() {
		return
	}
	z.javljeno = z.dan + " " + err.Error()
	log.Printf("dnevni model: oborina iz arhive: %v", err)
}

// danUcenja je zagrebački dan, kakav god bio sat računala: dnevni nizovi
// arhive stoje po lokalnim danima, a ponoć mora biti ista na svakom čvoru.
func danUcenja(t time.Time) string { return t.In(models.Zagreb).Format("2006-01-02") }

// nedostaje javlja ima li dnevni cilj inačicu koja nije naučena, a mogla bi
// se naučiti.
func nedostaje(modeli map[string][]*DnevniModel, oborine *OborinskiIzvor) bool {
	for _, c := range DnevniCiljevi {
		in := modeli[c.Letva]
		for i, cilj := range c.Inacice() {
			if (i >= len(in) || in[i] == nil) && mozeUciti(cilj, oborine) {
				return true
			}
		}
	}
	return false
}

// mozeUciti javlja može li se inačica uopće naučiti. Inačica s kišom ne može
// na čvoru bez kišomjera (baza oborina se ne da otvoriti ili registar nema
// kišomjera), pa se ni ne pokušava: svaki bi pokušaj uzalud čitao dnevne
// nizove iz arhive i namještao model.
func mozeUciti(cilj DnevniCilj, oborine *OborinskiIzvor) bool {
	return len(cilj.Slivovi) == 0 || imaKisomjera(oborine)
}

// imaKisomjera javlja ima li čvor kišomjere iz kojih dnevni model uči kišu.
func imaKisomjera(oborine *OborinskiIzvor) bool {
	return oborine != nil && len(oborine.Tocke) > 0
}

// nauci uči inačice dnevnih ciljeva kojih u staro nema; naučene iz staro
// ostaju kakve jesu. Mape i popisi su novi, jer stare još čita tko ih je
// dobio prošli put.
func (u *ucenjeDnevnih) nauci(staro map[string][]*DnevniModel) (map[string][]*DnevniModel, map[string]error) {
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
	arhiva     *sql.DB
	oborine    *OborinskiIzvor
	nizovi     map[string]DnevniNiz // nil dok ništa nije pročitano
	greskaKise error                // kiša se nije dala pročitati iz arhive
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
// Inačica koja se ne može naučiti (s kišom, a bez kišomjera) ostaje nil, bez
// greške: to nije kvar, a grešku letve daje inačica koja se mogla naučiti.
func (u *ucenjeDnevnih) inacica(cilj DnevniCilj, staro []*DnevniModel, i int) (*DnevniModel, error) {
	if i < len(staro) && staro[i] != nil {
		return staro[i], nil
	}
	if !mozeUciti(cilj, u.oborine) {
		return nil, nil
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
		u.nizovi, u.greskaKise = oborineZaUcenje(u.arhiva, u.oborine)
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
// kišomjera prazna je, a inačice s kišom se ni ne uče; kad se ne da
// pročitati, također je prazna, uz grešku, a inačice s kišom pokušavaju se
// opet idući sat.
func oborineZaUcenje(arhiva *sql.DB, oborine *OborinskiIzvor) (map[string]DnevniNiz, error) {
	if !imaKisomjera(oborine) {
		return map[string]DnevniNiz{}, nil
	}
	ob, err := DnevneOborine(arhiva, oborine.Tocke)
	if err != nil {
		return map[string]DnevniNiz{}, err
	}
	return ob, nil
}

// zigArhive je otisak datoteke arhive: veličina i vrijeme izmjene baze i
// njezina zapisnika (-wal). Svaki upis mijenja barem jedno od toga — gradnja
// letve, ugradnja paketa, ulaganje očitanja, kišomjeri, izvor ili niz
// promijenjen na stranici, izdvojen kvar telemetrije, ali i upis alatom iz
// drugog programa — a samo čitanje ništa. Prazan put znači da se otisak nije
// dao uzeti (npr. arhiva u memoriji).
//
// Otisak se mijenja i bez novih podataka: kad se zatvori zadnja veza na
// arhivu u programu, SQLite prepiše zapisnik u bazu i obriše ga. Tada se
// modeli uče iznova (desetak sekundi) iz istih podataka; pogrešnih modela
// nema. Poslužitelj ne zatvara besposlene veze (database/sql bez ograničenja
// trajanja veze), pa je to rijetko. Program veze arhive za čitanje otvara
// samo za čitanje (db.SamoZaCitanje), ali arhivu pišu i gradnja, ugradnja
// paketa i alati izvan programa, pa otisak ne smije računati na to da nitko
// ne piše.
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
