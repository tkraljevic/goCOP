package web

import (
	"fmt"
	"strconv"
	"strings"

	"gocop/internal/models"
	"gocop/internal/xlsxw"
)

const stupacaLetve = 9

type listLetve struct{ l *xlsxw.List }

func (x listLetve) naslov(s string) {
	x.l.Dodaj()
	r := x.l.Redak()
	x.l.Dodaj(xlsxw.T(s, xlsxw.Podnaslov))
	x.l.Spoji(0, r, stupacaLetve-1, r)
}

func (x listLetve) polje(ime, vrijednost string) {
	r := x.l.Redak()
	x.l.Dodaj(xlsxw.T(ime, xlsxw.TablicaPod), xlsxw.T(vrijednost, xlsxw.TablicaTekst))
	x.l.Spoji(1, r, stupacaLetve-1, r)
	x.l.Visina(r, visinaTeksta(vrijednost, 88, 18, 0))
}

func (x listLetve) napomena(s string) {
	if strings.TrimSpace(s) != "" {
		napomenaLista(x.l, s, stupacaLetve, visinaTeksta(s, 105, 25, 18))
	}
}

func (x listLetve) tablica(glave []string, redci [][]string) {
	if len(redci) == 0 {
		return
	}
	cols := len(glave)
	if cols > stupacaLetve {
		cols = stupacaLetve
	}
	g := make([]xlsxw.Celija, cols)
	for i := range g {
		g[i] = xlsxw.T(glave[i], xlsxw.Zaglavlje)
	}
	x.l.Dodaj(g...)
	for _, red := range redci {
		c := make([]xlsxw.Celija, cols)
		visina := 18.0
		for i := range c {
			v := ""
			if i < len(red) {
				v = red[i]
			}
			c[i] = xlsxw.T(v, xlsxw.TablicaTekst)
			visina = visinaTeksta(v, 24, 18, visina)
		}
		r := x.l.Redak()
		x.l.Dodaj(c...)
		x.l.Visina(r, visina)
	}
}

// KnjigaLetve daje tri vrste oblikovanog izvoza istim podacima kao stranica:
// karticu, historijat ili operativna očitanja. CSV i .cop ostaju zasebni jer
// služe sirovim podacima i prijenosu, a ova knjiga čitanju i ispisu.
func KnjigaLetve(iz IzvjesceLetve, z ZaglavljeIzvoza) *xlsxw.Knjiga {
	k := &xlsxw.Knjiga{LogoPNG: z.LogoPNG}
	naziv, naslov := "Kartica", "VODOMJERNA POSTAJA "+strings.ToUpper(iz.Station.Name)
	switch iz.Dio {
	case izvjesceHistorijat:
		naziv, naslov = "Historijat", "HISTORIJAT VODOMJERNE POSTAJE "+strings.ToUpper(iz.Station.Name)
	case izvjesceOcitanja:
		naziv, naslov = "Očitanja", "OČITANJA VODOMJERNE POSTAJE "+strings.ToUpper(iz.Station.Name)
	}
	l := k.NoviList(naziv)
	l.Vodoravno = true
	l.Sirine = []float64{18, 14, 14, 18, 14, 18, 20, 20, 28}
	pod := strings.Trim(strings.Join([]string{iz.Station.Watercourse, iz.Station.Stationing}, " · "), " ·")
	zaglavljeLista(l, z, naslov, pod, stupacaLetve)
	x := listLetve{l}
	x.osnovno(iz)
	switch iz.Dio {
	case izvjesceHistorijat:
		x.historijat(iz)
	case izvjesceOcitanja:
		x.ocitanja(iz)
	default:
		x.kartica(iz)
	}
	x.naslov("O ovom izvozu")
	x.polje("Sastavio", iz.Sastavio)
	x.polje("Sastavljeno", iz.Kad.In(models.Zagreb).Format("02.01.2006. 15:04"))
	x.napomena("Excel je sastavljen iz istih podataka koje prikazuje otvorena stranica i ne mijenja podatke u programu.")
	return k
}

func zaglavljeIzvozaLetve(t models.OrgTerms, s *models.Sector) ZaglavljeIzvoza {
	z := ZaglavljeIzvoza{Organizacija: t.OrgName}
	if t.HasLogo() && t.LogoMime == "image/png" {
		z.LogoPNG = t.Logo
	}
	if s != nil {
		z.Sektor, z.Odjel, z.Centar = s.ID, s.VgoName, s.CenterCop
		z.Mjesto = strings.TrimSpace(strings.TrimPrefix(s.CenterCop, t.CenterShort))
	}
	return z
}

func (x listLetve) osnovno(iz IzvjesceLetve) {
	st := iz.Station
	x.naslov("Osnovni podaci")
	x.polje("Naziv", st.Name)
	x.polje("Šifra", st.Code)
	if st.HasWatercourse() {
		x.polje("Vodotok", st.Watercourse)
	}
	x.polje("Stacionaža", st.Stationing)
	x.polje("Vodno područje", st.WaterArea)
	if st.ImaKoordinate() {
		x.polje("Položaj", st.KoordinateHR()+" ("+brojHRd(st.Latitude, 6)+", "+brojHRd(st.Longitude, 6)+")")
	}
	if len(iz.Sections) > 0 {
		šifre := make([]string, 0, len(iz.Sections))
		for _, s := range iz.Sections {
			šifre = append(šifre, s.Code)
		}
		x.polje("Mjerodavna za dionice", strings.Join(šifre, ", "))
	}
	if st.NeedsReview {
		x.napomena("Podaci nisu u cijelosti potvrđeni: " + st.ReviewNote)
	}
}

func (x listLetve) kartica(iz IzvjesceLetve) {
	st := iz.Station
	if len(iz.PragoviKote) > 0 {
		x.naslov("Pragovi obrane od poplava")
		var rows [][]string
		for _, p := range iz.PragoviKote {
			kote := make([]string, 0, len(p.Kote))
			for _, k := range p.Kote {
				kote = append(kote, brojHRf(k.Kota, 2)+" m "+k.Sustav)
			}
			q := ""
			if p.Q != nil {
				q = brojHRf(*p.Q, 0) + " m³/s"
			}
			rows = append(rows, []string{p.Naziv, strconv.Itoa(p.Cm), strings.Join(kote, "\n"), q})
		}
		x.tablica([]string{"Stupanj", "Vodostaj (cm)", "Kota vodne plohe", "Protok"}, rows)
	}
	if st.ImaKotuNule() || st.ZeroDatumBaltic != nil {
		x.naslov("Kota nule vodomjera")
		for _, k := range st.Kote(0) {
			x.polje(k.Sustav, brojHRf(k.Kota, 3)+" m")
		}
		if st.ZeroDatumBaltic != nil {
			x.polje("Baltička", brojHRf(*st.ZeroDatumBaltic, 3)+" m")
			x.polje("Baltički sustav", st.ZeroDatumBalticSystem)
			x.polje("Izvor baltičke kote", st.ZeroDatumBalticSource)
		}
		x.polje("Izvor", st.ZeroDatumSource)
		x.polje("Način", st.ZeroDatumMethod)
	}
	if len(st.Extremes) > 0 {
		x.naslov("Zabilježeni ekstremi")
		var rows [][]string
		for _, e := range st.Extremes {
			vrsta := "najniži"
			if e.Kind == models.ExtremeMax {
				vrsta = "najviši"
			}
			rows = append(rows, []string{vrsta, e.Label(), datumHR(e.OnDate), models.QualityLabel(e.Quality), strings.Trim(e.Source+" — "+e.Method+"\n"+e.Note, " —\n")})
		}
		x.tablica([]string{"Vrsta", "Vodostaj", "Datum", "Podrijetlo", "Izvor i način"}, rows)
		x.napomena("Vrijednost koja nije izmjerena na ovoj letvi ne ulazi u pragove ni u izračun stupnja obrane.")
	}
	if strings.TrimSpace(st.Notes) != "" {
		x.naslov("Napomena")
		x.napomena(st.Notes)
	}
}

func (x listLetve) historijat(iz IzvjesceLetve) {
	if pv := iz.Station.PovratniVodostaji(); len(pv) > 0 {
		x.naslov("Povratni vodostaji")
		var rows [][]string
		for _, p := range pv {
			raspon := ""
			if p.ImaRaspon() {
				raspon = p.RasponLabel()
			}
			rows = append(rows, []string{p.GodineLabel(), p.Label(), raspon, p.SansaLabel(), p.Method, p.Series, p.Source})
		}
		x.tablica([]string{"Jednom u", "Vodostaj", "Raspon", "Izgled u godini", "Metoda", "Niz", "Izračunao"}, rows)
		x.napomena("Procjena iz niza, ne mjerenje i ne propis.")
	}
	if len(iz.Station.ZeroDatumHistory) > 0 {
		x.naslov("Promjene kote nule")
		var rows [][]string
		for _, c := range iz.Station.ZeroDatumHistory {
			od := "od početka mjerenja"
			if c.ValidFrom != "" {
				od = datumHR(c.ValidFrom)
			}
			rows = append(rows, []string{od, brojHRd(c.Datum, 3), c.System, c.Note})
		}
		x.tablica([]string{"Vrijedi od", "Kota nule (m)", "Sustav", "Napomena"}, rows)
	}
	if len(iz.Sazetak) > 0 {
		x.naslov("Niz mjerenja")
		var rows [][]string
		for _, s := range iz.Sazetak {
			rows = append(rows, []string{models.NazivVelicine(s.Velicina), datumHR(s.Od) + " – " + datumHR(s.Do), brojHR(s.Zapisa), brojHRf(s.Srednjak, 1), brojHRf(s.Min, 1) + " (" + datumHR(s.MinNa) + ")", brojHRf(s.Max, 1) + " (" + datumHR(s.MaxNa) + ")"})
		}
		x.tablica([]string{"Veličina", "Razdoblje", "Zapisa", "Srednjak", "Najmanje", "Najviše"}, rows)
	}
	if len(iz.Spojevi) > 0 {
		x.naslov("Odakle podaci dolaze")
		var rows [][]string
		for _, s := range iz.Spojevi {
			var izvori []string
			for _, d := range s.Dijelovi {
				izvori = append(izvori, models.NazivIzvora(d.Izvor)+" "+d.UdioHR(s.Zapisa))
			}
			rows = append(rows, []string{models.NazivVelicine(s.Velicina), s.Korak, datumHR(s.Od) + " – " + datumHR(s.Do), brojHR(s.Zapisa), strings.Join(izvori, ", ")})
		}
		x.tablica([]string{"Veličina", "Korak", "Razdoblje", "Zapisa", "Izvori"}, rows)
	}
	if len(iz.Krivulje) > 0 {
		x.naslov("Krivulje protoka")
		var rows [][]string
		for _, k := range iz.Krivulje {
			do := "do danas"
			if k.VrijediDo != "" {
				do = datumHR(k.VrijediDo)
			}
			var raspon []string
			for _, o := range k.Odsjecci {
				raspon = append(raspon, fmt.Sprintf("%d do %d cm", o.OdCm, o.DoCm))
			}
			rows = append(rows, []string{datumHR(k.VrijediOd) + " – " + do, strconv.Itoa(len(k.Odsjecci)), strings.Join(raspon, "\n"), k.Izvor})
		}
		x.tablica([]string{"Vrijedi", "Odsječaka", "Raspon vodostaja", "Izvor"}, rows)
	}
	if len(iz.ValoviZbroj) > 0 {
		x.naslov("Valovi obrane u nizu")
		var rows [][]string
		for _, z := range iz.ValoviZbroj {
			rows = append(rows, []string{z.Stupanj.Label(), fmt.Sprintf("%+d", z.PragCm), strconv.Itoa(z.Valova), z.UkupnoHR(), z.UkupnoIznadHR(), z.UdioHR(iz.ValoviNiz.Trajanje()), z.NajduljeHR()})
		}
		x.tablica([]string{"Stanje", "Prag (cm)", "Valova", "Ukupno u stanju", "Iznad praga", "Udio", "Najdulje"}, rows)
	}
	if valovi := najvisiValovi(iz.Valovi, valovaUIzvjescu); len(valovi) > 0 {
		x.naslov(fmt.Sprintf("Najviših %d valova", len(valovi)))
		var rows [][]string
		for _, v := range valovi {
			rows = append(rows, []string{v.PoceloHR(), v.ZavrsiloHR(), v.VrhLabel(), v.VrhKadHR(), v.NajviseDosegnuto().Label()})
		}
		x.tablica([]string{"Početak", "Završetak", "Vrh", "Vrijeme vrha", "Najviši stupanj"}, rows)
	}
	if len(iz.Episodes) > 0 {
		x.naslov("Proglašene obrane")
		var rows [][]string
		for _, e := range iz.Episodes {
			do := "traje"
			if e.EndedAt != nil {
				do = e.EndedAt.In(models.Zagreb).Format("02.01.2006. 15:04")
			}
			rows = append(rows, []string{e.SectionCode, e.StartedAt.In(models.Zagreb).Format("02.01.2006. 15:04"), do, strconv.Itoa(e.Days()), e.PeakLabel(), e.Phase.Label()})
		}
		x.tablica([]string{"Dionica", "Od", "Do", "Dana", "Vrh", "Najviši stupanj"}, rows)
	}
}

func (x listLetve) ocitanja(iz IzvjesceLetve) {
	naslov := "Očitanja"
	if iz.OcitanjaOpis != "" {
		naslov += " — " + iz.OcitanjaOpis
	}
	x.naslov(naslov)
	if len(iz.Ocitanja) == 0 {
		x.napomena("U odabranom razdoblju nema upisanih očitanja.")
		return
	}
	glave := []string{"Vrijeme", "Vodostaj (cm)"}
	imaNizvodni := false
	for _, o := range iz.Ocitanja {
		imaNizvodni = imaNizvodni || o.Level2Cm != nil
	}
	if imaNizvodni {
		glave = append(glave, "Nizvodni (cm)")
	}
	kote := iz.Station.ImaKotuNule()
	if kote {
		glave = append(glave, "Kota vode")
	}
	if len(iz.Krivulje) > 0 {
		glave = append(glave, "Protok (m³/s)")
	}
	imaTemp, imaIzmjeren := false, false
	for _, o := range iz.Ocitanja {
		imaTemp, imaIzmjeren = imaTemp || o.TempC != nil, imaIzmjeren || o.FlowM3s != nil
	}
	if imaTemp {
		glave = append(glave, "Temperatura vode (°C)")
	}
	if imaIzmjeren {
		glave = append(glave, "Izmjereni protok (m³/s)")
	}
	glave = append(glave, "Stupanj obrane", "Očitao", "Odakle", "Napomena")
	zaglavlje := make([]xlsxw.Celija, len(glave))
	for i, g := range glave {
		zaglavlje[i] = xlsxw.T(g, xlsxw.Zaglavlje)
	}
	x.l.Dodaj(zaglavlje...)
	for _, o := range iz.Ocitanja {
		row := []xlsxw.Celija{xlsxw.T(o.LocalTime().Format("02.01.2006. 15:04"), xlsxw.TablicaTekst)}
		if o.LevelCm != nil {
			row = append(row, xlsxw.N(float64(*o.LevelCm), xlsxw.Tablica))
		} else {
			row = append(row, xlsxw.T("", xlsxw.Tablica))
		}
		if imaNizvodni {
			if o.Level2Cm != nil {
				row = append(row, xlsxw.N(float64(*o.Level2Cm), xlsxw.Tablica))
			} else {
				row = append(row, xlsxw.T("", xlsxw.Tablica))
			}
		}
		if kote {
			var vrijednosti []string
			if o.LevelCm != nil {
				for _, k := range iz.Station.Kote(*o.LevelCm) {
					vrijednosti = append(vrijednosti, brojHRf(k.Kota, 3)+" m "+k.Sustav)
				}
			}
			row = append(row, xlsxw.T(strings.Join(vrijednosti, "\n"), xlsxw.TablicaTekst))
		}
		if len(iz.Krivulje) > 0 {
			q, imaQ := 0.0, false
			if o.LevelCm != nil {
				if kr := krivuljaZa(iz.Krivulje, o.LocalTime()); kr != nil {
					if v, ok := kr.Protok(*o.LevelCm); ok {
						q, imaQ = v, true
					}
				}
			}
			if imaQ {
				row = append(row, xlsxw.N(q, xlsxw.TablicaBroj))
			} else {
				row = append(row, xlsxw.T("", xlsxw.Tablica))
			}
		}
		for _, d := range []struct {
			ima bool
			v   *float64
		}{{imaTemp, o.TempC}, {imaIzmjeren, o.FlowM3s}} {
			if !d.ima {
				continue
			}
			if d.v != nil {
				row = append(row, xlsxw.N(*d.v, xlsxw.TablicaBroj))
			} else {
				row = append(row, xlsxw.T("", xlsxw.Tablica))
			}
		}
		odakle := o.OriginLabel()
		if odakle == "" {
			odakle = o.Source
		}
		stupanj := ""
		if o.Phase.InForce() {
			stupanj = o.Phase.Label()
		}
		row = append(row,
			xlsxw.T(stupanj, xlsxw.TablicaTekst),
			xlsxw.T(o.Observer, xlsxw.TablicaTekst),
			xlsxw.T(odakle, xlsxw.TablicaTekst),
			xlsxw.T(o.Note, xlsxw.TablicaTekst))
		r := x.l.Redak()
		x.l.Dodaj(row...)
		x.l.Visina(r, visinaTeksta(o.Note, 30, 18, 18))
	}
	x.napomena("Kota vode i protok nisu mjereni nego preračunati iz kote nule vodomjera i službene krivulje protoka koja je tada vrijedila.")
}
