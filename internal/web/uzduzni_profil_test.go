package web

import (
	"encoding/json"
	"fmt"
	"gocop/internal/models"
	"gocop/internal/prognoza"
	"math"
	"regexp"
	"strings"
	"testing"
	"time"
)

func letvaProfila(letva string, rkm, kota, sada float64) LetvaProfila {
	return LetvaProfila{
		Letva: letva, Naziv: letva, Rkm: rkm, KotaNule: kota,
		SadaCm: sada, ImaSada: true, JutroCm: sada, ImaJutro: true, UobicajenoCm: sada, ImaUobicajeno: true,
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
	// Uz letvu piše vodostaj, bez kote: kotu svatko računa po svojem sustavu.
	if p.Tocke[0].Cm != "50" || p.Tocke[0].Kota != "" {
		t.Errorf("natpis uzvodne letve %q (%q)", p.Tocke[0].Cm, p.Tocke[0].Kota)
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

// Svaki doseg ima svoju crtu, redom od 6 h do šestog dana, a peti i šesti
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
	if p.Crte[0].Naziv != "za 6 h" || p.Crte[0].Class != "h1" {
		t.Errorf("prva crta %q/%q", p.Crte[0].Naziv, p.Crte[0].Class)
	}
	if zadnja := p.Crte[len(p.Crte)-1]; zadnja.Naziv != "za 6 d" || zadnja.Class != "h8" {
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

// Letve preblizu jedna drugoj dobiju natpis u drugom redu, naizmjence; kad
// ima mjesta, sve stoje u prvom.
func TestNatpisiSeNePreklapaju(t *testing.T) {
	var gusto []LetvaProfila
	for k := 0; k < 14; k++ {
		gusto = append(gusto, letvaProfila(fmt.Sprint("l", k), float64(100-k), 90, 50))
	}
	p := crtajUzduzni("Dunav", gusto, nil)
	for k, tk := range p.Tocke {
		if tk.Dolje != (k%2 == 1) {
			t.Errorf("letva %d: dolje=%v", k, tk.Dolje)
		}
	}
	p = crtajUzduzni("Dunav", []LetvaProfila{
		letvaProfila("a", 300, 100, 50), letvaProfila("b", 110, 90, 50), letvaProfila("c", 105, 89, 50),
	}, nil)
	for _, tk := range p.Tocke {
		if tk.Dolje {
			t.Errorf("%s u drugom redu, a mjesta ima", tk.Naziv)
		}
	}
}

// Natpis ispod crteža govori o toj rijeci, ne o nekoj drugoj.
func TestPadPoRijeci(t *testing.T) {
	p := crtajUzduzni("Dunav", []LetvaProfila{letvaProfila("Batina", 1425, 79.15, -104), letvaProfila("Ilok", 1299, 73.34, -36)}, nil)
	if p.Pad != "Dunav, Batina → Ilok: vodno lice jutros pada 5,1 m na 126 km" {
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

// Brana među letvama dobije okomitu crtu na svojem kilometru; brana iznad
// prve letve ne ulazi u crtež.
func TestBraneMeduLetvamaUlazeUProfil(t *testing.T) {
	a, b := letvaProfila("varazdin", 288, 165, 100), letvaProfila("botovo", 227, 122, 100)
	p := crtajUzduzni("Drava", []LetvaProfila{a, b}, nil,
		BranaUlaz{Naziv: "Brana HE Varaždin", Rkm: 308.6},
		BranaUlaz{Naziv: "Brana HE Čakovec", Rkm: 278.6},
		BranaUlaz{Naziv: "Brana HE Dubrava", Rkm: 255.05})
	if p == nil {
		t.Fatal("profila nema")
	}
	if len(p.Brane) != 2 || p.Brane[0].Naziv != "Brana HE Čakovec" || p.Brane[1].Naziv != "Brana HE Dubrava" {
		t.Fatalf("brane %+v — Varaždin je iznad prve letve", p.Brane)
	}
	xa, xb := p.Tocke[0].X, p.Tocke[1].X
	if x := p.Brane[0].X; x <= xa || x >= p.Brane[1].X || p.Brane[1].X >= xb {
		t.Errorf("brane moraju stajati redom među letvama: %.0f < %.0f < %.0f < %.0f", xa, x, p.Brane[1].X, xb)
	}
}

// Sve letve toka idu na jedan profil, s kotom u Trstu: naša i srpska iz
// Trsta, mađarska iz baltičke uz +0,675 m.
func TestProfilVodeCrtaUTrstu(t *testing.T) {
	trst, hv := 80.45, 80.189
	nasa := models.Station{Name: "Batina", Watercourse: "Dunav", ZeroDatum: &trst, ZeroDatumNew: &hv}
	if v, k, ok := profilVode(nasa); !ok || v != "Dunav" || k != trst {
		t.Errorf("naša letva: %q %v %v", v, k, ok)
	}
	rs := 80.64
	srpska := models.Station{Name: "Bezdan (Srbija)", Watercourse: "Dunav", ZeroDatum: &rs}
	if v, k, ok := profilVode(srpska); !ok || v != "Dunav" || k != rs {
		t.Errorf("srpska letva: %q %v %v", v, k, ok)
	}
	mbf := 95.65
	hu := models.Station{Name: "Budapest (Mađarska)", Watercourse: "Dunav", ZeroDatumBaltic: &mbf, ZeroDatumBalticSystem: "mBf (Mađarska)"}
	if v, k, ok := profilVode(hu); !ok || v != "Dunav" || math.Abs(k-96.325) > 1e-9 {
		t.Errorf("mađarska letva: %q %v %v", v, k, ok)
	}
	if _, _, ok := profilVode(models.Station{Watercourse: "Dunav"}); ok {
		t.Error("letva bez ijedne kote ne ide na profil")
	}
}

// Jutarnja crta stoji na odstupanju jutra od uobičajene vode: val koji je
// jutros u Budimpešti brijeg je iznad nule, a točka letve sjedi na jutarnjoj
// crti, ne na nuli. Uz letvu piše samo jutarnji vodostaj, bez kote.
func TestJutarnjaCrtaOdUobicajeneVode(t *testing.T) {
	bp := letvaProfila("Budapest", 1646, 96.3, 450)
	bp.UobicajenoCm = 300 // val: 150 cm iznad uobičajenog
	bt := letvaProfila("Batina", 1425, 80.45, 250)
	p := crtajUzduzni("Dunav", []LetvaProfila{bp, bt}, nil)
	if p == nil || p.JutroPut == "" {
		t.Fatal("nema jutarnje crte")
	}
	if !(p.Tocke[0].Y < p.NulaY-1) || math.Abs(p.Tocke[1].Y-p.NulaY) > 1e-9 {
		t.Errorf("Budimpešta mora biti iznad nule, Batina na nuli: %.1f %.1f (nula %.1f)", p.Tocke[0].Y, p.Tocke[1].Y, p.NulaY)
	}
	for i, cm := range []string{"450", "250"} {
		if p.Tocke[i].Cm != cm || p.Tocke[i].Kota != "" {
			t.Errorf("%s: %q (%q)", p.Tocke[i].Naziv, p.Tocke[i].Cm, p.Tocke[i].Kota)
		}
	}
	// Letva bez jutra ili bez uobičajene vode čeka: na profil ne ide.
	bez := letvaProfila("Mohács", 1447, 80.55, 300)
	bez.ImaJutro = false
	if q := crtajUzduzni("Dunav", []LetvaProfila{bp, bez, bt}, nil); len(q.Tocke) != 2 {
		t.Errorf("letva bez jutra ušla je u profil: %+v", q.Tocke)
	}
	// Klizač nosi jutro i uobičajenu vodu, da uz točku piše promjena od jutra.
	var n nizProfila
	if err := json.Unmarshal([]byte(p.Niz), &n); err != nil || n.Jutro[0] != 450 || n.Uob[0] != 300 {
		t.Errorf("niz: jutro %v uob %v (%v)", n.Jutro, n.Uob, err)
	}
	if v := n.V[0][-KlizacOd]; v == nil || *v != 150 {
		t.Errorf("u satu izdanja Budimpešta je 150 cm iznad uobičajenog, niz kaže %v", v)
	}
}

// Jutro je očitanje u 7 h; dok ga nema, najnovije od 4 h; prije 4 h nema ga.
func TestJutroLetve(t *testing.T) {
	dan := time.Date(2026, 9, 27, 0, 0, 0, 0, models.Zagreb)
	u := func(h, m int, cm float64) ocitanje {
		return ocitanje{Kad: dan.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute), Cm: cm}
	}
	if cm, _, ok := jutroLetve([]ocitanje{u(3, 0, 10), u(5, 0, 20), u(6, 0, 30), u(7, 0, 40), u(8, 0, 50)}, dan); !ok || cm != 40 {
		t.Errorf("s očitanjem u 7: %v %v", cm, ok)
	}
	if cm, _, ok := jutroLetve([]ocitanje{u(3, 0, 10), u(4, 0, 20), u(5, 0, 30)}, dan); !ok || cm != 30 {
		t.Errorf("prije 7 vrijedi najnovije od 4 h: %v %v", cm, ok)
	}
	if cm, _, ok := jutroLetve([]ocitanje{u(6, 50, 35), u(7, 20, 45)}, dan); !ok || cm != 35 {
		t.Errorf("najbliže 7 h: %v %v", cm, ok)
	}
	if _, _, ok := jutroLetve([]ocitanje{u(3, 0, 10), u(9, 0, 60)}, dan); ok {
		t.Error("letva bez očitanja od 4 do 7 h nema jutra")
	}
}

// Današnje jutro vrijedi čim ga ima pola letvi; do tada stoji jučerašnje.
func TestOdaberiJutro(t *testing.T) {
	danas := time.Date(2026, 9, 27, 0, 0, 0, 0, models.Zagreb)
	jucer := danas.AddDate(0, 0, -1)
	u := func(dan time.Time, h int, cm float64) ocitanje {
		return ocitanje{Kad: dan.Add(time.Duration(h) * time.Hour), Cm: cm}
	}
	sva := map[string][]ocitanje{
		"a": {u(jucer, 7, 100), u(danas, 7, 110)},
		"b": {u(jucer, 7, 200)},
		"c": {u(jucer, 7, 300)},
	}
	j := odaberiJutro(sva, danas.Add(8*time.Hour))
	if !j.Dan.Equal(jucer) || j.Cm["b"] != 200 {
		t.Errorf("s jednom od tri letve stoji jučerašnje jutro: %v %v", j.Dan, j.Cm)
	}
	sva["b"] = append(sva["b"], u(danas, 5, 210))
	j = odaberiJutro(sva, danas.Add(8*time.Hour))
	if !j.Dan.Equal(danas) || j.Cm["a"] != 110 || j.Cm["b"] != 210 {
		t.Errorf("s dvije od tri letve vrijedi današnje: %v %v", j.Dan, j.Cm)
	}
	if _, ima := j.Cm["c"]; ima {
		t.Error("letva bez današnjeg jutra ne smije nositi jučerašnje")
	}
}

// Razina akumulacije uz branu: točka s kotom nad morem, među letvama, izvan
// krivulje; ona iznad prve letve ne ulazi.
func TestAkumulacijaJeTockaIzvanKrivulje(t *testing.T) {
	a, b := letvaProfila("varazdin", 288, 165, 100), letvaProfila("botovo", 227, 122, 100)
	ak := LetvaProfila{Letva: "gvb-he-cakovec", Naziv: "Razina akumulacije Čakovec", Rkm: 278.6, Akumulacija: true,
		SadaCm: 16752, ImaSada: true, JutroCm: 16752, ImaJutro: true, UobicajenoCm: 16740, ImaUobicajeno: true, Cm: map[int]float64{}, Granice: map[int][2]float64{}, Pragovi: map[string]float64{}, Niz: map[int]float64{-6: 16740}}
	gore := ak
	gore.Letva, gore.Naziv, gore.Rkm = "gvb-he-varazdin", "Razina akumulacije Varaždin", 308.6
	p := crtajUzduzni("Drava", []LetvaProfila{a, ak, gore, b}, nil)
	if p == nil {
		t.Fatal("profila nema")
	}
	if len(p.Tocke) != 3 || p.Tocke[1].Naziv != "Razina akumulacije Čakovec" || p.Tocke[1].Kota != "167,52" || p.Tocke[1].Cm != "" {
		t.Fatalf("točke %+v", p.Tocke)
	}
	if len(p.XTicks) != 2 {
		t.Errorf("akumulacija ne dobiva oznaku kilometra: %+v", p.XTicks)
	}
	if !strings.Contains(string(p.Niz), `"akum":[false,true,false]`) {
		t.Errorf("niz za klizač mora označiti akumulaciju: %s", p.Niz)
	}
	if !strings.Contains(p.Pad, "Varaždin → botovo") && !strings.Contains(p.Pad, "varazdin → botovo") {
		t.Errorf("krajevi profila su prave letve: %s", p.Pad)
	}
}

// Red uz tok: ulaz stoji prije postaje koju hrani i kad mu je upisani
// kilometar manji; postaja bez kilometra staje tik ispred one koju hrani.
func TestRedUzTokSlijediUlaze(t *testing.T) {
	pojasi := map[string][]prognoza.Pojas{
		"botovo":    {{Letva: "botovo", Ulazi: []prognoza.Ulaz{{Letva: "he-dubrava"}}}},
		"nagybajcs": {{Letva: "nagybajcs", Ulazi: []prognoza.Ulaz{{Letva: "wildungsmauer"}}}},
		"komarom":   {{Letva: "komarom", Ulazi: []prognoza.Ulaz{{Letva: "nagybajcs"}}}},
	}
	km := map[string]float64{"botovo": 226.8, "he-dubrava": 225.0, "nagybajcs": 1801, "komarom": 1768, "wildungsmauer": -1}
	rkm := func(k string) float64 { return km[k] }
	if got := strings.Join(redUzTok([]string{"botovo", "he-dubrava"}, pojasi, rkm), ","); got != "he-dubrava,botovo" {
		t.Errorf("Drava: %s", got)
	}
	if got := strings.Join(redUzTok([]string{"komarom", "nagybajcs", "wildungsmauer"}, pojasi, rkm), ","); got != "wildungsmauer,nagybajcs,komarom" {
		t.Errorf("Dunav: %s", got)
	}
}

// Razmak letvi nije u mjerilu: gusto posađene letve dobiju mjesta, a redoslijed
// i kilometri između čvorova ostaju.
func TestOsRazmakaRaspoređujeLetve(t *testing.T) {
	// Budimpešta, Dunaföldvár, Batina, Siga, Petreš: 86, 136, 12, 19 km.
	x := osRazmaka([]float64{1646.5, 1560.6, 1424.8, 1412.2, 1393}, 0, 1000)
	if x(1646.5) != 0 || math.Abs(x(1393)-1000) > 1e-9 {
		t.Fatalf("krajevi %v %v", x(1646.5), x(1393))
	}
	uska := x(1412.2) - x(1424.8)
	if uska < 0.7*1000/4 {
		t.Errorf("Batina–Siga dobila %.0f točaka, manje od jednakog dijela", uska)
	}
	if siroka := x(1424.8) - x(1560.6); siroka <= uska {
		t.Errorf("duga dionica %.0f nije šira od kratke %.0f", siroka, uska)
	}
	if m := x(1418.5); m <= x(1424.8) || m >= x(1412.2) {
		t.Errorf("kilometar između letvi pada izvan njih: %v", m)
	}
}

func TestKratkoIme(t *testing.T) {
	if ime, z := kratkoIme("Komárom (Mađarska)"); ime != "Komárom" || z != "HU" {
		t.Errorf("%q %q", ime, z)
	}
	if ime, z := kratkoIme("Batina"); ime != "Batina" || z != "" {
		t.Errorf("%q %q", ime, z)
	}
}

// Dnevni model na dijelu letvi: crta se prekida, usamljena letva dobije kružić.
func TestDnevnaCrtaSePrekida(t *testing.T) {
	a, b, c, d := letvaProfila("a", 400, 130, 50), letvaProfila("b", 300, 120, 50),
		letvaProfila("c", 200, 110, 50), letvaProfila("d", 100, 80, 50)
	a.Cm[120], b.Cm[120], d.Cm[120] = 60, 70, 55 // c nema dnevnu prognozu
	p := crtajUzduzni("Drava", []LetvaProfila{a, b, c, d}, nil)
	var put string
	for _, cr := range p.Crte {
		if cr.Doseg == 120 {
			put = cr.Put
		}
	}
	if strings.Count(put, "M") != 2 || !strings.Contains(put, " a3.5") {
		t.Errorf("crta za 5 d nije prekinuta u c s kružićem na d: %q", put)
	}
}

// Razina akumulacije ne ulazi ni u jutarnju crtu ni u crte prognoze: njezino
// odstupanje od uobičajene kote nije val rijeke.
func TestAkumulacijaNijeUCrtama(t *testing.T) {
	a, b := letvaProfila("varazdin", 288, 165, 100), letvaProfila("botovo", 227, 122, 100)
	ak := LetvaProfila{Letva: "gvb-he-dubrava", Naziv: "Razina akumulacije Dubrava", Rkm: 255, Akumulacija: true,
		JutroCm: 14749, ImaJutro: true, UobicajenoCm: 14893, ImaUobicajeno: true,
		Cm: map[int]float64{24: 14760}, Granice: map[int][2]float64{24: {14700, 14800}}, Pragovi: map[string]float64{}}
	p := crtajUzduzni("Drava", []LetvaProfila{a, ak, b}, nil)
	if strings.Contains(p.JutroPut, " C") || strings.Count(p.JutroPut, "L") != 1 {
		t.Errorf("jutarnja crta prolazi kroz akumulaciju: %q", p.JutroPut)
	}
	for _, c := range p.Crte {
		if strings.Contains(c.Put, " C") {
			t.Errorf("crta %s prolazi kroz akumulaciju: %q", c.Naziv, c.Put)
		}
	}
}

// Nula je voda zadnjih 30 dana. Dugogodišnji medijan i srednjak ulaze u
// crtež samo kad su blizu; daleki ostaju izvan, da val dobije cijelu visinu,
// a gore piše gdje su.
func TestDalekaUobicajenaVodaNeSpljostiVal(t *testing.T) {
	a, b := letvaProfila("Batina", 1425, 80, 50), letvaProfila("Ilok", 1299, 74, 60)
	a.UobicajenoCm, b.UobicajenoCm = 60, 70     // mjesec: 10 cm ispod
	a.DugiMedijanCm, b.DugiMedijanCm = 300, 290 // deset godina: daleko iznad
	a.SrednjakCm, b.SrednjakCm = 320, 310
	p := crtajUzduzni("Dunav", []LetvaProfila{a, b}, nil)
	if !p.NulaUSlici || p.DugiPut != "" || p.SrednjakPut != "" ||
		p.NulaNapomena != "↑ uobičajena voda (10 g.) i srednji vodostaj su iznad crteža" {
		t.Errorf("nula %v, dugi %q, SV %q, napomena %q", p.NulaUSlici, p.DugiPut, p.SrednjakPut, p.NulaNapomena)
	}
	// Blizu: sve tri crte u slici, bez napomene.
	a.DugiMedijanCm, b.DugiMedijanCm, a.SrednjakCm, b.SrednjakCm = 70, 80, 90, 100
	q := crtajUzduzni("Dunav", []LetvaProfila{a, b}, nil)
	if !q.NulaUSlici || q.DugiPut == "" || q.SrednjakPut == "" || q.NulaNapomena != "" {
		t.Errorf("blizu: nula %v, dugi %q, SV %q, napomena %q", q.NulaUSlici, q.DugiPut, q.SrednjakPut, q.NulaNapomena)
	}
	// Kad je i mjesečna voda daleko (val je naglo spustio vodu), ni nule nema.
	a.UobicajenoCm, b.UobicajenoCm = 300, 290
	r := crtajUzduzni("Dunav", []LetvaProfila{a, b}, nil)
	if r.NulaUSlici || !strings.HasPrefix(r.NulaNapomena, "↑ voda zadnjih 30 dana (0)") || r.NulaY >= r.Vrh {
		t.Errorf("daleka nula: %v %q %.1f", r.NulaUSlici, r.NulaNapomena, r.NulaY)
	}
}

// Mjesečni medijan traži barem tjedan dana mjerenja.
func TestMedijanSati(t *testing.T) {
	poSatu := map[int64]float64{}
	for h := int64(0); h < 6*24; h++ {
		poSatu[h] = 20
	}
	if _, ok := medijanSati(poSatu); ok {
		t.Error("šest dana je premalo")
	}
	poSatu[6*24+1] = 100
	if m, ok := medijanSati(poSatu); !ok || m != 20 {
		t.Errorf("medijan %v %v", m, ok)
	}
}
