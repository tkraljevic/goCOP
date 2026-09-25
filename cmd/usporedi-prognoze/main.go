// usporedi-prognoze pušta naš model u trenucima kad su izdane tuđe prognoze i
// uspoređuje oboje s izmjerenim.
//
//	usporedi-prognoze -tude data/tude/hydroinfo_iz_tablica.csv -baza probna.db
//
// Tuđe prognoze stižu kao CSV (rijeka;naziv;izdano_lokalno;ciljni_lokalno;cm;
// raspon), kakav se izvuče iz pomoćnih tablica s prepisanim hydroinfo.hu.
// Redak s rasponom „danas" je njihovo jutarnje mjerenje: po njemu se vidi
// vode li letvu s istom nulom kao mi.
//
// Da bi usporedba bila poštena, baza prognoza mora biti namještena samo na
// podacima prije prve tuđe prognoze (namjesti-prognozu -do). Model koji je
// vidio val na kojem se mjeri unaprijed zna odgovor.
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
	"sort"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"

	_ "modernc.org/sqlite"
)

// sifre vežu njihove nazive uz naše letve. Nazivi dolaze kako su prepisani iz
// preglednika, s pogrešnim znakovima (Belišæe, Dunaszekcsõ), pa se navode
// takvi kakvi jesu.
var sifre = map[string]string{
	"Botovo": "botovo", "Novo Virje": "novo-virje", "Vrbovka": "vrbovka", "Moslavina": "moslavina",
	"Terezino Polje": "terezino-polje", "Donji Miholjac": "donji-miholjac",
	"Belišæe": "belisce", "Belišće": "belisce", "Osijek": "osijek", "Aljmaš": "aljmas",
	"Szentborbás": "szentborbas", "Drávaszabolcs": "dravaszabolcs", "Barcs": "barcs",
	"Letenye": "letenye", "Mohács": "mohacs", "Baja": "baja", "Dunaszekcsõ": "dunaszekcso",
	"Dunaszekcső": "dunaszekcso", "Paks": "paks", "Dunaföldvár": "dunafoldvar",
	"Budapest": "budapest", "Esztergom": "esztergom", "Komárom": "komarom", "Nagybajcs": "nagybajcs", "Wildungsmauer": "wildungsmauer",
}

type tuda struct {
	letva          string
	izdano, ciljni int64 // sat od epohe, UTC
	cm             float64
	danas          bool
}

type zbroj struct {
	n                   int
	oni, mi, post       float64 // zbroj apsolutnih promašaja
	oniKv, miKv, postKv float64 // zbroj kvadrata
	oniPomak, miPomak   float64
	miBoljih, oniBoljih int
}

func (z *zbroj) dodaj(oni, mi, post float64) {
	z.n++
	z.oni += math.Abs(oni)
	z.mi += math.Abs(mi)
	z.post += math.Abs(post)
	z.oniKv += oni * oni
	z.miKv += mi * mi
	z.postKv += post * post
	z.oniPomak += oni
	z.miPomak += mi
	switch {
	case math.Abs(mi) < math.Abs(oni):
		z.miBoljih++
	case math.Abs(oni) < math.Abs(mi):
		z.oniBoljih++
	}
}

func main() {
	tudePut := flag.String("tude", "data/tude/hydroinfo_iz_tablica.csv", "tuđe prognoze, CSV")
	bazaPut := flag.String("baza", "data/prognoze.db", "baza prognoza s namještenim modelom")
	arhivaPut := flag.String("arhiva", "data/vodostaji.db", "arhiva vodostaja")
	kasnjenje := flag.Int("kasnjenje", 1, "koliko sati prije njihova izdanja završavaju naša mjerenja")
	ispisi := flag.String("ispisi", "", "ispiši svako izdanje za ovu letvu")
	vrhoviS := flag.String("vrh", "", "vrhovi lanca kojima se za budućnost daje tuđa prognoza, npr. komarom,letenye")
	flag.Parse()
	vrhoviTudi := map[string]bool{}
	for _, v := range strings.Split(*vrhoviS, ",") {
		if v = strings.TrimSpace(v); v != "" {
			vrhoviTudi[v] = true
		}
	}

	tude, err := citajTude(*tudePut)
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

	// Svi ulazi modela, i vodostaj svake letve s kojom se uspoređuje.
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
	for _, t := range tude {
		ucitaj(prognoza.Izvor{Letva: t.letva, Velicina: "vodostaj"})
		if ps := pojasi[t.letva]; len(ps) > 0 && ps[0].Velicina == "protok" && krivulje[t.letva] == nil {
			k, err := prognoza.KrivuljeIzArhive(context.Background(), arhiva, t.letva)
			if err != nil {
				log.Fatal(err)
			}
			krivulje[t.letva] = k
		}
	}

	// Nula letve: njihovo jutarnje mjerenje prema našem u isti sat.
	nula := map[string][]float64{}
	for _, t := range tude {
		if !t.danas {
			continue
		}
		if v, ima := nizovi[prognoza.Izvor{Letva: t.letva, Velicina: "vodostaj"}].U(t.ciljni); ima {
			nula[t.letva] = append(nula[t.letva], t.cm-v)
		}
	}

	// Po izdanju: naš model pušten u njihovu trenutku.
	poIzdanju := map[int64][]tuda{}
	for _, t := range tude {
		if !t.danas {
			poIzdanju[t.izdano] = append(poIzdanju[t.izdano], t)
		}
	}
	izdanja := make([]int64, 0, len(poIzdanju))
	for iz := range poIzdanju {
		izdanja = append(izdanja, iz)
	}
	sort.Slice(izdanja, func(i, j int) bool { return izdanja[i] < izdanja[j] })

	tudiVrh := map[int64]map[string]prognoza.Niz{}
	tocke := map[int64]map[string]map[int64]float64{}
	for _, t := range tude {
		if !vrhoviTudi[t.letva] {
			continue
		}
		if tocke[t.izdano] == nil {
			tocke[t.izdano] = map[string]map[int64]float64{}
		}
		if tocke[t.izdano][t.letva] == nil {
			tocke[t.izdano][t.letva] = map[int64]float64{}
		}
		tocke[t.izdano][t.letva][t.ciljni] = t.cm
	}
	for izd, poLetvi := range tocke {
		tudiVrh[izd] = map[string]prognoza.Niz{}
		for l, tt := range poLetvi {
			tudiVrh[izd][l] = prognoza.NizIzTocaka(tt)
		}
	}

	po := map[string]map[int]*zbroj{}
	poVrh := map[string]map[int]*zbroj{}
	for _, izd := range izdanja {
		sada := izd - int64(*kasnjenje)
		r := prognoza.NovoRacunalo(pojasi, nizovi, sada)
		rv := prognoza.NovoRacunalo(pojasi, nizovi, sada)
		for l, n := range tudiVrh[izd] {
			rv.PostaviBuducnostVrha(prognoza.Izvor{Letva: l, Velicina: "vodostaj"}, n)
		}
		racunaj := func(r *prognoza.Racunalo) map[string]map[int64]float64 {
			nasi := map[string]map[int64]float64{}
			for _, t := range poIzdanju[izd] {
				if _, ima := nasi[t.letva]; ima || len(pojasi[t.letva]) == 0 {
					continue
				}
				nasi[t.letva] = map[int64]float64{}
				izdane, err := r.Prognoziraj(t.letva, 170, "usporedba")
				if err != nil {
					continue
				}
				for _, i := range izdane {
					v := i.Vrijednost
					if i.Velicina == "protok" {
						k := prognoza.KrivuljaZa(krivulje[t.letva], time.Unix(i.Ciljni*3600, 0).UTC())
						if k == nil {
							continue
						}
						cm, _, ok := prognoza.Pretvori(k, "vodostaj", v)
						if !ok {
							continue
						}
						v = cm
					}
					nasi[t.letva][i.Ciljni] = v
				}
			}
			return nasi
		}
		nasi, nasiVrh := racunaj(r), racunaj(rv)
		for _, t := range poIzdanju[izd] {
			vod := nizovi[prognoza.Izvor{Letva: t.letva, Velicina: "vodostaj"}]
			stvarno, ima := vod.U(t.ciljni)
			if !ima {
				continue
			}
			mi, imaMi := nasi[t.letva][t.ciljni]
			if !imaMi {
				continue
			}
			post, imaPost := vod.ZadnjiDo(sada)
			if !imaPost {
				continue
			}
			// Njihova prognoza svedena na našu nulu, ako se razlikuje.
			oni := t.cm - medijan(nula[t.letva])
			dan := int(math.Round(float64(t.ciljni-izd) / 24))
			if po[t.letva] == nil {
				po[t.letva] = map[int]*zbroj{}
			}
			if po[t.letva][dan] == nil {
				po[t.letva][dan] = &zbroj{}
			}
			po[t.letva][dan].dodaj(oni-stvarno, mi-stvarno, post-stvarno)
			if mv, ima := nasiVrh[t.letva][t.ciljni]; ima && len(vrhoviTudi) > 0 {
				if poVrh[t.letva] == nil {
					poVrh[t.letva] = map[int]*zbroj{}
				}
				if poVrh[t.letva][dan] == nil {
					poVrh[t.letva][dan] = &zbroj{}
				}
				poVrh[t.letva][dan].dodaj(oni-stvarno, mv-stvarno, post-stvarno)
			}
			if t.letva == *ispisi {
				fmt.Printf("%s → %s  izmjereno %5.0f  oni %5.0f  mi %5.0f  postojanost %5.0f\n",
					vrijeme(izd), vrijeme(t.ciljni), stvarno, oni, mi, post)
			}
		}
	}

	fmt.Printf("\n%d izdanja, od %s do %s; naša mjerenja završavaju %d h prije njihova izdanja\n",
		len(izdanja), vrijeme(izdanja[0]), vrijeme(izdanja[len(izdanja)-1]), *kasnjenje)
	fmt.Println("promašaj je srednja apsolutna pogreška u cm; „mi bolji\" broji slučajeve u kojima smo bliže")
	fmt.Printf("\n%-15s %6s %4s %9s %9s %9s %9s %9s %11s %9s\n",
		"letva", "nula", "dan", "slučaja", "oni", "mi", "postoj.", "pomak mi", "mi bolji", "mi+njihov vrh")
	letve := make([]string, 0, len(po))
	for l := range po {
		letve = append(letve, l)
	}
	sort.Strings(letve)
	for _, l := range letve {
		for dan := 1; dan <= 6; dan++ {
			z := po[l][dan]
			if z == nil || z.n == 0 {
				continue
			}
			n := float64(z.n)
			vrh := ""
			if zv := poVrh[l][dan]; zv != nil && zv.n > 0 {
				vrh = fmt.Sprintf("%9.1f", zv.mi/float64(zv.n))
			}
			fmt.Printf("%-15s %+6.0f %4d %9d %9.1f %9.1f %9.1f %+9.1f %6d/%-4d %s\n",
				l, medijan(nula[l]), dan, z.n, z.oni/n, z.mi/n, z.post/n, z.miPomak/n, z.miBoljih, z.n, vrh)
		}
	}
}

func vrijeme(sat int64) string {
	return time.Unix(sat*3600, 0).In(models.Zagreb).Format("02.01.2006. 15h")
}

func medijan(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	return s[len(s)/2]
}

func citajTude(put string) ([]tuda, error) {
	f, err := os.Open(put)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.Comma = ';'
	if _, err := r.Read(); err != nil { // zaglavlje
		return nil, err
	}
	var out []tuda
	for {
		red, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		letva, ima := sifre[strings.TrimSpace(red[1])]
		if !ima {
			continue
		}
		izd, err1 := time.ParseInLocation("2006-01-02 15:04", red[2], models.Zagreb)
		cilj, err2 := time.ParseInLocation("2006-01-02 15:04", red[3], models.Zagreb)
		cm, err3 := strconv.ParseFloat(red[4], 64)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		out = append(out, tuda{letva: letva, izdano: izd.Unix() / 3600, ciljni: cilj.Unix() / 3600,
			cm: cm, danas: red[5] == "danas"})
	}
	return out, nil
}
