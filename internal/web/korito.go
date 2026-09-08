package web

import (
	"fmt"
	"math"
	"strings"
	"time"

	"gocop/internal/models"
)

// Crtež poprečnog profila korita s vodom u njemu.
//
// Snimka korita je niz točaka (udaljenost od početka, apsolutna kota dna).
// Vodostaj je u centimetrima iznad kote nule letve, pa se obje veličine svode
// na istu apsolutnu kotu i onda na koordinate crteža. Time se vidi ono što se
// iz broja ne vidi: koliko je vode u koritu i gdje su obale.

// KoritoPostavke biraju kako se korito crta. Na kartici letve dovoljna je
// slika korita s vodom; ispod grafa očitanja treba i pojas kroz koji je voda
// išla i stupnjevi obrane, jer se tek s njima vidi koliko korita ostaje.
type KoritoPostavke struct {
	Sirina, Visina float64
	Lijevo         float64      // rub za oznake osi; uži crtež traži širi rub
	Pragovi        []PragKorita // stupnjevi obrane ucrtani na korito
	PojasOd        int          // najniži vodostaj razdoblja, cm
	PojasDo        int          // najviši
	ImaPojas       bool
	OsUCm          bool // podjele na okruglim centimetrima na letvi, ne na okruglim metrima
}

// PragKorita je jedan stupanj obrane iskazan u centimetrima na letvi.
type PragKorita struct {
	Cm    int
	Label string
	Class string
}

// KoritoCrtez je profil pripremljen za crtanje, u koordinatama slike.
type KoritoCrtez struct {
	Sirina, Visina int     // veličina slike
	Korito         string  // točke dna, za polyline
	Voda           string  // površina vode, za polygon
	ImaVode        bool    // vodostaj je iznad dna
	VodostajCm     int     // vodostaj koji je nacrtan
	KotaVode       float64 // apsolutna kota vodne plohe, m
	YVode          float64 // gdje crta vodne plohe stoji na slici
	Dno            float64 // najniža kota korita, m
	DubinaM        float64 // dubina nad najnižom točkom
	SirinaVodeM    float64 // širina vodne plohe
	KoteY          []KotaOznaka
	Datum          string

	// Ucrtani stupnjevi obrane i pojas kroz koji je voda išla u razdoblju
	// koje graf iznad prikazuje.
	Pragovi          []KotaOznaka
	PojasY, PojasH   float64
	ImaPojas         bool
	PojasOd, PojasDo int
	// Krajevi snimka u centimetrima na letvi. Snimanje počinje na lijevoj
	// obali — tako je označeno na izvornim listovima HIS-2000, okomitim
	// natpisima „Lijeva obala“ i „Desna obala“ na krajevima crteža.
	LijevaCm, DesnaCm int
	NizaObalaCm       int // niža od dvije: preko nje voda prva izlazi iz snimka
	OdrezanSnimak     bool // voda je viša od niže obale, pa snimak razinu ne pokriva
	DnoCm             int
	VodaX0, VodaX1   float64 // dokle vodna ploha seže na slici
	// Stacionaža krajeva snimka. Koja je to obala izvorne datoteke ne kažu,
	// pa crtež govori ono što zna: koliko je metara od početka snimanja.
	PocetakM, KrajM  float64
	PocetakX, KrajX  float64
	Lijevo           float64 // rub za oznake osi
	SirinaPlohe      float64 // od lijevog ruba do desnog kraja slike
}

// OsX je gdje stoje brojke okomite osi, poravnate desno.
func (c *KoritoCrtez) OsX() float64 { return c.Lijevo - 4 }

// NatpisX je gdje počinje natpis praga: odmah desno od osi.
func (c *KoritoCrtez) NatpisX() float64 { return c.Lijevo + 6 }

// KotaOznaka je vodoravna crta s ispisanom kotom. Uz apsolutnu kotu nosi i
// vodostaj na letvi: presjek se čita zajedno s grafom iznad, a graf govori u
// centimetrima.
type KotaOznaka struct {
	Y     float64
	Kota  float64
	Cm    int
	Label string
	Class string
}

// crtajKorito priprema profil za crtanje pri zadanom vodostaju.
func crtajKorito(p models.ProfilKorita, vodostajCm int) *KoritoCrtez {
	return crtajKoritoP(p, vodostajCm, KoritoPostavke{Sirina: 900, Visina: 320})
}

// Mjere presjeka ispod grafa. Uski nije samo stisnuti široki: u sustavu od
// 900 jedinica stisnutom na 340 slikovnih točaka oznake padnu na tri točke,
// pa uski ima svoj koordinatni sustav i širi lijevi rub za brojke.
var (
	sirokoKorito = KoritoPostavke{Sirina: 900, Visina: 340, Lijevo: 52, OsUCm: true}
	uskoKorito   = KoritoPostavke{Sirina: 520, Visina: 380, Lijevo: 74, OsUCm: true}
)

// sKoritom dopunjuje mjere pragovima i pojasom, da se obje veličine crtaju iz
// istog opisa.
func (o KoritoPostavke) sKoritom(pragovi []PragKorita, od, do int) KoritoPostavke {
	o.Pragovi = pragovi
	o.PojasOd, o.PojasDo, o.ImaPojas = od, do, do > od
	return o
}

// crtajKoritoP je isti crtež, ali s postavkama.
func crtajKoritoP(p models.ProfilKorita, vodostajCm int, o KoritoPostavke) *KoritoCrtez {
	if len(p.Tocke) < 2 {
		return nil
	}
	w, h := o.Sirina, o.Visina
	if w <= 0 || h <= 0 {
		w, h = 900, 320
	}
	const desno, gore, dolje = 12.0, 14.0, 26.0
	lijevo := o.Lijevo
	if lijevo <= 0 {
		lijevo = 52
	}
	x0, x1 := p.Tocke[0].Stacionaza, p.Tocke[len(p.Tocke)-1].Stacionaza
	minV, maxV := p.Tocke[0].Visina, p.Tocke[0].Visina
	for _, t := range p.Tocke {
		if t.Visina < minV {
			minV = t.Visina
		}
		if t.Visina > maxV {
			maxV = t.Visina
		}
	}
	kotaVode := p.KotaNule + float64(vodostajCm)/100
	if kotaVode > maxV {
		maxV = kotaVode
	}
	if kotaVode < minV {
		minV = kotaVode
	}
	// Prag iznad ruba snimka mora se vidjeti: upravo je to podatak koji se
	// traži — koliko korita ostaje iznad zadnjeg stupnja obrane.
	for _, pr := range o.Pragovi {
		k := p.KotaNule + float64(pr.Cm)/100
		if k > maxV {
			maxV = k
		}
	}
	// Malo zraka gore i dolje, da crta vode ne sjedne na rub. Kod visokog
	// korita razmjerni zrak postane metar i pol praznine, pa se ograničava:
	// gore treba stati samo natpis najvišeg praga.
	raspon := maxV - minV
	if raspon <= 0 {
		raspon = 1
	}
	zrakGore, zrakDolje := raspon*0.10, raspon*0.06
	if o.OsUCm {
		zrakGore = math.Min(zrakGore, 0.8)
		zrakDolje = math.Min(zrakDolje, 0.4)
	}
	minV -= zrakDolje
	maxV += zrakGore
	if x1 <= x0 {
		return nil
	}

	sx := func(s float64) float64 { return lijevo + (s-x0)/(x1-x0)*(w-lijevo-desno) }
	sy := func(v float64) float64 { return gore + (maxV-v)/(maxV-minV)*(h-gore-dolje) }

	var korito strings.Builder
	for i, t := range p.Tocke {
		if i > 0 {
			korito.WriteByte(' ')
		}
		fmt.Fprintf(&korito, "%.1f,%.1f", sx(t.Stacionaza), sy(t.Visina))
	}

	c := &KoritoCrtez{
		Sirina: int(w), Visina: int(h),
		Korito:     korito.String(),
		VodostajCm: vodostajCm,
		KotaVode:   kotaVode,
		YVode:      sy(kotaVode),
		Dno:        p.Dno(),
		DubinaM:    kotaVode - p.Dno(),
		Datum:      datumHR(p.Datum),
		DnoCm:      int((p.Dno() - p.KotaNule) * 100),
		PocetakM:   x0, KrajM: x1,
		PocetakX: sx(x0), KrajX: sx(x1),
		Lijevo: lijevo, SirinaPlohe: w - lijevo,
	}
	c.LijevaCm = int(math.Round((p.Tocke[0].Visina - p.KotaNule) * 100))
	c.DesnaCm = int(math.Round((p.Tocke[len(p.Tocke)-1].Visina - p.KotaNule) * 100))
	c.NizaObalaCm = c.LijevaCm
	if c.DesnaCm < c.NizaObalaCm {
		c.NizaObalaCm = c.DesnaCm
	}
	// Snimak ne seže uvijek do vrha obale: mjerenje 2020. na Batini počinje
	// na +166 cm, a mjerenje 2015. tek na 110. metru. Kad je voda iznad niže
	// obale, crtež je pri toj razini presječen i to se mora reći — inače bi
	// se iz njega čitalo da korita ima koliko ga na papiru ima.
	c.OdrezanSnimak = vodostajCm > c.NizaObalaCm

	// Pojas kroz koji je voda išla u prikazanom razdoblju. Bez njega presjek
	// pokazuje samo trenutak, a graf iznad govori o razdoblju.
	if o.ImaPojas && o.PojasDo > o.PojasOd {
		y0 := sy(p.KotaNule + float64(o.PojasDo)/100)
		y1 := sy(p.KotaNule + float64(o.PojasOd)/100)
		if y1-y0 < 2 {
			y1 = y0 + 2 // razlika manja od dva piksela ipak mora biti vidljiva
		}
		c.PojasY, c.PojasH, c.ImaPojas = y0, y1-y0, true
		c.PojasOd, c.PojasDo = o.PojasOd, o.PojasDo
	}
	for _, pr := range o.Pragovi {
		k := p.KotaNule + float64(pr.Cm)/100
		c.Pragovi = append(c.Pragovi, KotaOznaka{
			Y: sy(k), Kota: k, Cm: pr.Cm, Label: pr.Label, Class: pr.Class})
	}

	// Vodna ploha: dno između mjesta gdje kota vode siječe obale, zatvoreno
	// vodoravnom crtom po vrhu. Presjecišta se traže po odsječcima, jer korito
	// nije glatko nego niz izmjerenih točaka.
	var voda []string
	var prvi, zadnji float64
	imaVode := false
	for i := 1; i < len(p.Tocke); i++ {
		a, b := p.Tocke[i-1], p.Tocke[i]
		if a.Visina > kotaVode && b.Visina > kotaVode {
			continue
		}
		if a.Visina > kotaVode {
			u := (a.Visina - kotaVode) / (a.Visina - b.Visina)
			s := a.Stacionaza + u*(b.Stacionaza-a.Stacionaza)
			if !imaVode {
				prvi = s
			}
			voda = append(voda, fmt.Sprintf("%.1f,%.1f", sx(s), sy(kotaVode)))
			imaVode = true
		}
		if !imaVode {
			prvi = a.Stacionaza
			voda = append(voda, fmt.Sprintf("%.1f,%.1f", sx(a.Stacionaza), sy(a.Visina)))
			imaVode = true
		}
		voda = append(voda, fmt.Sprintf("%.1f,%.1f", sx(b.Stacionaza), sy(nize(b.Visina, kotaVode))))
		zadnji = b.Stacionaza
		if b.Visina > kotaVode {
			u := (kotaVode - a.Visina) / (b.Visina - a.Visina)
			zadnji = a.Stacionaza + u*(b.Stacionaza-a.Stacionaza)
			voda[len(voda)-1] = fmt.Sprintf("%.1f,%.1f", sx(zadnji), sy(kotaVode))
		}
	}
	if imaVode && len(voda) > 2 {
		voda = append(voda, fmt.Sprintf("%.1f,%.1f", sx(zadnji), sy(kotaVode)),
			fmt.Sprintf("%.1f,%.1f", sx(prvi), sy(kotaVode)))
		c.Voda = strings.Join(voda, " ")
		c.ImaVode = true
		c.SirinaVodeM = zadnji - prvi
		c.VodaX0, c.VodaX1 = sx(prvi), sx(zadnji)
	}

	// Vodoravne podjele. Kad se presjek čita uz graf, os govori u
	// centimetrima na letvi i podjele moraju biti okrugle u njima: kota nule
	// je 80,45 m, pa bi okrugli metar dao −845, −445, −45 i nitko to ne čita.
	if o.OsUCm {
		odCm, doCm := (minV-p.KotaNule)*100, (maxV-p.KotaNule)*100
		korakCm := niceStep((doCm - odCm) / 6)
		for k := math.Ceil(odCm/korakCm) * korakCm; k <= doCm; k += korakCm {
			cm := int(math.Round(k))
			c.KoteY = append(c.KoteY, KotaOznaka{
				Y: sy(p.KotaNule + k/100), Kota: p.KotaNule + k/100, Cm: cm})
		}
		return c
	}
	korak := 1.0
	for (maxV-minV)/korak > 8 {
		korak *= 2
	}
	for k := float64(int(minV/korak)) * korak; k <= maxV; k += korak {
		if k < minV {
			continue
		}
		c.KoteY = append(c.KoteY, KotaOznaka{Y: sy(k), Kota: k,
			Cm: int(math.Round((k - p.KotaNule) * 100))})
	}
	return c
}

// datumHR ispisuje datum snimke onako kako se kod nas piše. U arhivi stoji
// kao 2020-08-18, jer se tako i sortira.
func datumHR(s string) string {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	return t.Format("2.1.2006.")
}

// nize vraća nižu od dvije kote
func nize(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
