package arhiva

import (
	"bytes"
	"cmp"
	"database/sql"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"gocop/internal/models"
)

// krivuljeGradnje je što je gradnja napravila s krivuljama protoka koje nisu
// mogle jednostavno pratiti datoteku: koje je maknula, koje je zadržala jer
// je letva stigla paketom i koje iz datoteke nije upisala jer bi zamijenile
// drukčiju krivulju letve primljene paketom.
type krivuljeGradnje struct {
	maknute, zadrzane, neupisane []MaknutaKrivulja
}

// zateceneKrivulje su, po letvi koja je stigla paketom, krivulje koje je imala
// u arhivi prije gradnje, po početku valjanosti. Uzimaju se prije prvog upisa
// iz datoteka letve, pa druga datoteka iste letve i dalje smije zamijeniti
// krivulju koju je upisala prva.
type zateceneKrivulje map[string]map[string]krivuljaUPaketu

// letve vraća krivulje koje je letva imala prije gradnje. Letva koja nije
// stigla paketom nema zatečenih: njezine krivulje prate datoteku.
func (z zateceneKrivulje) letve(db *sql.DB, letva string) (map[string]krivuljaUPaketu, error) {
	if k, ima := z[letva]; ima {
		return k, nil
	}
	p, err := Primljeno(db, letva)
	if err != nil {
		return nil, fmt.Errorf("je li letva %s stigla paketom: %w", letva, err)
	}
	k := map[string]krivuljaUPaketu{}
	if p != nil {
		sve, err := ucitajKrivulje(db, letva)
		if err != nil {
			return nil, fmt.Errorf("krivulje letve %s u arhivi: %w", letva, err)
		}
		for _, x := range sve {
			k[x.VrijediOd] = x
		}
	}
	z[letva] = k
	return k, nil
}

// upisiDatotekuKrivulja upisuje krivulje iz jedne datoteke krivulja letve i
// vraća početke valjanosti koje datoteka ima.
//
// Krivulja iz datoteke ne zamjenjuje krivulju koju je letva primljena paketom
// već imala (Odluke (13. 10. 2026.), „primljeno paketom se ne dira”). Ista se
// preskače, a drukčija se vraća kao neupisana, da je izvještaj gradnje pokaže.
// Prije je lokalna datoteka bez traga vraćala staru krivulju preko ispravka
// koji je pod istim datumom stigao s drugog čvora.
func upisiDatotekuKrivulja(db *sql.DB, put, letva string, zatecene zateceneKrivulje) (map[string]bool, []MaknutaKrivulja, error) {
	kriv, err := citajDatotekuKrivulja(put)
	if err != nil {
		return nil, nil, err
	}
	prije, err := zatecene.letve(db, letva)
	if err != nil {
		return nil, nil, err
	}
	uDatoteci := make(map[string]bool, len(kriv))
	var neupisane []MaknutaKrivulja
	for _, k := range kriv {
		uDatoteci[k.VrijediOd] = true
		if z, ima := prije[k.VrijediOd]; ima {
			if !isteKrivulje(z, k) {
				neupisane = append(neupisane, MaknutaKrivulja{Letva: letva, VrijediOd: k.VrijediOd, VrijediDo: k.VrijediDo})
			}
			continue
		}
		if err := postaviKrivulju(db, letva, k); err != nil {
			return nil, nil, err
		}
	}
	fmt.Printf("%-8s %-16s HQ krivulje: %d\n", "", letva, len(kriv))
	return uDatoteci, neupisane, nil
}

// isteKrivulje javlja jesu li dvije krivulje istog početka valjanosti iste u
// svemu što arhiva o njima čuva.
func isteKrivulje(a, b krivuljaUPaketu) bool {
	return a.VrijediDo == b.VrijediDo && a.Izvor == b.Izvor && a.Napomena == b.Napomena &&
		slices.Equal(a.Odsjecci, b.Odsjecci)
}

// postaviKrivulju postavlja krivulju iz datoteke u arhivu. Stara krivulja istog
// početka valjanosti briše se s odsječcima, pa se nova upisuje iznova, da
// odsječak maknut iz datoteke ne ostane visjeti.
func postaviKrivulju(db *sql.DB, letva string, k krivuljaUPaketu) error {
	if _, err := db.Exec(`DELETE FROM hq_krivulje WHERE letva=? AND vrijedi_od=?`, letva, k.VrijediOd); err != nil {
		return fmt.Errorf("brisanje stare krivulje %s letve %s: %w", k.VrijediOd, letva, err)
	}
	res, err := db.Exec(`INSERT INTO hq_krivulje (letva, vrijedi_od, vrijedi_do, izvor, napomena)
		VALUES (?,?,?,?,?)`, letva, k.VrijediOd, k.VrijediDo, k.Izvor, k.Napomena)
	if err != nil {
		return fmt.Errorf("upis krivulje %s letve %s: %w", k.VrijediOd, letva, err)
	}
	id, _ := res.LastInsertId()
	for _, o := range k.Odsjecci {
		if _, err := db.Exec(`INSERT OR REPLACE INTO hq_odsjecci
			(krivulja, od_cm, do_cm, oblik, p1, p2, p3, p4) VALUES (?,?,?,?,?,?,?,?)`,
			id, o.OdCm, o.DoCm, o.Oblik, o.P1, o.P2, o.P3, o.P4); err != nil {
			return fmt.Errorf("upis odsječka krivulje %s letve %s: %w", k.VrijediOd, letva, err)
		}
	}
	return nil
}

// citajDatotekuKrivulja čita krivulje iz datoteke krivulja. Datoteka iz koje
// se ništa ne da pročitati zaustavlja gradnju (procitanoIzKrivulja).
func citajDatotekuKrivulja(put string) ([]krivuljaUPaketu, error) {
	sve, err := redciDatotekeKrivulja(put)
	if err != nil {
		return nil, err
	}
	kriv := slozeneKrivulje(sve)
	if err := procitanoIzKrivulja(filepath.Base(put), len(sve), len(kriv)); err != nil {
		return nil, err
	}
	return kriv, nil
}

// redciDatotekeKrivulja čita sve retke datoteke krivulja, sa zaglavljem.
func redciDatotekeKrivulja(put string) ([][]string, error) {
	b, err := os.ReadFile(put)
	if err != nil {
		return nil, fmt.Errorf("datoteka krivulja %s: %w", filepath.Base(put), err)
	}
	cr := csv.NewReader(bytes.NewReader(b))
	cr.Comma = ';'
	cr.FieldsPerRecord = -1
	sve, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("datoteka krivulja %s: %w", filepath.Base(put), err)
	}
	return sve, nil
}

// slozeneKrivulje slaže retke datoteke krivulja u krivulje, redom kako se
// prvi put pojavljuju. Datoteka nosi po jedan redak za svaki odsječak, a
// zaglavlje krivulje (početak i kraj valjanosti, izvor, napomena) ponavlja se
// i vrijedi ono iz prvog retka. Redak kraći od osam stupaca se preskače.
func slozeneKrivulje(sve [][]string) []krivuljaUPaketu {
	if len(sve) == 0 {
		return nil
	}
	stupci := stupciIzZaglavlja(sve[0])
	var out []krivuljaUPaketu
	gdje := map[string]int{}
	for _, r := range sve[1:] {
		if len(r) < 8 {
			continue
		}
		od := strings.TrimSpace(r[0])
		j, ima := gdje[od]
		if !ima {
			j = len(out)
			gdje[od] = j
			out = append(out, stupci.krivulja(r))
		}
		out[j].Odsjecci = dodajOdsjecak(out[j].Odsjecci, stupci.odsjecak(r))
	}
	return out
}

// dodajOdsjecak dodaje odsječak krivulji, poredano po donjoj granici. Odsječak
// iste donje granice zamijeni raniji, kao i u arhivi, gdje je donja granica
// ključ odsječka.
func dodajOdsjecak(odsjecci []odsjecakUPaketu, o odsjecakUPaketu) []odsjecakUPaketu {
	i, ima := slices.BinarySearchFunc(odsjecci, o.OdCm, func(a odsjecakUPaketu, od int) int { return cmp.Compare(a.OdCm, od) })
	if ima {
		odsjecci[i] = o
		return odsjecci
	}
	return slices.Insert(odsjecci, i, o)
}

// stupciKrivulja su mjesta stupaca datoteke krivulja po imenu iz zaglavlja.
// Stupac p4 je došao naknadno, s potencijskim odsječcima. Zatečena datoteka
// bez njega mora se i dalje čitati, pa se stupci traže po zaglavlju umjesto
// da se broje, bez obzira na velika slova i razmake.
type stupciKrivulja map[string]int

func stupciIzZaglavlja(zaglavlje []string) stupciKrivulja {
	s := stupciKrivulja{}
	for i, ime := range zaglavlje {
		s[strings.ToLower(strings.TrimSpace(ime))] = i
	}
	return s
}

// polje je vrijednost stupca u retku; stupac koji datoteka ili redak nema
// čita se kao prazan.
func (s stupciKrivulja) polje(r []string, ime string) string {
	i, ok := s[ime]
	if !ok || i >= len(r) {
		return ""
	}
	return strings.TrimSpace(r[i])
}

// krivulja je zaglavlje krivulje iz retka, bez odsječaka. Početak i kraj
// valjanosti uvijek su prva dva stupca.
func (s stupciKrivulja) krivulja(r []string) krivuljaUPaketu {
	return krivuljaUPaketu{VrijediOd: strings.TrimSpace(r[0]), VrijediDo: strings.TrimSpace(r[1]),
		Izvor: s.polje(r, "izvor"), Napomena: s.polje(r, "napomena")}
}

// odsjecak je odsječak krivulje iz retka. Oblik koji nije potencija čita se
// kao polinom, decimalni zarez kao točka, a broj koji se ne čita kao nula.
func (s stupciKrivulja) odsjecak(r []string) odsjecakUPaketu {
	oblik := s.polje(r, "oblik")
	if oblik != models.OblikPotencija {
		oblik = models.OblikPolinom
	}
	broj := func(ime string) float64 {
		v, _ := strconv.ParseFloat(strings.ReplaceAll(s.polje(r, ime), ",", "."), 64)
		return v
	}
	cijeli := func(ime string) int {
		v, _ := strconv.Atoi(s.polje(r, ime))
		return v
	}
	return odsjecakUPaketu{OdCm: cijeli("od_cm"), DoCm: cijeli("do_cm"), Oblik: oblik,
		P1: broj("p1"), P2: broj("p2"), P3: broj("p3"), P4: broj("p4")}
}

// OpisNeupisane je rečenica za izvještaj gradnje o krivulji iz datoteke koja
// nije upisana, jer je letva stigla paketom, a arhiva od istog dana ima
// drukčiju krivulju.
func (k MaknutaKrivulja) OpisNeupisane() string {
	if k.VrijediDo == "" {
		return fmt.Sprintf("Krivulja protoka iz datoteke krivulja koja vrijedi od %s nije upisana u arhivu, jer je letva stigla paketom s drugog čvora, a arhiva od istog dana ima drukčiju krivulju.",
			danHR(k.VrijediOd))
	}
	return fmt.Sprintf("Krivulja protoka iz datoteke krivulja koja je vrijedila od %s do %s nije upisana u arhivu, jer je letva stigla paketom s drugog čvora, a arhiva od istog dana ima drukčiju krivulju.",
		danHR(k.VrijediOd), danHR(k.VrijediDo))
}
