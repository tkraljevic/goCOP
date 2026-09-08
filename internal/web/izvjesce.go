package web

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"gocop/internal/docx"
	"gocop/internal/models"
)

// Izvješće o letvi: sve što program o njoj zna i sve što je iz njezina niza
// izračunao, u dokumentu koji se dalje uređuje rukom. Ne zamjenjuje stranicu
// nego joj daje oblik koji se prilaže i potpisuje.
//
// Grafova nema. Za sliku bi trebao pretvarač SVG-a u rastersku sliku, a to je
// vanjska ovisnost radi ukrasa; brojke koje graf pokazuje ionako stoje u
// tablicama ispod, i ondje se mogu prepisati.

// IzvjesceLetve su podaci od kojih se izvješće sastavlja. Isti su oni koje
// prikazuje kartica letve — izvješće ništa ne računa iznova, da se dokument i
// stranica ne mogu razići.
type IzvjesceLetve struct {
	Station     models.Station
	Sastavio    string
	Kad         time.Time
	PragoviKote []PragKota
	PragoviQ    []PragProtok
	Krivulje    []models.HQKrivulja
	Profili     []models.ProfilKorita
	Profil      *models.ProfilKorita
	Crtez       *KoritoCrtez
	Sazetak     []models.SazetakVelicine
	Spojevi     []models.SpojDoseg
	Zadnji      *models.HidroTocka
	ZadnjiIzvor string
	Valovi      []models.Val
	ValoviZbroj []models.ZbrojStupnja
	ValoviNiz   models.RazdobljeNiza
	Sections    []models.Section
}

// valovaUIzvjescu je koliko se najviših valova ispisuje poimence. Batinin niz
// ima 693 vala; svi bi dali tablicu od dvadesetak stranica koju nitko ne čita,
// a zbroj iznad nje ionako govori o svima.
const valovaUIzvjescu = 25

// Sastavi gradi dokument.
func (iz IzvjesceLetve) Sastavi() *docx.Dokument {
	st := iz.Station
	d := docx.Novi("Izvješće o vodomjernoj postaji "+st.Name, iz.Sastavio, iz.Kad)

	d.Naslov("Vodomjerna postaja " + st.Name)
	pod := []string{}
	if st.HasWatercourse() {
		pod = append(pod, st.Watercourse)
	}
	if st.Stationing != "" {
		pod = append(pod, st.Stationing)
	}
	d.Podnaslov(strings.Join(pod, " · "))

	iz.osnovno(d)
	iz.kotaNule(d)
	iz.pragovi(d)
	iz.ekstremi(d)
	iz.povratni(d)
	iz.niz(d)
	iz.valoviObrane(d)
	iz.koritoOpis(d)
	iz.krivuljeProtoka(d)
	iz.podrijetloPodataka(d)
	return d
}

func (iz IzvjesceLetve) osnovno(d *docx.Dokument) {
	st := iz.Station
	d.Poglavlje("Osnovni podaci")
	d.Par("Naziv", st.Name)
	d.Par("Šifra", st.Code)
	if st.HasWatercourse() {
		v := st.Watercourse
		if p := st.PodrijetloVodotoka(); p != "" {
			v += " (" + p + ")"
		}
		d.Par("Vodotok", v)
	}
	d.Par("Stacionaža", st.Stationing)
	d.Par("Vodno područje", st.WaterArea)
	if st.ImaKoordinate() {
		d.Par("Položaj", fmt.Sprintf("%s (%s, %s)", st.KoordinateHR(),
			brojHRd(st.Latitude, 6), brojHRd(st.Longitude, 6)))
	}
	if len(iz.Sections) > 0 {
		var k []string
		for _, s := range iz.Sections {
			k = append(k, s.Code)
		}
		d.Par("Mjerodavna za dionice", strings.Join(k, ", "))
	}
	if st.NeedsReview {
		d.Napomena("Podaci ove postaje nisu u cijelosti potvrđeni: " + st.ReviewNote)
	}
}

func (iz IzvjesceLetve) kotaNule(d *docx.Dokument) {
	st := iz.Station
	if !st.ImaKotuNule() {
		return
	}
	d.Poglavlje("Kota nule vodomjera")
	for _, k := range st.Kote(0) {
		d.Par("Kota nule — "+k.Sustav, brojHRf(k.Kota, 3)+" m")
	}
	if r := st.RazlikaVisinskihSustava(); r != 0 {
		d.Par("Razlika visinskih sustava", brojHRf(r, 3)+" m")
	}
	d.Par("Izvor kote", st.ZeroDatumSource)
	d.Par("Način", st.ZeroDatumMethod)
	if st.ZeroDatumSurveyDate != "" {
		d.Par("Izmjereno", datumHR(st.ZeroDatumSurveyDate))
	}
	if st.ZeroDatumDocumentDate != "" {
		d.Par("Elaborat", datumHR(st.ZeroDatumDocumentDate))
	}

	var redci [][]string
	for _, c := range st.ZeroDatumHistory {
		od := "od početka mjerenja"
		if c.ValidFrom != "" {
			od = datumHR(c.ValidFrom)
		}
		redci = append(redci, []string{od, brojHRd(c.Datum, 3) + " m", c.System, c.Note})
	}
	if len(redci) > 0 {
		d.Odjeljak("Promjene kote nule")
		d.Tablica([]string{"Vrijedi od", "Kota nule", "Sustav", "Napomena"}, redci)
		d.Napomena("Vodostaji u bazi svedeni su na zadnju kotu i ne preračunavaju se. " +
			"Povijest služi da se zna što je koja starija evidencija mjerila.")
	}
}

func (iz IzvjesceLetve) pragovi(d *docx.Dokument) {
	if len(iz.PragoviKote) == 0 {
		return
	}
	d.Poglavlje("Pragovi obrane od poplava")
	glave := []string{"Stupanj", "Vodostaj"}
	imaKote := len(iz.PragoviKote) > 0
	if imaKote {
		glave = append(glave, "Kota vodne plohe")
	}
	protok := map[string]string{}
	for _, q := range iz.PragoviQ {
		protok[q.Naziv] = brojHRf(q.Q, 0) + " m³/s"
	}
	if len(protok) > 0 {
		glave = append(glave, "Protok po krivulji")
	}
	var redci [][]string
	for _, p := range iz.PragoviKote {
		r := []string{p.Naziv, brojHR(p.Cm) + " cm"}
		if imaKote {
			var k []string
			for _, v := range p.Kote {
				k = append(k, brojHRf(v.Kota, 2)+" m "+v.Sustav)
			}
			r = append(r, strings.Join(k, "\n"))
		}
		if len(protok) > 0 {
			r = append(r, protok[p.Naziv])
		}
		redci = append(redci, r)
	}
	d.Tablica(glave, redci)
	d.Napomena("Prag je propisana granica pri kojoj se poduzimaju mjere obrane. " +
		"Protok je preračunat službenom krivuljom DHMZ-a koja je vrijedila u trenutku očitanja.")
}

func (iz IzvjesceLetve) ekstremi(d *docx.Dokument) {
	st := iz.Station
	if len(st.Extremes) == 0 {
		return
	}
	d.Poglavlje("Zabilježeni ekstremi")
	var redci [][]string
	for _, e := range st.Extremes {
		vrsta := "najniži"
		if e.Kind == models.ExtremeMax {
			vrsta = "najviši"
		}
		datum := "—"
		if e.OnDate != "" {
			datum = datumHR(e.OnDate)
		}
		podrijetlo := "izmjereno"
		if !e.IsMeasured() {
			podrijetlo = models.QualityLabel(e.Quality)
		}
		opis := e.Source
		if e.Method != "" {
			opis += " — " + e.Method
		}
		if e.Note != "" {
			opis += "\n" + e.Note
		}
		redci = append(redci, []string{vrsta, e.Label(), datum, podrijetlo, opis})
	}
	d.Tablica([]string{"Vrsta", "Vodostaj", "Datum", "Podrijetlo", "Izvor i način"}, redci)
	d.Napomena("Vrijednost koja nije izmjerena na ovoj letvi ne ulazi u pragove " +
		"ni u izračun stupnja obrane.")
}

func (iz IzvjesceLetve) povratni(d *docx.Dokument) {
	pv := iz.Station.PovratniVodostaji()
	if len(pv) == 0 {
		return
	}
	d.Poglavlje("Povratni vodostaji")
	var redci [][]string
	for _, p := range pv {
		raspon := "—"
		if p.ImaRaspon() {
			raspon = p.RasponLabel()
		}
		redci = append(redci, []string{p.GodineLabel(), p.Label(), raspon, p.SansaLabel()})
	}
	d.Tablica([]string{"Jednom u", "Vodostaj", "Raspon", "Izgled u godini"}, redci)

	prvi := pv[0]
	d.Par("Metoda", prvi.Method)
	d.Par("Niz", prvi.Series)
	d.Par("Izračunao", prvi.Source)
	if prvi.ComputedOn != "" {
		d.Par("Izračunato", datumHR(prvi.ComputedOn))
	}
	if prvi.Note != "" {
		d.Napomena(prvi.Note)
	}
	d.Napomena("Procjena iz niza, ne mjerenje i ne propis. „Jednom u 100 godina“ znači " +
		"1 % izgleda svake pojedine godine, a ne da se neće ponoviti prije isteka stoljeća — " +
		"dvije takve vode mogu doći uzastopce.")
}

func (iz IzvjesceLetve) niz(d *docx.Dokument) {
	if len(iz.Sazetak) == 0 && len(iz.Spojevi) == 0 {
		return
	}
	d.Poglavlje("Niz mjerenja")
	var redci [][]string
	for _, s := range iz.Sazetak {
		redci = append(redci, []string{
			models.NazivVelicine(s.Velicina),
			datumHR(s.Od) + " – " + datumHR(s.Do),
			brojHR(s.Zapisa),
			brojHRf(s.Srednjak, 1),
			brojHRf(s.Min, 1) + " (" + datumHR(s.MinNa) + ")",
			brojHRf(s.Max, 1) + " (" + datumHR(s.MaxNa) + ")",
		})
	}
	d.Tablica([]string{"Veličina", "Razdoblje", "Zapisa", "Srednjak", "Najmanje", "Najviše"}, redci)

	var izvori [][]string
	for _, s := range iz.Spojevi {
		var d1 []string
		for _, dio := range s.Dijelovi {
			d1 = append(d1, fmt.Sprintf("%s %s", dio.Izvor, dio.UdioHR(s.Zapisa)))
		}
		izvori = append(izvori, []string{
			models.NazivVelicine(s.Velicina), s.Korak,
			datumHR(s.Od) + " – " + datumHR(s.Do), brojHR(s.Zapisa), strings.Join(d1, ", "),
		})
	}
	if len(izvori) > 0 {
		d.Odjeljak("Odakle podaci dolaze")
		d.Tablica([]string{"Veličina", "Korak", "Razdoblje", "Zapisa", "Izvori"}, izvori)
	}
	if iz.Zadnji != nil {
		d.Par("Zadnje očitanje", fmt.Sprintf("%s cm, %s, %s",
			brojHRf(iz.Zadnji.Vrijednost, 0), iz.Zadnji.Kad.In(models.Zagreb).Format("2.1.2006. 15:04"),
			models.NazivIzvora(iz.ZadnjiIzvor)))
	}
}

func (iz IzvjesceLetve) valoviObrane(d *docx.Dokument) {
	if len(iz.ValoviZbroj) == 0 {
		return
	}
	d.Poglavlje("Valovi obrane u nizu")
	d.Odlomak("Kada bi po vodostaju počelo i završilo koje stanje i koliko bi trajalo, " +
		"izračunato iz cijelog niza mjerenja — ne iz proglašenih obrana. Odgovara na pitanje " +
		"što bi po vodi bilo, a ne što je odlučeno.")

	var zbroj [][]string
	for _, z := range iz.ValoviZbroj {
		valova := "—"
		if z.Valova > 0 {
			valova = brojHR(z.Valova)
		}
		najdulje := z.NajduljeHR()
		if z.Valova > 0 {
			najdulje += fmt.Sprintf(" (%d.)", z.NajduljiKad.Year())
		}
		zbroj = append(zbroj, []string{
			z.Stupanj.Label(), fmt.Sprintf("%+d cm", z.PragCm), valova,
			z.UkupnoHR(), z.UkupnoIznadHR() + " (" + z.UdioHR(iz.ValoviNiz.Trajanje()) + ")", najdulje,
		})
	}
	d.Odjeljak("Zbroj za cijeli niz")
	d.Tablica([]string{"Stanje", "Prag", "Valova", "Ukupno u stanju", "Ukupno iznad praga", "Najdulje u jednom valu"}, zbroj)
	if iz.ValoviNiz.Ima() {
		d.Napomena(fmt.Sprintf("Računato na %s mjerenja od %d. do %d. "+
			"„U stanju“ se broji kao u obrascu obrane — stanja se ne preklapaju, pa dok traje "+
			"redovna obrana pripremno stanje ne teče, a kad se redovna prekine pripremno se "+
			"nastavlja. „Iznad praga“ broji sve vrijeme iznad te kote, uključujući i više stupnjeve.",
			brojHR(iz.ValoviNiz.Zapisa), iz.ValoviNiz.Od.Year(), iz.ValoviNiz.Do.Year()))
	}

	najvisi := najvisiValovi(iz.Valovi, valovaUIzvjescu)
	if len(najvisi) == 0 {
		return
	}
	glave := []string{"Val", "Vrh", "Najviši stupanj"}
	for _, z := range iz.ValoviZbroj {
		glave = append(glave, z.Stupanj.Label())
	}
	var redci [][]string
	for _, v := range najvisi {
		r := []string{v.PoceloHR() + "\ndo " + v.ZavrsiloHR(),
			v.VrhLabel() + "\n" + v.VrhKadHR(), v.NajviseDosegnuto().Label()}
		for _, z := range iz.ValoviZbroj {
			s := v.StupanjZa(z.Stupanj)
			if s == nil {
				r = append(r, "—")
				continue
			}
			c := fmt.Sprintf("%s h\n%s → %s", brojHR(s.Sati()), s.PoceloHR(), s.ZavrsiloHR())
			if s.Prekidan() {
				c += fmt.Sprintf("\nu %d navrata", s.Navrata)
			}
			r = append(r, c)
		}
		redci = append(redci, r)
	}
	d.Odjeljak(fmt.Sprintf("Najviših %d valova", len(najvisi)))
	d.Tablica(glave, redci)
	d.Napomena("Prijelaz praga se traži pravocrtno između dva mjerenja, pa i dnevni niz daje " +
		"trajanje točnije od dana. Pad ispod pripremnog stanja kraći od tri dana ne prekida val.")
}

// najvisiValovi bira valove po vrhu, pa ih vraća od najnovijeg. Popis od
// sedamsto valova nitko ne čita; zbroj iznad njega ionako govori o svima.
func najvisiValovi(valovi []models.Val, koliko int) []models.Val {
	if len(valovi) == 0 {
		return nil
	}
	po := append([]models.Val(nil), valovi...)
	sort.SliceStable(po, func(i, j int) bool { return po[i].VrhCm > po[j].VrhCm })
	if len(po) > koliko {
		po = po[:koliko]
	}
	sort.SliceStable(po, func(i, j int) bool { return po[i].Pocelo.After(po[j].Pocelo) })
	return po
}

func (iz IzvjesceLetve) koritoOpis(d *docx.Dokument) {
	c := iz.Crtez
	if c == nil {
		return
	}
	d.Poglavlje("Korito")
	if iz.Profil != nil && iz.Profil.Spojen() {
		var dijelovi []string
		for _, s := range iz.Profil.Sastav {
			dijelovi = append(dijelovi, fmt.Sprintf("%s (%s–%s m)",
				datumHR(s.Datum), brojHRf(s.OdM, 0), brojHRf(s.DoM, 0)))
		}
		d.Par("Presjek spojen iz snimaka", strings.Join(dijelovi, ", "))
	} else {
		d.Par("Presjek snimljen", c.Datum)
	}
	d.Par("Voda ucrtana po", fmt.Sprintf("%s cm, kota %s m", brojHR(c.VodostajCm), brojHRf(c.KotaVode, 2)))
	d.Par("Dubina nad dnom", brojHRf(c.DubinaM, 2)+" m")
	if c.SirinaVodeM > 0 {
		d.Par("Širina vode", brojHRf(c.SirinaVodeM, 0)+" m")
	}
	d.Par("Dno korita", fmt.Sprintf("%s cm na letvi, kota %s m", brojHR(c.DnoCm), brojHRf(c.Dno, 2)))
	d.Par("Obale snimka", fmt.Sprintf("lijeva %s cm, desna %s cm", brojHR(c.LijevaCm), brojHR(c.DesnaCm)))
	if c.Sustav != "" {
		d.Napomena(fmt.Sprintf("Kote su preračunate u %s; profil je snimljen u sustavu %s. "+
			"Razlika se računa iz kota nule same letve.", c.Sustav, iz.Station.ZeroDatumSystem))
	}
	if c.OdrezanSnimak {
		d.Napomena(fmt.Sprintf("Snimak seže do %s cm, a voda je iznad toga — koliko korita "+
			"ostaje iznad te razine iz njega se ne vidi.", brojHR(c.NizaObalaCm)))
	}
}

func (iz IzvjesceLetve) krivuljeProtoka(d *docx.Dokument) {
	if len(iz.Krivulje) == 0 {
		return
	}
	d.Poglavlje("Krivulje protoka")
	var redci [][]string
	for _, k := range iz.Krivulje {
		do := k.VrijediDo
		if do == "" {
			do = "do danas"
		} else {
			do = datumHR(do)
		}
		var opseg []string
		for _, o := range k.Odsjecci {
			opseg = append(opseg, fmt.Sprintf("%s do %s cm", brojHR(o.OdCm), brojHR(o.DoCm)))
		}
		redci = append(redci, []string{
			datumHR(k.VrijediOd) + " – " + do,
			brojHR(len(k.Odsjecci)),
			strings.Join(opseg, "\n"),
			k.Izvor,
		})
	}
	d.Tablica([]string{"Vrijedi", "Odsječaka", "Raspon vodostaja", "Izvor"}, redci)
	d.Napomena("Protok se računa krivuljom koja je vrijedila u trenutku očitanja. " +
		"Kad je vodostaj izvan raspona za koji je krivulja postavljena, protok se ne ispisuje: " +
		"izmišljen protok gori je od nikakvog.")
}

func (iz IzvjesceLetve) podrijetloPodataka(d *docx.Dokument) {
	d.Poglavlje("O ovom izvješću")
	d.Par("Postaja", iz.Station.Name)
	d.Par("Sastavio", iz.Sastavio)
	d.Par("Sastavljeno", iz.Kad.In(models.Zagreb).Format("2.1.2006. u 15:04"))
	d.Napomena("Izvješće je sastavljeno iz podataka programa i ne mijenja ih. Sve brojke " +
		"računaju se pri sastavljanju iz istog niza koji prikazuje kartica letve, pa se " +
		"dokument i stranica ne mogu razići. Grafovi nisu ugrađeni; brojke koje pokazuju " +
		"stoje u tablicama.")
}
