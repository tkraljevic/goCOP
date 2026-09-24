package web

import (
	"encoding/json"
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
	polja := strings.Fields(strings.NewReplacer("M", " ", "L", " ", "C", " ", "Z", " ").Replace(put))
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
	p := crtajUzduzni("Drava", []LetvaProfila{a, b}, nil)
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
	}, nil)
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
	p := crtajUzduzni("Drava", []LetvaProfila{a, b}, nil)
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
	}, nil)
	if len(p.Pragovi) != 0 {
		t.Errorf("nacrtano %d pragova, a pripremna je 250 cm iznad vode", len(p.Pragovi))
	}
}

// Kad voda naraste, prag sam uđe u sliku — i to je trenutak kad ga treba
// vidjeti.
func TestBliskiPragUlaziUSliku(t *testing.T) {
	a := letvaProfila("a", 200, 120, 290)
	b := letvaProfila("b", 100, 80, 285)
	p := crtajUzduzni("Drava", []LetvaProfila{a, b}, nil)
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
	}, nil)
	if p == nil {
		t.Fatal("nema profila")
	}
	if len(p.Tocke) != 2 {
		t.Errorf("%d točaka, letva bez kote nije ispuštena", len(p.Tocke))
	}
}

// S jednom letvom profila nema — jedna točka ne pokazuje kako val putuje.
func TestProfilTraziBaremDvijeLetve(t *testing.T) {
	if crtajUzduzni("Drava", []LetvaProfila{letvaProfila("a", 200, 120, 10)}, nil) != nil {
		t.Error("profil nacrtan iz jedne letve")
	}
}

// Put pojasa mora biti valjan SVG: dva slova jedno do drugoga preglednik
// odbacuje, pa se pojas ne nacrta, a greške nigdje nema.
func TestPojasJeValjanPut(t *testing.T) {
	p := crtajUzduzni("Drava", []LetvaProfila{
		letvaProfila("a", 200, 120, 50), letvaProfila("b", 100, 80, 50),
	}, nil)
	loše := regexp.MustCompile(`[MLCZ]\s*[MLCZ]`)
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

// Letva bez vrijednosti se preskače, a susjedi se spoje: crta i pojas prolaze
// kroz dvije od tri letve umjesto da se prekinu. S jednom letvom nema crte.
func TestLetvaBezVrijednostiSePreskace(t *testing.T) {
	a, b, c := letvaProfila("a", 300, 130, 50), letvaProfila("b", 200, 120, 50), letvaProfila("c", 100, 80, 50)
	delete(b.Cm, 24)
	delete(b.Granice, 24)
	p := crtajUzduzni("Drava", []LetvaProfila{a, b, c}, nil)
	for _, cr := range p.Crte {
		if cr.Naziv != "za 24 h" {
			continue
		}
		if strings.Count(cr.Put, "M") != 1 || strings.Contains(cr.Put, "C") {
			t.Errorf("crta za 24 h nije jedan ravan potez kroz a i c: %q", cr.Put)
		}
		if cr.Pojas == "" {
			t.Error("pojas za 24 h nije nacrtan kroz a i c")
		}
	}
	delete(c.Cm, 24)
	p = crtajUzduzni("Drava", []LetvaProfila{a, b, c}, nil)
	for _, cr := range p.Crte {
		if cr.Naziv == "za 24 h" {
			t.Error("crta kroz jednu letvu ne postoji, a nacrtana je")
		}
	}
}

// Kraj pritoke s vrijednostima letve glavnog toka produžuje krivulju do ušća,
// ali nema natpisa, oznake ni utjecaja na pad.
func TestKrajPritokeProduzujeKrivulju(t *testing.T) {
	botovo, osijek := letvaProfila("Botovo", 226.8, 121.3, -3), letvaProfila("Osijek", 19.1, 79.8, -144)
	aljmas := letvaProfila("Aljmaš", 0, 77.4, -47)
	aljmas.Usce, aljmas.Naziv = true, "ušće u Dunav · Aljmaš"
	p := crtajUzduzni("Drava", []LetvaProfila{botovo, osijek, aljmas},
		[]UsceUlaz{{Naziv: "ušće u Dunav", Rkm: 0, Tekst: "Aljmaš -47 cm", Vezano: true, Letva: "Aljmaš"}})
	if len(p.Tocke) != 2 || p.Tocke[1].Naziv != "Osijek" || p.Tocke[1].Sidro != "end" {
		t.Errorf("točke: %+v", p.Tocke)
	}
	if x := xKrajnje(p.Crte[0].Put); math.Abs(x-p.Usca[0].X) > 0.11 {
		t.Errorf("crta završava na %.1f, a ušće je na %.1f", x, p.Usca[0].X)
	}
	if !strings.HasPrefix(p.Pad, "Drava, Botovo → Osijek") {
		t.Errorf("pad računa ušće kao letvu: %q", p.Pad)
	}
}

// visinaKrajnje vraća okomiti položaj zadnje točke puta.
func visinaKrajnje(put string) float64 {
	polja := strings.Fields(strings.NewReplacer("M", " ", "L", " ", "C", " ").Replace(put))
	if len(polja) < 2 {
		return 0
	}
	var v float64
	fmt.Sscanf(polja[len(polja)-1], "%g", &v)
	return v
}

// Crta kroz tri i više letvi je glatka (Bézierovi lukovi), ali prolazi točno
// kroz svaku letvu: krajevi luka su same letve.
func TestCrtaJeGlatkaIProlaziKrozLetve(t *testing.T) {
	p := crtajUzduzni("Drava", []LetvaProfila{
		letvaProfila("a", 300, 130, 50), letvaProfila("b", 200, 120, 50), letvaProfila("c", 100, 80, 50),
	}, nil)
	if p == nil || len(p.Crte) == 0 {
		t.Fatal("nema crta")
	}
	put := p.Crte[0].Put
	if !strings.Contains(put, " C") {
		t.Errorf("crta kroz tri letve nije glatka: %q", put)
	}
	// Zadnja točka puta je zadnja letva.
	if x := xKrajnje(put); math.Abs(x-p.Tocke[2].X) > 0.11 {
		t.Errorf("crta završava na %.1f, a zadnja letva je na %.1f", x, p.Tocke[2].X)
	}
	// Dvije letve: ravna crta, bez luka.
	p2 := crtajUzduzni("Drava", []LetvaProfila{letvaProfila("a", 200, 120, 50), letvaProfila("b", 100, 80, 50)}, nil)
	if strings.Contains(p2.Crte[0].Put, "C") {
		t.Error("kroz dvije letve nema što glačati, a put ima luk")
	}
}

// Svaki doseg ima svoju crtu, redom od sutra do šestog dana, a peti i šesti
// dan pišu se u danima.
func TestSvakiDosegImaSvojuCrtu(t *testing.T) {
	a, b := letvaProfila("a", 200, 120, 50), letvaProfila("b", 100, 80, 50)
	for _, l := range []*LetvaProfila{&a, &b} {
		for _, d := range dosezniProfila {
			l.Cm[d] = l.SadaCm + float64(d)/4
		}
	}
	p := crtajUzduzni("Drava", []LetvaProfila{a, b}, nil)
	if len(p.Crte) != len(dosezniProfila) {
		t.Fatalf("%d crta za %d dosega", len(p.Crte), len(dosezniProfila))
	}
	if p.Crte[0].Naziv != "za 24 h" || p.Crte[0].Class != "h1" {
		t.Errorf("prva crta %q/%q", p.Crte[0].Naziv, p.Crte[0].Class)
	}
	if zadnja := p.Crte[len(p.Crte)-1]; zadnja.Naziv != "za 6 d" || zadnja.Class != "h6" {
		t.Errorf("zadnja crta %q/%q", zadnja.Naziv, zadnja.Class)
	}
}

// Klizač dobiva niz po satu: mjerenja unatrag, nulu u satu izdanja i prognozu
// naprijed, preslikano u koordinate crteža; sat bez vrijednosti je null.
func TestNizZaKlizac(t *testing.T) {
	a, b := letvaProfila("a", 200, 120, 50), letvaProfila("b", 100, 80, 50)
	a.Niz = map[int]float64{-48: 30, -1: 48, 24: 70, 96: 90}
	b.Niz = map[int]float64{-24: 40, 24: 60}
	p := crtajUzduzni("Drava", []LetvaProfila{a, b}, nil)
	if p.SatOd != KlizacOd || p.SatDo != 96 {
		t.Errorf("raspon klizača %d..%d", p.SatOd, p.SatDo)
	}
	var n nizProfila
	if err := json.Unmarshal([]byte(p.Niz), &n); err != nil {
		t.Fatalf("niz nije JSON: %v", err)
	}
	if len(n.Sati) != 96-KlizacOd+1 || n.Sati[0] != KlizacOd || len(n.V) != 2 {
		t.Fatalf("sati %d (prvi %d), letvi %d", len(n.Sati), n.Sati[0], len(n.V))
	}
	i0 := -KlizacOd // sat izdanja
	if n.V[0][i0] == nil || *n.V[0][i0] != 0 {
		t.Error("u satu izdanja odstupanje nije nula")
	}
	if n.V[0][0] == nil || *n.V[0][0] != -20 {
		t.Errorf("prije 48 h letva a bila je 20 cm niže, a niz kaže %v", n.V[0][0])
	}
	if n.V[1][0] != nil {
		t.Error("letva b prije 48 h nema mjerenje, a niz ima vrijednost")
	}
	// Mjerenje od prije dva dana ulazi u mjerilo: −20 cm mora stati u sliku.
	if n.NulaY-(-20)*n.PoCm > float64(p.Height)-p.Dno {
		t.Error("mjerenje unatrag ispada iz crteža")
	}
}

// Letve preblizu jedna drugoj dobiju natpis u drugom redu; udaljene ostaju u prvom.
func TestNatpisiSeNePreklapaju(t *testing.T) {
	p := crtajUzduzni("Dunav", []LetvaProfila{
		letvaProfila("a", 300, 100, 50), letvaProfila("b", 110, 90, 50),
		letvaProfila("c", 105, 89, 50), letvaProfila("d", 100, 88, 50),
	}, nil)
	// a, b gore; c dolje; d ne stane nigdje pa ide gdje je susjed dalje — dolje je c preblizu, gore b još bliže: dolje.
	if p.Tocke[0].Dolje || p.Tocke[1].Dolje || !p.Tocke[2].Dolje {
		t.Errorf("redovi natpisa: %v %v %v %v", p.Tocke[0].Dolje, p.Tocke[1].Dolje, p.Tocke[2].Dolje, p.Tocke[3].Dolje)
	}
}

// Natpis ispod crteža govori o toj rijeci, ne o nekoj drugoj.
func TestPadPoRijeci(t *testing.T) {
	p := crtajUzduzni("Dunav", []LetvaProfila{letvaProfila("Batina", 1425, 79.15, -104), letvaProfila("Ilok", 1299, 73.34, -36)}, nil)
	if p.Pad != "Dunav, Batina → Ilok: vodno lice pada 5,1 m na 126 km" {
		t.Errorf("pad: %q", p.Pad)
	}
}

func xKrajnje(put string) float64 {
	polja := strings.Fields(strings.NewReplacer("M", " ", "L", " ", "C", " ").Replace(put))
	if len(polja) < 2 {
		return 0
	}
	var v float64
	fmt.Sscanf(polja[len(polja)-2], "%g", &v)
	return v
}

// Ušće blizu krajnje letve uđe u sliku i produži crtež do sebe; daleko ne.
func TestUsceUlaziUSlikuKadJeBlizu(t *testing.T) {
	letve := []LetvaProfila{letvaProfila("Botovo", 226.8, 121.3, -3), letvaProfila("Osijek", 19.1, 79.8, -143)}
	p := crtajUzduzni("Drava", letve, []UsceUlaz{
		{Naziv: "ušće u Dunav", Rkm: 0, Tekst: "Aljmaš -45 cm", Vezano: true},
		{Naziv: "ušće Mure", Rkm: 236.3, Vezano: true},
		{Naziv: "ušće Plitvice", Rkm: 250, Vezano: false}, // blizu, ali bez letvi s druge strane
		{Naziv: "predaleko", Rkm: 400, Vezano: true},
	})
	if len(p.Usca) != 2 {
		t.Fatalf("%d ušća u slici, očekivano 2: %+v", len(p.Usca), p.Usca)
	}
	// Ušće u Dunav je desno od Osijeka (rkm 0 je nizvodno), ušće Mure lijevo od Botova.
	if !(p.Usca[1].X > p.Tocke[1].X) || !(p.Usca[0].X < p.Tocke[0].X) {
		t.Errorf("ušća nisu na svojim stranama: %v %v prema letvama %v %v", p.Usca[0].X, p.Usca[1].X, p.Tocke[0].X, p.Tocke[1].X)
	}
	// Ušća idu redom toka: Mura prvo, ušće u Dunav zadnje.
	if p.Usca[0].Naziv != "ušće Mure" || p.Usca[1].Tekst != "Aljmaš -45 cm" || p.Usca[1].Sidro != "end" {
		t.Errorf("ušća: %+v", p.Usca)
	}
}

// Ušće između letvi ne mijenja mjerilo, samo dobije oznaku.
func TestUsceIzmeduLetvi(t *testing.T) {
	letve := []LetvaProfila{letvaProfila("Batina", 1424.8, 79.15, -104), letvaProfila("Ilok", 1298.7, 73.34, -36)}
	bez := crtajUzduzni("Dunav", letve, nil)
	sa := crtajUzduzni("Dunav", letve, []UsceUlaz{{Naziv: "ušće Drave", Rkm: 1382.5, Tekst: "Osijek -143 cm"}})
	if sa.Tocke[0].X != bez.Tocke[0].X || sa.Tocke[1].X != bez.Tocke[1].X {
		t.Error("ušće između letvi pomaknulo je letve")
	}
	if len(sa.Usca) != 1 || sa.Usca[0].X <= sa.Tocke[0].X || sa.Usca[0].X >= sa.Tocke[1].X {
		t.Errorf("ušće Drave nije između Batine i Iloka: %+v", sa.Usca)
	}
}

// Točka ušća ne smije sama tvoriti crtu: kad vrijednost ima samo jedna prava
// letva, crte za taj doseg nema — ni u crtežu ni u nizu za klizač.
func TestUsceSamoProduzujeKrivulju(t *testing.T) {
	botovo, osijek := letvaProfila("Botovo", 226.8, 121.3, -3), letvaProfila("Osijek", 19.1, 79.8, -144)
	aljmas := letvaProfila("Aljmaš", 0, 77.4, -47)
	aljmas.Usce = true
	delete(botovo.Cm, 48)
	delete(botovo.Granice, 48)
	p := crtajUzduzni("Drava", []LetvaProfila{botovo, osijek, aljmas}, nil)
	for _, c := range p.Crte {
		if c.Naziv == "za 48 h" {
			t.Errorf("crta za 48 h razapeta između Osijeka i ušća: %q", c.Put)
		}
	}
	var n nizProfila
	if err := json.Unmarshal([]byte(p.Niz), &n); err != nil || len(n.Usce) != 3 || !n.Usce[2] || n.Usce[0] {
		t.Errorf("niz ne označava točku ušća: %v %v", n.Usce, err)
	}
}

// Letva na kanalu („nkm”) nije na rijeci: profil je ne uzima, ma koliko
// kilometar izgledao kao riječni.
func TestRijecniKmOdbijaKanal(t *testing.T) {
	if _, ok := rijecniKm("nkm 19,55"); ok {
		t.Error("nkm pročitan kao riječni kilometar")
	}
	if km, ok := rijecniKm("rkm 1424+850"); !ok || km < 1424 || km > 1425 {
		t.Errorf("rkm 1424+850 → %v %v", km, ok)
	}
}
