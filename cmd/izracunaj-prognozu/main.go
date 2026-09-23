// izracunaj-prognozu uzima zadnja očitanja, prenosi ih lancem nizvodno i
// zapisuje prognozu po satu.
//
//	izracunaj-prognozu -probno    samo ispiši
//	izracunaj-prognozu            izračunaj i zapiši
//
// Lanac se sam zaustavi ondje gdje mu ponestane ulaza, i to je doseg
// prognoze. Bez oborine dalje od toga nema što reći.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"

	_ "modernc.org/sqlite"
)

// Model je oznaka pod kojom se prognoza zapisuje, da se poslije zna po čemu je
// izdana. Mijenja se kad se promijeni oblik računa, ne kad se samo osvježe
// koeficijenti.
const Model = "lanac-1"

func main() {
	ocitanjaPut := flag.String("ocitanja", "data/gocop.db", "baza očitanja")
	arhivaPut := flag.String("arhiva", "data/vodostaji.db", "arhiva, zbog krivulja")
	bazaPut := flag.String("baza", "data/prognoze.db", "baza prognoza")
	najdalje := flag.Int("najdalje", 96, "dokle se računa, u satima")
	probno := flag.Bool("probno", false, "samo ispiši, ne zapisuj")
	poluvijek := flag.Float64("poluvijek", prognoza.PoluvijekIspravka,
		"za koliko sati ispravak prema mjerenju oslabi na pola; 0 isključuje")
	ispisi := flag.String("ispisi", "", "ispiši niz po satu za jednu letvu")
	flag.Parse()
	prognoza.PoluvijekIspravka = *poluvijek

	baza, err := prognoza.Otvori(*bazaPut)
	if err != nil {
		log.Fatal(err)
	}
	defer baza.Close()
	pojasi, err := prognoza.SviPojasi(baza)
	if err != nil {
		log.Fatal(err)
	}
	// Promašaji su izmjereni puštanjem prognoze unatrag po arhivi. Ondje gdje
	// ih ima, oni kažu i koliko treba oduzeti i koliko se smije obećati —
	// bolje od rasapa namještanja, koji ne zna za ispravak.
	promasaji, err := prognoza.Promasaji(baza)
	if err != nil {
		log.Fatal(err)
	}
	if len(pojasi) == 0 {
		log.Fatal("baza prognoza je prazna; prvo pokreni namjesti-prognozu")
	}

	ocitanja, err := sql.Open("sqlite", *ocitanjaPut+"?mode=ro")
	if err != nil {
		log.Fatal(err)
	}
	defer ocitanja.Close()
	arhiva, err := sql.Open("sqlite", *arhivaPut+"?mode=ro")
	if err != nil {
		log.Fatal(err)
	}
	defer arhiva.Close()

	trebani, vrhovi := trebaniIzvori(pojasi)
	od := time.Now().UTC().AddDate(0, 0, -14)
	nizovi := map[prognoza.Izvor]prognoza.Niz{}
	for iz := range trebani {
		n, err := ucitajNiz(ocitanja, arhiva, iz, od)
		if err != nil {
			log.Fatalf("%s (%s): %v", iz.Letva, iz.Velicina, err)
		}
		nizovi[iz] = n
	}

	sada, ok := zadnjiZajednicki(nizovi, vrhovi)
	if !ok {
		log.Fatal("nijedna ulazna letva nema svježa očitanja")
	}
	fmt.Printf("izdano za %s UTC\n", time.Unix(sada*3600, 0).UTC().Format("2006-01-02 15:04"))
	for iz := range vrhovi {
		z, _ := nizovi[iz].Zadnji()
		fmt.Printf("   %-16s %s  zadnje %s\n", iz.Letva, iz.Velicina,
			time.Unix(z*3600, 0).UTC().Format("02.01. 15:04"))
	}

	r := prognoza.NovoRacunalo(pojasi, nizovi, sada)
	var sve []prognoza.Izdana
	fmt.Printf("\n%-16s %-9s %8s %10s %12s\n", "letva", "veličina", "doseg", "za 6 h", "na kraju")
	for _, letva := range redom(pojasi) {
		izdane, err := r.Prognoziraj(letva, *najdalje, Model)
		if err != nil {
			fmt.Printf("%-16s %v\n", letva, err)
			continue
		}
		if len(izdane) == 0 {
			fmt.Printf("%-16s nema ulaza za nijedan sat unaprijed\n", letva)
			continue
		}
		for i := range izdane {
			p, ima := promasaji[letva][int(izdane[i].Ciljni-sada)]
			if !ima {
				continue
			}
			izdane[i].Vrijednost -= p.Pomak
			izdane[i].Raspon = p.Rasap
		}
		sve = append(sve, izdane...)
		if *ispisi == letva {
			for _, i := range izdane {
				fmt.Printf("   %s  %+3d h  %8.1f ± %-6.1f %s\n",
					time.Unix(i.Ciljni*3600, 0).UTC().Format("02.01. 15:04"),
					i.Ciljni-sada, i.Vrijednost, i.Raspon, jedinica(i.Velicina))
			}
		}
		jed := jedinica(izdane[0].Velicina)
		zad := izdane[len(izdane)-1]
		sest := izdane[min(5, len(izdane)-1)]
		fmt.Printf("%-16s %-9s %6d h %7.0f±%-3.0f %7.0f±%-3.0f %s%s\n",
			letva, izdane[0].Velicina, len(izdane),
			sest.Vrijednost, sest.Raspon, zad.Vrijednost, zad.Raspon, jed,
			slabija(promasaji[letva], len(izdane)))
	}

	if *probno {
		fmt.Printf("\nproba — ništa nije zapisano; %d vrijednosti bi ušlo\n", len(sve))
		return
	}
	if err := prognoza.SpremiIzdane(baza, sve); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nzapisano %d vrijednosti\n", len(sve))
}

// trebaniIzvori nabraja sve letve koje račun dira, i posebno one koje nemaju
// svoj račun — vrhove lanca, o kojima ovisi dokle prognoza seže.
func trebaniIzvori(pojasi map[string][]prognoza.Pojas) (svi, vrhovi map[prognoza.Izvor]bool) {
	svi = map[prognoza.Izvor]bool{}
	vrhovi = map[prognoza.Izvor]bool{}
	for letva, ps := range pojasi {
		for _, p := range ps {
			svi[prognoza.Izvor{Letva: letva, Velicina: p.Velicina}] = true
			for _, u := range p.Ulazi {
				iz := prognoza.Izvor{Letva: u.Letva, Velicina: u.Velicina}
				svi[iz] = true
				if len(pojasi[u.Letva]) == 0 {
					vrhovi[iz] = true
				}
			}
		}
	}
	return svi, vrhovi
}

// zadnjiZajednicki je zadnji sat koji imaju sve ulazne letve. Uzeti kasniji
// značilo bi računati iz onoga čega još nema.
func zadnjiZajednicki(nizovi map[prognoza.Izvor]prognoza.Niz, vrhovi map[prognoza.Izvor]bool) (int64, bool) {
	var naj int64
	prvi := true
	for iz := range vrhovi {
		z, ima := nizovi[iz].Zadnji()
		if !ima {
			return 0, false
		}
		if prvi || z < naj {
			naj, prvi = z, false
		}
	}
	return naj, !prvi
}

// ucitajNiz čita satni niz iz očitanja. Protok se uzima kako je izmjeren, a
// gdje ga nema računa se iz krivulje — Terezino Polje šalje samo centimetre, a
// model mu traži kubike.
func ucitajNiz(ocitanja, arhiva *sql.DB, iz prognoza.Izvor, od time.Time) (prognoza.Niz, error) {
	var krivulje []models.HQKrivulja
	if iz.Velicina == "protok" {
		var err error
		if krivulje, err = ucitajKrivulje(arhiva, iz.Letva); err != nil {
			return prognoza.Niz{}, err
		}
	}
	r, err := ocitanja.Query(`SELECT o.measured_at, o.level_cm, o.flow_m3s
		FROM readings o JOIN stations s ON s.id = o.station_id
		WHERE s.code = ? AND o.measured_at >= ?`, iz.Letva, od)
	if err != nil {
		return prognoza.Niz{}, err
	}
	defer r.Close()

	vrijednosti := map[int64]float64{}
	odmak := map[int64]time.Duration{}
	for r.Next() {
		// Stupac je DATETIME, pa ga upravljač sam pretvara u vrijeme; čitan
		// kao tekst dolazi u drugom zapisu nego što u bazi stoji.
		var t time.Time
		var cm sql.NullInt64
		var q sql.NullFloat64
		if err := r.Scan(&t, &cm, &q); err != nil {
			return prognoza.Niz{}, err
		}
		t = t.UTC()
		v, ima := uVelicini(iz.Velicina, cm, q, krivulje, t)
		if !ima {
			continue
		}
		// Očitanje s pola sata pripada najbližem satu; kad ih na isti sat
		// padne više, ostaje ono bliže punoj uri.
		sat := int64(t.Add(30*time.Minute).Unix() / 3600)
		raz := t.Sub(time.Unix(sat*3600, 0))
		if raz < 0 {
			raz = -raz
		}
		if prije, bilo := odmak[sat]; !bilo || raz < prije {
			vrijednosti[sat] = v
			odmak[sat] = raz
		}
	}
	return prognoza.NoviNiz(vrijednosti), r.Err()
}

func uVelicini(velicina string, cm sql.NullInt64, q sql.NullFloat64,
	krivulje []models.HQKrivulja, kad time.Time) (float64, bool) {
	if velicina == "vodostaj" {
		if !cm.Valid {
			return 0, false
		}
		return float64(cm.Int64), true
	}
	if q.Valid {
		return q.Float64, true
	}
	if !cm.Valid {
		return 0, false
	}
	k := krivuljaZa(krivulje, kad)
	if k == nil {
		return 0, false
	}
	v, _, ok := k.ProtokProsiren(int(cm.Int64))
	return v, ok
}

func krivuljaZa(krivulje []models.HQKrivulja, kad time.Time) *models.HQKrivulja {
	d := kad.Format("2006-01-02")
	for i := range krivulje {
		k := &krivulje[i]
		if k.VrijediOd <= d && (k.VrijediDo == "" || d <= k.VrijediDo) {
			return k
		}
	}
	return nil
}

func ucitajKrivulje(arhiva *sql.DB, letva string) ([]models.HQKrivulja, error) {
	r, err := arhiva.Query(`SELECT id, vrijedi_od, vrijedi_do FROM hq_krivulje
		WHERE letva = ? ORDER BY vrijedi_od DESC`, letva)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var out []models.HQKrivulja
	for r.Next() {
		var k models.HQKrivulja
		if err := r.Scan(&k.ID, &k.VrijediOd, &k.VrijediDo); err != nil {
			return nil, err
		}
		k.Letva = letva
		out = append(out, k)
	}
	if err := r.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		o, err := arhiva.Query(`SELECT od_cm, do_cm, oblik, p1, p2, p3, p4
			FROM hq_odsjecci WHERE krivulja = ? ORDER BY od_cm`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for o.Next() {
			var s models.HQOdsjecak
			if err := o.Scan(&s.OdCm, &s.DoCm, &s.Oblik, &s.P1, &s.P2, &s.P3, &s.P4); err != nil {
				o.Close()
				return nil, err
			}
			out[i].Odsjecci = append(out[i].Odsjecci, s)
		}
		o.Close()
	}
	return out, nil
}

// redom slaže letve tako da uzvodne idu prije nizvodnih, koliko se dade iz
// samih veza. Ispis je time čitljiv kao lanac, a ne kao abecedni popis.
func redom(pojasi map[string][]prognoza.Pojas) []string {
	gotovo := map[string]bool{}
	var out []string
	var stavi func(string)
	stavi = func(l string) {
		if gotovo[l] || len(pojasi[l]) == 0 {
			return
		}
		gotovo[l] = true
		for _, u := range pojasi[l][0].Ulazi {
			stavi(u.Letva)
		}
		out = append(out, l)
	}
	for l := range pojasi {
		stavi(l)
	}
	return out
}

// slabija javlja na kojim dosezima prognoza ne pobjeđuje postojanost. Ondje
// se ne isplati izdavati je: bolje je reći da se ništa neće promijeniti.
func slabija(po map[int]prognoza.Promasaj, doseg int) string {
	var od, do int
	for d := 1; d <= doseg; d++ {
		p, ima := po[d]
		if !ima || p.BoljaOdPostojanosti() {
			continue
		}
		if od == 0 {
			od = d
		}
		do = d
	}
	if od == 0 {
		return ""
	}
	if od == do {
		return fmt.Sprintf("   slabija od postojanosti na %d h", od)
	}
	return fmt.Sprintf("   slabija od postojanosti od %d do %d h", od, do)
}

func jedinica(velicina string) string {
	if velicina == "protok" {
		return "m³/s"
	}
	return "cm"
}
