// provjeri-valove pušta prognozu kroz zadane poplavne valove i mjeri ono što
// na obrani treba: koliki je vrh najavljen dan, dva, tri i četiri unaprijed,
// i koliko je prognoza promašivala kroz cijeli val.
//
//	provjeri-valove -baza probna.db -valovi valovi.csv > ishod.csv
//
// valovi.csv: val;letva;vrh_utc (YYYY-MM-DD HH);vrh_cm — vrh izmjeren na toj
// letvi. Ishod ide na izlaz kao CSV, da se valovi iz više namještanja mogu
// zbrojiti: svaki val provjerava se modelom koji ga nije vidio
// (namjesti-prognozu -bez).
package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"strconv"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"

	_ "modernc.org/sqlite"
)

// Dosezi su koliko sati prije vrha se prognoza izdaje.
var Dosezi = []int{24, 48, 72, 96}

type val struct {
	id, letva string
	vrh       int64 // sat od epohe
	vrhCm     float64
}

func main() {
	bazaPut := flag.String("baza", "data/prognoze.db", "baza prognoza")
	arhivaPut := flag.String("arhiva", "data/vodostaji.db", "arhiva vodostaja")
	valoviPut := flag.String("valovi", "", "CSV s valovima")
	prije := flag.Int("prije", 240, "koliko sati prije vrha počinje provjera kroz val")
	poslije := flag.Int("poslije", 72, "koliko sati poslije vrha završava")
	korak := flag.Int("korak", 6, "sati između dva izdanja kroz val")
	flag.Parse()

	valovi, err := citaj(*valoviPut)
	if err != nil {
		log.Fatal(err)
	}
	baza, err := prognoza.Otvori(*bazaPut)
	if err != nil {
		log.Fatal(err)
	}
	defer baza.Close()
	pojasi, err := prognoza.SviPojasi(baza)
	if err != nil {
		log.Fatal(err)
	}
	arhiva, err := sql.Open("sqlite", *arhivaPut+"?mode=ro")
	if err != nil {
		log.Fatal(err)
	}
	defer arhiva.Close()

	nizovi := map[prognoza.Izvor]prognoza.Niz{}
	ucitaj := func(iz prognoza.Izvor) {
		if _, ima := nizovi[iz]; ima {
			return
		}
		v, err := prognoza.NizIzArhive(arhiva, iz.Letva, iz.Velicina)
		if err != nil {
			log.Fatal(err)
		}
		nizovi[iz] = prognoza.NoviNiz(v)
	}
	for letva, ps := range pojasi {
		ucitaj(prognoza.Izvor{Letva: letva, Velicina: ps[0].Velicina})
		for _, p := range ps {
			for _, u := range p.Ulazi {
				ucitaj(prognoza.Izvor{Letva: u.Letva, Velicina: u.Velicina})
			}
		}
	}
	krivulje := map[string][]models.HQKrivulja{}
	for _, v := range valovi {
		ucitaj(prognoza.Izvor{Letva: v.letva, Velicina: "vodostaj"})
		if ps := pojasi[v.letva]; len(ps) > 0 && ps[0].Velicina == "protok" && krivulje[v.letva] == nil {
			k, err := prognoza.KrivuljeIzArhive(context.Background(), arhiva, v.letva)
			if err != nil {
				log.Fatal(err)
			}
			krivulje[v.letva] = k
		}
	}

	// prognoza vraća naš niz za letvu u centimetrima, izdan u satu sada.
	prognozaCm := func(letva string, sada int64, najdalje int) map[int64]float64 {
		r := prognoza.NovoRacunalo(pojasi, nizovi, sada)
		izdane, err := r.Prognoziraj(letva, najdalje, "valovi")
		if err != nil {
			return nil
		}
		out := map[int64]float64{}
		for _, i := range izdane {
			v := i.Vrijednost
			if i.Velicina == "protok" {
				k := prognoza.KrivuljaZa(krivulje[letva], time.Unix(i.Ciljni*3600, 0).UTC())
				if k == nil {
					continue
				}
				cm, _, ok := prognoza.Pretvori(k, "vodostaj", v)
				if !ok {
					continue
				}
				v = cm
			}
			out[i.Ciljni] = v
		}
		return out
	}

	w := csv.NewWriter(os.Stdout)
	w.Comma = ';'
	w.Write([]string{"vrsta", "val", "letva", "doseg_h", "izmjereno", "prognoza", "vrh_prognoze", "postojanost"})
	for _, v := range valovi {
		if len(pojasi[v.letva]) == 0 {
			log.Printf("%s: letva se ne prognozira", v.letva)
			continue
		}
		vod := nizovi[prognoza.Izvor{Letva: v.letva, Velicina: "vodostaj"}]
		// Najava vrha: izdanje d sati prije izmjerenog vrha. Uz vrijednost u
		// satu vrha ide i najviša prognozirana unutar ±24 h, jer je na obrani
		// visina vrha važnija od sata u koji padne.
		for _, d := range Dosezi {
			sada := v.vrh - int64(d)
			p := prognozaCm(v.letva, sada, d+24)
			uVrhu, ima := p[v.vrh]
			if !ima {
				continue
			}
			najvise := math.Inf(-1)
			for t := v.vrh - 24; t <= v.vrh+24; t++ {
				if x, ima := p[t]; ima && x > najvise {
					najvise = x
				}
			}
			post, _ := vod.ZadnjiDo(sada)
			w.Write([]string{"vrh", v.id, v.letva, strconv.Itoa(d), f(v.vrhCm), f(uVrhu), f(najvise), f(post)})
		}
		// Kroz cijeli val: izdanje svakih korak sati, promašaj na svakom dosegu.
		for sada := v.vrh - int64(*prije); sada <= v.vrh+int64(*poslije); sada += int64(*korak) {
			p := prognozaCm(v.letva, sada, 96)
			post, imaPost := vod.ZadnjiDo(sada)
			for _, d := range Dosezi {
				x, ima := p[sada+int64(d)]
				stvarno, imaS := vod.U(sada + int64(d))
				if !ima || !imaS || !imaPost {
					continue
				}
				w.Write([]string{"kroz", v.id, v.letva, strconv.Itoa(d), f(stvarno), f(x), "", f(post)})
			}
		}
	}
	w.Flush()
}

func f(x float64) string { return strconv.FormatFloat(x, 'f', 1, 64) }

func citaj(put string) ([]val, error) {
	fh, err := os.Open(put)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	r := csv.NewReader(fh)
	r.Comma = ';'
	if _, err := r.Read(); err != nil {
		return nil, err
	}
	var out []val
	for {
		red, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		t, err := time.Parse("2006-01-02 15", red[2])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", red[2], err)
		}
		cm, _ := strconv.ParseFloat(red[3], 64)
		out = append(out, val{id: red[0], letva: red[1], vrh: t.Unix() / 3600, vrhCm: cm})
	}
	return out, nil
}
