package web

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"testing"
)

func letvaProfila(letva string, rkm, kota, sada float64) LetvaProfila {
	return LetvaProfila{
		Letva: letva, Naziv: letva, Rkm: rkm, KotaNule: kota,
		SadaCm: sada, ImaSada: true,
		Cm:      map[int]float64{24: sada + 20, 48: sada + 40},
		Granice: map[int][2]float64{24: {sada + 10, sada + 30}, 48: {sada + 20, sada + 60}},
		Pragovi: map[string]float64{"prep": 300, "regular": 400, "emerg": 500},
	}
}

// visinaCrte je koliko okomitog prostora crta zauzima.
func visinaCrte(put string) float64 {
	najn, najv := math.Inf(1), math.Inf(-1)
	polja := strings.Fields(strings.NewReplacer("M", " ", "L", " ", "Z", " ").Replace(put))
	for i := 1; i < len(polja); i += 2 {
		var v float64
		if _, err := fmt.Sscanf(polja[i], "%g", &v); err == nil {
			najn, najv = math.Min(najn, v), math.Max(najv, v)
		}
	}
	if math.IsInf(najn, 1) {
		return 0
	}
	return najv - najn
}

// Profil crta promjenu, ne kotu. Dvije letve s posve različitim kotama, a
// jednakom promjenom vode, moraju ležati na istoj visini — inače crtež pokazuje
// pad rijeke umjesto vala.
func TestProfilCrtaPromjenuANeKotu(t *testing.T) {
	a := letvaProfila("uzvodna", 200, 120, 50)
	b := letvaProfila("nizvodna", 100, 80, 50) // 40 m niža kota nule
	p := crtajUzduzni("Drava", []LetvaProfila{a, b})
	if p == nil {
		t.Fatal("nema profila")
	}
	// Obje letve sjede na crti današnjeg stanja.
	for _, tk := range p.Tocke {
		if math.Abs(tk.Y-p.NulaY) > 1e-9 {
			t.Errorf("%s nije na crti današnjeg stanja", tk.Naziv)
		}
	}
	// Ali kota se ne gubi — piše uz letvu.
	if p.Tocke[0].Kota != "120,50" {
		t.Errorf("kota uzvodne letve %q, očekivano 120,50", p.Tocke[0].Kota)
	}
	// Nizvodno je desno.
	if p.Tocke[0].X >= p.Tocke[1].X {
		t.Error("nizvodna letva nije desno od uzvodne")
	}
}

// Mjerilo se ravna prema valu: promjena od četrdesetak centimetara mora
// zauzeti dobar dio visine, inače crtež ne pokazuje ono zbog čega postoji.
func TestMjeriloSeRavnaPremaValu(t *testing.T) {
	p := crtajUzduzni("Drava", []LetvaProfila{
		letvaProfila("a", 200, 120, 50), letvaProfila("b", 100, 80, 50),
	})
	if p == nil || len(p.Crte) == 0 {
		t.Fatal("nema crta")
	}
	visina := float64(p.Height) - p.Vrh - p.Dno
	for _, c := range p.Crte {
		if c.Naziv != "za 48 h" {
			continue
		}
		// Crta ide od nule do +40 cm; mora zauzeti barem trećinu plohe.
		if v := math.Abs(p.NulaY - visinaKrajnje(c.Put)); v < visina/3 {
			t.Errorf("crta za 48 h odmaknuta %.0f od %.0f točaka visine", v, visina)
		}
	}
}

// Na mirnoj vodi mjerilo se ne smije rastegnuti na šum: dva centimetra
// promjene ne smiju izgledati kao val.
func TestMirnaVodaNeRastegneMjerilo(t *testing.T) {
	a := letvaProfila("a", 200, 120, 50)
	b := letvaProfila("b", 100, 80, 50)
	a.Cm = map[int]float64{24: 51, 48: 52}
	b.Cm = map[int]float64{24: 50, 48: 51}
	a.Granice, b.Granice = nil, nil
	p := crtajUzduzni("Drava", []LetvaProfila{a, b})
	if p == nil {
		t.Fatal("nema profila")
	}
	visina := float64(p.Height) - p.Vrh - p.Dno
	for _, c := range p.Crte {
		if v := visinaCrte(c.Put); v > visina/2 {
			t.Errorf("crta %q zauzima %.0f od %.0f — dva centimetra izgledaju kao val",
				c.Naziv, v, visina)
		}
	}
}

// Pri maloj vodi prag je metrima iznad i spljoštio bi crtež, pa se ne crta.
func TestDalekiPragNeUlaziUSliku(t *testing.T) {
	p := crtajUzduzni("Drava", []LetvaProfila{
		letvaProfila("a", 200, 120, 50), letvaProfila("b", 100, 80, 50),
	})
	if len(p.Pragovi) != 0 {
		t.Errorf("nacrtano %d pragova, a pripremna je 250 cm iznad vode", len(p.Pragovi))
	}
}

// Kad voda naraste, prag sam uđe u sliku — i to je trenutak kad ga treba
// vidjeti.
func TestBliskiPragUlaziUSliku(t *testing.T) {
	a := letvaProfila("a", 200, 120, 290)
	b := letvaProfila("b", 100, 80, 285)
	p := crtajUzduzni("Drava", []LetvaProfila{a, b})
	if len(p.Pragovi) == 0 {
		t.Fatal("pripremna je desetak centimetara iznad vode, a nije nacrtana")
	}
	if p.Pragovi[0].Naziv != "pripremna" {
		t.Errorf("prvi nacrtani prag je %q", p.Pragovi[0].Naziv)
	}
	// Izvanredna je i dalje dva metra iznad; nju ne treba crtati.
	for _, pr := range p.Pragovi {
		if pr.Naziv == "izvanredna" {
			t.Error("izvanredna obrana nacrtana iako je dva metra iznad")
		}
	}
}

// Letva bez kote nule ili bez stacionaže ne može na profil: ne zna se ni gdje
// je ni koliko visoko. Ispuštanje je poštenije od nagađanja.
func TestProfilIzostavljaLetvuBezKote(t *testing.T) {
	p := crtajUzduzni("Drava", []LetvaProfila{
		letvaProfila("a", 200, 120, 10),
		letvaProfila("bez-kote", 150, 0, 50),
		letvaProfila("b", 100, 90, 300),
	})
	if p == nil {
		t.Fatal("nema profila")
	}
	if len(p.Tocke) != 2 {
		t.Errorf("%d točaka, letva bez kote nije ispuštena", len(p.Tocke))
	}
}

// S jednom letvom profila nema — jedna točka ne pokazuje kako val putuje.
func TestProfilTraziBaremDvijeLetve(t *testing.T) {
	if crtajUzduzni("Drava", []LetvaProfila{letvaProfila("a", 200, 120, 10)}) != nil {
		t.Error("profil nacrtan iz jedne letve")
	}
}

// Put pojasa mora biti valjan SVG: dva slova jedno do drugoga preglednik
// odbacuje, pa se pojas ne nacrta, a greške nigdje nema.
func TestPojasJeValjanPut(t *testing.T) {
	p := crtajUzduzni("Drava", []LetvaProfila{
		letvaProfila("a", 200, 120, 50), letvaProfila("b", 100, 80, 50),
	})
	loše := regexp.MustCompile(`[MLZ]\s*[MLZ]`)
	for _, c := range p.Crte {
		if c.Pojas == "" {
			continue
		}
		if m := loše.FindString(c.Pojas); m != "" {
			t.Errorf("pojas %q ima dva poteza zaredom: %q", c.Naziv, m)
		}
		if !strings.HasPrefix(c.Pojas, "M") || !strings.HasSuffix(c.Pojas, " Z") {
			t.Errorf("pojas %q nije zatvorena ploha", c.Naziv)
		}
	}
}

// Pojas s rupom ne smije se nacrtati: spojio bi granice preko letve za koju se
// ne zna, i pokazao ogradu koja ne postoji.
func TestPojasSRupomSeNeCrta(t *testing.T) {
	a := letvaProfila("a", 200, 120, 50)
	b := letvaProfila("b", 100, 80, 50)
	delete(b.Granice, 24)
	p := crtajUzduzni("Drava", []LetvaProfila{a, b})
	for _, c := range p.Crte {
		if c.Naziv == "za 24 h" && c.Pojas != "" {
			t.Error("pojas nacrtan preko letve bez granica")
		}
	}
}

// visinaKrajnje vraća okomiti položaj zadnje točke puta.
func visinaKrajnje(put string) float64 {
	polja := strings.Fields(strings.NewReplacer("M", " ", "L", " ").Replace(put))
	if len(polja) < 2 {
		return 0
	}
	var v float64
	fmt.Sscanf(polja[len(polja)-1], "%g", &v)
	return v
}
