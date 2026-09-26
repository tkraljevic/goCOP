package prognoza

import (
	"path/filepath"
	"testing"
	"time"
)

// Isti sat izdan dvaput: prva verzija ostaje u izdane_ranije, a zapis o
// izdanju ima obje verzije, s tuđom prognozom koja je tada bila u rukama i s
// kišom kakvu je dnevni model imao.
func TestPonovnoIzdanjeCuvaPrvuVerzijuIZapis(t *testing.T) {
	db, err := Otvori(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const sat = int64(500000)
	izd := time.Unix((sat-3)*3600, 0).UTC()
	if _, err := SpremiTude(db, Podrijetlo, []Letva{{Naziv: "Komárom", Izdano: izd,
		Dani: []Dan{{Kad: izd.Add(24 * time.Hour), Cm: 150}}}}, Sifra); err != nil {
		t.Fatal(err)
	}
	upisi := func(v float64) {
		if err := SpremiIzdane(db, []Izdana{{Letva: "botovo", Velicina: "vodostaj", Izdano: sat, Ciljni: sat + 6,
			Vrijednost: v, Dolje: v - 5, Gore: v + 5, Model: ModelLanac}}); err != nil {
			t.Fatal(err)
		}
	}
	upisi(100)
	v1, err := SpremiZapisIzdanja(db, ZapisIzdanja{Izdano: sat, Kisa: map[string]DnevniNiz{"A": {-1: 3.5, 0: 1, 2: 12}}})
	if err != nil || v1 != 1 {
		t.Fatalf("prva verzija %d, %v", v1, err)
	}
	if err := ObrisiIzdanje(db, sat); err != nil {
		t.Fatal(err)
	}
	upisi(110)
	v2, err := SpremiZapisIzdanja(db, ZapisIzdanja{Izdano: sat})
	if err != nil || v2 != 2 {
		t.Fatalf("druga verzija %d, %v", v2, err)
	}

	var sada, prije float64
	var verzija int
	db.QueryRow(`SELECT vrijednost FROM izdane WHERE letva='botovo' AND izdano=?`, sat).Scan(&sada)
	db.QueryRow(`SELECT vrijednost, verzija FROM izdane_ranije WHERE letva='botovo' AND izdano=?`, sat).Scan(&prije, &verzija)
	if sada != 110 || prije != 100 || verzija != 1 {
		t.Errorf("izdano sada %v, ranije %v (verzija %d); očekivano 110 i 100 (verzija 1)", sada, prije, verzija)
	}
	var tude string
	db.QueryRow(`SELECT tude FROM izdanja WHERE izdano=? AND verzija=1`, sat).Scan(&tude)
	if tude != `{"hydroinfo.hu":499997}` {
		t.Errorf("tuđa prognoza u rukama: %s", tude)
	}
	var n int
	var prognoza float64
	db.QueryRow(`SELECT count(*) FROM kisa_izdanja WHERE izdano=? AND verzija=1`, sat).Scan(&n)
	db.QueryRow(`SELECT mm FROM kisa_izdanja WHERE izdano=? AND verzija=1 AND sliv='A' AND dan=2`, sat).Scan(&prognoza)
	if n != 3 || prognoza != 12 {
		t.Errorf("kiša pri izdanju: %d dana, prognoza za 2. dan %v", n, prognoza)
	}
}
