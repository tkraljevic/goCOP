package web

import (
	"bytes"
	"context"
	"database/sql"
	"html/template"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
	"gocop/web"
)

// danNule je dan jutra u testovima: 9. 10. 2026. u ponoć po zagrebačkom vremenu.
var danNule = time.Date(2026, 10, 9, 0, 0, 0, 0, models.Zagreb)

// nizOd puni satni niz od (dana prije danNule) do ponoći danNule vrijednošću
// koju daje f(sati od početka).
func nizOd(danaPrije int, f func(h int) float64) map[int64]float64 {
	pocetak := danNule.AddDate(0, 0, -danaPrije).Unix() / 3600
	kraj := danNule.Unix() / 3600
	niz := map[int64]float64{}
	for h := pocetak; h < kraj; h++ {
		niz[h] = f(int(h - pocetak))
	}
	return niz
}

// nulaDunava slaže ulaz za sloziNulu: letve istog toka s kilometrima.
func nulaDunava(rkm map[string]float64) NulaProfila {
	n := NulaProfila{Dan: danNule, Voda: map[string]string{}, Rkm: rkm, Naziv: map[string]string{}}
	for l := range rkm {
		n.Voda[l] = "Dunav"
		n.Naziv[l] = strings.ToUpper(l[:1]) + l[1:]
	}
	return n
}

// Siga 2026.: mjerilo dva tjedna stoji na 19 cm, a Batina i Aljmaš padaju.
// Takva letva crtala bi grbu na nuli, pa s profila ide uz napomenu; susjedi
// ostaju.
func TestMjeriloKojeStojiIspadaKadSeSusjediMicu(t *testing.T) {
	pada := func(h int) float64 { return -90 - float64(h)/10 }
	nizovi := map[string]map[int64]float64{
		"gornja":  nizOd(30, pada),
		"zapela":  nizOd(20, func(h int) float64 { return map[bool]float64{true: 40 - float64(h)/5, false: 19}[h < 105] }),
		"donja":   nizOd(30, pada),
		"zapela2": nizOd(20, func(h int) float64 { return 25 + float64(h%2) }),
	}
	n := sloziNulu(nizovi, nulaDunava(map[string]float64{"gornja": 1425, "zapela": 1412, "zapela2": 1393, "donja": 1380}), nil)
	for _, l := range []string{"zapela", "zapela2"} {
		if _, ima := n.Cm[l]; ima || !strings.HasPrefix(n.Razlog[l], "mjerni uređaj stoji na ") {
			t.Errorf("%s: nula %v, razlog %q", l, n.Cm[l], n.Razlog[l])
		}
	}
	if !strings.Contains(n.Razlog["zapela"], "19 cm od ") {
		t.Errorf("razlog bez vrijednosti i dana: %q", n.Razlog["zapela"])
	}
	for _, l := range []string{"gornja", "donja"} {
		if _, ima := n.Cm[l]; !ima || n.Razlog[l] != "" {
			t.Errorf("%s ispala: %q", l, n.Razlog[l])
		}
	}
}

// Aljmaš 9. 10. 2026.: četiri dana unutar centimetra dok je Batina pala
// osam. Dunav pod usporom miruje — to nije kvar, letva ostaje na profilu.
func TestMirniDunavPodUsporomNijeKvar(t *testing.T) {
	nizovi := map[string]map[int64]float64{
		"batina":  nizOd(30, func(h int) float64 { return -100 - float64(h)/12 }),
		"aljmas":  nizOd(30, func(h int) float64 { return map[bool]float64{true: -100 - float64(h)/12, false: -117}[h < 26*24] }),
		"vukovar": nizOd(30, func(h int) float64 { return -90 - float64(h)/15 }),
	}
	n := sloziNulu(nizovi, nulaDunava(map[string]float64{"batina": 3, "aljmas": 2, "vukovar": 1}), nil)
	if _, ima := n.Cm["aljmas"]; !ima || n.Razlog["aljmas"] != "" {
		t.Errorf("Aljmaš ispao: %q", n.Razlog["aljmas"])
	}
}

// Kad se ni susjedi ne miču, mirno mjerilo je mirna voda, a ne kvar.
func TestMirnaVodaNijeKvar(t *testing.T) {
	mirno := func(h int) float64 { return -100 }
	nizovi := map[string]map[int64]float64{"a": nizOd(30, mirno), "b": nizOd(30, mirno), "c": nizOd(30, mirno)}
	n := sloziNulu(nizovi, nulaDunava(map[string]float64{"a": 3, "b": 2, "c": 1}), nil)
	if len(n.Razlog) != 0 || len(n.Cm) != 3 {
		t.Errorf("mirna voda: nule %v, razlozi %v", n.Cm, n.Razlog)
	}
}

// Moslavina 2026.: nakon pet dana bez mjerenja letva nastavi 31 cm niže, a
// susjedi su mirni. Nula iz starih dana crtala bi udubinu; letva čeka nove dane.
// Kad ih skupi dovoljno, nula se računa samo od prekida.
func TestPrekidSaSkokomRacunaNuluOdPrekida(t *testing.T) {
	sPrekidom := func(prijeDana int) map[int64]float64 {
		niz := nizOd(30, func(h int) float64 { return -95 })
		kraj := danNule.Unix() / 3600
		for h := kraj - int64(prijeDana*24) - 5*24; h < kraj-int64(prijeDana*24); h++ {
			delete(niz, h)
		}
		for h := kraj - int64(prijeDana*24); h < kraj; h++ {
			niz[h] = -127
		}
		return niz
	}
	mirno := nizOd(30, func(h int) float64 { return -150 })
	n := sloziNulu(map[string]map[int64]float64{"m": sPrekidom(2), "d": mirno},
		nulaDunava(map[string]float64{"m": 2, "d": 1}), nil)
	if _, ima := n.Cm["m"]; ima || !strings.Contains(n.Razlog["m"], "nakon prekida mjerenja") || !strings.Contains(n.Razlog["m"], "-32 cm") {
		t.Errorf("kratko nakon prekida: nula %v, razlog %q", n.Cm["m"], n.Razlog["m"])
	}
	n = sloziNulu(map[string]map[int64]float64{"m": sPrekidom(15), "d": mirno},
		nulaDunava(map[string]float64{"m": 2, "d": 1}), nil)
	if n.Cm["m"] != -127 || n.Razlog["m"] != "" {
		t.Errorf("dovoljno dana nakon prekida: nula %v, razlog %q", n.Cm["m"], n.Razlog["m"])
	}
}

// Mađarska letva ima cijeli mjesec, s valom na početku, a naša tek zadnjih
// 20 dana. Nula obiju računa se iz istih dana, pa voda koja se jednako mijenja
// stoji na profilu jednako.
func TestNulaIzIstihDanaNaCijelomToku(t *testing.T) {
	voda := func(h int) float64 { // val prvih 10 dana, zatim jednoliki pad
		if h < 10*24 {
			return 40
		}
		return -100 - float64(h-10*24)/10
	}
	madjarska := nizOd(30, voda)
	nasa := nizOd(20, func(h int) float64 { return voda(h+10*24) + 7 })
	n := sloziNulu(map[string]map[int64]float64{"hu": madjarska, "hr": nasa},
		nulaDunava(map[string]float64{"hu": 2, "hr": 1}), nil)
	if n.Cm["hr"]-n.Cm["hu"] != 7 {
		t.Errorf("nule %v: različiti dani daju različitu nulu", n.Cm)
	}
	if k := n.NulaKratko("Dunav"); k != "od 19. 9." {
		t.Errorf("razdoblje nule %q", k)
	}
}

// Letva s mjerenjima kraćim od dva tjedna ne skraćuje nulu cijelom toku: čeka.
func TestKratkiNizCekaNulu(t *testing.T) {
	n := sloziNulu(map[string]map[int64]float64{
		"stara":  nizOd(30, func(h int) float64 { return 10 }),
		"nova":   nizOd(10, func(h int) float64 { return 20 }),
		"prazna": {},
	}, nulaDunava(map[string]float64{"stara": 2, "nova": 1, "prazna": 0}), nil)
	if _, ima := n.Cm["nova"]; ima || !strings.HasPrefix(n.Razlog["nova"], "mjerenja tek od 29. 9.") {
		t.Errorf("nova: %v %q", n.Cm["nova"], n.Razlog["nova"])
	}
	if n.Cm["stara"] != 10 || n.NulaKratko("Dunav") != "zadnjih 30 dana" {
		t.Errorf("stara %v, razdoblje %q", n.Cm["stara"], n.NulaKratko("Dunav"))
	}
	if _, ima := n.Razlog["prazna"]; ima {
		t.Error("letva bez ijednog mjerenja nije ni izostavljena: nema je")
	}
}

// Letva s premalo dana unutar zajedničkih dana nema nulu.
func TestPremaloDanaUZajednickojNuli(t *testing.T) {
	rijetka := map[int64]float64{}
	pocetak := danNule.AddDate(0, 0, -20).Unix() / 3600
	for d := int64(0); d < 5; d++ {
		rijetka[pocetak+d*24] = 5
	}
	n := sloziNulu(map[string]map[int64]float64{"r": rijetka, "s": nizOd(20, func(int) float64 { return 1 })},
		nulaDunava(map[string]float64{"r": 2, "s": 1}), nil)
	if n.Razlog["r"] != "premalo mjerenja za nulu" {
		t.Errorf("rijetka: %q", n.Razlog["r"])
	}
}

// Akumulacija zadržava svoju nulu i ne ulazi u zajedničke dane toka.
func TestAkumulacijaZadrzavaSvojuNulu(t *testing.T) {
	n := sloziNulu(map[string]map[int64]float64{
		"gvb-x": nizOd(8, func(int) float64 { return 14800 }),
		"r":     nizOd(30, func(int) float64 { return 3 }),
	}, nulaDunava(map[string]float64{"gvb-x": 2, "r": 1}), map[string]bool{"gvb-x": true})
	if n.Cm["gvb-x"] != 14800 || n.Cm["r"] != 3 || len(n.Razlog) != 0 {
		t.Errorf("akumulacija: %v %v", n.Cm, n.Razlog)
	}
}

// Napomena ispod crteža nabraja izostavljene letve toka redom niz tok, a
// natpis nule kaže iz kojih je dana.
func TestOpisiIzostavljenihINatpisNule(t *testing.T) {
	n := NulaProfila{Dan: danNule,
		Razlog: map[string]string{"petres": "mjerilo stoji na 25 cm od 26. 9.", "siga": "mjerilo stoji na 19 cm od 25. 9.", "vrbovka": "x"},
		Voda:   map[string]string{"petres": "Dunav", "siga": "Dunav", "vrbovka": "Drava"},
		Rkm:    map[string]float64{"petres": 1393, "siga": 1412.2, "vrbovka": 100},
		Naziv:  map[string]string{"petres": "Petreš", "siga": "Siga", "vrbovka": "Vrbovka"},
		Od:     map[string]time.Time{"Dunav": time.Date(2026, 9, 20, 21, 0, 0, 0, models.Zagreb)},
	}
	p := &UzduzniProfil{}
	n.opisi(p, "Dunav")
	if len(p.Izostavljeno) != 2 || !strings.HasPrefix(p.Izostavljeno[0], "Siga — mjerilo stoji na 19 cm") ||
		!strings.HasPrefix(p.Izostavljeno[1], "Petreš — ") {
		t.Errorf("izostavljeno %q", p.Izostavljeno)
	}
	if p.NulaNaziv() != "voda od 20. 9." {
		t.Errorf("natpis nule %q", p.NulaNaziv())
	}
	if (&UzduzniProfil{}).NulaNaziv() != "voda zadnjih 30 dana" {
		t.Error("prazan profil: zadani natpis nule")
	}
}

// Crtež piše natpis nule iz profila, a stranica napomenu o izostavljenim
// letvama.
func TestStranicaPiseRazdobljeNuleIIzostavljene(t *testing.T) {
	a, b := letvaProfila("a", 400, 130, 50), letvaProfila("b", 300, 120, 50)
	p := crtajUzduzni("Dunav", []LetvaProfila{a, b}, nil)
	p.NulaKratko, p.Izostavljeno = "od 20. 9.", []string{"Siga — mjerilo stoji na 19 cm od 25. 9."}
	predlosci, err := fs.Sub(web.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(predlosci, DijeloviPredloska("prognoze.html")...)
	if err != nil {
		t.Fatal(err)
	}
	var crtez bytes.Buffer
	if err := tmpl.ExecuteTemplate(&crtez, "uzduzniProfil", p); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(crtez.String(), "voda od 20. 9. (medijan)") {
		t.Error("natpis nule na crtežu nije iz profila")
	}
	src, err := fs.ReadFile(predlosci, "prognoze.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "Izostavljeno s profila:") || !strings.Contains(string(src), `data-nula="{{$p.NulaKratko}}"`) {
		t.Error("stranica ne piše izostavljene letve ili razdoblje nule za oblačić")
	}
}

// probnaArhivaNula pravi arhivu s tablicama koje čitanje traži i satnim
// vrijednostima zadanim kao letva → velicina → sat → vrijednost.
func probnaArhivaNula(t *testing.T, vrijednosti map[string]map[string]map[time.Time]float64) *repository.ArhivaRepository {
	t.Helper()
	put := filepath.Join(t.TempDir(), "vodostaji.db")
	a, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Exec(`
		CREATE TABLE nizovi (id INTEGER PRIMARY KEY, sliv TEXT NOT NULL DEFAULT 'drava',
			letva TEXT NOT NULL, izvor TEXT NOT NULL, velicina TEXT NOT NULL, vrsta TEXT NOT NULL,
			od TEXT NOT NULL DEFAULT '', do TEXT NOT NULL DEFAULT '', zapisa INTEGER NOT NULL DEFAULT 0);
		CREATE TABLE spoj (letva TEXT NOT NULL, velicina TEXT NOT NULL, korak TEXT NOT NULL,
			vrijeme INTEGER NOT NULL, vrijednost REAL NOT NULL, izvor TEXT NOT NULL,
			vrsta TEXT NOT NULL DEFAULT 'trenutna', tocnost REAL NOT NULL DEFAULT 0,
			PRIMARY KEY (letva, velicina, korak, vrijeme)) WITHOUT ROWID;`); err != nil {
		t.Fatal(err)
	}
	for letva, po := range vrijednosti {
		for velicina, sati := range po {
			for kad, v := range sati {
				if _, err := a.Exec(`INSERT INTO spoj (letva, velicina, korak, vrijeme, vrijednost, izvor) VALUES (?, ?, 'satni', ?, ?, 'proba')`,
					letva, velicina, kad.Unix(), v); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	a.Close()
	arh, err := repository.OpenArhiva(put)
	if err != nil || arh == nil {
		t.Fatalf("arhiva: %v", err)
	}
	t.Cleanup(func() { arh.Close() })
	return arh
}

// Nula se slaže iz arhive i očitanja: očitanje istog sata ima prednost,
// akumulacija se čita kao kota, letva izvan registra nema niza, a niz se
// čita jednom na dan.
func TestMjesecniNiziIzArhiveIOcitanja(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "nula.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	rec := ledger.New(baza, "test")
	stanice := repository.NewStationRepository(baza, rec)
	ocitanja := repository.NewReadingRepository(baza, rec)
	ctx := context.Background()
	postaje := map[string]models.Station{}
	for _, kod := range []string{"primjerovo", "gvb-primjer"} {
		st := models.Station{ID: uuid.New(), Code: kod, Name: kod, Watercourse: "Primjerica", Stationing: "rkm 10,0"}
		if err := stanice.CreateStation(ctx, &st); err != nil {
			t.Fatal(err)
		}
		postaje[kod] = st
	}
	sat := func(danaPrije, h int) time.Time {
		return danNule.AddDate(0, 0, -danaPrije).Add(time.Duration(h) * time.Hour)
	}
	for d := 1; d <= 10; d++ {
		cm := 50 + d
		if err := ocitanja.Create(ctx, &models.Reading{StationID: postaje["primjerovo"].ID.String(), MeasuredAt: sat(d, 7).UTC(), LevelCm: &cm, Source: "RUČNO"}); err != nil {
			t.Fatal(err)
		}
	}
	arhiva := map[string]map[string]map[time.Time]float64{
		"primjerovo":  {"vodostaj": {sat(1, 7): 999, sat(20, 7): 30}},
		"gvb-primjer": {"kota": {sat(2, 0): 14800}, "vodostaj": {sat(2, 0): 1}},
	}
	h := &PrognozeHandler{readings: service.NewReadingService(ocitanja, stanice, nil, nil, nil)}
	h.SetArhiva(func() *repository.ArhivaRepository { return probnaArhivaNula(t, arhiva) })
	nizovi := h.mjesecniNizovi(ctx, postaje, []string{"primjerovo", "gvb-primjer", "nepoznata"}, danNule)
	p := nizovi["primjerovo"]
	if len(p) != 11 || p[sat(1, 7).Unix()/3600] != 51 || p[sat(20, 7).Unix()/3600] != 30 {
		t.Errorf("niz letve: %v", p)
	}
	if g := nizovi["gvb-primjer"]; len(g) != 1 || g[sat(2, 0).Unix()/3600] != 14800 {
		t.Errorf("akumulacija mora čitati kotu: %v", g)
	}
	if _, ima := nizovi["nepoznata"]; ima {
		t.Error("letva izvan registra ne smije imati niz")
	}
	// Isti dan: niz se ne čita iznova, ni kad arhive više nema.
	h.SetArhiva(func() *repository.ArhivaRepository { return nil })
	if drugi := h.mjesecniNizovi(ctx, postaje, []string{"primjerovo"}, danNule); len(drugi["primjerovo"]) != 11 {
		t.Errorf("drugo čitanje istog dana: %v", drugi)
	}
	// Novi dan bez arhive: samo očitanja.
	if sutra := h.mjesecniNizovi(ctx, postaje, []string{"primjerovo"}, danNule.AddDate(0, 0, 1)); len(sutra["primjerovo"]) != 10 {
		t.Errorf("bez arhive samo očitanja: %v", sutra)
	}
	n := h.mjesecno(ctx, postaje, []string{"primjerovo"}, danNule.AddDate(0, 0, 1))
	if n.Rkm["primjerovo"] != 10 || n.Naziv["primjerovo"] != "primjerovo" || !strings.HasPrefix(n.Razlog["primjerovo"], "mjerenja tek od") {
		t.Errorf("nula: %+v", n)
	}
	// Greška čitanja ne ruši profil: letva ostaje bez niza.
	zatvorena, err := db.OpenDB(filepath.Join(t.TempDir(), "zatvorena.db"))
	if err != nil {
		t.Fatal(err)
	}
	zatvorena.Close()
	poSatu := map[int64]float64{}
	dodajOcitanja(ctx, service.NewReadingService(repository.NewReadingRepository(zatvorena, rec), nil, nil, nil, nil), postaje["primjerovo"], danNule.AddDate(0, 0, -30), danNule, poSatu)
	dodajIzArhive(ctx, probnaArhivaNula(t, nil), models.Station{Code: "primjerovo"}, danNule.AddDate(0, 0, -30), danNule, poSatu)
	if len(poSatu) != 0 {
		t.Errorf("greška čitanja: %v", poSatu)
	}
}
