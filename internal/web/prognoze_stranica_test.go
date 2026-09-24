package web

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
)

// bazaPrognoza slaže malu bazu s dvije letve u lancu i jednim izdanjem.
func bazaPrognoza(t *testing.T, izdano int64) string {
	t.Helper()
	put := filepath.Join(t.TempDir(), "prognoze.db")
	db, err := prognoza.Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	pojasi := []prognoza.Pojas{
		{Letva: "belisce", Velicina: "vodostaj", Od: -200, Do: 600, Rasap: 8,
			Ulazi: []prognoza.Ulaz{{Letva: "donji-miholjac", Velicina: "vodostaj",
				PomakH: 7, Sirina: 5, Nagib: 0.9}}},
		{Letva: "donji-miholjac", Velicina: "vodostaj", Od: -200, Do: 600, Rasap: 9,
			Ulazi: []prognoza.Ulaz{{Letva: "moslavina", Velicina: "vodostaj",
				PomakH: 5, Sirina: 1, Nagib: 1.1}}},
	}
	if err := prognoza.Spremi(db, pojasi, "proba"); err != nil {
		t.Fatal(err)
	}
	var izdane []prognoza.Izdana
	for _, letva := range []string{"belisce", "donji-miholjac"} {
		for sat := int64(0); sat <= 72; sat++ {
			v := 30 + float64(sat)
			izdane = append(izdane, prognoza.Izdana{
				Letva: letva, Velicina: "vodostaj", Izdano: izdano, Ciljni: izdano + sat,
				Vrijednost: v, Dolje: v - float64(sat)/2, Gore: v + float64(sat)/2,
				Model: "proba",
			})
		}
	}
	if err := prognoza.SpremiIzdane(db, izdane); err != nil {
		t.Fatal(err)
	}
	// Na 48 sati Belišće ne pobjeđuje postojanost; to mora doći do stranice.
	promasaji := []prognoza.Promasaj{
		{Letva: "belisce", Velicina: "vodostaj", DosegH: 24, Rasap: 8, Postojanost: 23, Slucaja: 2000},
		{Letva: "belisce", Velicina: "vodostaj", DosegH: 48, Rasap: 40, Postojanost: 38, Slucaja: 2000},
	}
	if err := prognoza.SpremiPromasaje(db, promasaji, "proba"); err != nil {
		t.Fatal(err)
	}
	return put
}

// Pregled mora složiti letve kako voda teče, a ne po abecedi: Donji Miholjac
// je uzvodno od Belišća, pa ide prvi.
func TestPregledSlazeLetveKakoVodaTece(t *testing.T) {
	izdano := time.Date(2026, 9, 23, 5, 0, 0, 0, time.UTC).Unix() / 3600
	c, err := OtvoriPrognoze(bazaPrognoza(t, izdano))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	kad, letve, err := c.Pregled()
	if err != nil {
		t.Fatal(err)
	}
	if len(letve) != 2 {
		t.Fatalf("%d letvi umjesto 2", len(letve))
	}
	if letve[0].Letva != "donji-miholjac" {
		t.Errorf("prva je %s, a uzvodna je donji-miholjac", letve[0].Letva)
	}
	if kad.Unix()/3600 != izdano {
		t.Errorf("izdanje %s", kad)
	}
	if letve[0].Doseg != 72 {
		t.Errorf("doseg %d h umjesto 72", letve[0].Doseg)
	}
	// Vrijednost u satu izdavanja je sidro prognoze, ne prva prognozirana ura.
	if letve[0].Sada["vodostaj"] != 30 {
		t.Errorf("sad %g umjesto 30", letve[0].Sada["vodostaj"])
	}
}

// Doseg na kojem prognoza ne pobjeđuje postojanost mora se na stranici vidjeti.
// Brojka koja izgleda jednako dobro kao ostale obmanjuje.
func TestPregledOznaciDosegSlabijiOdPostojanosti(t *testing.T) {
	izdano := time.Date(2026, 9, 23, 5, 0, 0, 0, time.UTC).Unix() / 3600
	c, err := OtvoriPrognoze(bazaPrognoza(t, izdano))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	_, letve, _ := c.Pregled()
	var belisce PregledLetve
	for _, l := range letve {
		if l.Letva == "belisce" {
			belisce = l
		}
	}
	if belisce.Po["vodostaj"][24].BoljaOdPostojanosti != true {
		t.Error("na 24 h Belišće pobjeđuje postojanost, a nije tako označeno")
	}
	if belisce.Po["vodostaj"][48].BoljaOdPostojanosti != false {
		t.Error("na 48 h Belišće ne pobjeđuje postojanost, a označeno je kao da pobjeđuje")
	}
	// Doseg za koji promašaj nije izmjeren ne smije ispasti kao slabiji.
	if belisce.Po["vodostaj"][6].BoljaOdPostojanosti != true {
		t.Error("neizmjeren doseg proglašen slabijim")
	}
}

// Stranica mora pokazati izdanje, brojke, raspon i upozorenje.
func TestStranicaPrognozaPokazujeIzdanjeIRaspon(t *testing.T) {
	html := iscrtaj(t, "prognoze.html", PrognozePageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		ActiveNav:   "prognoze", Dosezi: DoseziPregleda, Udio: 68,
		Izdano: "23.9.2026. u 07:00", Bliski: BliziDosezi, Dani: []string{"čet 24.9.", "pet 25.9."},
		ImaTudih: true,
		Tablice: []TablicaPrognoza{{Naslov: "Drava i Mura", Letve: []LetvaPrognoze{{
			Kod: "belisce", Naziv: "Belišće", Voda: "Drava", Racuna: "vodostaj",
			URL: "/readings/station/x", SadaCm: "35", SadaQ: "231", Doseg: 96, ImaTermina: true,
			Vrijednosti: []VrijednostPrognoze{
				{DosegH: 6, Ima: true, Cm: "41", CmRaspon: "39 do 43", Q: "245", QRaspon: "240 do 251"},
				{DosegH: 12, Ima: true, Cm: "48", CmRaspon: "44 do 52"},
				{DosegH: 24, Ima: true, Cm: "57", CmRaspon: "49 do 65"},
				{DosegH: 48, Ima: true, Cm: "-62", CmRaspon: "-86 do -38", Slabija: true},
				{DosegH: 72, Ima: false},
			},
			Dani: []CelijaDana{
				{Naslov: "čet 24.9.", Cm: "57", Raspon: "49 do 65", Q: "301", Razina: "prep", Tude: []TudaCelija{
					{Oznaka: "HU", Klasa: "hu", Cm: "60", Raspon: "±9"}, {Oznaka: "RS", Klasa: "rs", Cm: "58"}}},
				{Cm: "-62", Raspon: "-86 do -38", Dnevna: true, Moguce: "regular"},
			},
		}}}},
	})
	for _, want := range []string{"Belišće", "Drava", "23.9.2026. u 07:00", "68 %",
		"49 do 65", "245", "240 do 251", "-86 do -38", "prog-slabija", "čet 24.9.",
		"HU 60 ±9", "prog-tuda-rs", "RS 58", "Metoda", "metoda analognih situacija", "u 32 % izlazi", "301 m³/s", "prog-kartica", "dan-prep", "dan-moguce-regular", "prog-oznaka", "Drava i Mura",
		"/readings/station/x",
		"računa se u vodostaju"} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica prognoza nema %q", want)
		}
	}
}

// Bez baze prognoza stranica mora reći zašto je prazna, a ne pući.
func TestStranicaPrognozaBezBazeKaze(t *testing.T) {
	html := iscrtaj(t, "prognoze.html", PrognozePageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		ActiveNav:   "prognoze", Nema: true, Razlog: "Baza prognoza nije otvorena.",
	})
	if !strings.Contains(html, "Baza prognoza nije otvorena.") {
		t.Error("stranica ne kaže zašto je prazna")
	}
}

// Gumb „Generiraj” stoji uz izvoz kad čvor ima krug preuzimanja; dok krug
// traje, gumb je ugašen i stranica se sama osvježava.
func TestStranicaPrognozaGumbGeneriraj(t *testing.T) {
	osnova := PrognozePageData{
		CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		ActiveNav: "prognoze", Nema: true, Razlog: "još ništa",
	}
	html := iscrtaj(t, "prognoze.html", osnova)
	if strings.Contains(html, "/prognoze/generiraj") {
		t.Error("gumb Generiraj stoji, a čvor nema krug preuzimanja")
	}
	osnova.MozeGenerirati = true
	html = iscrtaj(t, "prognoze.html", osnova)
	if !strings.Contains(html, `action="/prognoze/generiraj"`) || strings.Contains(html, "disabled") {
		t.Error("gumb Generiraj nije spreman za klik")
	}
	osnova.Generira = true
	html = iscrtaj(t, "prognoze.html", osnova)
	if !strings.Contains(html, "Generiranje u tijeku") || !strings.Contains(html, "disabled") || !strings.Contains(html, "location.replace('/prognoze')") {
		t.Error("dok krug traje gumb mora biti ugašen, a stranica se sama osvježiti")
	}
}
