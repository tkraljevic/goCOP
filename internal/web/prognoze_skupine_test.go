package web

import (
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// registarSektora oponaša stvarni registar sektora B: Dunav od Budimpešte
// do Bačke Palanke s Batinom i Bezdanom na granici, Drava s mađarskim
// letvama, Botovo kao ulaz iz gornje Drave, Varaždin u sektoru A, Mura bez
// sektora s mađarskom i austrijskom letvom, naša letva bez sektora koja je
// samo ulaz, inundacija, letva bez vodotoka i letva koje u registru nema.
// Šifre, kilometri i veze su izmišljeni po uzoru na stvarne.
func registarSektora() ([]LetvaPrognoze, map[string]models.Station, []models.Sector, map[string][]string) {
	type red struct {
		kod, ime, voda, km string
		ulazi              []string
		sektori            []string
	}
	redovi := []red{
		{"wildungsmauer", "Wildungsmauer (Austrija)", "Dunav", "", nil, nil},
		{"komarom", "Komárom (Mađarska)", "Dunav", "rkm 1.768,35", []string{"wildungsmauer"}, nil},
		{"budapest", "Budapest (Mađarska)", "Dunav", "rkm 1.646,50", []string{"komarom"}, []string{"B"}},
		{"paks", "Paks (Mađarska)", "Dunav", "rkm 1.531,30", []string{"budapest"}, nil},
		{"mohacs", "Mohács (Mađarska)", "Dunav", "rkm 1.446,90", []string{"paks"}, nil},
		{"bezdan", "Bezdan (Srbija)", "Dunav", "rkm 1.425,59", nil, nil},
		{"batina", "Batina", "Dunav", "rkm 1424+850", []string{"mohacs"}, []string{"B"}},
		{"vukovar", "Vukovar", "Dunav", "rkm 1.333,45", []string{"batina"}, []string{"B"}},
		{"backa-palanka", "Bačka Palanka (Srbija)", "Dunav", "rkm 1.298,56", nil, nil},
		{"zlatna-greda", "Zlatna Greda", "Kormanjski Dunavac", "", []string{"batina"}, nil},
		{"varazdin", "Varaždin", "Drava", "rkm 288+185", nil, []string{"A"}},
		{"granicni-brod", "Granični Brod (Austrija)", "Mura", "", nil, nil},
		{"pogranicje", "Pogranicje (Mađarska)", "Mura", "rkm 80,10", nil, nil},
		{"mursko", "Mursko Središće", "Mura", "rkm 67,70", []string{"pogranicje"}, nil},
		{"botovo", "Botovo", "Drava", "rkm 226,83", []string{"varazdin", "mursko"}, nil},
		{"prolaz", "Prolaz", "Drava", "rkm 210,00", nil, nil},
		{"novo-virje", "Novo Virje", "Drava", "rkm 200,60", []string{"botovo", "prolaz"}, []string{"B"}},
		{"donji-miholjac", "Donji Miholjac", "Drava", "rkm 80,60", []string{"novo-virje"}, []string{"B"}},
		{"dravaszabolcs", "Drávaszabolcs (Mađarska)", "Drava", "rkm 77,70", []string{"donji-miholjac"}, nil},
		{"osijek", "Osijek", "Drava", "rkm 19,10", []string{"donji-miholjac"}, []string{"B"}},
		{"kapelna", "Kapelna", "Karašica", "rkm 52,570", nil, []string{"B"}},
		{"bez-vode", "Crpna stanica Proba", "", "", nil, []string{"B"}},
		{"nepoznata", "", "", "", nil, nil},
	}
	var letve []LetvaPrognoze
	postaje := map[string]models.Station{}
	vlastiti := map[string][]string{}
	for _, r := range redovi {
		letve = append(letve, LetvaPrognoze{Kod: r.kod, Naziv: r.ime, Voda: r.voda, Ulazi: r.ulazi})
		if r.kod != "nepoznata" {
			postaje[r.kod] = models.Station{ID: uuid.New(), Code: r.kod, Name: r.ime, Watercourse: r.voda, Stationing: r.km}
		}
		if r.sektori != nil {
			vlastiti[r.kod] = r.sektori
		}
	}
	sektori := []models.Sector{{ID: "A", Name: "Sektor A — Gornji primjer"}, {ID: "B", Name: "Sektor B — Donji primjer"},
		{ID: "C", Name: "Sektor C — Prazni primjer"}}
	return letve, postaje, sektori, vlastiti
}

// vodotociSektora je sektor kao „ime: letve” po vodotoku, za usporedbu.
func vodotociSektora(s SektorPrognoze) map[string]string {
	out := map[string]string{}
	for _, v := range s.Vodotoci {
		out[v.Oznaka] = v.Ime + ": " + strings.Join(v.Letve, " ")
	}
	return out
}

// Sektor B: Dunav podijeljen kod Batine i Bezdana (Mohács uzvodno, Bezdan i
// Bačka Palanka nizvodno), mađarski Dunav uz B jer je ulaz Batine, Drava od
// Novog Virja s Drávaszabolcsom (računat iz Donjeg Miholjca), inundacija uz
// B jer se računa iz Batine. Botovo i Prolaz su naše letve koje su samo ulaz
// Novog Virja, pa ne idu u B: Botovo je u A jer se računa iz Varaždina,
// Prolaz u ostalima. Mura bez sektora je u ostalima, s mađarskom letvom
// (nema naše letve sa sektorom na Muri) i austrijskom bez kilometra i veze.
func TestSkupinePrognozaSektorB(t *testing.T) {
	letve, postaje, sektori, vlastiti := registarSektora()
	sk := skupinePrognoza(letve, postaje, sektori, vlastiti)
	if len(sk) != 4 || sk[3].ID != oznakaOstalih {
		t.Fatalf("skupine %d, zadnja %q; želim A, B, C i ostale", len(sk), sk[len(sk)-1].ID)
	}
	if sk[1].Ime != "Donji primjer" || sk[2].imaLetvi() {
		t.Errorf("ime sektora %q, C ima letvi %v", sk[1].Ime, sk[2].imaLetvi())
	}
	zeli := map[string]string{
		"dunav-uzvodno":     "Dunav uzvodno: wildungsmauer komarom budapest paks mohacs",
		"dunav-nizvodno":    "Dunav nizvodno: bezdan batina vukovar backa-palanka",
		"inundacija-dunava": "Inundacija Dunava: zlatna-greda",
		"drava":             "Drava: novo-virje donji-miholjac dravaszabolcs osijek",
		"karasica":          "Karašica: kapelna",
		"ostale":            "Ostale letve: bez-vode",
	}
	if got := vodotociSektora(sk[1]); !reflect.DeepEqual(got, zeli) {
		t.Errorf("sektor B:\n%v\nželim\n%v", got, zeli)
	}
	var redom []string
	for _, v := range sk[1].Vodotoci {
		redom = append(redom, v.Oznaka)
	}
	if strings.Join(redom, " ") != "dunav-uzvodno dunav-nizvodno inundacija-dunava drava karasica ostale" {
		t.Errorf("redoslijed vodotoka B: %v", redom)
	}
	if got := vodotociSektora(sk[0]); !reflect.DeepEqual(got, map[string]string{"drava": "Drava: varazdin botovo"}) {
		t.Errorf("sektor A: %v", got)
	}
	if got := vodotociSektora(sk[3]); !reflect.DeepEqual(got, map[string]string{"mura": "Mura: granicni-brod pogranicje mursko",
		"drava": "Drava: prolaz", "ostale": "Ostale letve: nepoznata"}) {
		t.Errorf("ostale letve: %v", got)
	}
	cijela := cijeliVodotok(sk, "Drava", letveKodovi(letve))
	if strings.Join(cijela, " ") != "varazdin botovo prolaz novo-virje donji-miholjac dravaszabolcs osijek" {
		t.Errorf("cijela Drava: %v", cijela)
	}
	if cijeliVodotok(sk, "", letveKodovi(letve)) != nil {
		t.Error("vodotok bez imena nema cijelog toka")
	}
}

func letveKodovi(letve []LetvaPrognoze) []string {
	var out []string
	for _, l := range letve {
		out = append(out, l.Kod)
	}
	return out
}

// Nijedna letva izdanja ne smije nestati: jedinstvenih šifri u svim
// skupinama (i ostalima) ima koliko letvi u izdanju, a vodotok kroz dva
// sektora se ne broji dvaput.
func TestZbrojLetviSkupinaJeCijeloIzdanje(t *testing.T) {
	letve, postaje, sektori, vlastiti := registarSektora()
	vlastiti["osijek"] = []string{"A", "B"} // ista letva u dva sektora
	sk := skupinePrognoza(letve, postaje, sektori, vlastiti)
	jedinstvene, ukupno := map[string]bool{}, 0
	for _, s := range sk {
		for _, v := range s.Vodotoci {
			for _, k := range v.Letve {
				jedinstvene[k] = true
				ukupno++
			}
		}
	}
	if len(jedinstvene) != len(letve) {
		t.Errorf("u skupinama %d letvi, u izdanju %d", len(jedinstvene), len(letve))
	}
	if ukupno != len(letve)+1 { // Osijek je u A i B
		t.Errorf("Osijek mora biti u oba sektora, ostale letve u jednoj skupini: %d pojava", ukupno)
	}
	// i bez ijednog sektora sve ide u ostale
	sk = skupinePrognoza(letve, postaje, nil, nil)
	n := 0
	for _, v := range sk[0].Vodotoci {
		n += len(v.Letve)
	}
	if len(sk) != 1 || n != len(letve) {
		t.Errorf("bez sektora: %d skupina, %d letvi", len(sk), n)
	}
}

// Granica Dunava se čita iz registra; bez stacionaže presuđuje država.
func TestDioDunava(t *testing.T) {
	_, postaje, _, _ := registarSektora()
	g := granicaIzRegistra(postaje)
	if !g.ima || g.km < 1425.5 || g.km > 1425.6 || strings.Join(g.imena, ",") != "Batina,Bezdan" {
		t.Fatalf("granica %+v", g)
	}
	for _, x := range []struct {
		st   models.Station
		zeli string
	}{
		{models.Station{Name: "Mohács (Mađarska)", Stationing: "rkm 1.446,90"}, DunavUzvodno},
		{models.Station{Name: "Bezdan (Srbija)", Stationing: "rkm 1.425,59"}, DunavNizvodno},
		{models.Station{Name: "Proba bez kilometra"}, DunavNizvodno},
		{models.Station{Name: "Novi Sad (Srbija)"}, DunavNizvodno},
		{models.Station{Name: "Wildungsmauer (Austrija)"}, DunavUzvodno},
	} {
		if got := dioDunava(x.st, g); got != x.zeli {
			t.Errorf("%s: %s, želim %s", x.st.Name, got, x.zeli)
		}
	}
	if got := dioDunava(models.Station{Name: "Bez granice", Stationing: "rkm 1.000,00"}, granicaDunava{}); got != DunavNizvodno {
		t.Errorf("bez granice naša letva: %s", got)
	}
	if imenaGranice(granicaDunava{}) != "granice" {
		t.Error("granica bez imena")
	}
}

// Sektori iz registra: dionice, upisani sektor i sektor upisanog područja;
// direkcija bez područja nije čip.
func TestVlastitiSektoriISektoriSPodrucjima(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	postaje := map[string]models.Station{
		"prva":   {ID: a, Code: "prva", PratePodrucja: []int{20}},
		"druga":  {ID: b, Code: "druga", PrateSektori: []string{"B"}},
		"treca":  {ID: uuid.New(), Code: "treca"},
		"cetvrt": {ID: uuid.New(), Code: "cetvrt", PrateSektori: []string{"B", "B", ""}},
	}
	podrucja := []models.Area{{ID: 20, SectorID: "A"}, {ID: 34, SectorID: "B"}}
	got := vlastitiSektori(postaje, map[string][]string{b.String(): {"B", "D"}}, podrucja)
	zeli := map[string][]string{"prva": {"A"}, "druga": {"B", "D"}, "cetvrt": {"B"}}
	if !reflect.DeepEqual(got, zeli) {
		t.Errorf("vlastiti sektori %v", got)
	}
	s := sektoriSPodrucjima([]models.Sector{{ID: "A"}, {ID: "B"}, {ID: "DIREKCIJA"}}, podrucja)
	if len(s) != 2 || s[1].ID != "B" {
		t.Errorf("sektori s područjima %v", s)
	}
	for ulaz, zeli := range map[models.Sector]string{
		{ID: "B", Name: "Sektor B — Dunav i donja Drava"}: "Dunav i donja Drava",
		{ID: "A", Name: "Sektor A"}:                       "Sektor A",
		{ID: "X", Name: "Primjer bez početka"}:            "Primjer bez početka",
	} {
		if got := imeSektora(ulaz); got != zeli {
			t.Errorf("ime sektora %q: %q", ulaz.Name, got)
		}
	}
}

// Naslov iznad letvi i natpis u glavi profila.
func TestNaslovIOpisDijela(t *testing.T) {
	for _, x := range []struct {
		v    VodotokPrognoze
		zeli string
	}{
		{VodotokPrognoze{Oznaka: "drava", Ime: "Drava", Voda: "Drava"}, "Letve na Dravi, od uzvodne prema nizvodnoj"},
		{VodotokPrognoze{Oznaka: "dunav-uzvodno", Ime: DunavUzvodno, Voda: "Dunav"}, "Letve na Dunavu uzvodno, od uzvodne prema nizvodnoj"},
		{VodotokPrognoze{Oznaka: "inundacija-dunava", Ime: SkupinaInundacije}, "Letve u inundaciji Dunava, redom kako voda teče"},
		{VodotokPrognoze{Oznaka: oznakaOstalih, Ime: OstaleLetve}, OstaleLetve},
		{VodotokPrognoze{Oznaka: "jezero-proba", Ime: "jezero Proba", Voda: "jezero Proba"}, "Letve: jezero Proba, od uzvodne prema nizvodnoj"},
		{VodotokPrognoze{Oznaka: "prokop", Ime: "Prokop", Voda: "Prokop"}, "Letve na Prokopu, od uzvodne prema nizvodnoj"},
		{VodotokPrognoze{Oznaka: "kanal-o", Ime: "Kanalo", Voda: "Kanalo"}, "Letve: Kanalo, od uzvodne prema nizvodnoj"},
		{VodotokPrognoze{Oznaka: "vujnovac", Ime: "Vujnovac", Voda: "Vujnovac"}, "Letve na Vujnovcu, od uzvodne prema nizvodnoj"},
		{VodotokPrognoze{Oznaka: "peric", Ime: "Perić", Voda: "Perić"}, "Letve na Periću, od uzvodne prema nizvodnoj"},
		{VodotokPrognoze{Oznaka: "sio", Ime: "Sió", Voda: "Sió"}, "Letve: Sió, od uzvodne prema nizvodnoj"},
		{VodotokPrognoze{Oznaka: "vocinska", Ime: "Voćinska", Voda: "Voćinska"}, "Letve: Voćinska, od uzvodne prema nizvodnoj"},
		{VodotokPrognoze{Oznaka: "skres-jelenska", Ime: "Skres-Jelenska", Voda: "Skres-Jelenska"}, "Letve: Skres-Jelenska, od uzvodne prema nizvodnoj"},
	} {
		if got := naslovLetvi(x.v, false); got != x.zeli {
			t.Errorf("%s: %q", x.v.Ime, got)
		}
	}
	if got := naslovLetvi(VodotokPrognoze{Ime: DunavNizvodno, Voda: "Dunav"}, true); got != "Letve na Dunavu, od uzvodne prema nizvodnoj" {
		t.Errorf("cijeli Dunav: %q", got)
	}
	g := granicaDunava{km: 1425.59, ima: true, imena: []string{"Batina", "Bezdan"}}
	b := SektorPrognoze{ID: "B"}
	drava := VodotokPrognoze{Ime: "Drava", Letve: []string{"botovo", "osijek"}}
	cijela := []string{"varazdin", "botovo", "osijek"}
	for _, x := range []struct {
		s      SektorPrognoze
		v      VodotokPrognoze
		cijela []string
		usce   string
		zeli   string
	}{
		{b, drava, cijela, "Dunav", "dio u sektoru B, do ušća u Dunav"},
		{b, drava, cijela, "", "dio u sektoru B"},
		{b, VodotokPrognoze{Ime: "Drava", Letve: []string{"varazdin"}}, cijela, "Dunav", "dio u sektoru B"},
		{b, drava, drava.Letve, "Dunav", ""},
		{SektorPrognoze{ID: oznakaOstalih}, drava, cijela, "Dunav", ""},
		{b, VodotokPrognoze{Ime: "Karašica", Letve: []string{"a"}}, []string{"b", "a"}, "Drava", "dio u sektoru B, do ušća u Dravu"},
		{b, VodotokPrognoze{Ime: DunavUzvodno}, nil, "", "uzvodno od Batine i Bezdana"},
		{b, VodotokPrognoze{Ime: DunavNizvodno}, nil, "", "od Batine i Bezdana nizvodno"},
	} {
		if got := opisDijela(x.s, x.v, x.cijela, g, x.usce); got != x.zeli {
			t.Errorf("%s %v: %q, želim %q", x.s.ID, x.v.Letve, got, x.zeli)
		}
	}
}

// Ušće vodotoka letve: po vezi letve na registar; šifra se prevodi u ime.
func TestUsceLetvi(t *testing.T) {
	postaje := map[string]models.Station{
		"osijek":  {WatercourseCode: "rijeka-drava"},
		"kapelna": {WatercourseCode: "rijeka-karasica-proba"},
		"izvor":   {WatercourseCode: "potok-bez-usca"},
		"bez":     {},
	}
	vodotoci := []models.Watercourse{{Code: "rijeka-drava", Name: "Drava", FlowsInto: "Dunav"},
		{Code: "rijeka-karasica-proba", Name: "Karašica", FlowsInto: "rijeka-drava"}, {Code: "potok-bez-usca", Name: "Potok"}}
	zeli := map[string]string{"osijek": "Dunav", "kapelna": "Drava"}
	if got := usceLetvi(postaje, vodotoci); !reflect.DeepEqual(got, zeli) {
		t.Errorf("ušća %v", got)
	}
}

// ulazIzboraProbe je izbor nad izmišljenim registrom.
func ulazIzboraProbe(moji ...string) ulazIzbora {
	letve, postaje, sektori, vlastiti := registarSektora()
	return ulazIzbora{skupine: skupinePrognoza(letve, postaje, sektori, vlastiti), redom: letveKodovi(letve),
		moji: moji, granica: granicaIzRegistra(postaje), usce: map[string]string{"osijek": "Dunav"}}
}

// Adresa: bez parametara sektor iz dužnosti, inače prvi s letvama, i prvi
// vodotok; nepoznat sektor ili vodotok daje zadano; sektor bez letvi se ne
// može odabrati; ?dan= ostaje u svim poveznicama izbora.
func TestIzborIzAdrese(t *testing.T) {
	izbor := func(upit string, moji ...string) *IzborPrognoze {
		t.Helper()
		q := upitProbe(t, upit)
		iz := izborIzAdrese(q, ulazIzboraProbe(moji...))
		if iz == nil {
			t.Fatalf("%q: nema izbora", upit)
		}
		return iz
	}
	for upit, zeli := range map[string]string{
		"":                            "A drava",
		"sektor=B":                    "B dunav-uzvodno",
		"sektor=b&vodotok=drava":      "B drava",
		"sektor=Q&vodotok=nema":       "A drava",
		"sektor=C":                    "A drava",
		"sektor=B&vodotok=nepoznat":   "B dunav-uzvodno",
		"sektor=ostale&vodotok=mura":  "ostale mura",
		"vodotok=vuka&sektor=ostale":  "ostale mura",
		"vodotok=drava&sektor=ostale": "ostale drava",
	} {
		iz := izbor(upit)
		if got := iz.Sektor.ID + " " + iz.Vodotok.Oznaka; got != zeli {
			t.Errorf("%q: %s, želim %s", upit, got, zeli)
		}
	}
	if iz := izbor("", "C", "B"); iz.Sektor.ID != "B" {
		t.Errorf("sektor iz dužnosti (C bez letvi, pa B): %s", iz.Sektor.ID)
	}

	iz := izbor("sektor=B&vodotok=drava&dan=2")
	if iz.Opis != "dio u sektoru B, do ušća u Dunav" || iz.NaslovLetvi != "Letve na Dravi, od uzvodne prema nizvodnoj" {
		t.Errorf("opis %q, naslov %q", iz.Opis, iz.NaslovLetvi)
	}
	if iz.CijeliURL != "/prognoze?cijeli=1&dan=2&sektor=B&vodotok=drava" || iz.SektorURL != "" {
		t.Errorf("cijeli vodotok %q, natrag %q", iz.CijeliURL, iz.SektorURL)
	}
	if strings.Join(iz.kodovi, " ") != "novo-virje donji-miholjac dravaszabolcs osijek" {
		t.Errorf("letve Drave u B: %v", iz.kodovi)
	}
	cipovi := map[string]CipPrognoze{}
	for _, c := range iz.Sektori {
		cipovi[c.Oznaka] = c
	}
	if c := cipovi["C"]; !c.Prazan || c.URL != "" || c.Naziv != "C · Prazni primjer" ||
		c.Napomena != "Nijedna letva iz prognoze nije povezana s ovim sektorom. Nepovezane letve su pod „Ostale letve”." {
		t.Errorf("prazan sektor %+v", c)
	}
	if c := cipovi["B"]; !c.Odabran || c.URL != "/prognoze?dan=2&sektor=B" {
		t.Errorf("čip B %+v", c)
	}
	if c := cipovi[oznakaOstalih]; c.Naziv != OstaleLetve {
		t.Errorf("čip ostalih %+v", c)
	}
	if v := iz.Vodotoci[3]; !v.Odabran || v.URL != "/prognoze?dan=2&sektor=B&vodotok=drava" {
		t.Errorf("čip Drave %+v", v)
	}
	data := PrognozePageData{Izbor: iz}
	if got := data.AdresaDana("3"); got != "?dan=3&sektor=B&vodotok=drava" {
		t.Errorf("čip dana %q", got)
	}
	if got := data.AdresaIzvoza(); got != "/prognoze.xlsx?sektor=B&vodotok=drava&" {
		t.Errorf("izvoz %q", got)
	}

	iz = izbor("sektor=B&vodotok=drava&cijeli=1")
	if !iz.Cijeli || iz.CijeliURL != "" || iz.SektorURL != "/prognoze?sektor=B&vodotok=drava" ||
		strings.Join(iz.kodovi, " ") != "varazdin botovo prolaz novo-virje donji-miholjac dravaszabolcs osijek" {
		t.Errorf("cijela Drava: %+v %v", iz, iz.kodovi)
	}
	if got := (PrognozePageData{Izbor: iz}).AdresaDana("1"); got != "?cijeli=1&dan=1&sektor=B&vodotok=drava" {
		t.Errorf("čip dana u cijelom vodotoku %q", got)
	}
	// cijeli=1 na vodotoku koji je sav u sektoru ne mijenja ništa
	if iz := izbor("sektor=B&vodotok=karasica&cijeli=1"); iz.Cijeli || iz.CijeliURL != "" || iz.Opis != "" {
		t.Errorf("Karašica je sva u B: %+v", iz)
	}
	if izborIzAdrese(url.Values{}, ulazIzbora{}) != nil {
		t.Error("bez letvi nema izbora")
	}
	bez := PrognozePageData{}
	if bez.AdresaDana("2") != "?dan=2" || bez.AdresaIzvoza() != "/prognoze.xlsx?" || bez.DanUAdresi() != "" {
		t.Error("bez izbora adrese ostaju kakve su bile")
	}
	if (PrognozePageData{Termin: TerminProfila{Oznaka: "2"}}).DanUAdresi() != "2" {
		t.Error("dan za skriveno polje")
	}
}

// Stranica pokazuje letve odabranog vodotoka pod naslovom iz dizajna, izvoz
// iste letve po listovima vode, a glavno upozorenje bira se među
// upozorenjima vodotoka.
func TestSuziStranicuIIzvoz(t *testing.T) {
	letve, _, _, _ := registarSektora()
	q := upitProbe(t, "sektor=B&vodotok=drava")
	iz := izborIzAdrese(q, ulazIzboraProbe())
	tablice := []TablicaPrognoza{{Naslov: "Dunav", Letve: letve[:10]}, {Naslov: "Drava i Mura", Letve: letve[10:]}}
	data := PrognozePageData{Izbor: iz, Tablice: tablice, Letve: letve,
		Upozorenja: []UpozorenjeStranice{
			{Sto: "Batina", letve: []string{"batina"}, tezina: 3},
			{Sto: "Botovo", letve: []string{"botovo"}, tezina: 1},
			{Sto: "Osijek", letve: []string{"osijek"}, tezina: 2},
		}}
	if g := data.GlavnoUpozorenje(); g == nil || g.Sto != "Osijek" {
		t.Errorf("glavno upozorenje Drave: %+v", g)
	}
	if data.JosUpozorenja() != 2 || data.SvaUpozorenja() != 3 {
		t.Error("„i još” i popis nose sva upozorenja izdanja")
	}
	izvoz := data
	suziIzvoz(&izvoz)
	if len(izvoz.Tablice) != 1 || izvoz.Tablice[0].Naslov != "Drava i Mura" || len(izvoz.Letve) != 4 ||
		strings.Join(letveKodovi(izvoz.Tablice[0].Letve), " ") != strings.Join(iz.kodovi, " ") {
		t.Errorf("izvoz Drave: %+v", izvoz.Tablice)
	}
	suziStranicu(&data)
	if len(data.Tablice) != 1 || data.Tablice[0].Naslov != "Letve na Dravi, od uzvodne prema nizvodnoj" ||
		strings.Join(letveKodovi(data.Tablice[0].Letve), " ") != "novo-virje donji-miholjac dravaszabolcs osijek" {
		t.Errorf("stranica Drave: %+v", data.Tablice)
	}
	if len(data.Letve) != len(letve) {
		t.Error("stranica ne smije dirati sve letve izdanja")
	}

	q = upitProbe(t, "sektor=B&vodotok=karasica")
	data = PrognozePageData{Izbor: izborIzAdrese(q, ulazIzboraProbe()), Upozorenja: []UpozorenjeStranice{{Sto: "Batina", letve: []string{"batina"}}}}
	if g := data.GlavnoUpozorenje(); g == nil || g.Sto != "Batina" {
		t.Error("bez upozorenja vodotoka glavno je ono izdanja")
	}
	bez := PrognozePageData{Tablice: tablice, Letve: letve}
	suziStranicu(&bez)
	suziIzvoz(&bez)
	if len(bez.Tablice) != 2 || len(bez.Letve) != len(letve) {
		t.Error("bez izbora stranica i izvoz ostaju cijeli")
	}
	if !izvozPoIzboru(url.Values{"vodotok": {"drava"}}) || izvozPoIzboru(url.Values{"dan": {"2026-10-05"}}) {
		t.Error("izvoz se sužava samo uz sektor ili vodotok")
	}
}

// Profil izbora: samo letve izbora i kraj pritoke na ušću.
func TestSamoIzbora(t *testing.T) {
	letve := []LetvaProfila{{Letva: "botovo"}, {Letva: "osijek"}, {Letva: "usce:aljmas", Usce: true}, {Letva: "varazdin"}}
	if got := samoIzbora(letve, nil); len(got) != 4 {
		t.Error("bez izbora sve letve")
	}
	got := samoIzbora(letve, map[string]bool{"botovo": true, "osijek": true})
	if len(got) != 3 || got[2].Letva != "usce:aljmas" {
		t.Errorf("izbor %v", got)
	}
}

// upitProbe čita upit adrese iz testa.
func upitProbe(t *testing.T, upit string) url.Values {
	t.Helper()
	q, err := url.ParseQuery(upit)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

// Ušće dijela bez letvi nema.
func TestUsceDijelaBezLetvi(t *testing.T) {
	if usceDijela(VodotokPrognoze{}, map[string]string{"x": "Dunav"}) != "" {
		t.Error("vodotok bez letvi nema ušća")
	}
	if usceDijela(VodotokPrognoze{Letve: []string{"a", "x"}}, map[string]string{"x": "Dunav"}) != "Dunav" {
		t.Error("ušće zadnje letve")
	}
}
