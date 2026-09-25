package web

import (
	"context"
	"encoding/csv"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
	"gocop/internal/repository"
)

// Podaci za ponavljanje računa: tko model proučava izvan aplikacije treba
// sirove parove prognoza–mjerenje, a ne naše brojke. Dva izvoza: izdane
// prognoze iz žive baze, spojene s izmjerenim vrijednostima, i datoteke
// provjere unatrag koje zapiše provjeri-prognozu -csv u mapu podataka.

// IzvozDatoteka je jedna datoteka za preuzimanje na stranici „O prognozi”.
type IzvozDatoteka struct {
	Naziv, URL, Opis string
	Velicina         string
}

// reHindcast dopušta samo datoteke koje alat zapisuje, i samo po imenu — nikakav put.
var reHindcast = regexp.MustCompile(`^hindcast[A-Za-z0-9_.-]*\.(csv|zip)$`)

// SetPodaciDir daje stranici mapu u kojoj alati ostavljaju datoteke provjere
// unatrag; traži se pri svakom pozivu, jer se putanja baze veže poslije ruta.
func (h *PrognozeHandler) SetPodaciDir(dir func() string) { h.podaciDir = dir }

// izvozi nabraja što se s „O prognozi” može preuzeti.
func (h *PrognozeHandler) izvozi() []IzvozDatoteka {
	do := time.Now().In(models.Zagreb)
	od := do.AddDate(0, -1, 0)
	out := []IzvozDatoteka{{
		Naziv: "izdanja.csv",
		URL:   "/prognoze/izdanja.csv?od=" + od.Format("2006-01-02") + "&do=" + do.Format("2006-01-02"),
		Opis: "izdane prognoze iz žive baze za zadnjih mjesec dana (parametri od i do u adresi), " +
			"svaki sat izdanja i svaki ciljni sat, s izmjerenom vrijednošću gdje je već poznata",
	}}
	if h.podaciDir == nil {
		return out
	}
	dir := h.podaciDir()
	if dir == "" {
		return out
	}
	unosi, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	var datoteke []IzvozDatoteka
	for _, u := range unosi {
		if u.IsDir() || !reHindcast.MatchString(u.Name()) {
			continue
		}
		info, err := u.Info()
		if err != nil {
			continue
		}
		opis := "provjera unatrag: prognoza puštena kroz arhivu, izdanje po izdanje, uz izmjerenu vrijednost i postojanost"
		switch {
		case strings.Contains(u.Name(), "valovi_nizovi"):
			opis = "provjera na poplavnim valovima: za svaki val zaseban CSV s cijelim nizom — svako izdanje kroz val (svakih 6 h, od 10 dana prije vrha do 3 poslije), svaki sat unaprijed do 96 h, uz izmjereno; model koji val nije vidio"
		case strings.Contains(u.Name(), "valovi_sazetak"):
			opis = "provjera na poplavnim valovima, sažetak: najavljeni vrh 24/48/72/96 h unaprijed (vrsta „vrh”) i promašaj kroz val (vrsta „kroz”), uz postojanost; model koji val nije vidio"
		}
		datoteke = append(datoteke, IzvozDatoteka{
			Naziv: u.Name(), URL: "/prognoze/podaci/" + u.Name(),
			Opis:     opis + " (zapisano " + info.ModTime().In(models.Zagreb).Format("2.1.2006.") + ")",
			Velicina: velicinaDatoteke(info.Size()),
		})
	}
	sort.Slice(datoteke, func(i, j int) bool { return datoteke[i].Naziv < datoteke[j].Naziv })
	return append(out, datoteke...)
}

func velicinaDatoteke(b int64) string {
	switch {
	case b >= 1<<20:
		return brojHRf(float64(b)/(1<<20), 1) + " MB"
	case b >= 1<<10:
		return brojHRf(float64(b)/(1<<10), 0) + " kB"
	}
	return strconv.FormatInt(b, 10) + " B"
}

// PosluziPodatke šalje jednu datoteku provjere unatrag iz mape podataka.
func (h *PrognozeHandler) PosluziPodatke(w http.ResponseWriter, r *http.Request) {
	ime := r.PathValue("ime")
	if h.podaciDir == nil || !reHindcast.MatchString(ime) {
		http.NotFound(w, r)
		return
	}
	put := filepath.Join(h.podaciDir(), ime)
	if _, err := os.Stat(put); err != nil {
		http.NotFound(w, r)
		return
	}
	if strings.HasSuffix(ime, ".zip") {
		w.Header().Set("Content-Type", "application/zip")
	} else {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+ime+`"`)
	http.ServeFile(w, r, put)
}

// IzvoziIzdanja šalje izdane prognoze iz žive baze kao CSV, spojene s
// izmjerenim vrijednostima. Razdoblje se odnosi na sat izdanja; zadano je
// zadnjih mjesec dana, a najviše se daje godina odjednom.
func (h *PrognozeHandler) IzvoziIzdanja(w http.ResponseWriter, r *http.Request) {
	var c *CitacPrognoza
	if h.citac != nil {
		c = h.citac()
	}
	if c == nil {
		http.Error(w, "baza prognoza nije otvorena", http.StatusNotFound)
		return
	}
	do := time.Now().In(models.Zagreb)
	od := do.AddDate(0, -1, 0)
	if v := r.URL.Query().Get("od"); v != "" {
		if t, err := time.ParseInLocation("2006-01-02", v, models.Zagreb); err == nil {
			od = t
		}
	}
	if v := r.URL.Query().Get("do"); v != "" {
		if t, err := time.ParseInLocation("2006-01-02", v, models.Zagreb); err == nil {
			do = t.AddDate(0, 0, 1)
		}
	}
	if do.Sub(od) > 366*24*time.Hour {
		od = do.AddDate(-1, 0, 0)
	}
	izdane := c.Izdanja(od, do)
	postaje := h.postaje(r.Context())
	mjereno := h.mjerenoZaIzvoz(r.Context(), postaje, izdane)

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="izdanja_%s_%s.csv"`,
		od.Format("2006-01-02"), do.AddDate(0, 0, -1).Format("2006-01-02")))
	// BOM da Excel prepozna UTF-8, i točka-zarez kako ga hrvatske postavke čitaju.
	w.Write([]byte("\ufeff"))
	cw := csv.NewWriter(w)
	cw.Comma = ';'
	cw.Write([]string{"izdano_utc", "letva", "velicina", "ciljni_utc", "doseg_h", "prognoza", "dolje", "gore",
		"model", "iz_krivulje", "izmjereno"})
	for _, i := range izdane {
		red := []string{
			time.Unix(i.Izdano*3600, 0).UTC().Format("2006-01-02 15:04"),
			i.Letva, i.Velicina,
			time.Unix(i.Ciljni*3600, 0).UTC().Format("2006-01-02 15:04"),
			strconv.FormatInt(i.Ciljni-i.Izdano, 10),
			broj(i.Vrijednost), broj(i.Dolje), broj(i.Gore), i.Model, daNe(i.Racunata), "",
		}
		if v, ima := mjereno[prognoza.Izvor{Letva: i.Letva, Velicina: i.Velicina}][i.Ciljni]; ima {
			red[10] = broj(v)
		}
		cw.Write(red)
	}
	cw.Flush()
}

func broj(v float64) string { return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64) }

func daNe(b bool) string {
	if b {
		return "da"
	}
	return "ne"
}

// mjerenoZaIzvoz čita izmjerene vrijednosti letvi iz izvoza, po satu: vodostaj
// iz očitanja, protok gdje je izmjeren. Očitanje koje nije na puni sat
// pripisuje se najbližem.
func (h *PrognozeHandler) mjerenoZaIzvoz(ctx context.Context, postaje map[string]models.Station,
	izdane []prognoza.Izdana) map[prognoza.Izvor]map[int64]float64 {
	out := map[prognoza.Izvor]map[int64]float64{}
	if h.readings == nil || len(izdane) == 0 {
		return out
	}
	letve := map[string][2]int64{}
	for _, i := range izdane {
		r, bilo := letve[i.Letva]
		if !bilo {
			r = [2]int64{i.Ciljni, i.Ciljni}
		}
		r[0], r[1] = min(r[0], i.Ciljni), max(r[1], i.Ciljni)
		letve[i.Letva] = r
	}
	for letva, r := range letve {
		st, ima := postaje[letva]
		if !ima {
			continue
		}
		rs, err := h.readings.List(ctx, repository.ReadingFilter{StationID: st.ID.String(),
			From: time.Unix(r[0]*3600, 0).Add(-30 * time.Minute), To: time.Unix(r[1]*3600, 0).Add(30 * time.Minute)})
		if err != nil {
			continue
		}
		vod := map[int64]float64{}
		pro := map[int64]float64{}
		odmak := map[int64]time.Duration{}
		for _, rd := range rs {
			sat := rd.MeasuredAt.Round(time.Hour).Unix() / 3600
			o := rd.MeasuredAt.Sub(time.Unix(sat*3600, 0)).Abs()
			if prije, bilo := odmak[sat]; bilo && o >= prije {
				continue
			}
			odmak[sat] = o
			if rd.LevelCm != nil {
				vod[sat] = float64(*rd.LevelCm)
			}
			if rd.FlowM3s != nil {
				pro[sat] = *rd.FlowM3s
			}
		}
		out[prognoza.Izvor{Letva: letva, Velicina: "vodostaj"}] = vod
		out[prognoza.Izvor{Letva: letva, Velicina: "protok"}] = pro
	}
	return out
}

// suradnja je rečenica o tome tko model radi i proučava; ide i na stranicu i
// u Excel.
const suradnja = "Model je nastao u neslužbenoj suradnji Tomislava Kraljevića i prof. dr. Nenada Šuvaka " +
	"s Fakulteta primijenjene matematike i informatike Sveučilišta u Osijeku."

// suradnjaVeza je adresa profila profesora, za poveznicu na stranici.
const suradnjaVeza = "https://www.mathos.unios.hr/moj_profil/nenad-suvak/"
