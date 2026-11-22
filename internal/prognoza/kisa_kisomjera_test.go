package prognoza

import (
	"context"
	"errors"
	"testing"
	"time"

	"gocop/internal/models"
)

// Oznaka kiše kišomjera razlikuje se od oznake međusliva i vraća šifru.
func TestOznakaKiseKisomjera(t *testing.T) {
	if s, je := KisomjerIzSliva(KisaKisomjera("km-jezero")); !je || s != "km-jezero" {
		t.Errorf("oznaka kišomjera: %q %v", s, je)
	}
	for _, sliv := range []string{"P", "", "kisomjer:"} {
		if _, je := KisomjerIzSliva(sliv); je {
			t.Errorf("%q nije kiša kišomjera", sliv)
		}
	}
}

// Iz registra se kao kiša kišomjera čitaju aktivni stvarni kišomjeri, svaki sa
// svojim međuslivom; izvedene točke ostaju zasebno, a zajedno idu u učenje.
func TestKisomjeriKiseIzRegistra(t *testing.T) {
	db := registarKisomjera(t)
	if _, err := db.Exec(`INSERT INTO kisomjeri (code, sliv, tezina, aktivan, vrsta) VALUES
		('z-1', 'Z', 1, 1, 'izvedeni'), ('km-jezero', 'Z', NULL, 1, 'stvarni'),
		('km-ugasen', 'Z', NULL, 0, 'stvarni'), ('km-bez-sliva', '', NULL, 1, 'stvarni')`); err != nil {
		t.Fatal(err)
	}
	k, err := KisomjeriKise(db)
	if err != nil || len(k) != 2 {
		t.Fatalf("kišomjeri: %+v, %v", k, err)
	}
	if k[0] != (OborinskaTocka{Code: "km-bez-sliva", Sliv: "kisomjer:km-bez-sliva", Tezina: 1}) ||
		k[1] != (OborinskaTocka{Code: "km-jezero", Sliv: "kisomjer:km-jezero", Tezina: 1, SlivMjesta: "Z"}) {
		t.Errorf("kišomjeri: %+v", k)
	}
	sve, err := TockeSKisomjerima(db)
	if err != nil || len(sve) != 3 || sve[0].Code != "z-1" {
		t.Errorf("točke s kišomjerima: %+v, %v", sve, err)
	}
	// redak bez međusliva u starijem registru ne da se pročitati
	if _, err := db.Exec(`DROP TABLE kisomjeri; CREATE TABLE kisomjeri (code TEXT, sliv TEXT, aktivan INTEGER, vrsta TEXT);
		INSERT INTO kisomjeri VALUES ('km-jezero', NULL, 1, 'stvarni')`); err != nil {
		t.Fatal(err)
	}
	if _, err := KisomjeriKise(db); err == nil {
		t.Error("nečitljiv redak mora javiti grešku")
	}
	if _, err := db.Exec(`DROP TABLE kisomjeri`); err != nil {
		t.Fatal(err)
	}
	if _, err := KisomjeriKise(db); err == nil {
		t.Error("registar bez tablice mora javiti grešku")
	}
	if _, err := TockeSKisomjerima(db); err == nil {
		t.Error("registar bez tablice mora javiti grešku i za sve točke")
	}
}

// satKise je trenutak u kojem završava sat h zagrebačkog dana.
func satKise(godina int, mjesec time.Month, dan, h int) int64 {
	return time.Date(godina, mjesec, dan, h, 0, 0, 0, models.Zagreb).Unix()
}

func danOd1970(godina int, mjesec time.Month, dan int) int64 {
	return time.Date(godina, mjesec, dan, 0, 0, 0, 0, time.UTC).Unix() / 86400
}

// Dan kojeg u dnevnom nizu kišomjera nema dobiva zbroj satnih mjerenja tog
// zagrebačkog dana, ali samo kad ih ima dovoljno; dan koji dnevni niz već ima
// ostaje kakav je. Dan promjene sata ima 23 sata i ulazi.
func TestDnevnaKisaIzSatnih(t *testing.T) {
	arhiva, _ := arhivaUDatoteci(t)
	upisi := func(kad int64, mm float64) {
		t.Helper()
		if _, err := arhiva.Exec(`INSERT INTO spoj VALUES ('km-jezero', 'oborina', 'satni', ?, ?)`, kad, mm); err != nil {
			t.Fatal(err)
		}
	}
	for h := 1; h <= 24; h++ {
		upisi(satKise(2024, 3, 10, h), 0.5) // cijeli dan
		upisi(satKise(2024, 3, 12, h), 2)   // dan koji dnevni niz već ima
	}
	for h := 1; h <= 10; h++ {
		upisi(satKise(2024, 3, 11, h), 1) // krnji dan
	}
	for h := 1; h <= 23; h++ {
		upisi(satKise(2024, 3, 31, h)+int64(3600*btoi(h >= 2)), 1) // 31. 3. sat preskače s 2 na 3
	}
	n := DnevniNiz{danOd1970(2024, 3, 12): 7}
	if err := dopuniIzSatnih(arhiva, "km-jezero", n); err != nil {
		t.Fatal(err)
	}
	if len(n) != 3 || n[danOd1970(2024, 3, 10)] != 12 || n[danOd1970(2024, 3, 12)] != 7 || n[danOd1970(2024, 3, 31)] != 23 {
		t.Errorf("dnevna kiša iz satnih: %v", n)
	}
	// ponoćni sat pripada danu koji je završio
	if danKise(satKise(2024, 3, 11, 0)) != danOd1970(2024, 3, 10) {
		t.Error("sat koji završava u ponoć pripada prethodnom danu")
	}
	upisi(0, 1)
	if _, err := arhiva.Exec(`UPDATE spoj SET vrijeme = 'nije broj' WHERE vrijeme = 0`); err != nil {
		t.Fatal(err)
	}
	if err := dopuniIzSatnih(arhiva, "km-jezero", DnevniNiz{}); err == nil {
		t.Error("nečitljiv redak mora javiti grešku")
	}
	if err := arhiva.Close(); err != nil {
		t.Fatal(err)
	}
	if err := dopuniIzSatnih(arhiva, "km-jezero", DnevniNiz{}); err == nil {
		t.Error("zatvorena arhiva mora javiti grešku")
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Model koji traži kišu kišomjera uči palu kišu iz arhive kišomjera, a buduću
// iz međusliva u kojem kišomjer stoji. Kišomjer koji model ne traži ne čita se,
// a kišomjer bez međusliva buduću kišu uči iz vlastitog niza.
func TestUcenjeIzKiseKisomjera(t *testing.T) {
	arhiva, _ := arhivaUDatoteci(t)
	upisiDnevne(t, arhiva, "jezero", "vodostaj", 1200, valIzmisljeni)
	upisiDnevne(t, arhiva, "km-jezero", "oborina", 1200, kisaIzmisljena)
	upisiDnevne(t, arhiva, "km-drugi", "oborina", 1200, kisaIzmisljena)
	upisiDnevne(t, arhiva, "z-1", "oborina", 1200, func(d int64) float64 { return kisaIzmisljena(d) / 2 })
	tocke := []OborinskaTocka{{Code: "z-1", Sliv: "Z", Tezina: 1},
		{Code: "km-jezero", Sliv: KisaKisomjera("km-jezero"), Tezina: 1, SlivMjesta: "Z"},
		{Code: "km-drugi", Sliv: KisaKisomjera("km-drugi"), Tezina: 1, SlivMjesta: "Z"}}
	kisa := KisaKisomjera("km-jezero")
	c := DnevniCilj{Letva: "jezero", Slivovi: []string{kisa}, NajmanjeDana: NajmanjeDanaZapisa}
	if cilja := tockeCilja(tocke, c); len(cilja) != 2 || cilja[1].Code != "km-jezero" {
		t.Fatalf("točke cilja: %+v", cilja)
	}
	nizovi, err := DnevniNizoviCilja(arhiva, tocke, c)
	if err != nil {
		t.Fatal(err)
	}
	if n := nizovi[OborinaKljuc(kisa)]; len(n) != 1200 || n[15] != kisaIzmisljena(15) {
		t.Errorf("pala kiša kišomjera: %d dana", len(n))
	}
	if n := nizovi[OborinaUnaprijedKljuc(kisa)]; len(n) != 1200 || n[15] != kisaIzmisljena(15)/2 {
		t.Errorf("buduća kiša iz međusliva: %d dana", len(n))
	}
	if _, ima := nizovi[OborinaKljuc(KisaKisomjera("km-drugi"))]; ima {
		t.Error("kišomjer koji model ne traži ne smije među nizove")
	}
	if _, err := NamjestiDnevni(nizovi, c, 0); err != nil {
		t.Errorf("model s kišom kišomjera mora se naučiti: %v", err)
	}
	tocke[1].SlivMjesta = ""
	nizovi, err = DnevniNizoviCilja(arhiva, tocke, c)
	if _, ima := nizovi[OborinaUnaprijedKljuc(kisa)]; err != nil || ima {
		t.Errorf("kišomjer bez međusliva nema zasebne buduće kiše: %v", err)
	}
	// kišomjer bez ijednog dana u arhivi daje prazan niz, pa učenje javi što fali
	nizovi, err = DnevniNizoviCilja(arhiva, []OborinskaTocka{{Code: "km-novi", Sliv: KisaKisomjera("km-novi"), Tezina: 1}},
		DnevniCilj{Letva: "jezero", Slivovi: []string{KisaKisomjera("km-novi")}})
	if n, ima := nizovi[OborinaKljuc(KisaKisomjera("km-novi"))]; err != nil || !ima || len(n) != 0 {
		t.Errorf("kišomjer bez arhive: %v %v", n, err)
	}
	if err := arhiva.Close(); err != nil {
		t.Fatal(err)
	}
	if err := dodajKisuKisomjera(arhiva, map[string]DnevniNiz{}, map[string]DnevniNiz{}, tocke, c); err == nil {
		t.Error("zatvorena arhiva mora javiti grešku")
	}
}

// Uživo kišomjer daje sate koje je izmjerio, a ostale, i sve iduće, međusliv u
// kojem stoji. Kišomjer bez međusliva nema kiše, kao ni dan u kojem međuslivu
// fali sat.
func TestKisaKisomjeraUzivo(t *testing.T) {
	const sada = int64(500000)
	satne := func(od, do int64) (map[string]map[int64]float64, error) {
		a, b := map[int64]float64{}, map[int64]float64{}
		for h := od; h <= do; h++ {
			a[h], b[h] = 1, 3
		}
		delete(a, sada-60) // teža točka nema jedan sat pretprošlog dana
		return map[string]map[int64]float64{"z-1": a, "z-2": b}, nil
	}
	izmjerene := func(od, do int64) (map[string]map[int64]float64, error) {
		m := map[int64]float64{}
		for h := sada - 47; h <= sada-24; h++ {
			m[h] = 0.5 // jučer je kišomjer izmjerio 12 mm
		}
		return map[string]map[int64]float64{"km-jezero": m}, nil
	}
	iz := &OborinskiIzvor{Satne: satne, Izmjerene: izmjerene,
		Tocke: []OborinskaTocka{{Code: "z-1", Sliv: "Z", Tezina: 0.75}, {Code: "z-2", Sliv: "Z", Tezina: 0.25}, {Code: "z-0", Sliv: "Z"}},
		Kisomjeri: []OborinskaTocka{{Code: "km-jezero", Sliv: KisaKisomjera("km-jezero"), Tezina: 1, SlivMjesta: "Z"},
			{Code: "km-bez-sliva", Sliv: KisaKisomjera("km-bez-sliva"), Tezina: 1}}}
	kisa, err := OborineOkoSada(iz, sada, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	n := kisa[OborinaKljuc(KisaKisomjera("km-jezero"))]
	// međusliv daje 0,75·1 + 0,25·3 = 1,5 mm na sat, 36 mm na dan
	if len(n) != 4 || n[0] != 36 || n[-1] != 12 || n[1] != 36 || n[2] != 36 {
		t.Errorf("kiša kišomjera po danima: %v", n)
	}
	if _, ima := n[-2]; ima {
		t.Error("dan u kojem međuslivu fali sat mora izostati")
	}
	if _, ima := kisa[OborinaKljuc(KisaKisomjera("km-bez-sliva"))]; ima {
		t.Error("kišomjer bez međusliva nema kiše")
	}
	// bez mjerenja, ili kad se ne daju pročitati, ostaje kiša međusliva
	for _, izmj := range []func(od, do int64) (map[string]map[int64]float64, error){nil,
		func(int64, int64) (map[string]map[int64]float64, error) {
			return nil, errors.New("baza oborina ne odgovara")
		}} {
		iz.Izmjerene = izmj
		kisa, err := OborineOkoSada(iz, sada, 3, 2)
		if n := kisa[OborinaKljuc(KisaKisomjera("km-jezero"))]; err != nil || n[-1] != 36 {
			t.Errorf("bez mjerenja: %v, %v", n, err)
		}
	}
	iz.Kisomjeri = nil
	if kisa, err := OborineOkoSada(iz, sada, 3, 2); err != nil || len(kisa) != 1 {
		t.Errorf("bez kišomjera ostaju samo međuslivovi: %v, %v", kisa, err)
	}
}

// Letva čiji zapis traži kišu kišomjera uči i računa kad izvor oborina nosi
// kišomjere iz registra.
func TestDnevniKrugSKisomKisomjera(t *testing.T) {
	o, zadnji := osvjezivacDnevnihZapisa(t)
	kisa := KisaKisomjera("kisomjer-x")
	upisiZapis(t, o.Baza, dnevniZapis(t, "donja", []string{"gornja"}, []string{kisa}))
	o.Oborine = &OborinskiIzvor{Tocke: []OborinskaTocka{{Code: "kisomjer-x", Sliv: "X", Tezina: 1}},
		Kisomjeri: []OborinskaTocka{{Code: "kisomjer-x", Sliv: kisa, Tezina: 1, SlivMjesta: "X"}},
		Satne: func(od, do int64) (map[string]map[int64]float64, error) {
			n := map[int64]float64{}
			for h := od; h <= do; h++ {
				n[h] = 0.2
			}
			return map[string]map[int64]float64{"kisomjer-x": n}, nil
		}}
	i := praznoIzdanje(zadnji)
	o.dnevniKrugZapisa(context.Background(), i, nil, zadnji.Add(-10*24*time.Hour))
	if dnevnihZa(i, "donja") != DnevniDosezi+1 {
		t.Errorf("letva s kišom kišomjera nije računala: %d, %v", dnevnihZa(i, "donja"), i.BezDnevne["donja"])
	}
}
